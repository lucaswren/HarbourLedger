package main

import (
	"bytes"
	"context"
	"github.com/phpdave11/gofpdf"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
)

func scanPDFTestFile(t *testing.T) string { return typedPDFTestFile(t, "scanned") }
func typedPDFTestFile(t *testing.T, kind string) string {
	t.Helper()
	p := gofpdf.New("P", "mm", "A4", "")
	p.AddPage()
	if kind == "scanned" || kind == "mixed" {
		var b bytes.Buffer
		png.Encode(&b, image.NewGray(image.Rect(0, 0, 600, 800)))
		p.RegisterImageOptionsReader("scan", gofpdf.ImageOptions{ImageType: "PNG"}, &b)
		p.ImageOptions("scan", 0, 0, 210, 280, false, gofpdf.ImageOptions{ImageType: "PNG"}, 0, "")
	}
	if kind == "mixed" {
		p.AddPage()
	}
	if kind == "text" || kind == "mixed" {
		p.SetFont("Arial", "", 12)
		p.Text(10, 20, "Invoice Number INV-TEST Total USD 100.00")
	}
	path := filepath.Join(t.TempDir(), "document.pdf")
	if e := p.OutputFileAndClose(path); e != nil {
		t.Fatal(e)
	}
	return path
}
func TestPDFTypeRouting(t *testing.T) {
	for _, want := range []string{"text", "scanned", "mixed", "unknown"} {
		p := typedPDFTestFile(t, want)
		got, e := detectPDFType(p)
		if e != nil || got != want {
			t.Fatalf("%s: %s %v", want, got, e)
		}
	}
	p := filepath.Join(t.TempDir(), "broken.pdf")
	os.WriteFile(p, []byte("broken"), 0644)
	if _, e := detectPDFType(p); e == nil {
		t.Fatal("invalid PDF accepted")
	}
}
func TestTextInvoiceNeverCallsCloud(t *testing.T) {
	var calls atomic.Int32
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); w.WriteHeader(500) }))
	defer mock.Close()
	s, e := newTestStore(t)
	if e != nil {
		t.Fatal(e)
	}
	defer s.db.Close()
	s.ocr = &TencentOCR{secretID: "id", secretKey: "key", endpoint: mock.URL, client: mock.Client()}
	p := typedPDFTestFile(t, "text")
	if _, e = s.importPDF(context.Background(), p, "scan-name.pdf", "发票"); e != nil {
		t.Fatal(e)
	}
	if _, e = s.rebuildLedger(context.Background(), true, func(int, int, string) {}); e != nil {
		t.Fatal(e)
	}
	if calls.Load() != 0 {
		t.Fatal("text PDF uploaded")
	}
	d, _ := s.load()
	if str(d.Unclassified[0], "pdf_type") != "text" || str(d.Unclassified[0], "recognition_provider") != "local" {
		t.Fatal(d.Invoices)
	}
}
