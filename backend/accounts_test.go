package main

import (
	"encoding/json"
	"testing"
)

func TestMultiCurrencyAccountUsesOneMainNumber(t *testing.T) {
	s, e := newTestStore(t)
	if e != nil {
		t.Fatal(e)
	}
	defer s.db.Close()
	putDoc(t, s, "bank", "银行", Record{"tx": []Record{{"id": "T1", "bank_number": "111-222222-838", "account": "HKD Current", "ccy": "HKD"}, {"id": "T2", "bank_number": "111-222222-838", "account": "Foreign Currency Savings", "ccy": "USD"}}, "checks": []Record{{"period": "2025-04", "bank_number": "111-222222-838", "account": "HKD Current", "ccy": "HKD"}, {"period": "2025-03", "bank_number": "111-222222-838", "account": "HKD Savings", "ccy": "HKD"}, {"period": "2025-03", "bank_number": "111-222222-838", "account": "Foreign Currency Savings", "ccy": "USD"}}})
	list, e := s.accounts()
	if e != nil || len(list) != 1 {
		t.Fatal("multi-currency account split", e)
	}
	a := list[0]
	if len(texts(a, "currencies")) != 2 || len(arr(a, "subaccounts")) != 3 || str(a, "first_statement_period") != "2025-03" || str(a, "first_statement_sha") != "bank" || str(a, "opened_on") != "" {
		t.Fatalf("wrong evidence: %#v", a)
	}
	a["bank_name"] = "HSBC"
	a["opened_on"] = "2025-04-01"
	if _, e = s.saveAccount(a); e == nil {
		t.Fatal("opening after existing statement accepted")
	}
	a["opened_on"] = ""
	if _, e = s.saveAccount(a); e != nil {
		t.Fatal(e)
	}
	list, e = s.accounts()
	if e != nil || len(list) != 1 || str(list[0], "opened_on") != "" {
		t.Fatal("inferred month became actual opening date")
	}
}

func TestLegacyCurrencyMetadataConflictsPreserved(t *testing.T) {
	s, e := newTestStore(t)
	if e != nil {
		t.Fatal(e)
	}
	defer s.db.Close()
	for _, a := range []Record{{"id": "a", "bank_name": "HSBC", "bank_number": "123-456", "account": "HKD Savings", "ccy": "HKD", "opened_on": "2024-01-01", "status": "正常"}, {"id": "b", "bank_name": "HSBC", "bank_number": "123456", "account": "Foreign Currency Savings", "ccy": "USD", "opened_on": "2025-01-01", "status": "正常"}} {
		b, _ := json.Marshal(a)
		if _, e = s.db.Exec("INSERT INTO accounts VALUES(?,?)", str(a, "id"), string(b)); e != nil {
			t.Fatal(e)
		}
	}
	list, e := s.accounts()
	if e != nil || len(list) != 1 {
		t.Fatal(e)
	}
	if str(list[0], "opened_on") != "" || len(texts(list[0], "metadata_conflicts")) == 0 || len(arr(list[0], "legacy_records")) != 2 {
		t.Fatal("legacy conflict hidden")
	}
	var count int
	s.db.QueryRow("SELECT count(*) FROM accounts").Scan(&count)
	if count != 2 {
		t.Fatal("legacy metadata removed on read")
	}
}

func TestAccountMetadata(t *testing.T) {
	s, e := newTestStore(t)
	if e != nil {
		t.Fatal(e)
	}
	defer s.db.Close()
	putDoc(t, s, "bank", "银行", Record{"tx": []Record{{"id": "TX-1", "bank_number": "123", "account": "HKD Current", "ccy": "HKD"}, {"id": "TX-2", "bank_number": "123", "account": "HKD Current", "ccy": "HKD"}}})
	list, e := s.accounts()
	if e != nil || len(list) != 1 || str(list[0], "opened_on") != "" {
		t.Fatalf("inferred accounts: %v %v", list, e)
	}
	v := clone(list[0])
	v["bank_name"] = "HSBC"
	v["opened_on"] = "2025-01-01"
	v["status"] = "正常"
	saved, e := s.saveAccount(v)
	if e != nil {
		t.Fatal(e)
	}
	list, e = s.accounts()
	if e != nil || len(list) != 1 || str(list[0], "opened_on") != "2025-01-01" {
		t.Fatalf("saved accounts: %v %v", list, e)
	}
	if _, e = s.saveAccount(v); e == nil {
		t.Fatal("duplicate account accepted")
	}
	saved["closed_on"] = "2024-01-01"
	if _, e = s.saveAccount(saved); e == nil {
		t.Fatal("closing before opening accepted")
	}
	saved["closed_on"] = ""
	saved["status"] = "已销户"
	if _, e = s.saveAccount(saved); e == nil {
		t.Fatal("closed account without date accepted")
	}
	saved["status"] = "正常"
	saved["opened_on"] = "2025-02-30"
	if _, e = s.saveAccount(saved); e == nil {
		t.Fatal("invalid date accepted")
	}
}
