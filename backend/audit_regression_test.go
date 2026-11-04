package main

import (
	"archive/zip"
	"os"
	"path/filepath"
	"testing"
)

func TestBackupIncludesDirectorDrafts(t *testing.T) {
	s, e := newTestStore(t)
	if e != nil {
		t.Fatal(e)
	}
	defer s.db.Close()
	p := filepath.Join(s.root, "董事往來草稿", "2026", "借款草稿.pdf")
	os.MkdirAll(filepath.Dir(p), 0755)
	os.WriteFile(p, []byte("draft"), 0644)
	name, e := s.backup()
	if e != nil {
		t.Fatal(e)
	}
	z, e := zip.OpenReader(name)
	if e != nil {
		t.Fatal(e)
	}
	defer z.Close()
	for _, f := range z.File {
		if f.Name == "董事往來草稿/2026/借款草稿.pdf" {
			return
		}
	}
	t.Fatal("director draft omitted")
}
func TestUnknownOriginalFXCannotAllocateMultipleInvoices(t *testing.T) {
	d := emptyData()
	d.Tx = []Record{{"id": "T", "ccy": "HKD", "wd": 780.0}}
	for _, id := range []string{"A", "B"} {
		d.Invoices = append(d.Invoices, Record{"id": id, "ccy": "USD", "amount": 100.0})
		d.Matches = append(d.Matches, Record{"tx": "T", "invoice": id, "ccy": "USD", "amount": 100.0, "status": "已确认", "reviewer": "test"})
	}
	verdicts(&d)
	for _, m := range d.Matches {
		if str(m, "validity") == "有效确认" {
			t.Fatal("unbounded FX allocations accepted")
		}
	}
}
func TestBankCurrencyAllocationChecksActualDebit(t *testing.T) {
	d := emptyData()
	d.Tx = []Record{{"id": "T", "ccy": "HKD", "orig_ccy": "USD", "orig_amount": 100.0, "wd": 780.0}}
	d.Invoices = []Record{{"id": "I", "ccy": "HKD", "amount": 1000.0}}
	d.Matches = []Record{{"tx": "T", "invoice": "I", "ccy": "HKD", "amount": 400.0, "status": "已确认", "reviewer": "test"}}
	verdicts(&d)
	if str(d.Tx[0], "state") != "部分付款已关联，仍需补凭证" {
		t.Fatal(d.Tx)
	}
	d.Matches[0]["amount"] = 800.0
	verdicts(&d)
	if str(d.Matches[0], "validity") != "流水超额分配" {
		t.Fatal(d.Matches)
	}
}
func TestManualCategoryRemovesOldExemption(t *testing.T) {
	for _, tc := range []struct {
		row  Record
		want string
	}{{Record{"category": "银行费用", "need": "银行记录", "manual_category": "资源采购", "wd": 10.0}, "待補採購發票"}, {Record{"category": "董事往来", "need": "董事往来说明", "manual_category": "服务收入", "dep": 10.0}, "待補銷售發票"}} {
		serviceEvidence(tc.row)
		if str(tc.row, "gap_type") != tc.want {
			t.Fatal(tc.row)
		}
	}
}
func TestCompletingUnclassifiedInvoiceEnablesAutomaticReceipt(t *testing.T) {
	s, e := newTestStore(t)
	if e != nil {
		t.Fatal(e)
	}
	defer s.db.Close()
	s.settings(Record{"auto_match_exact": "true"})
	putAssociationDoc(t, s, "pending", "待分类发票", Record{"id": "I", "direction_review_required": true})
	putAssociationDoc(t, s, "bank", "银行", Record{"tx": []Record{{"id": "T", "date": "2026-01-12", "ccy": "USD", "orig_ccy": "USD", "orig_amount": 100.0, "dep": 100.0, "details": "DEPOSIT"}}})
	e = s.saveInvoice(Record{"id": "I", "direction": "销售发票", "date": "2026-01-01", "customer": "Customer", "number": "S1", "ccy": "USD", "amount": 100.0})
	if e != nil {
		t.Fatal(e)
	}
	d, e := s.load()
	if e != nil {
		t.Fatal(e)
	}
	if len(d.Sales) != 1 || yes(d.Sales[0], "direction_review_required") || !yes(d.Sales[0], "extraction_complete") {
		t.Fatal(d.Sales)
	}
	if len(d.Matches) != 1 || str(d.Matches[0], "validity") != "有效确认" {
		t.Fatal(d.Matches)
	}
}

func TestResolvedExtractionWarningPreservesOtherNotes(t *testing.T) {
	for _, tc := range []struct {
		complete   bool
		note, want string
	}{{true, "识别字段不完整，请打开原件核验", ""}, {false, "识别字段不完整，请打开原件核验", "识别字段不完整，请打开原件核验"}, {true, "客戶備註", "客戶備註"}} {
		i := Record{"extraction_complete": tc.complete, "note": tc.note}
		clearResolvedExtractionWarning(i)
		if str(i, "note") != tc.want {
			t.Fatal(i)
		}
	}
}
