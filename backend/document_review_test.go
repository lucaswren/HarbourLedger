package main

import "testing"

func TestMissingDocumentsExcludeCandidatesAndDirectors(t *testing.T) {
	d := emptyData()
	d.Invoices = []Record{{"id": "I"}}
	d.Matches = []Record{{"tx": "T", "invoice": "I", "status": "待确认", "ccy": "USD"}}
	a := Record{"id": "T", "state": "待确认候选", "gap_type": "待補採購發票", "orig_ccy": "HKD"}
	classifyDocumentReview(a, &d)
	if str(a, "review_type") != "已找到票據" {
		t.Fatal(a)
	}
	b := Record{"id": "B", "category": "董事往来", "gap_type": "用途待確認"}
	classifyDocumentReview(b, &d)
	if str(b, "review_type") != "董事往來（交會計師分類）" {
		t.Fatal(b)
	}
	c := Record{"id": "C", "gap_type": "待補銷售發票"}
	classifyDocumentReview(c, &d)
	if str(c, "review_type") != "未找到銷售憑證" {
		t.Fatal(c)
	}
}

func TestNamedHKDSaleAutomaticallyMatchesNamedReceipt(t *testing.T) {
	s, e := newTestStore(t)
	if e != nil {
		t.Fatal(e)
	}
	defer s.db.Close()
	s.settings(Record{"auto_match_exact": "true"})
	putDoc(t, s, "i", "销售发票", Record{"id": "I", "kind": "销售发票", "date": "2026-01-05", "customer": "JOHN CUSTOMER", "number": "EXI26010501", "ccy": "HKD", "amount": 18000.0, "extraction_complete": true})
	putDoc(t, s, "b", "银行", Record{"tx": []Record{{"id": "T", "date": "2026-01-05", "ccy": "HKD", "orig_ccy": "HKD", "orig_amount": 18000.0, "dep": 18000.0, "details": "B/FBALANCE JOHNCUSTOMER N10571108636"}}})
	if e = s.refreshCandidates(); e != nil {
		t.Fatal(e)
	}
	d, e := s.load()
	if e != nil || len(d.Matches) != 1 || str(d.Matches[0], "validity") != "有效确认" {
		t.Fatal(d.Matches, e)
	}
	i := clone(d.Sales[0])
	i["customer"] = "EXAMPLE CUSTOMER"
	if automaticReceiptPair(d.Tx[0], i) {
		t.Fatal("other customer matched")
	}
}

func TestOwnFXLegsUniqueWithoutDealtAmount(t *testing.T) {
	txs := []Record{{"id": "D", "date": "2025-03-25", "bank_number": "111-222222-838", "category": "内部转账/换汇待确认", "details": "TO111-222222-838 GOLD/EXCHANGEDEBIT", "ccy": "HKD", "orig_ccy": "HKD", "orig_amount": 140229.0, "wd": 140229.0}, {"id": "C", "date": "2025-03-25", "bank_number": "111-222222-838", "category": "内部转账/换汇待确认", "details": "DEPOSIT BIB-EXAMPLE SERVICES LIMITED", "ccy": "USD", "orig_ccy": "USD", "orig_amount": 18000.0, "dep": 18000.0}}
	classifyOwnTransferReceipts(txs)
	if !yes(txs[0], "transfer_verified") || !yes(txs[1], "transfer_verified") {
		t.Fatal(txs)
	}
	if str(txs[0], "orig_ccy") != "HKD" || num(txs[0], "orig_amount") != 140229 {
		t.Fatal("dealt amount invented")
	}
}
