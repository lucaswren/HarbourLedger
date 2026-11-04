package main

import (
	"bytes"
	"context"
	"github.com/phpdave11/gofpdf"
	"mime/multipart"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func duplicatePDF(t *testing.T, title string) string {
	t.Helper()
	p := gofpdf.New("P", "mm", "A4", "")
	p.SetTitle(title, false)
	p.AddPage()
	p.SetFont("Arial", "", 12)
	p.MultiCell(180, 8, "Example Limited\nInvoice Number: A1\nInvoice Date: 2026-01-01\nBill To: EXAMPLE SERVICES LIMITED\nTotal Amount: USD 100.00", "", "", false)
	path := filepath.Join(t.TempDir(), title+".pdf")
	if e := p.OutputFileAndClose(path); e != nil {
		t.Fatal(e)
	}
	return path
}
func TestDuplicatePDFContentIgnoresContainerMetadata(t *testing.T) {
	a, b := duplicatePDF(t, "one"), duplicatePDF(t, "two")
	ab, _ := os.ReadFile(a)
	bb, _ := os.ReadFile(b)
	if hashBytes(ab) == hashBytes(bb) {
		t.Fatal("fixtures must differ in bytes")
	}
	af, _ := pdfContentFingerprint(a)
	bf, _ := pdfContentFingerprint(b)
	if af == "" || af != bf {
		t.Fatal("equivalent content not detected")
	}
	s, _ := newTestStore(t)
	defer s.db.Close()
	if ok, e := s.importPDF(context.Background(), a, "first.pdf", "发票"); e != nil || !ok {
		t.Fatal(e)
	}
	if ok, e := s.importPDF(context.Background(), b, "unrelated-name.pdf", "发票"); e != nil || ok {
		t.Fatal("duplicate imported", e)
	}
	meta := Record{"date": "2026-01-01", "vendor": "Example Limited", "number": "A1", "ccy": "USD", "amount": 100.0}
	storedContent(t, s, bb, "old-duplicate.pdf", meta)
	r, e := s.organizeDocuments()
	if e != nil || num(r, "removed") != 1 {
		t.Fatal(r, e)
	}
	d, _ := s.load()
	if len(d.Invoices) != 1 {
		t.Fatal(d.Invoices)
	}
	backups, _ := filepath.Glob(filepath.Join(s.root, "备份", "*.zip"))
	if len(backups) == 0 {
		t.Fatal("missing pre-cleanup backup")
	}
}
func TestInvoiceImportRequiresBankFirst(t *testing.T) {
	s, _ := newTestStore(t)
	defer s.db.Close()
	a := &server{s: s, assets: t.TempDir()}
	var b bytes.Buffer
	w := multipart.NewWriter(&b)
	w.WriteField("kind", "销售发票")
	part, _ := w.CreateFormFile("files", "sale.pdf")
	part.Write([]byte("%PDF-test"))
	w.Close()
	r := httptest.NewRequest("POST", "/api/import", &b)
	r.Host = "localhost:8080"
	r.Header.Set("Content-Type", w.FormDataContentType())
	out := httptest.NewRecorder()
	a.routes().ServeHTTP(out, r)
	if out.Code != 400 {
		t.Fatal(out.Code, out.Body.String())
	}
	d, _ := s.load()
	if len(d.Docs) != 0 {
		t.Fatal("invoice imported before bank")
	}
}
func TestPreviousMonthAcrossYearAndShortMonth(t *testing.T) {
	for _, tc := range []struct {
		now  time.Time
		want string
	}{{time.Date(2026, 1, 1, 0, 0, 0, 0, time.FixedZone("Shanghai", 8*3600)), "2025-12"}, {time.Date(2026, 3, 31, 0, 0, 0, 0, time.UTC), "2026-02"}} {
		_, end := defaultPeriod(emptyData(), tc.now)
		if end != tc.want {
			t.Fatal(end, tc.want)
		}
	}
}
