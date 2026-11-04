package main

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/phpdave11/gofpdf"
)

// Generate fictional inputs locally: no private ledger, original invoices or
// network services are required to exercise import, rebuild and export.
func bankPDF(t *testing.T) string {
	t.Helper()
	p := gofpdf.New("P", "pt", "A4", "")
	p.AddPage()
	p.SetFont("Arial", "", 10)
	p.Text(30, 40, "HSBC Business Direct Statement")
	p.Text(30, 60, "31 January 2026")
	p.Text(30, 80, "Number: 111-222222-838")
	p.Text(30, 110, "HSBC Business Direct Foreign Currency Savings")
	p.Text(110, 140, "B/F BALANCE")
	p.Text(500, 140, "1000.00")
	p.Text(30, 170, "01 Jan")
	p.Text(110, 170, "BIB-VENDOR LIMITED")
	p.Text(410, 170, "100.00")
	p.Text(500, 170, "900.00")
	p.Text(30, 200, "02 Jan")
	p.Text(110, 200, "DEPOSIT CLIENT LIMITED")
	p.Text(330, 200, "200.00")
	p.Text(500, 200, "1100.00")
	p.Text(30, 230, "Total No. of Deposits: 1 Total No. of Withdrawals: 1")
	p.Text(30, 250, "Total Deposit Amount: 200.00 Total Withdrawal Amount: 100.00")
	path := filepath.Join(t.TempDir(), "statement.pdf")
	if err := p.OutputFileAndClose(path); err != nil {
		t.Fatal(err)
	}
	return path
}

func sampleLedger(t *testing.T) *Store {
	t.Helper()
	s, err := newTestStore(t)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.db.Close() })
	for _, input := range []struct{ path, kind string }{
		{bankPDF(t), "银行"},
		{rolePDF(t, "Vendor Limited", "EXAMPLE SERVICES LIMITED"), "自动发票"},
	} {
		if _, err := s.importPDF(context.Background(), input.path, filepath.Base(input.path), input.kind); err != nil {
			t.Fatal(err)
		}
	}
	return s
}
