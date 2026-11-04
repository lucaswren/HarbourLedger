package main

import "testing"

func TestBlankCompanyDoesNotAssumeInvoiceDirection(t *testing.T) {
	s, err := openStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.db.Close()
	p := rolePDF(t, "EXAMPLE SERVICES LIMITED", "Buyer Limited")
	m, err := s.detectInvoiceDirection(p, Record{})
	if err != nil || str(m, "detected_direction") != "待分类发票" || str(m, "company_reference") != "" {
		t.Fatal(m, err)
	}
	if _, err = s.settings(Record{"company": "Another Company Limited", "company_aliases": "EXAMPLE SERVICES LIMITED"}); err != nil {
		t.Fatal(err)
	}
	m, err = s.detectInvoiceDirection(p, Record{})
	if err != nil || str(m, "detected_direction") != "销售发票" {
		t.Fatal(m, err)
	}
}

func TestDirectorRulesBelongToEachCompany(t *testing.T) {
	for _, names := range []string{"", "OTHER PERSON", "JANE DOE"} {
		tx := Record{"details": "TRANSFER JANEDOE N10000000001", "dep": 100.0}
		classifyKnownParty(tx, Record{"director_names": names})
		if (str(tx, "category") == "董事往来") != (names == "JANE DOE") {
			t.Fatal(names, tx)
		}
	}
	if descriptionHasName("TRANSFER JANE DOEOTHER", "JANE DOE") {
		t.Fatal("partial name matched")
	}
}
