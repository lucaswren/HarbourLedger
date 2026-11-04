package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func storedContent(t *testing.T, s *Store, content []byte, name string, m Record) (string, string) {
	t.Helper()
	sha := hashBytes(content)
	m["hash"] = sha
	m["id"] = "INV-" + sha[:12]
	m["source"] = name
	rel := filepath.Join("凭证", sha, name)
	if e := os.MkdirAll(filepath.Join(s.root, "凭证", sha), 0755); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(filepath.Join(s.root, rel), content, 0644); e != nil {
		t.Fatal(e)
	}
	raw, _ := json.Marshal(m)
	if _, e := s.db.Exec("INSERT INTO documents VALUES(?,?,?,?,?)", sha, "发票", name, rel, string(raw)); e != nil {
		t.Fatal(e)
	}
	return sha, rel
}

func TestOrganizeByContent(t *testing.T) {
	s, e := newTestStore(t)
	if e != nil {
		t.Fatal(e)
	}
	defer s.db.Close()
	content := []byte("%PDF-fixture-A")
	meta := Record{"date": "2026-10-01", "vendor": "Real Vendor", "number": "INV-7", "ccy": "USD", "amount": 120.5, "verified": true}
	sha, old := storedContent(t, s, content, "20200101-Fake-9999.pdf", meta)
	extra := filepath.Join(s.root, "凭证", "different-name.pdf")
	if e = os.WriteFile(extra, content, 0644); e != nil {
		t.Fatal(e)
	}
	unknown := filepath.Join(s.root, "凭证", "20200101-Fake-9999.pdf")
	if e = os.WriteFile(unknown, []byte("%PDF-different-content"), 0644); e != nil {
		t.Fatal(e)
	}
	result, e := s.organizeDocuments()
	if e != nil {
		t.Fatal(e)
	}
	if num(result, "renamed") != 1 || num(result, "removed") != 1 || len(result["unknown_files"].([]string)) != 1 || str(result, "backup_url") == "" {
		t.Fatalf("bad result: %#v", result)
	}
	kept, e := s.documentPath(sha)
	if e != nil {
		t.Fatal(e)
	}
	if filepath.Base(kept) != "2026-10-01_採購發票_Real Vendor_INV-7_USD_120.50.pdf" {
		t.Fatal(kept)
	}
	b, e := os.ReadFile(kept)
	if e != nil || hashBytes(b) != sha {
		t.Fatal("retained content changed")
	}
	for _, p := range []string{filepath.Join(s.root, old), extra} {
		if _, e = os.Stat(p); !os.IsNotExist(e) {
			t.Fatalf("extra remains: %s %v", p, e)
		}
	}
	if _, e = os.Stat(unknown); e != nil {
		t.Fatal("different content was removed")
	}
	d, e := s.load()
	if e != nil || str(d.Docs[0], "name") != filepath.Base(kept) {
		t.Fatal("canonical filename was not recorded")
	}
	result, e = s.organizeDocuments()
	if e != nil || num(result, "renamed") != 0 || num(result, "removed") != 0 {
		t.Fatal("organize not idempotent", e)
	}
	if e = s.saveInvoice(Record{"reviewer": "Test Reviewer", "id": "INV-" + sha[:12], "number": "INV-8"}); e != nil {
		t.Fatal(e)
	}
	if _, e = os.Stat(kept); !os.IsNotExist(e) {
		t.Fatal("invoice edit left old copy")
	}
	newPath, e := s.documentPath(sha)
	if e != nil || !strings.Contains(newPath, "INV-8") {
		t.Fatal(newPath, e)
	}
}

func TestOrganizePreservesDifferentContentsAndCorruption(t *testing.T) {
	s, e := newTestStore(t)
	if e != nil {
		t.Fatal(e)
	}
	defer s.db.Close()
	sha, rel := storedContent(t, s, []byte("%PDF-A"), "same.pdf", Record{})
	other, _ := storedContent(t, s, []byte("%PDF-B"), "same.pdf", Record{})
	if _, e = s.organizeDocuments(); e != nil {
		t.Fatal(e)
	}
	for _, h := range []string{sha, other} {
		if _, e = s.documentPath(h); e != nil {
			t.Fatal("same filename lost a document")
		}
	}
	path, _ := s.documentPath(sha)
	if e = os.WriteFile(path, []byte("tampered"), 0644); e != nil {
		t.Fatal(e)
	}
	extra := filepath.Join(s.root, "憑證", "extra.pdf")
	os.WriteFile(extra, []byte("%PDF-B"), 0644)
	if _, e = s.organizeDocuments(); e == nil {
		t.Fatal("corruption did not stop cleanup")
	}
	if _, e = os.Stat(extra); e != nil {
		t.Fatal("deleted before checking original")
	}
	_ = rel
}

func TestImportRenamesFromContentAndSkipsRenamedCopy(t *testing.T) {
	path := rolePDF(t, "Vendor Limited", "EXAMPLE SERVICES LIMITED")
	s, e := newTestStore(t)
	if e != nil {
		t.Fatal(e)
	}
	defer s.db.Close()
	added, e := s.importPDF(context.Background(), path, "totally-wrong-name.pdf", "发票")
	if e != nil || !added {
		t.Fatal(e)
	}
	d, e := s.load()
	if e != nil {
		t.Fatal(e)
	}
	name := str(d.Docs[0], "archive_name")
	if name == "totally-wrong-name.pdf" || !strings.Contains(name, "EXI1") || !strings.Contains(name, "USD_100.00") {
		t.Fatal(name)
	}
	added, e = s.importPDF(context.Background(), path, "another-name.pdf", "发票")
	if e != nil || added {
		t.Fatal("same contents under different names imported twice", e)
	}
}
