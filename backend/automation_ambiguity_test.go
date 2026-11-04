package main

import "testing"

func TestAutomaticAssociationLeavesAmbiguity(t *testing.T) {
	s, e := newTestStore(t)
	if e != nil {
		t.Fatal(e)
	}
	defer s.db.Close()
	for _, id := range []string{"A", "B"} {
		putDoc(t, s, id, "发票", Record{"id": id, "date": "2026-01-01", "number": id, "vendor": id, "ccy": "USD", "amount": 100.0, "verified": true, "accounts": []string{"123-123456-123"}})
	}
	putDoc(t, s, "bank", "银行", Record{"tx": []Record{{"id": "T", "date": "2026-01-01", "wd": 100.0, "orig_amount": 100.0, "orig_ccy": "USD", "details": "TO 123-123456-123"}}})
	s.settings(Record{"automation": "delete_unmatched"})
	if e = s.refreshCandidates(); e != nil {
		t.Fatal(e)
	}
	d, _ := s.load()
	if len(d.Invoices) != 2 || len(d.Matches) != 2 {
		t.Fatal(d)
	}
	for _, m := range d.Matches {
		if str(m, "status") != "待确认" {
			t.Fatal(m)
		}
	}
}
