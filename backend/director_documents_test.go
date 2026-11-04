package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDirectorDocumentUsesStoredAmountsAndChecksNature(t *testing.T) {
	d := emptyData()
	d.Tx = []Record{{"id": "T", "party": "JANE DOE（董事本人）", "category": "董事往來", "date": "2026-01-02", "ccy": "HKD", "dep": 80000.0, "bank_number": "111-222222-838"}, {"id": "R", "party": "JANE DOE（董事本人）", "category": "董事往來", "date": "2026-02-02", "ccy": "HKD", "wd": 2000.0}}
	input := Record{"kind": "borrow", "tx_ids": []string{"T"}, "amount": 1.0}
	r, e := directorDocumentData(d, input, "Example Limited")
	if e != nil {
		t.Fatal(e)
	}
	if cents(arr(r, "rows")[0], "amount") != 8000000 || str(r, "director") != "JANE DOE" {
		t.Fatal(r)
	}
	for _, bad := range []Record{{"kind": "repay", "tx_ids": []string{"T"}}, {"kind": "borrow", "tx_ids": []string{"R"}}, {"kind": "borrow", "tx_ids": []string{"missing"}}, {"kind": "borrow", "tx_ids": []string{"T", "T"}}} {
		if _, e := directorDocumentData(d, bad, "Example Limited"); e == nil {
			t.Fatal("invalid request accepted", bad)
		}
	}
	// Save sample output only when explicitly requested for visual QA.
	if os.Getenv("DIRECTOR_PDF_QA") != "1" {
		return
	}
	folder := filepath.Join("..", "build", "director-proof-qa")
	if e = os.MkdirAll(folder, 0755); e != nil {
		t.Fatal(e)
	}
	font := filepath.Join("..", "assets", "fonts", "NotoSansSC-Regular.ttf")
	for _, kind := range []string{"borrow", "repay"} {
		id := "T"
		if kind == "repay" {
			id = "R"
		}
		r, e = directorDocumentData(d, Record{"kind": kind, "tx_ids": []string{id}}, "Example Limited")
		if e != nil {
			t.Fatal(e)
		}
		b, e := renderDirectorDocument(r, font)
		if e != nil {
			t.Fatal(e)
		}
		if e = os.WriteFile(filepath.Join(folder, kind+".pdf"), b, 0644); e != nil {
			t.Fatal(e)
		}
	}
}
