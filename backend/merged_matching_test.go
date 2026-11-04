package main

import "testing"

func TestOnePaymentMatchesTwoInvoices(t *testing.T) {
	for _, ambiguous := range []bool{false, true} {
		t.Run(map[bool]string{false: "unique", true: "ambiguous"}[ambiguous], func(t *testing.T) {
			s, e := newTestStore(t)
			if e != nil {
				t.Fatal(e)
			}
			defer s.db.Close()
			s.settings(Record{"auto_match_exact": "true"})
			amounts := []float64{288, 1580}
			if ambiguous {
				amounts = append(amounts, 1000, 868)
			}
			for n, amount := range amounts {
				id := string(rune('A' + n))
				putDoc(t, s, id, "发票", Record{"id": id, "date": "2026-09-14", "vendor": "示例商務有限公司", "number": "I" + id, "ccy": "HKD", "amount": amount, "accounts": []string{"444-555555-838"}, "extraction_complete": true})
			}
			putDoc(t, s, "bank", "银行", Record{"tx": []Record{{"id": "T", "date": "2026-09-30", "ccy": "HKD", "orig_ccy": "HKD", "orig_amount": 1868.0, "wd": 1868.0, "details": "CRTO444-555555-838"}}})
			if e = s.refreshCandidates(); e != nil {
				t.Fatal(e)
			}
			d, e := s.load()
			if e != nil {
				t.Fatal(e)
			}
			if ambiguous {
				for _, m := range d.Matches {
					if str(m, "status") == "已确认" {
						t.Fatal("ambiguous combination confirmed")
					}
				}
				return
			}
			if len(d.Matches) != 2 {
				t.Fatal(d.Matches)
			}
			for _, m := range d.Matches {
				if str(m, "validity") != "有效确认" || !yes(m, "auto_merged") {
					t.Fatal(m)
				}
			}
			if str(d.Tx[0], "review_type") != "已完成" || cents(d.Tx[0], "wd") != 186800 {
				t.Fatal(d.Tx)
			}
			if e = s.refreshCandidates(); e != nil {
				t.Fatal(e)
			}
			again, _ := s.load()
			if len(again.Matches) != 2 {
				t.Fatal("duplicate allocations")
			}
			d.Matches[0]["amount"] = 289.0
			verdicts(&d)
			for _, m := range d.Matches {
				if str(m, "validity") == "有效确认" {
					t.Fatal("changed group remains confirmed")
				}
			}
		})
	}
}
