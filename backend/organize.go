package main

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Naming uses parsed document fields, never the uploaded filename.
func contentName(kind string, m Record, sha string) string {
	date := str(m, "date")
	if kind == "银行" {
		date = str(m, "statement_date")
	}
	label := documentNameLabel(kind, m)
	if kind == "银行" && validDate(date) && len(arr(m, "checks")) > 0 {
		return date[:7] + "_" + label + "_HSBC_" + safeName(str(arr(m, "checks")[0], "bank_number")) + ".pdf"
	}
	if (invoiceKind(kind) || kind == "银行通知") && validDate(date) && str(m, "number") != "" && str(m, "ccy") != "" && cents(m, "amount") > 0 {
		party := str(m, "vendor")
		if isSale(m) {
			party = str(m, "customer")
		}
		if party != "" {
			return date + "_" + label + "_" + canonicalParty(party) + "_" + safeName(str(m, "number")) + "_" + str(m, "ccy") + fmt.Sprintf("_%.2f.pdf", num(m, "amount"))
		}
	}
	if validDate(date) && str(m, "number") != "" {
		return date + "_" + label + "_" + safeName(str(m, "number")) + ".pdf"
	}
	return "待核對_" + label + ".pdf"
}

// Write a canonical copy first, then update the database. The live original is
// never removed before its replacement is durable and referenced.
func (s *Store) saveCanonical(sha, kind, original string, m Record, content []byte) error {
	if !rx(`^[a-f0-9]{64}$`).MatchString(sha) || hashBytes(content) != sha {
		return fmt.Errorf("凭证内容校验失败")
	}
	var old string
	if e := s.db.QueryRow("SELECT path FROM documents WHERE sha=?", sha).Scan(&old); e != nil {
		return e
	}
	if e := s.applyConfirmedGoogleCurrency(m); e != nil {
		return e
	}
	rel, e := s.archivePath(kind, m, sha, nil)
	if e != nil {
		return e
	}
	parent, e := withinRoot(s.root, filepath.Dir(rel))
	if e != nil {
		return e
	}
	target := filepath.Join(parent, filepath.Base(rel))
	if info, err := os.Lstat(target); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("目标文件为符号链接，停止整理")
	}
	if b, err := os.ReadFile(target); err == nil {
		if hashBytes(b) != sha {
			return fmt.Errorf("目标文件存在且内容不同，停止覆盖")
		}
	} else if !os.IsNotExist(err) {
		return err
	} else {
		f, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
		if err != nil {
			return err
		}
		_, err = f.Write(content)
		if err == nil {
			err = f.Sync()
		}
		closeErr := f.Close()
		if err == nil {
			err = closeErr
		}
		if err != nil {
			return err
		}
	}
	canonicalMetadata(m, filepath.Base(rel))
	raw, e := json.Marshal(m)
	if e != nil {
		return e
	}
	if _, e = s.db.Exec("UPDATE documents SET meta=?,path=?,kind=?,name=? WHERE sha=?", string(raw), rel, kind, filepath.Base(rel), sha); e != nil {
		return e
	}
	if filepath.Clean(old) == filepath.Clean(rel) || strings.EqualFold(filepath.Clean(old), filepath.Clean(rel)) {
		return nil
	}
	return s.removeContentDuplicate(old, sha)
}

func (s *Store) removeContentDuplicate(rel, sha string) error {
	var refs int
	if e := s.db.QueryRow("SELECT count(*) FROM documents WHERE path=?", rel).Scan(&refs); e != nil {
		return e
	}
	if refs > 0 {
		return nil
	}
	path, e := withinRoot(s.root, rel)
	if os.IsNotExist(e) {
		return nil
	}
	if e != nil {
		return e
	}
	b, e := os.ReadFile(path)
	if e != nil {
		return e
	}
	if hashBytes(b) != sha {
		return fmt.Errorf("重复文件内容已变化，停止清理")
	}
	// Verify the retained copy immediately before removing the extra copy.
	kept, e := s.documentPath(sha)
	if e != nil {
		return e
	}
	if strings.EqualFold(filepath.Clean(path), filepath.Clean(kept)) {
		return nil
	}
	b, e = os.ReadFile(kept)
	if e != nil || hashBytes(b) != sha {
		return fmt.Errorf("保留原件校验失败，停止清理")
	}
	return os.Remove(path)
}

func (s *Store) organizeDocuments() (Record, error) {

	registry, e := s.deduplicateRegistry()
	if e != nil {
		return nil, e
	}
	d, e := s.load()
	if e != nil {
		return nil, e
	}
	known := map[string]bool{}
	referenced := map[string]bool{}
	renames := []Record{}
	duplicates := []Record{}
	unknown := []string{}
	reserved := map[string]bool{}
	for _, doc := range d.Docs {
		sha := str(doc, "sha")
		path, err := s.documentPath(sha)
		if err != nil {
			return nil, err
		}
		b, err := os.ReadFile(path)
		if err != nil || hashBytes(b) != sha {
			return nil, fmt.Errorf("原件校验失败：%s", str(doc, "name"))
		}
		known[sha] = true
		referenced[filepath.Clean(str(doc, "path"))] = true
		var raw string
		if e = s.db.QueryRow("SELECT meta FROM documents WHERE sha=?", sha).Scan(&raw); e != nil {
			return nil, e
		}
		var m Record
		if e = json.Unmarshal([]byte(raw), &m); e != nil {
			return nil, e
		}
		if typ, err := detectPDFType(path); err == nil {
			annotatePDFType(m, typ)
		}
		rel, err := s.archivePath(str(doc, "kind"), m, sha, reserved)
		if err != nil {
			return nil, err
		}
		if rel != filepath.Clean(str(doc, "path")) || str(doc, "name") != filepath.Base(rel) || str(m, "source") != filepath.Base(rel) {
			renames = append(renames, Record{"sha": sha, "from": str(doc, "path"), "to": rel, "kind": str(doc, "kind"), "name": str(doc, "name"), "meta": m})
		}
	}
	archiveRoot, e := withinRoot(s.root, "憑證")
	if e != nil {
		return nil, e
	}
	roots := []string{archiveRoot}
	if old, err := withinRoot(s.root, "凭证"); err == nil {
		roots = append(roots, old)
	}
	walk := func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("凭证目录含符号链接，停止清理")
		}
		if entry.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(s.root, path)
		if err != nil {
			return err
		}
		if referenced[filepath.Clean(rel)] {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		sha := hashBytes(b)
		if known[sha] {
			duplicates = append(duplicates, Record{"path": rel, "sha": sha, "bytes": len(b)})
		} else {
			unknown = append(unknown, rel)
		}
		return nil
	}
	for _, root := range roots {
		if e = filepath.WalkDir(root, walk); e != nil {
			return nil, e
		}
	}
	if e != nil {
		return nil, e
	}
	report := Record{"registered_duplicates_removed": num(registry, "removed"), "referenced_duplicates_retained": num(registry, "retained_referenced"), "renamed": 0, "removed": num(registry, "removed"), "reclaimed_bytes": 0, "unknown_files": unknown, "renames": []Record{}, "duplicates": duplicates, "backup_url": str(registry, "backup_url")}
	if len(renames) == 0 && len(duplicates) == 0 {
		return report, nil
	}
	backup, e := s.backup()
	if e != nil {
		return nil, e
	}
	report["backup_url"] = "/api/downloads/backup/" + filepath.Base(backup)
	report["backup_name"] = filepath.Base(backup)
	// Persist the complete plan before any mutation, including original paths.
	logs := filepath.Join(s.root, "整理记录")
	if e = os.MkdirAll(logs, 0755); e != nil {
		return nil, e
	}
	logPath := filepath.Join(logs, time.Now().Format("20060102_150405")+"_"+uuid.NewString()[:8]+".json")
	plan := Record{"backup": backup, "renames": renames, "duplicates": duplicates, "unknown_files": unknown, "status": "执行中"}
	writeLog := func() error {
		b, err := json.MarshalIndent(plan, "", "  ")
		if err != nil {
			return err
		}
		return os.WriteFile(logPath, b, 0644)
	}
	if e = writeLog(); e != nil {
		return nil, e
	}
	defer func() {
		if e != nil {
			plan["status"] = "中断"
			plan["error"] = e.Error()
			_ = writeLog()
		}
	}()
	for _, r := range renames {
		var path string
		path, e = s.documentPath(str(r, "sha"))
		if e != nil {
			return nil, e
		}
		var b []byte
		b, e = os.ReadFile(path)
		if e != nil {
			return nil, e
		}
		m := r["meta"].(Record)
		e = s.saveCanonical(str(r, "sha"), str(r, "kind"), str(r, "name"), m, b)
		if e != nil {
			return nil, e
		}
		report["renamed"] = num(report, "renamed") + 1
		report["renames"] = append(report["renames"].([]Record), Record{"from": r["from"], "to": r["to"]})
	}
	for _, r := range duplicates {
		// A previously unreferenced target may now be the canonical retained file.
		var refs int
		e = s.db.QueryRow("SELECT count(*) FROM documents WHERE path=?", str(r, "path")).Scan(&refs)
		if e != nil {
			return nil, e
		}
		if refs > 0 {
			continue
		}
		e = s.removeContentDuplicate(str(r, "path"), str(r, "sha"))
		if e != nil {
			return nil, e
		}
		report["removed"] = num(report, "removed") + 1
		report["reclaimed_bytes"] = num(report, "reclaimed_bytes") + num(r, "bytes")
	}
	// Remove obsolete empty hash directories after originals are safely relocated.
	dirs := []string{}
	for _, root := range roots {
		if err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() && path != archiveRoot {
				dirs = append(dirs, path)
			}
			return nil
		}); err == nil {
			for n := len(dirs) - 1; n >= 0; n-- {
				if entries, err := os.ReadDir(dirs[n]); err == nil && len(entries) == 0 {
					_ = os.Remove(dirs[n])
				}
			}
		}
	}
	plan["status"] = "完成"
	plan["result"] = report
	e = writeLog()
	if e != nil {
		return nil, e
	}
	return report, nil
}
