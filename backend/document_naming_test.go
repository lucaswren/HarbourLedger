package main

import "testing"

func TestApprovedDocumentNames(t *testing.T) {
	bank := Record{"statement_date": "2026-03-31", "checks": []Record{{"bank_number": "111-222222-838"}}}
	if got := contentName("银行", bank, ""); got != "2026-03_銀行月結單_HSBC_111-222222-838.pdf" {
		t.Fatal(got)
	}
	i := Record{"date": "2025-10-11", "vendor": "Google Asia Pacific Pte. Ltd.", "number": "M0000000000000001", "ccy": "USD", "amount": 10.0, "document_type": "payment_receipt"}
	if got := contentName("发票", i, ""); got != "2025-10-11_採購收據_Google Asia Pacific Pte. Ltd_M0000000000000001_USD_10.00.pdf" {
		t.Fatal(got)
	}
	if archiveCategory("发票") != "採購憑證" || archiveCategory("销售发票") != "銷售憑證" || archiveCategory("平台结算") != "其他憑證" {
		t.Fatal("archive categories")
	}
}

func TestGoogleCurrencyScopedAndUniquePayment(t *testing.T) {
	s, e := newTestStore(t)
	if e != nil {
		t.Fatal(e)
	}
	defer s.db.Close()
	s.settings(Record{"google_billing_id": "1111-2222-3333", "google_billing_currency": "USD", "auto_match_exact": "true"})
	i := googleReceiptFields("Payment Receipt\nGoogle Asia Pacific Pte. Ltd.\nBilling ID 70 Pasir Panjang Road\n1111-2222-3333\nPayment date Oct 11, 2025\nPayment number M0000000000000001\nPayment amount $10.00")
	i["id"] = "I"
	i["verified"] = false
	if e = s.applyConfirmedGoogleCurrency(i); e != nil || str(i, "ccy") != "USD" || !yes(i, "extraction_complete") {
		t.Fatal(i, e)
	}
	other := clone(i)
	other["billing_id"] = "another-account"
	delete(other, "ccy")
	delete(other, "currency_confirmed_by_user")
	s.applyConfirmedGoogleCurrency(other)
	if str(other, "ccy") != "" {
		t.Fatal("unrelated billing account inherited currency")
	}
	bank := Record{"id": "T", "date": "2025-10-13", "ccy": "USD", "orig_ccy": "USD", "orig_amount": 10.0, "wd": 10.0, "details": "MDC GOOGLE ADS"}
	putDoc(t, s, "b", "银行", Record{"tx": []Record{bank}})
	putDoc(t, s, "i", "发票", i)
	if e = s.refreshCandidates(); e != nil {
		t.Fatal(e)
	}
	d, e := s.load()
	if e != nil || len(d.Matches) != 1 || str(d.Matches[0], "status") != "已确认" {
		t.Fatal(d.Matches, e)
	}
	bad := clone(bank)
	bad["details"] = "OTHER MERCHANT"
	if automaticPaymentPair(bad, i) {
		t.Fatal("unrelated merchant matched")
	}
	bad = clone(bank)
	bad["date"] = "2025-10-22"
	if automaticPaymentPair(bad, i) {
		t.Fatal("distant payment matched")
	}
}
