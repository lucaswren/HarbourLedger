package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
)

func TestBankImportAndRebuildNeverCallCloud(t *testing.T) {
	path := bankPDF(t)
	s, e := newTestStore(t)
	if e != nil {
		t.Fatal(e)
	}
	defer s.db.Close()
	var calls atomic.Int32
	cloud := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		sendJSON(w, 500, Record{"error": "bank must not call cloud"})
	}))
	defer cloud.Close()
	s.ocr = &TencentOCR{secretID: "id", secretKey: "key", endpoint: cloud.URL, client: cloud.Client()}
	added, e := s.importPDF(context.Background(), path, "bank.pdf", "银行")
	if e != nil || !added {
		t.Fatal(e)
	}
	before, e := s.load()
	if e != nil {
		t.Fatal(e)
	}
	result, e := s.rebuildLedger(context.Background(), true, func(int, int, string) {})
	if e != nil {
		t.Fatal(e)
	}
	after, e := s.load()
	if e != nil {
		t.Fatal(e)
	}
	if calls.Load() != 0 {
		t.Fatal("bank upload/rebuild made a cloud request")
	}
	if len(after.Tx) != len(before.Tx) || len(after.Checks) != len(before.Checks) || num(result, "issues") != 0 {
		t.Fatal("local rebuild changed bank records")
	}
	for _, c := range after.Checks {
		if !yes(c, "ok") {
			t.Fatal("local bank check failed")
		}
	}
	if _, e = s.ocr.recognize(context.Background(), path, "bank.pdf", str(before.Docs[0], "sha"), "银行"); e == nil || calls.Load() != 0 {
		t.Fatal("cloud recognizer did not block bank")
	}
	bad := filepath.Join(s.root, "unsupported.pdf")
	if e = os.WriteFile(bad, []byte("%PDF-not-a-bank-statement"), 0644); e != nil {
		t.Fatal(e)
	}
	if _, e = s.importPDF(context.Background(), bad, "unsupported.pdf", "银行"); e == nil || calls.Load() != 0 {
		t.Fatal("failed local bank parsing fell back to cloud")
	}
}

func TestRebuildInvoiceStillCallsCloud(t *testing.T) {
	s, e := newTestStore(t)
	if e != nil {
		t.Fatal(e)
	}
	defer s.db.Close()
	b, e := os.ReadFile(scanPDFTestFile(t))
	if e != nil {
		t.Fatal(e)
	}
	storedContent(t, s, b, "invoice.pdf", Record{"date": "", "vendor": "", "number": "", "ccy": "", "amount": nil, "verified": false, "accounts": []string{}})
	var calls atomic.Int32
	cloud := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Header.Get("X-TC-Action") != "RecognizeGeneralInvoice" {
			t.Error("wrong invoice action")
		}
		sendJSON(w, 200, Record{"Response": Record{"MixedInvoiceItems": []Record{{"Code": "OK", "SingleInvoiceInfos": Record{"OverseasInvoice": Record{"Content": []Record{{"Name": "发票日期", "Value": "2025-04-09"}, {"Name": "客户名称", "Value": "EXAMPLE SERVICES LIMITED"}, {"Name": "供应商", "Value": "Invoice Cloud Vendor"}, {"Name": "发票编号", "Value": "CLOUD-REBUILD"}, {"Name": "币种", "Value": "USD"}, {"Name": "总金额", "Value": "1741.94"}}}}}}}})
	}))
	defer cloud.Close()
	s.ocr = &TencentOCR{secretID: "id", secretKey: "key", endpoint: cloud.URL, client: cloud.Client()}
	if _, e = s.rebuildLedger(context.Background(), true, func(int, int, string) {}); e != nil {
		t.Fatal(e)
	}
	d, e := s.load()
	if e != nil {
		t.Fatal(e)
	}
	if calls.Load() != 1 || str(d.Invoices[0], "number") != "CLOUD-REBUILD" {
		t.Fatal("invoice rebuild did not use cloud")
	}
}
