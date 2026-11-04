package main

import (
	"testing"
)

func TestStripePayoutVerificationDoesNotSettleSales(t *testing.T) {
	s, e := newTestStore(t)
	if e != nil {
		t.Fatal(e)
	}
	defer s.db.Close()
	scope := "acct_live/live"
	s.settings(Record{"stripe_scope": scope})
	putDoc(t, s, "bank", "银行", Record{"tx": []Record{{"id": "TX-stripe", "date": "2026-03-06", "ccy": "HKD", "orig_ccy": "HKD", "dep": 1195.70, "wd": 0, "details": "STRIPEPAYMENTSEURO", "category": "收款待来源确认", "gap_type": "待補銷售發票"}}})
	if _, e = s.db.Exec(`INSERT INTO stripe_records VALUES (?, 'po_1', 'payout', '{"status":"paid","currency":"hkd","amount":119570}')`, scope); e != nil {
		t.Fatal(e)
	}
	if _, e = s.db.Exec(`INSERT INTO platform_reports VALUES ('report', '{"settlement_kind":"Stripe提现","scope":"acct_live/live","payout_id":"po_1","status":"已核验","tx":"TX-stripe","ccy":"HKD","net":1195.70}')`); e != nil {
		t.Fatal(e)
	}
	d, e := s.load()
	if e != nil {
		t.Fatal(e)
	}
	bank := d.Tx[0]
	if !yes(bank, "stripe_payout_verified") || str(bank, "state") != "银行凭证已具备" || str(bank, "gap_type") != "" || len(d.Matches) != 0 || len(d.Sales) != 0 {
		t.Fatal("withdrawal was treated as sales", bank)
	}
	if _, e = s.db.Exec(`DELETE FROM stripe_records`); e != nil {
		t.Fatal(e)
	}
	d, e = s.load()
	if e != nil || yes(d.Tx[0], "stripe_payout_verified") {
		t.Fatal("missing source remained verified", e)
	}
}
