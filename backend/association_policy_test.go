package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestExactOriginalPaymentTakesPrecedenceOverFXAssociation(t *testing.T) {
	d := emptyData()
	i := Record{"id": "I", "date": "2026-01-14", "vendor": "Supplier", "number": "I1", "ccy": "USD", "amount": 100.0, "accounts": []string{"123-123456-123"}}
	j := clone(i)
	j["id"] = "J"
	j["number"] = "I2"
	j["amount"] = 200.0
	exact := Record{"id": "E", "date": "2026-01-15", "ccy": "USD", "orig_ccy": "USD", "orig_amount": 100.0, "wd": 100.0, "details": "TO123-123456-123"}
	fx := Record{"id": "F", "date": "2026-01-16", "ccy": "HKD", "wd": 1550.0, "details": "TO123-123456-123 EXCHANGEDEBIT"}
	d.Invoices = []Record{i, j}
	d.Tx = []Record{exact, fx}
	if !validAutomaticExact(&d, Record{"tx": "E", "invoice": "I", "amount": 100.0, "ccy": "USD"}) {
		t.Fatal("FX candidate invalidated precise original payment")
	}
	if automaticCandidatePair(&d, fx, i) {
		t.Fatal("FX must not take invoice supported by exact original payment")
	}
	if !validAutomaticExact(&d, Record{"tx": "F", "invoice": "J", "amount": 200.0, "ccy": "USD"}) {
		t.Fatal("remaining unique FX invoice should associate")
	}
}

func TestUniqueAnonymousReceiptAutoAssociatesAndRejectsAmbiguity(t *testing.T) {
	s, e := newTestStore(t)
	if e != nil {
		t.Fatal(e)
	}
	defer s.db.Close()
	s.settings(Record{"auto_match_exact": "true"})
	i := Record{"id": "I", "kind": "销售发票", "date": "2026-01-01", "customer": "Example Customer, Inc.", "number": "S1", "ccy": "USD", "amount": 6950.9, "extraction_complete": true}
	putAssociationDoc(t, s, "i", "销售发票", i)
	putAssociationDoc(t, s, "b", "银行", Record{"tx": []Record{{"id": "T", "date": "2026-01-12", "ccy": "USD", "orig_ccy": "USD", "orig_amount": 6950.9, "dep": 6950.9, "details": "DEPOSIT"}}})
	if e = s.refreshCandidates(); e != nil {
		t.Fatal(e)
	}
	d, e := s.load()
	if e != nil || len(d.Matches) != 1 || str(d.Matches[0], "validity") != "有效确认" {
		t.Fatal(d.Matches, e)
	}
	if yes(d.Sales[0], "verified") {
		t.Fatal("automatic association must not claim manual verification")
	}
	i2 := clone(i)
	i2["id"] = "I2"
	i2["number"] = "S2"
	d.Sales = append(d.Sales, i2)
	verdicts(&d)
	if str(d.Matches[0], "validity") == "有效确认" {
		t.Fatal("new ambiguity must invalidate automatic link")
	}
	putAssociationDoc(t, s, "i2", "销售发票", i2)
	if e = s.refreshCandidates(); e != nil {
		t.Fatal("new ambiguity blocked refreshing other matches", e)
	}
	later, e := s.load()
	if e != nil {
		t.Fatal(e)
	}
	for _, m := range later.Matches {
		if str(m, "status") == "已确认" {
			t.Fatal("ambiguous link remained confirmed", m)
		}
	}
	bank := clone(d.Tx[0])
	bank["category"] = "董事往來"
	if automaticReceiptPair(bank, i) {
		t.Fatal("director deposit must not become sales")
	}
}

func TestFXAssociationPreservesActualDebitWithoutInventingOriginal(t *testing.T) {
	s, e := newTestStore(t)
	if e != nil {
		t.Fatal(e)
	}
	defer s.db.Close()
	s.settings(Record{"auto_match_exact": "true"})
	putDoc(t, s, "i", "发票", Record{"id": "I", "date": "2026-01-14", "vendor": "EXAMPLE SUPPLIER LIMITED", "number": "IT1", "ccy": "USD", "amount": 7715.0, "accounts": []string{"222-333333-838"}})
	putDoc(t, s, "b", "银行", Record{"tx": []Record{{"id": "T", "date": "2026-01-27", "ccy": "HKD", "orig_ccy": "HKD", "orig_amount": 60294.41, "wd": 60294.41, "details": "TO222-333333-838 - GOLD/EXCHANGEDEBIT"}}})
	if e = s.refreshCandidates(); e != nil {
		t.Fatal(e)
	}
	d, e := s.load()
	if e != nil || len(d.Matches) != 1 || str(d.Matches[0], "validity") != "有效确认" {
		t.Fatal(d.Matches, e)
	}
	if cents(d.Tx[0], "wd") != 6029441 || d.Tx[0]["orig_amount"] != nil || d.Tx[0]["fx"] != nil || cents(d.Invoices[0], "amount") != 771500 {
		t.Fatal(d.Tx, d.Invoices)
	}
	other := clone(d.Invoices[0])
	other["id"] = "I2"
	other["number"] = "IT2"
	d.Invoices = append(d.Invoices, other)
	if validAutomaticExact(&d, d.Matches[0]) {
		t.Fatal("two account/date candidates must not auto associate")
	}
}

func putAssociationDoc(t *testing.T, s *Store, key, kind string, meta Record) {
	t.Helper()
	content := []byte("test-original-" + key)
	sha := hashBytes(content)
	rel := filepath.Join("憑證", key+".pdf")
	if e := os.WriteFile(filepath.Join(s.root, rel), content, 0644); e != nil {
		t.Fatal(e)
	}
	b, _ := json.Marshal(meta)
	if _, e := s.db.Exec("INSERT INTO documents VALUES(?,?,?,?,?)", sha, kind, key+".pdf", rel, string(b)); e != nil {
		t.Fatal(e)
	}
}
