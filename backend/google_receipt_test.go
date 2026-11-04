package main

import "testing"

func TestGooglePaymentReceiptIsPurchaseAndNotSales(t *testing.T) {
	s, e := newTestStore(t)
	if e != nil {
		t.Fatal(e)
	}
	defer s.db.Close()
	path := rolePDF(t, "Payment Receipt\nGoogle Asia Pacific Pte. Ltd.\nEXAMPLE SERVICES LIMITED\nPayment date Oct 11, 2025\nPayment number M0000000000000001\nPayment amount $10.00", "EXAMPLE SERVICES LIMITED")
	decision, e := s.detectInvoiceDirection(path, Record{"seller": "EXAMPLE SERVICES LIMITED"})
	if e != nil || str(decision, "detected_direction") != "发票" {
		t.Fatal(decision, e)
	}
	m, e := parseInvoice(path, "receipt.pdf", hashBytes([]byte("receipt")))
	if e != nil || str(m, "date") != "2025-10-11" || str(m, "number") != "M0000000000000001" || cents(m, "amount") != 1000 {
		t.Fatal(m, e)
	}

	receipt := googleReceiptFields("Payment Receipt\nGoogle Asia Pacific Pte. Ltd.\nPayment date Nov 1, 2025\nPayment number A0000000000000001\nPayment amount $4.46")
	if str(receipt, "date") != "2025-11-01" || cents(receipt, "amount") != 446 || str(receipt, "ccy") != "" {
		t.Fatal(receipt)
	}
}
func TestGoogleDollarReceiptCreatesCandidateWithoutAutomaticConfirmation(t *testing.T) {
	s, e := newTestStore(t)
	if e != nil {
		t.Fatal(e)
	}
	defer s.db.Close()
	s.settings(Record{"auto_match_exact": "true"})
	putDoc(t, s, "b", "银行", Record{"tx": []Record{{"id": "T", "date": "2025-10-13", "ccy": "USD", "orig_ccy": "USD", "orig_amount": 10.0, "wd": 10.0, "details": "MDC GOOGLE ADS"}}})
	putDoc(t, s, "i", "发票", Record{"id": "I", "date": "2025-10-11", "number": "M0000000000000001", "vendor": "Google Asia Pacific Pte. Ltd.", "document_type": "payment_receipt", "provider": "google", "amount": 10.0, "ccy": "", "verified": false})
	if e = s.refreshCandidates(); e != nil {
		t.Fatal(e)
	}
	d, e := s.load()
	if e != nil || len(d.Matches) != 1 || str(d.Matches[0], "status") != "待确认" || yes(d.Matches[0], "auto_confirmed") {
		t.Fatal(d.Matches, e)
	}
}
