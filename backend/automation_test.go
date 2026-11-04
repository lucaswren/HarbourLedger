package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestKnownPartyClassification(t *testing.T) {
	for _, v := range []struct {
		text    string
		dep, wd float64
		want    string
	}{{"CREDIT INTEREST EXAMPLE SERVICES LIMITED", 1, 0, "银行利息"}, {"CREDITINTEREST", 1, 0, "银行利息"}, {"B/FBALANCE CREDITINTEREST", 1, 0, "银行利息"}, {"DEBIT INTEREST", 0, 2, "银行利息支出"}, {"TRANSFER JANE DOE", 100, 0, "董事往来"}, {"TRANSFER JOHN CUSTOMER", 100, 0, ""}} {
		r := Record{"details": v.text, "dep": v.dep, "wd": v.wd}
		classifyKnownParty(r, Record{"company": "EXAMPLE SERVICES LIMITED", "director_names": "JANE DOE"})
		if str(r, "category") != v.want {
			t.Fatal(r)
		}
	}
	if accountMatch(Record{"accounts": []string{""}}, Record{"details": "anything"}) {
		t.Fatal("empty account matched")
	}
}
func TestAutomationRetainsUnmatchedAndRequiresHumanReview(t *testing.T) {
	s, e := newTestStore(t)
	if e != nil {
		t.Fatal(e)
	}
	defer s.db.Close()
	_, gone := storedContent(t, s, []byte("unmatched-original"), "unmatched.pdf", Record{"date": "2026-01-01", "ccy": "USD", "amount": 999.0})
	_, keep := storedContent(t, s, []byte("matched-original"), "matched.pdf", Record{"date": "2026-01-01", "ccy": "USD", "amount": 100.0, "verified": true, "accounts": []string{"123-123456-123"}})
	bankSHA, _ := storedContent(t, s, []byte("bank-original"), "bank.pdf", Record{"tx": []Record{{"id": "T1", "date": "2026-01-01", "wd": 100.0, "orig_amount": 100.0, "orig_ccy": "USD", "ccy": "USD", "details": "TO 123-123456-123"}}})
	if _, e = s.db.Exec("UPDATE documents SET kind='银行' WHERE sha=?", bankSHA); e != nil {
		t.Fatal(e)
	}
	if _, e = s.settings(Record{"automation": "delete_unmatched"}); e != nil {
		t.Fatal(e)
	}
	if e = s.refreshCandidates(); e != nil {
		t.Fatal(e)
	}
	d, e := s.load()
	if e != nil || len(d.Invoices) != 2 || len(d.Matches) != 1 || str(d.Matches[0], "status") != "待确认" {
		t.Fatalf("%+v %v", d, e)
	}
	if _, e = os.Stat(filepath.Join(s.root, gone)); e != nil {
		t.Fatal("unmatched original was removed")
	}
	if _, e = os.Stat(filepath.Join(s.root, keep)); e != nil {
		t.Fatal(e)
	}
	backups, _ := filepath.Glob(filepath.Join(s.root, "备份", "*.zip"))
	if len(backups) != 0 {
		t.Fatal("non-destructive matching should not create cleanup backups")
	}
	if e = s.refreshCandidates(); e != nil {
		t.Fatal(e)
	}
}

func TestCompactBankDescriptionsDoNotCreateFalseInvoiceGaps(t *testing.T) {
	for _, tc := range []struct {
		details string
		dep, wd float64
		need    string
	}{
		{"JANEDOE N10000000001", 80000, 0, "董事往来说明"},
		{"CHGFORAUDITCONFO N31032026", 0, 760, "银行记录"},
		{"CASHREBATE5133", 0.05, 0, "银行记录"},
		{"BIB-TO111-222222-838", 0, 100, "转账/换汇说明"},
		{"EXAMPLE SERVICES LIMITED CRTO444-555555-838", 0, 5800, "付款凭证待判定"},
	} {
		r := Record{"details": tc.details, "dep": tc.dep, "wd": tc.wd, "bank_number": "111-222222-838", "need": "转账/换汇说明"}
		classifyKnownParty(r, Record{"company": "EXAMPLE SERVICES LIMITED", "director_names": "JANE DOE"})
		if str(r, "need") != tc.need {
			t.Fatal(tc, r)
		}
	}
	a := Record{"date": "2025-05-06", "ccy": "HKD", "dep": 20001.88, "details": "EXAMPLESERVICESLIMITED N10000000002(06MAY25)"}
	b := Record{"date": "2025-05-06", "ccy": "HKD", "wd": 20001.88, "details": "CRTO111-222222-001 N10000000002(06MAY25)", "category": "内部转账/换汇待确认"}
	classifyOwnTransferReceipts([]Record{a, b}, Record{"company": "EXAMPLE SERVICES LIMITED"})
	if str(a, "need") != "转账/换汇说明" {
		t.Fatal(a)
	}
}
