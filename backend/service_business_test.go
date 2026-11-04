package main

import "testing"

func TestServerServiceEvidence(t *testing.T) {
	for _, tc := range []struct {
		category, need string
		wd, dep        float64
		want           string
	}{
		{"付款待分类", "付款凭证待判定", 780, 0, "待補採購發票"},
		{"收款待来源确认", "收款来源及销售凭证", 0, 100, "待補銷售發票"},
		{"董事往来", "董事往来说明", 100, 0, "用途待確認"},
		{"内部转账/换汇待确认", "转账/换汇说明", 100, 0, "用途待確認"},
		{"银行利息", "银行记录", 0, 1, "無需發票"},
	} {
		r := Record{"category": tc.category, "need": tc.need, "wd": tc.wd, "dep": tc.dep}
		serviceEvidence(r)
		if str(r, "gap_type") != tc.want {
			t.Fatal(tc, r)
		}
	}
}

func TestConfirmedInternalTransferNeedsOnlyBankRecords(t *testing.T) {
	for _, row := range []Record{
		{"category": "收款待来源确认", "manual_category": "内部转账/换汇", "dep": 100.0},
		{"category": "内部转账/换汇", "transfer_verified": true, "need": "转账/换汇说明", "wd": 100.0},
	} {
		serviceEvidence(row)
		if str(row, "gap_type") != "無需發票" || transactionState(row, nil) != "银行凭证已具备" {
			t.Fatal(row)
		}
	}
}

func TestUSDInvoiceAgainstHKDDebit(t *testing.T) {
	s, e := newTestStore(t)
	if e != nil {
		t.Fatal(e)
	}
	defer s.db.Close()
	bank := Record{"id": "FX", "date": "2026-01-10", "period": "2026-01", "ccy": "HKD", "wd": 780.0, "details": "TO123-123456-123 /OCMT/ usd 100.00 /EXCH/7.8"}
	decorate(bank)
	if str(bank, "orig_ccy") != "USD" || cents(bank, "orig_amount") != 10000 {
		t.Fatal(bank)
	}
	putDoc(t, s, "bank", "银行", Record{"tx": []Record{bank}})
	putDoc(t, s, "invoice", "发票", Record{"id": "I", "date": "2026-01-10", "vendor": "Supplier", "number": "S1", "ccy": "USD", "amount": 100.0, "verified": true, "accounts": []string{"123-123456-123"}})
	if e = s.refreshCandidates(); e != nil {
		t.Fatal(e)
	}
	d, e := s.load()
	if e != nil || len(d.Matches) != 1 {
		t.Fatal(d.Matches, e)
	}
	m := d.Matches[0]
	m["status"] = "已确认"
	m["reviewer"] = "Reviewer"
	if e = s.saveMatch(m); e != nil {
		t.Fatal(e)
	}
	d, _ = s.load()
	if str(d.Matches[0], "validity") != "有效确认" || cents(d.Matches[0], "bank_amount") != 78000 || str(d.Matches[0], "fx_basis") == "" {
		t.Fatal(d.Matches)
	}
	// Confirmed USD allocations are capped by USD original amount, not HKD debit.
	n := clone(m)
	n["id"] = "second"
	n["amount"] = 0.01
	if e = s.saveMatch(n); e == nil {
		t.Fatal("cross-currency over-allocation accepted")
	}
}
func TestManualCrossCurrencyAssociationWithoutSeparateFXProof(t *testing.T) {
	d := emptyData()
	d.Tx = []Record{{"id": "T", "ccy": "HKD", "orig_ccy": "HKD", "orig_amount": 780.0, "wd": 780.0}}
	d.Invoices = []Record{{"id": "I", "ccy": "USD", "amount": 100.0, "verified": true}}
	d.Matches = []Record{{"id": "M", "tx": "T", "invoice": "I", "ccy": "USD", "amount": 100.0, "status": "已确认", "reviewer": "R"}}
	verdicts(&d)
	if str(d.Matches[0], "validity") != "有效确认" {
		t.Fatal(d.Matches)
	}
}

func TestAutomaticUniquePaymentAndAmbiguity(t *testing.T) {
	for _, ambiguous := range []bool{false, true} {
		s, e := newTestStore(t)
		if e != nil {
			t.Fatal(e)
		}
		_, e = s.settings(Record{"auto_match_exact": "true"})
		if e != nil {
			t.Fatal(e)
		}
		putDoc(t, s, "bank", "银行", Record{"tx": []Record{{"id": "T", "date": "2026-01-10", "ccy": "HKD", "wd": 780.0, "orig_ccy": "USD", "orig_amount": 100.0, "details": "TO123-123456-123", "category": "付款待分类", "need": "付款凭证待判定"}}})
		i := Record{"id": "I", "date": "2026-01-10", "vendor": "Supplier", "number": "S1", "ccy": "USD", "amount": 100.0, "verified": false, "extraction_complete": true, "accounts": []string{"123-123456-123"}}
		putDoc(t, s, "i", "发票", i)
		if ambiguous {
			other := clone(i)
			other["id"] = "I2"
			other["number"] = "S2"
			putDoc(t, s, "i2", "发票", other)
		}
		if e = s.refreshCandidates(); e != nil {
			t.Fatal(e)
		}
		d, e := s.load()
		if e != nil {
			t.Fatal(e)
		}
		n := 0
		for _, m := range d.Matches {
			if str(m, "validity") == "有效确认" {
				n++
				if !yes(m, "auto_confirmed") || str(m, "reviewer") != "系統自動匹配" {
					t.Fatal(m)
				}
			}
		}
		if ambiguous && n != 0 || !ambiguous && n != 1 {
			t.Fatal(ambiguous, n, d.Matches)
		}
		if yes(d.Invoices[0], "verified") {
			t.Fatal("automatic matching impersonated invoice human verification")
		}
		s.db.Close()
	}
}

func TestExchangeDebitWithoutOriginalAmountIsNotHKDOriginal(t *testing.T) {
	bank := Record{"ccy": "HKD", "wd": 53229.34, "orig_ccy": "HKD", "orig_amount": 53229.34, "details": "TO222-333333-838 GOLD/EXCHANGEDEBIT"}
	normalizeUnlistedOriginalAmount(bank)
	if str(bank, "orig_ccy") != "" || bank["orig_amount"] != nil || flowOriginal(bank) != 5322934 || cents(bank, "wd") != 5322934 {
		t.Fatal(bank)
	}
	shown := Record{"ccy": "HKD", "wd": 780.0, "orig_ccy": "USD", "orig_amount": 100.0, "details": "GOLD/EXCHANGEDEBIT /OCMT/USD100.00"}
	normalizeUnlistedOriginalAmount(shown)
	if str(shown, "orig_ccy") != "USD" || cents(shown, "orig_amount") != 10000 {
		t.Fatal(shown)
	}
}
