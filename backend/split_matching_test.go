package main

import "testing"

func TestAutomaticSplitPaymentAsCompleteSet(t *testing.T) {
	s, e := newTestStore(t)
	if e != nil {
		t.Fatal(e)
	}
	defer s.db.Close()
	s.settings(Record{"auto_match_exact": "true"})
	putDoc(t, s, "i", "发票", Record{"id": "I", "date": "2025-04-15", "number": "A", "vendor": "示例商務", "ccy": "HKD", "amount": 4185.0, "accounts": []string{"444-555555-838"}, "extraction_complete": true})
	putDoc(t, s, "b", "银行", Record{"tx": []Record{{"id": "T1", "date": "2025-04-15", "ccy": "HKD", "orig_ccy": "HKD", "orig_amount": 2200.0, "wd": 2200.0, "details": "TO444-555555-838"}, {"id": "T2", "date": "2025-04-15", "ccy": "HKD", "orig_ccy": "HKD", "orig_amount": 1985.0, "wd": 1985.0, "details": "TO444-555555-838"}}})
	if e = s.refreshCandidates(); e != nil {
		t.Fatal(e)
	}
	d, e := s.load()
	if e != nil || len(d.Matches) != 2 {
		t.Fatal(d.Matches, e)
	}
	for _, m := range d.Matches {
		if str(m, "validity") != "有效确认" || !yes(m, "auto_split") {
			t.Fatal(m)
		}
	}
	d.Matches[0]["amount"] = 2201.0
	verdicts(&d)
	for _, m := range d.Matches {
		if str(m, "validity") == "有效确认" {
			t.Fatal("broken split remains valid", m)
		}
	}
}
