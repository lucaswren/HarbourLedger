package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMoney(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want int64
	}{{"0.005", 0}, {"0.015", 2}, {"-0.015", -2}, {"1741.94", 174194}, {"1.225", 122}, {"1.235", 124}, {"-1.225", -122}, {"-1.235", -124}} {
		got, e := money(tc.in)
		if e != nil || got != tc.want {
			t.Errorf("money(%s)=%d,%v want %d", tc.in, got, e, tc.want)
		}
	}
	for _, s := range []string{"NaN", "Infinity", "bad", "1e999"} {
		if _, e := money(s); e == nil {
			t.Errorf("expected invalid money: %s", s)
		}
	}
}
func TestAllocationGuards(t *testing.T) {
	s, e := newTestStore(t)
	if e != nil {
		t.Fatal(e)
	}
	defer s.db.Close()
	meta := Record{"tx": []Record{{"id": "TX-1", "date": "2026-01-10", "period": "2026-01", "ccy": "HKD", "orig_ccy": "USD", "orig_amount": 100.0, "wd": 780.0, "details": "123-123456-123", "account": "HKD Current", "bank_number": "111-222222-838"}}}
	putDoc(t, s, "bank", "银行", meta)
	putDoc(t, s, "invoice", "发票", Record{"id": "INV-1", "hash": "invoice", "date": "2026-01-10", "vendor": "Vendor", "number": "1", "ccy": "USD", "amount": 100.0, "verified": true, "accounts": []string{"123-123456-123"}})
	m := Record{"id": "m1", "tx": "TX-1", "invoice": "INV-1", "ccy": "USD", "amount": 70.0, "status": "已确认", "reviewer": "Alice", "note": "", "evidence": ""}
	if e = s.saveMatch(m); e != nil {
		t.Fatal(e)
	}
	n := clone(m)
	n["id"] = "m2"
	n["amount"] = 40.0
	if e = s.saveMatch(n); e == nil {
		t.Fatal("over-allocation allowed")
	}
	n["amount"] = 30.0
	n["reviewer"] = ""
	if e = s.saveMatch(n); e == nil {
		t.Fatal("missing reviewer allowed")
	}
	n["reviewer"] = "Bob"
	n["ccy"] = "HKD"
	if e = s.saveMatch(n); e == nil {
		t.Fatal("wrong currency allowed")
	}
	n["ccy"] = "USD"
	if e = s.saveMatch(n); e != nil {
		t.Fatal(e)
	}
	if e = s.saveInvoice(Record{"reviewer": "Test Reviewer", "id": "INV-1", "verified": false}); e == nil {
		t.Fatal("confirmed invoice invalidation allowed")
	}
}
func putDoc(t *testing.T, s *Store, sha, kind string, meta Record) {
	t.Helper()
	b, _ := json.Marshal(meta)
	if _, e := s.db.Exec("INSERT INTO documents VALUES(?,?,?,?,?)", sha, kind, sha+".pdf", "unused.pdf", string(b)); e != nil {
		t.Fatal(e)
	}
}
func TestHTTPBoundaries(t *testing.T) {
	s, e := newTestStore(t)
	if e != nil {
		t.Fatal(e)
	}
	defer s.db.Close()
	a := &server{s: s, assets: t.TempDir()}
	h := a.routes()
	for _, tc := range []struct {
		host, origin, path string
		want               int
	}{{"127.0.0.1:8080", "", "/api/health", 200}, {"attacker.invalid", "", "/api/data", 403}, {"localhost:8080", "https://evil.example", "/api/data", 403}, {"localhost:8080", "http://localhost:9999", "/api/data", 403}, {"localhost:8080", "http://localhost:8080", "/api/data", 200}, {"localhost:8080", "", "/api/documents/invalid", 404}, {"localhost:8080", "", "/api/downloads/export/..%2Fsecret.zip", 404}, {"localhost:8080", "", "/api/unknown", 404}} {
		r := httptest.NewRequest("GET", tc.path, nil)
		r.Host = tc.host
		r.Header.Set("Origin", tc.origin)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != tc.want {
			t.Errorf("%+v got %d: %s", tc, w.Code, w.Body.String())
		}
	}
	r := httptest.NewRequest(http.MethodPut, "/api/settings", strings.NewReader(`{"company":"A"}{"company":"B"}`))
	r.Host = "localhost:8080"
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 400 {
		t.Fatal("multiple JSON objects accepted")
	}
}
func TestImportedLedgerPreservesBalances(t *testing.T) {
	s := sampleLedger(t)
	d, err := s.load()
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Docs) != 2 || len(d.Tx) != 2 || len(d.Checks) != 1 || len(d.Invoices) != 1 {
		t.Fatal("import lost records", len(d.Docs), len(d.Tx), len(d.Checks), len(d.Invoices))
	}
	if !yes(d.Checks[0], "ok") || cents(d.Checks[0], "opening") != 100000 || cents(d.Checks[0], "closing") != 110000 {
		t.Fatal("statement balances changed", d.Checks)
	}
}

func TestSplitCandidatesAndDuplicateEdit(t *testing.T) {
	s, e := newTestStore(t)
	if e != nil {
		t.Fatal(e)
	}
	defer s.db.Close()
	putDoc(t, s, "bank", "银行", Record{"tx": []Record{{"id": "T1", "date": "2026-01-01", "period": "2026-01", "wd": 40.0, "orig_amount": 40.0, "ccy": "USD", "orig_ccy": "USD", "category": "付款待分类", "details": "123-123456-123"}, {"id": "T2", "date": "2026-01-02", "period": "2026-01", "wd": 60.0, "orig_amount": 60.0, "ccy": "USD", "orig_ccy": "USD", "category": "付款待分类", "details": "123-123456-123"}}})
	putDoc(t, s, "invoice", "发票", Record{"id": "INV-Z", "hash": "invoice", "date": "2026-01-01", "vendor": "Vendor", "number": "1", "ccy": "USD", "amount": 100.0, "accounts": []string{"123-123456-123"}, "verified": true})
	if e = s.refreshCandidates(); e != nil {
		t.Fatal(e)
	}
	d, e := s.load()
	if e != nil || len(d.Matches) != 2 {
		t.Fatalf("split candidates %d: %v", len(d.Matches), e)
	}
	for _, m := range d.Matches {
		m["status"] = "已确认"
		m["reviewer"] = "Alice"
		if e = s.saveMatch(m); e != nil {
			t.Fatal(e)
		}
	}
	putDoc(t, s, "invoice2", "发票", Record{"id": "INV-A", "hash": "invoice2", "date": "2026-01-01", "vendor": "Other", "number": "2", "ccy": "USD", "amount": 100.0, "accounts": []string{}, "verified": true})
	if e = s.saveInvoice(Record{"reviewer": "Test Reviewer", "id": "INV-A", "vendor": "Vendor", "number": "1"}); e == nil || !strings.Contains(e.Error(), "失效") {
		t.Fatalf("editing another invoice invalidated existing confirmed allocation: %v", e)
	}
	if e = s.refreshCandidates(); e != nil {
		t.Fatal(e)
	}
	d, _ = s.load()
	if len(d.Matches) != 2 {
		t.Fatal("refresh duplicated candidates")
	}
}
func TestPeriodAndItems(t *testing.T) {
	if _, e := months("2026-03", "2026-01"); e == nil {
		t.Fatal("reversed range allowed")
	}
	for _, s := range []string{"2026-02-30", "2026-2-01", "bad"} {
		if validDate(s) {
			t.Errorf("invalid date accepted: %s", s)
		}
	}
	items, total, e := lineItems("服务 | 1.5 | 12.35\nSupport | 1 | 1.00")
	if e != nil || len(items) != 2 || total != 1952 {
		t.Fatal(fmt.Sprint(items, total, e))
	}
	if _, _, e = lineItems("Bad | -1 | 2"); e == nil {
		t.Fatal("negative quantity allowed")
	}
}
