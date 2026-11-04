package main

import (
	"archive/zip"
	"context"
	"database/sql"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
)

var backupTables = []struct{ name, columns string }{
	{"documents", "sha,kind,name,path,meta"},
	{"reviews", "id,category,note"},
	{"allocations", "id,meta"},
	{"settings", "key,value"},
	{"accounts", "id,meta"},
	{"platform_reports", "id,meta"},
	{"stripe_records", "scope,id,kind,meta"},
	{"stripe_links", "scope,id,document_id"},
	{"audit_log", "seq,at,entity,record_id,action,before_json,after_json"},
}

func (s *Store) emptyForRestore() error {
	for _, table := range backupTables {
		if table.name == "settings" || table.name == "audit_log" {
			continue
		}
		var count int
		if err := s.db.QueryRow("SELECT count(*) FROM " + table.name).Scan(&count); err != nil {
			return err
		}
		if count != 0 {
			return fmt.Errorf("恢復備份需要空白帳簿；請使用另一個空白資料目錄，既有帳簿不會被覆蓋")
		}
	}
	return nil
}

// Restore only data into this application's schema, never source triggers,
// executable files, credentials or arbitrary archive paths. All database changes
// commit together; originals are written to fresh directories and removed on error.
func (s *Store) restoreBackup(ctx context.Context, archive string, replaceExisting ...bool) (Record, error) {
	replace := len(replaceExisting) > 0 && replaceExisting[0]
	if err := s.emptyForRestore(); err != nil && !replace {
		return nil, err
	}
	z, err := zip.OpenReader(archive)
	if err != nil {
		return nil, fmt.Errorf("無法讀取備份 ZIP")
	}
	defer z.Close()
	if len(z.File) > 5000 {
		return nil, fmt.Errorf("備份檔案數超過5000")
	}
	entries := map[string]*zip.File{}
	var total uint64
	for _, f := range z.File {
		name := f.Name
		clean := strings.TrimSuffix(name, "/")
		if clean == "" || strings.ContainsAny(name, "\\:\x00") || strings.HasPrefix(name, "/") || filepath.ToSlash(filepath.Clean(clean)) != clean || clean == ".." || strings.HasPrefix(clean, "../") || f.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("備份包含不安全的路徑")
		}
		key := strings.ToLower(clean)
		if entries[key] != nil {
			return nil, fmt.Errorf("備份包含重複路徑")
		}
		if !f.FileInfo().IsDir() && !f.Mode().IsRegular() {
			return nil, fmt.Errorf("備份包含不支援的檔案類型")
		}
		if f.UncompressedSize64 > 512<<20 || total > (512<<20)-f.UncompressedSize64 {
			return nil, fmt.Errorf("備份解壓總量超過512MB")
		}
		total += f.UncompressedSize64
		entries[key] = f
	}
	dbEntry := entries["账务.sqlite3"]
	if dbEntry == nil || dbEntry.FileInfo().IsDir() {
		return nil, fmt.Errorf("請選擇「備份帳簿」產生的ZIP，會計師資料包無法恢復帳簿")
	}
	stage, err := os.MkdirTemp(s.root, ".restore-")
	if err != nil {
		return nil, err
	}
	retainStage := false
	defer func() {
		if !retainStage {
			os.RemoveAll(stage)
		}
	}()
	copyEntry := func(f *zip.File, target string) error {
		if f == nil || f.FileInfo().IsDir() {
			return fmt.Errorf("備份缺少原件")
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		r, err := f.Open()
		if err != nil {
			return err
		}
		defer r.Close()
		if err = os.MkdirAll(filepath.Dir(target), 0700); err != nil {
			return err
		}
		w, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			return err
		}
		n, err := io.Copy(w, io.LimitReader(r, int64(f.UncompressedSize64)+1))
		closeErr := w.Close()
		if err != nil {
			return err
		}
		if uint64(n) != f.UncompressedSize64 {
			return fmt.Errorf("備份檔案大小不符")
		}
		return closeErr
	}
	snapshot := filepath.Join(stage, "账务.sqlite3")
	if err = copyEntry(dbEntry, snapshot); err != nil {
		return nil, err
	}
	u := url.URL{Scheme: "file", OmitHost: true, Path: filepath.ToSlash(snapshot), RawQuery: "mode=ro"}
	source, err := sql.Open("sqlite", u.String())
	if err != nil {
		return nil, err
	}
	defer source.Close()
	var integrity string
	if err = source.QueryRowContext(ctx, "PRAGMA integrity_check").Scan(&integrity); err != nil || integrity != "ok" {
		return nil, fmt.Errorf("備份資料庫完整性檢查未通過")
	}
	for _, table := range backupTables {
		rows, e := source.QueryContext(ctx, "SELECT "+table.columns+" FROM "+table.name+" LIMIT 0")
		if e != nil {
			return nil, fmt.Errorf("備份格式不相容：缺少所需資料表或欄位")
		}
		rows.Close()
	}
	validationStore := &Store{db: source, root: stage}
	if _, err = validationStore.load(); err != nil {
		return nil, fmt.Errorf("備份業務資料格式無效：%w", err)
	}
	rows, err := source.QueryContext(ctx, "SELECT sha,path FROM documents")
	if err != nil {
		return nil, err
	}
	paths := map[string]string{}
	for rows.Next() {
		var sha, path string
		if err = rows.Scan(&sha, &path); err != nil {
			rows.Close()
			return nil, err
		}
		if !rx(`^[a-f0-9]{64}$`).MatchString(sha) {
			rows.Close()
			return nil, fmt.Errorf("備份憑證編號無效")
		}
		paths[sha] = strings.ReplaceAll(path, "\\", "/")
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	// Validate all original files before attaching or changing the live database.
	for sha, path := range paths {
		if !strings.EqualFold(filepath.Ext(path), ".pdf") {
			return nil, fmt.Errorf("備份原件必須為PDF")
		}
		if err = copyEntry(entries[strings.ToLower(path)], filepath.Join(stage, sha+".pdf")); err != nil {
			return nil, err
		}
		b, e := os.ReadFile(filepath.Join(stage, sha+".pdf"))
		if e != nil || hashBytes(b) != sha {
			return nil, fmt.Errorf("備份原件SHA256校驗失敗，未恢復任何記錄")
		}
	}
	previousBackup := ""
	if replace {
		previousBackup, err = s.backup()
		if err != nil {
			return nil, fmt.Errorf("目前帳簿備份失敗，停止恢復：%w", err)
		}
	}
	importID := "匯入-" + uuid.NewString()
	docDir := filepath.Join(s.root, "憑證", importID)
	draftDir := filepath.Join(s.root, "董事往來草稿", importID)
	draftRoot := filepath.Join(s.root, "董事往來草稿")
	oldDrafts := filepath.Join(stage, "previous-drafts")
	draftsMoved := false
	if _, e := os.Stat(draftRoot); e == nil {
		if !replace {
			return nil, fmt.Errorf("目前帳簿包含董事草稿，請確認替換並備份後再恢復")
		}
		if err = os.Rename(draftRoot, oldDrafts); err != nil {
			return nil, err
		}
		draftsMoved = true
	} else if !os.IsNotExist(e) {
		return nil, e
	}
	committed := false
	defer func() {
		if !committed {
			os.RemoveAll(docDir)
			os.RemoveAll(draftDir)
			if draftsMoved {
				os.Remove(draftRoot)
				if e := os.Rename(oldDrafts, draftRoot); e != nil {
					// Retain the rollback copy if an external lock prevents restoration.
					retainStage = true
				}
			}
		}
	}()
	if err = os.MkdirAll(docDir, 0700); err != nil {
		return nil, err
	}
	for sha := range paths {
		if err = os.Rename(filepath.Join(stage, sha+".pdf"), filepath.Join(docDir, sha+".pdf")); err != nil {
			return nil, err
		}
	}
	drafts := 0
	for _, f := range z.File {
		if !f.FileInfo().IsDir() && strings.HasPrefix(f.Name, "董事往來草稿/") && strings.EqualFold(filepath.Ext(f.Name), ".pdf") {
			target := filepath.Join(draftDir, filepath.FromSlash(strings.TrimPrefix(f.Name, "董事往來草稿/")))
			if err = copyEntry(f, target); err != nil {
				return nil, err
			}
			drafts++
		}
	}
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	if _, err = conn.ExecContext(ctx, "ATTACH DATABASE ? AS restoration", u.String()); err != nil {
		return nil, err
	}
	defer conn.ExecContext(context.Background(), "DETACH DATABASE restoration")
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	for _, table := range backupTables {
		if table.name != "settings" && table.name != "audit_log" {
			var count int
			if err = tx.QueryRow("SELECT count(*) FROM main." + table.name).Scan(&count); err != nil || count != 0 && !replace {
				return nil, fmt.Errorf("目標帳簿已包含資料，未覆蓋")
			}
		}
	}
	// Preserve the source audit history without synthesizing duplicate import rows.
	for _, table := range backupTables {
		for _, action := range []string{"insert", "update", "delete"} {
			if _, err = tx.Exec("DROP TRIGGER IF EXISTS main.audit_" + table.name + "_" + action); err != nil {
				return nil, err
			}
		}
	}
	if _, err = tx.Exec("DROP TRIGGER IF EXISTS main.audit_no_delete; DROP TRIGGER IF EXISTS main.audit_no_update"); err != nil {
		return nil, err
	}
	for _, table := range backupTables {
		if _, err = tx.Exec("DELETE FROM main." + table.name); err != nil {
			return nil, err
		}
		if _, err = tx.Exec("INSERT INTO main." + table.name + "(" + table.columns + ") SELECT " + table.columns + " FROM restoration." + table.name); err != nil {
			return nil, fmt.Errorf("備份資料無法恢復：%w", err)
		}
	}
	for sha := range paths {
		path := filepath.Join("憑證", importID, sha+".pdf")
		if _, err = tx.Exec("UPDATE main.documents SET path=? WHERE sha=?", path, sha); err != nil {
			return nil, err
		}
	}
	if err = setupControlsWith(tx); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	committed = true
	if previousBackup != "" {
		previousBackup = filepath.Base(previousBackup)
	}
	return Record{"documents": len(paths), "director_drafts": drafts, "status": "restored", "previous_backup": previousBackup}, nil
}

func (a *server) restoreRoute(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/restore", func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, 256<<20)
		if err := r.ParseMultipartForm(8 << 20); err != nil {
			fail(w, fmt.Errorf("備份上傳失敗，ZIP上限256MB"))
			return
		}
		defer r.MultipartForm.RemoveAll()
		file, _, err := r.FormFile("file")
		if err != nil {
			fail(w, fmt.Errorf("請選擇備份ZIP"))
			return
		}
		defer file.Close()
		tmp, err := os.CreateTemp(a.s.root, ".restore-upload-*.zip")
		if err != nil {
			fail(w, err)
			return
		}
		defer os.Remove(tmp.Name())
		_, err = io.Copy(tmp, file)
		closeErr := tmp.Close()
		if err == nil {
			err = closeErr
		}
		var result Record
		if err == nil {
			result, err = a.s.restoreBackup(r.Context(), tmp.Name(), r.FormValue("replace_existing") == "true")
		}
		if err != nil {
			fail(w, err)
			return
		}
		sendJSON(w, 200, result)
	})
}
