package main

import (
	"github.com/phpdave11/gofpdf"
	"path/filepath"
	"testing"
)

func TestTwoColumnServerInvoicePreservesCustomerAndTotal(t *testing.T) {
	s, e := newTestStore(t)
	if e != nil {
		t.Fatal(e)
	}
	defer s.db.Close()
	p := gofpdf.New("P", "mm", "A4", "")
	p.AddPage()
	p.SetFont("Arial", "", 12)
	p.Text(10, 15, "Invoice No. EXI25080501 Date of Issue 2025/08/05")
	p.Text(10, 25, "Form")
	p.Text(105, 25, "Billed to")
	p.Text(10, 35, "EXAMPLE SERVICES LIMITED")
	p.Text(105, 35, "Example Customer, Inc.")
	p.Text(10, 50, "Total Amount $ 9,489.64")
	p.Text(10, 65, "All amounts are in United States Dollars (USD).")
	path := filepath.Join(t.TempDir(), "invoice.pdf")
	if e = p.OutputFileAndClose(path); e != nil {
		t.Fatal(e)
	}
	m, e := parseInvoice(path, "invoice.pdf", hashBytes([]byte("server")))
	if e != nil || str(m, "number") != "EXI25080501" || cents(m, "amount") != 948964 {
		t.Fatal(m, e)
	}
	decision, e := s.detectInvoiceDirection(path, m)
	if e != nil || str(decision, "detected_direction") != "销售发票" || str(decision, "buyer") != "Example Customer, Inc." {
		t.Fatal(decision, e)
	}
}
