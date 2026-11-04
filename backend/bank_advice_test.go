package main

import (
	"github.com/phpdave11/gofpdf"
	"path/filepath"
	"testing"
)

func TestCreditAdviceGrossFeesNet(t *testing.T) {
	text := "Credit Advice\nTransaction reference: HK000000EXAMPLE1\nTransaction date: 7 Sep 2026\nPayment amount: USD494.09 Local charges: HKD15.00\nAmount credited: USD492.17 Total charges: USD1.92\nCredited account: 111-222222-838 (FCY)\nPARTNER COMMISSION"
	m, e := creditAdviceFields(text, "By order of :\nEXAMPLE PARTNER INC", hashBytes([]byte(text)))
	if e != nil || str(m, "payer") != "EXAMPLE PARTNER INC" || str(m, "date") != "2026-09-07" || cents(m, "fees") != 192 || cents(m, "local_fee") != 1500 {
		t.Fatal(m, e)
	}
	d := emptyData()
	m["document_sha"] = "proof"
	d.Advices = []Record{m}
	d.Tx = []Record{{"id": "T", "date": "2026-09-07", "bank_number": "111-222222-838", "ccy": "USD", "orig_ccy": "USD", "orig_amount": 492.17, "dep": 492.17, "category": "收款待来源确认", "gap_type": "待補銷售發票"}}
	applyCreditAdvices(&d)
	verdicts(&d)
	if str(d.Tx[0], "party") != "EXAMPLE PARTNER INC" || str(d.Tx[0], "review_type") != "已有收款通知，待補佣金結算資料" || cents(d.Tx[0], "dep") != 49217 {
		t.Fatal(d.Tx)
	}
	d.Tx = append(d.Tx, clone(d.Tx[0]))
	delete(d.Tx[0], "advice_sha")
	delete(d.Tx[1], "advice_sha")
	applyCreditAdvices(&d)
	if str(d.Tx[0], "advice_sha") != "" {
		t.Fatal("ambiguous receipts linked")
	}
}

func TestCreditAdviceTwoColumnPayer(t *testing.T) {
	p := gofpdf.New("P", "pt", "A4", "")
	p.AddPage()
	p.SetFont("Arial", "", 10)
	lines := []string{"Credit Advice", "Transaction reference: HK000000EXAMPLE1", "Transaction date: 7 Sep 2026", "Payment amount: USD494.09 Local charges: HKD15.00", "Amount credited: USD492.17 Total charges: USD1.92", "Credited account: 111-222222-838 (FCY)", "PARTNER COMMISSION"}
	for n, line := range lines {
		p.SetXY(72, float64(60+n*18))
		p.CellFormat(460, 14, line, "", 0, "", false, 0, "")
	}
	p.SetXY(72, 250)
	p.CellFormat(200, 14, "By order of :", "", 0, "", false, 0, "")
	p.SetXY(297, 250)
	p.CellFormat(200, 14, "Bank to bank information:", "", 0, "", false, 0, "")
	p.SetXY(72, 268)
	p.CellFormat(200, 14, "EXAMPLE PARTNER INC", "", 0, "", false, 0, "")
	path := filepath.Join(t.TempDir(), "advice.pdf")
	if e := p.OutputFileAndClose(path); e != nil {
		t.Fatal(e)
	}
	m, e := parseCreditAdvice(path, "advice.pdf", hashBytes([]byte("test")))
	if e != nil || str(m, "payer") != "EXAMPLE PARTNER INC" {
		t.Fatal(m, e)
	}
}
