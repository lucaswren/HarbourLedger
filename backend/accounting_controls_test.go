package main

import (
	"archive/zip"
	"context"
	"encoding/json"
	"github.com/phpdave11/gofpdf"
	"github.com/xuri/excelize/v2"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSalesImportAndHumanVerification(t *testing.T) {
	s, e := newTestStore(t)
	if e != nil {
		t.Fatal(e)
	}
	defer s.db.Close()
	p := gofpdf.New("P", "mm", "A4", "")
	p.AddPage()
	p.SetFont("Arial", "", 12)
	p.MultiCell(180, 8, "EXAMPLE SERVICES LIMITED\nInvoice Number: EXI26010101\nInvoice Date: 2026-01-01\nBill To: Example Customer Limited\nTotal Amount: USD 100.00", "", "", false)
	path := filepath.Join(t.TempDir(), "sale.pdf")
	if e = p.OutputFileAndClose(path); e != nil {
		t.Fatal(e)
	}
	if _, e = s.importPDF(context.Background(), path, "sale.pdf", "销售发票"); e != nil {
		t.Fatal(e)
	}
	d, _ := s.load()
	if len(d.Invoices) != 0 || len(d.Sales) != 1 || yes(d.Sales[0], "verified") || str(d.Sales[0], "customer") != "Example Customer Limited" || cents(d.Sales[0], "amount") != 10000 {
		t.Fatal(d.Sales)
	}
	id := str(d.Sales[0], "id")
	if e = s.saveInvoice(Record{"id": id, "verified": true}); e == nil {
		t.Fatal("machine recognition became human verification without reviewer")
	}
	if e = s.saveInvoice(Record{"id": id, "verified": true, "reviewer": "Accountant"}); e != nil {
		t.Fatal(e)
	}
	if _, e = s.rebuildLedger(context.Background(), false, func(int, int, string) {}); e != nil {
		t.Fatal(e)
	}
	d, _ = s.load()
	if !yes(d.Sales[0], "verified") || str(d.Sales[0], "customer") != "Example Customer Limited" {
		t.Fatal(d.Sales)
	}
	if !strings.Contains(str(d.Docs[0], "path"), "銷售發票") {
		t.Fatal(d.Docs)
	}
}
func TestReceiptDirectionPartialAndCutoff(t *testing.T) {
	s, _ := newTestStore(t)
	defer s.db.Close()
	putDoc(t, s, "sale", "销售发票", Record{"id": "S", "kind": "销售发票", "date": "2026-01-01", "customer": "Customer Limited", "vendor": "Customer Limited", "number": "S1", "ccy": "USD", "amount": 100.0, "verified": true})
	putDoc(t, s, "bank", "银行", Record{"tx": []Record{{"id": "R1", "date": "2026-01-10", "dep": 40.0, "orig_amount": 40.0, "orig_ccy": "USD", "ccy": "USD", "details": "DEPOSIT"}, {"id": "R2", "date": "2026-02-10", "dep": 60.0, "orig_amount": 60.0, "orig_ccy": "USD", "ccy": "USD", "details": "DEPOSIT"}, {"id": "W", "date": "2026-01-10", "wd": 100.0, "orig_amount": 100.0, "orig_ccy": "USD", "ccy": "USD"}}})
	m := Record{"id": "M1", "tx": "W", "invoice": "S", "ccy": "USD", "amount": 40.0, "status": "已确认", "reviewer": "Human", "evidence": "客户付款通知"}
	if e := s.saveMatch(m); e == nil {
		t.Fatal("sales matched withdrawal")
	}
	m["tx"] = "R1"
	m["evidence"] = ""
	if e := s.saveMatch(m); e == nil {
		t.Fatal("anonymous deposit accepted without provenance")
	}
	m["evidence"] = "客户付款通知"
	if e := s.saveMatch(m); e != nil {
		t.Fatal(e)
	}
	m2 := clone(m)
	m2["id"] = "M2"
	m2["tx"] = "R2"
	m2["amount"] = 60.0
	if e := s.saveMatch(m2); e != nil {
		t.Fatal(e)
	}
	d, _ := s.load()
	rows := settlementSchedule(d, "2026-01-31")
	if len(rows) != 1 || cents(rows[0], "remaining_amount") != 6000 || rows[0]["overdue_days"] != nil {
		t.Fatal(rows)
	}
	rows = settlementSchedule(d, "2026-02-28")
	if cents(rows[0], "remaining_amount") != 0 {
		t.Fatal(rows)
	}
	m2["amount"] = 60.01
	if e := s.saveMatch(m2); e == nil {
		t.Fatal("over allocation accepted")
	}
	if e := s.setLock(Record{"locked_until": "2026-01-31", "reviewer": "Human", "reason": "January closed"}); e != nil {
		t.Fatal(e)
	}
	if e := s.saveMatch(m); e == nil {
		t.Fatal("locked receipt modified")
	}
	if e := s.setLock(Record{"locked_until": "", "reviewer": "Human", "reason": "Reopen for correction"}); e != nil {
		t.Fatal(e)
	}
	logs, e := s.auditHistory()
	if e != nil || len(logs) < 5 {
		t.Fatal(logs, e)
	}
	found := false
	for _, r := range logs {
		if str(r, "entity") == "allocations" && str(r, "action") == "INSERT" && strings.Contains(str(r, "after"), "客户付款通知") {
			found = true
		}
	}
	if !found {
		t.Fatal("missing allocation history")
	}
	if _, e = s.db.Exec("DELETE FROM audit_log"); e == nil {
		t.Fatal("audit deletion allowed")
	}
}
func TestPlatformGrossNetAndReceiptLimits(t *testing.T) {
	s, _ := newTestStore(t)
	defer s.db.Close()
	sha, _ := storedContent(t, s, []byte("platform-evidence"), "platform.pdf", Record{})
	s.db.Exec("UPDATE documents SET kind='平台结算' WHERE sha=?", sha)
	putDoc(t, s, "sale", "销售发票", Record{"id": "S", "kind": "销售发票", "date": "2026-01-01", "customer": "Client", "number": "S1", "ccy": "USD", "amount": 100.0, "verified": true})
	putDoc(t, s, "bank", "银行", Record{"tx": []Record{{"id": "P", "date": "2026-01-10", "dep": 97.0, "orig_amount": 97.0, "orig_ccy": "USD", "ccy": "USD", "details": "STRIPE"}}})
	p := Record{"id": "PL", "tx": "P", "payout_id": "po_1", "ccy": "USD", "gross": 100.0, "fees": 2.0, "refunds": 0.0, "adjustments": 0.0, "status": "已核验", "reviewer": "Human", "evidence_sha": sha}
	if e := s.savePlatform(p); e == nil {
		t.Fatal("incorrect net accepted")
	}
	p["fees"] = 3.0
	if e := s.savePlatform(p); e != nil {
		t.Fatal(e)
	}
	m := Record{"id": "M", "tx": "P", "invoice": "S", "ccy": "USD", "amount": 100.0, "status": "已确认", "reviewer": "Human", "evidence": "orders and platform report"}
	if e := s.saveMatch(m); e == nil {
		t.Fatal("gross accepted against net without settlement")
	}
	m["platform_id"] = "PL"
	if e := s.saveMatch(m); e != nil {
		t.Fatal(e)
	}
	p["gross"] = 101.0
	p["refunds"] = 1.0
	if e := s.savePlatform(p); e == nil {
		t.Fatal("refund change invalidated existing confirmation")
	}
}
func TestPolicyUpgradePreservesEvidenceAndHistory(t *testing.T) {
	s, _ := newTestStore(t)
	defer s.db.Close()
	sha, rel := storedContent(t, s, []byte("old-invoice"), "invoice.pdf", Record{"date": "2026-01-01", "vendor": "Vendor", "number": "A", "amount": 100.0, "ccy": "USD", "verified": true, "method": "Go PDF内容核验"})
	if e := s.upgradeReviewPolicy(); e != nil {
		t.Fatal(e)
	}
	d, _ := s.load()
	if yes(d.Invoices[0], "verified") || !yes(d.Invoices[0], "extraction_complete") {
		t.Fatal(d.Invoices)
	}
	if _, e := os.Stat(filepath.Join(s.root, rel)); e != nil {
		t.Fatal(e)
	}
	if str(d.Invoices[0], "document_sha") != sha {
		t.Fatal(d.Invoices)
	}
	cfg, _ := s.settings(nil)
	if str(cfg, "automation") != "retain_unmatched" {
		t.Fatal(cfg)
	}
	if e := s.upgradeReviewPolicy(); e != nil {
		t.Fatal(e)
	}
	files, _ := filepath.Glob(filepath.Join(s.root, "备份", "*.zip"))
	if len(files) != 1 {
		t.Fatal(files)
	}
}
func TestExportIgnoresRetiredSubsequentReviewOption(t *testing.T) {
	s, _ := newTestStore(t)
	defer s.db.Close()
	for n, day := range []string{"2026-03-31", "2026-04-30"} {
		b := []byte(day)
		sha, rel := storedContent(t, s, b, day+".pdf", Record{})
		m := Record{"statement_date": day, "tx": []Record{{"id": day, "date": day, "period": day[:7], "dep": 10.0, "ccy": "USD", "orig_ccy": "USD", "orig_amount": 10.0}}, "checks": []Record{{"period": day[:7], "ok": true, "bank_number": "123", "account": "USD", "ccy": "USD", "closing": float64(n+1) * 10}}}
		raw, _ := json.Marshal(m)
		s.db.Exec("UPDATE documents SET kind='银行',meta=?,path=? WHERE sha=?", string(raw), rel, sha)
	}
	storedContent(t, s, []byte("subsequent-invoice"), "april.pdf", Record{"date": "2026-04-10", "vendor": "Supplier", "number": "APRIL", "ccy": "USD", "amount": 100.0})
	path, e := s.export("2026-03", "2026-03", false, "Company", Record{"report_start": "2026-04-01", "report_end": "2026-04-30", "subsequent_end": "2026-04-30"})
	if e != nil {
		t.Fatal(e)
	}
	z, e := zip.OpenReader(path)
	if e != nil {
		t.Fatal(e)
	}
	defer z.Close()
	pdfs, subsequent := 0, 0
	for _, f := range z.File {
		if strings.HasSuffix(f.Name, ".pdf") {
			pdfs++
			if strings.HasPrefix(f.Name, "期後核查/2026-04-01至2026-04-30/") {
				subsequent++
			}
		}
		if strings.HasSuffix(f.Name, ".xlsx") {
			r, _ := f.Open()
			book, e := excelize.OpenReader(r)
			r.Close()
			if e != nil {
				t.Fatal(e)
			}
			rows, _ := book.GetRows("流水明細")
			if len(rows) != 2 || rows[1][1] != "2026-03-31" {
				t.Fatal(rows)
			}
			book.Close()
		}
	}
	if pdfs != 1 || subsequent != 0 {
		t.Fatal("export included documents outside selected range", pdfs)
	}
}
