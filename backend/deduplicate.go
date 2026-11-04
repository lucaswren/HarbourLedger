package main

import (
	"encoding/json"
	"fmt"
	pdf "github.com/ledongthuc/pdf"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Compare decoded page commands, text and embedded image data, excluding PDF
// container metadata. Annotated/signed/interactive versions remain separate.
func pdfContentFingerprint(path string) (result string, err error) {
	defer func() {
		if recover() != nil {
			result = ""
			err = nil
		}
	}()
	lines, pages, e := readPDF(path)
	if e != nil {
		return "", nil
	}
	f, r, e := pdf.Open(path)
	if e != nil {
		return "", nil
	}
	defer f.Close()
	if !r.Trailer().Key("Root").Key("AcroForm").IsNull() {
		return "", nil
	}
	parts := []string{fmt.Sprint(pages)}
	for _, l := range lines {
		parts = append(parts, strings.Join(strings.Fields(l.text(0, 10000)), " "))
	}
	readStream := func(v pdf.Value) string {
		rd := v.Reader()
		defer rd.Close()
		b, e := io.ReadAll(io.LimitReader(rd, 32*1024*1024+1))
		if e != nil || len(b) > 32*1024*1024 {
			return ""
		}
		return hashBytes(b)
	}
	var resources func(pdf.Value, int) bool
	resources = func(v pdf.Value, depth int) bool {
		if depth > 8 {
			return false
		}
		xs := v.Key("XObject")
		keys := xs.Keys()
		sort.Strings(keys)
		for _, k := range keys {
			x := xs.Key(k)
			switch x.Key("Subtype").Name() {
			case "Image":
				h := readStream(x)
				if h == "" {
					return false
				}
				parts = append(parts, k, fmt.Sprint(x.Key("Width").Int64()), fmt.Sprint(x.Key("Height").Int64()), h)
			case "Form":
				h := readStream(x)
				if h == "" || !resources(x.Key("Resources"), depth+1) {
					return false
				}
				parts = append(parts, k, h)
			}
		}
		return true
	}
	for p := 1; p <= pages; p++ {
		page := r.Page(p)
		if page.V.Key("Annots").Len() > 0 {
			return "", nil
		}
		contents := page.V.Key("Contents")
		if contents.Kind() == pdf.Array {
			for n := 0; n < contents.Len(); n++ {
				h := readStream(contents.Index(n))
				if h == "" {
					return "", nil
				}
				parts = append(parts, h)
			}
		} else if contents.Kind() == pdf.Stream {
			h := readStream(contents)
			if h == "" {
				return "", nil
			}
			parts = append(parts, h)
		} else {
			return "", nil
		}
		if !resources(page.Resources(), 0) {
			return "", nil
		}
	}
	return hashBytes([]byte(strings.Join(parts, "\n"))), nil
}
func (s *Store) duplicateOriginal(path, kind string) (string, error) {
	fingerprint, _ := pdfContentFingerprint(path)
	if fingerprint == "" {
		return "", nil
	}
	d, e := s.load()
	if e != nil {
		return "", e
	}
	for _, doc := range d.Docs {
		if str(doc, "kind") != kind && !(kind == "自动发票" && invoiceKind(str(doc, "kind"))) {
			continue
		}
		p, e := s.documentPath(str(doc, "sha"))
		if e != nil {
			return "", e
		}
		b, e := os.ReadFile(p)
		if e != nil || hashBytes(b) != str(doc, "sha") {
			return "", fmt.Errorf("保留原件校验失败，停止去重")
		}
		fp, _ := pdfContentFingerprint(p)
		if fp == fingerprint {
			return str(doc, "sha"), nil
		}
	}
	return "", nil
}
func (s *Store) deduplicateRegistry() (Record, error) {
	report := Record{"removed": 0, "retained_referenced": 0, "backup_url": ""}
	d, e := s.load()
	if e != nil {
		return report, e
	}
	refs := map[string]bool{}
	for _, m := range d.Matches {
		for _, i := range append(append([]Record{}, d.Invoices...), d.Sales...) {
			if str(i, "id") == str(m, "invoice") {
				refs[str(i, "document_sha")] = true
			}
		}
		for _, t := range d.Tx {
			if str(t, "id") == str(m, "tx") {
				refs[str(t, "document_sha")] = true
			}
		}
	}
	for _, t := range d.Tx {
		if str(t, "manual_category") != "" || str(t, "manual_note") != "" {
			refs[str(t, "document_sha")] = true
		}
	}
	for _, p := range d.Platforms {
		refs[str(p, "evidence_sha")] = true
		for _, t := range d.Tx {
			if str(t, "id") == str(p, "tx") {
				refs[str(t, "document_sha")] = true
			}
		}
	}
	for _, i := range d.Issued {
		for _, t := range d.Tx {
			if str(t, "id") == str(i, "tx_id") {
				refs[str(t, "document_sha")] = true
			}
		}
	}
	sort.SliceStable(d.Docs, func(i, j int) bool { return refs[str(d.Docs[i], "sha")] && !refs[str(d.Docs[j], "sha")] })
	seen := map[string]string{}
	drops := []Record{}
	for _, doc := range d.Docs {
		kind := str(doc, "kind")
		if kind != "银行" && kind != "发票" && kind != "销售发票" {
			continue
		}
		p, e := s.documentPath(str(doc, "sha"))
		if e != nil {
			return report, e
		}
		b, e := os.ReadFile(p)
		if e != nil || hashBytes(b) != str(doc, "sha") {
			return report, fmt.Errorf("原件校验失败，停止去重")
		}
		fp, _ := pdfContentFingerprint(p)
		if fp == "" {
			continue
		}
		key := kind + "|" + fp
		if keep := seen[key]; keep != "" {
			if refs[str(doc, "sha")] {
				report["retained_referenced"] = num(report, "retained_referenced") + 1
				continue
			}
			drops = append(drops, Record{"sha": str(doc, "sha"), "keep": keep, "path": str(doc, "path")})
		} else {
			seen[key] = str(doc, "sha")
		}
	}
	if len(drops) == 0 {
		return report, nil
	}
	backup, e := s.backup()
	if e != nil {
		return report, e
	}
	report["backup_url"] = "/api/downloads/backup/" + filepath.Base(backup)
	dir := filepath.Join(s.root, "整理记录")
	if e = os.MkdirAll(dir, 0755); e != nil {
		return report, e
	}
	log := filepath.Join(dir, "重复原件清理_"+time.Now().Format("20060102_150405.000000000")+".json")
	plan := Record{"backup": backup, "duplicates": drops, "status": "planned"}
	b, _ := json.MarshalIndent(plan, "", "  ")
	if e = os.WriteFile(log, b, 0644); e != nil {
		return report, e
	}
	tx, e := s.db.Begin()
	if e != nil {
		return report, e
	}
	defer tx.Rollback()
	for _, drop := range drops {
		if _, e = tx.Exec("DELETE FROM documents WHERE sha=?", str(drop, "sha")); e != nil {
			return report, e
		}
	}
	if e = tx.Commit(); e != nil {
		return report, e
	}
	for _, drop := range drops {
		p, e := withinRoot(s.root, str(drop, "path"))
		if e != nil {
			return report, e
		}
		var count int
		if e = s.db.QueryRow("SELECT count(*) FROM documents WHERE path=?", str(drop, "path")).Scan(&count); e != nil {
			return report, e
		}
		if count == 0 {
			b, e := os.ReadFile(p)
			if e != nil || hashBytes(b) != str(drop, "sha") {
				return report, fmt.Errorf("重复原件已变化，停止移除")
			}
			if e = os.Remove(p); e != nil {
				return report, e
			}
		}
		report["removed"] = num(report, "removed") + 1
	}
	plan["status"] = "completed"
	b, _ = json.MarshalIndent(plan, "", "  ")
	e = os.WriteFile(log, b, 0644)
	return report, e
}
