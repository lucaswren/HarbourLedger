package main

import (
	"context"
	"github.com/phpdave11/gofpdf"
	"os"
	"path/filepath"
	"testing"
)

func rolePDF(t *testing.T, seller, buyer string) string {
	t.Helper()
	p := gofpdf.New("P", "mm", "A4", "")
	p.AddPage()
	p.SetFont("Arial", "", 12)
	p.MultiCell(180, 8, seller+"\nInvoice Number: EXI1\nInvoice Date: 2026-01-01\nBill To: "+buyer+"\nTotal Amount: USD 100.00", "", "", false)
	path := filepath.Join(t.TempDir(), "arbitrary.pdf")
	if e := p.OutputFileAndClose(path); e != nil {
		t.Fatal(e)
	}
	return path
}
func TestInvoiceDirectionFromOwnCompanyRole(t *testing.T) {
	for _, tc := range []struct{ seller, buyer, want string }{{"EXAMPLE SERVICES LIMITED", "Client Limited", "销售发票"}, {"Vendor Limited", "EXAMPLE SERVICES LIMITED", "发票"}, {"Vendor Limited", "Other Limited", "待分类发票"}, {"EXAMPLE SERVICES LIMITED", "EXAMPLE SERVICES LIMITED", "待分类发票"}} {
		s, _ := newTestStore(t)
		p := rolePDF(t, tc.seller, tc.buyer)
		if _, e := s.importPDF(context.Background(), p, "misleading-purchase.pdf", "自动发票"); e != nil {
			t.Fatal(e)
		}
		d, _ := s.load()
		if len(d.Docs) != 1 || str(d.Docs[0], "kind") != tc.want {
			t.Fatal(tc, d.Docs)
		}
		if str(d.Docs[0], "name") == "misleading-purchase.pdf" {
			t.Fatal("original upload name persisted")
		}
		s.db.Close()
	}
}
func TestSalesPreviewDoesNotPersistOrConsumeNumber(t *testing.T) {
	s, _ := newTestStore(t)
	defer s.db.Close()
	font := filepath.Join("..", "assets", "fonts", "NotoSansSC-Regular.ttf")
	v := Record{"company": "EXAMPLE SERVICES LIMITED", "party": "Client Limited", "date": "2026-10-16", "ccy": "USD", "status": "草稿", "number": "PREVIEW", "items": "Technical service | 1.5 | 120.00", "company_address": "Hong Kong", "payment_terms": "As agreed"}
	r, e := s.generate("销售发票", v, font, true)
	if e != nil || str(r, "pdf_base64") == "" {
		t.Fatal(e)
	}
	d, _ := s.load()
	if len(d.Docs) != 0 {
		t.Fatal("preview registered as invoice")
	}
	v["number"] = ""
	v["status"] = "正式"
	v["confirmed"] = true
	v["reviewer"] = "Reviewer"
	r, e = s.generate("销售发票", v, font)
	if e != nil || str(r, "number") != "SI-202610-001" {
		t.Fatal(r, e)
	}
	if path := os.Getenv("LEDGER_INVOICE_PREVIEW"); path != "" {
		dataPath, _ := s.documentPath(str(r, "document_sha"))
		b, _ := os.ReadFile(dataPath)
		if e = os.WriteFile(path, b, 0644); e != nil {
			t.Fatal(e)
		}
	}
}
func TestTraditionalPresentationPreservesInternalCodes(t *testing.T) {
	if got := traditional("销售发票、采购发票、腾讯云、复核、期后"); got != "銷售發票、採購發票、騰訊雲、覆核、期後" {
		t.Fatal(got)
	}
}

func TestBillToOwnCompanyOverridesStaleSeller(t *testing.T) {
	s, e := newTestStore(t)
	if e != nil {
		t.Fatal(e)
	}
	defer s.db.Close()
	p := rolePDF(t, "INVOICE", "EXAMPLE SERVICES LIMITED")
	decision, e := s.detectInvoiceDirection(p, Record{"seller": "EXAMPLE SERVICES LIMITED", "vendor": "EXAMPLE SERVICES LIMITED"})
	if e != nil || str(decision, "detected_direction") != "发票" || str(decision, "seller") == "EXAMPLE SERVICES LIMITED" {
		t.Fatal(decision, e)
	}
}
