package main

import "testing"

func TestPurchaseAmountMatchNeedsNoManualVerificationOrAccount(t *testing.T) {
	invoice := Record{"id": "I", "vendor": "Supplier", "number": "INV1", "date": "2026-01-01", "ccy": "USD", "amount": 100.0, "verified": false, "extraction_complete": false}
	bank := Record{"id": "T", "date": "2026-01-02", "ccy": "USD", "orig_ccy": "USD", "orig_amount": 100.0, "wd": 100.0, "details": "PAYMENT", "purpose": "雲服務器", "category": "伺服器租用"}
	if !automaticPaymentPair(bank, invoice) {
		t.Fatal("unique purchase amount should not require manual verification or bank account")
	}
	bank["orig_amount"] = 99.0
	if automaticPaymentPair(bank, invoice) {
		t.Fatal("different amounts must not match")
	}
	bank["orig_amount"] = 100.0
	bank["orig_ccy"] = "HKD"
	if automaticPaymentPair(bank, invoice) {
		t.Fatal("different currencies must not match by number alone")
	}
}
