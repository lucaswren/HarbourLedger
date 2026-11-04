package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestTencentInvoiceAndFallback(t *testing.T) {
	s, e := newTestStore(t)
	if e != nil {
		t.Fatal(e)
	}
	defer s.db.Close()
	path := scanPDFTestFile(t)
	var calls atomic.Int32
	cloud := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if !strings.HasPrefix(r.Header.Get("Authorization"), "TC3-HMAC-SHA256 Credential=test-id/") {
			t.Error("missing signature")
		}
		if r.Header.Get("X-TC-Action") != "RecognizeGeneralInvoice" {
			t.Error("wrong action")
		}
		var input Record
		if json.NewDecoder(r.Body).Decode(&input) != nil || str(input, "ImageBase64") == "" || !yes(input, "EnableMultiplePage") {
			t.Error("invalid OCR payload")
		}
		sendJSON(w, 200, Record{"Response": Record{"RequestId": "mock-request", "MixedInvoiceItems": []Record{{"Code": "OK", "SingleInvoiceInfos": Record{"OverseasInvoice": Record{"Content": []Record{{"Name": "发票日期", "Value": "2025-04-09"}, {"Name": "客户名称", "Value": "EXAMPLE SERVICES LIMITED"}, {"Name": "供应商", "Value": "Cloud Vendor"}, {"Name": "发票编号", "Value": "CLOUD-1"}, {"Name": "币种", "Value": "USD"}, {"Name": "总金额", "Value": "1741.94"}}}}}}}})
	}))
	defer cloud.Close()
	s.ocr = &TencentOCR{secretID: "test-id", secretKey: "test-key", endpoint: cloud.URL, client: cloud.Client()}
	ok, e := s.importPDF(context.Background(), path, "invoice.pdf", "发票")
	if e != nil || !ok {
		t.Fatal(e)
	}
	d, e := s.load()
	if e != nil {
		t.Fatal(e)
	}
	if len(d.Invoices) != 1 {
		t.Fatalf("expected one OCR purchase invoice: %+v", d)
	}
	i := d.Invoices[0]
	if str(i, "vendor") != "Cloud Vendor" || str(i, "recognition_provider") != "tencent" || yes(i, "verified") || cents(i, "amount") != 174194 {
		t.Fatalf("bad cloud result: %#v", i)
	}
	ok, e = s.importPDF(context.Background(), path, "same.pdf", "发票")
	if e != nil || ok || calls.Load() != 1 {
		t.Fatal("duplicate triggered paid OCR call")
	}
	fallback, e := newTestStore(t)
	if e != nil {
		t.Fatal(e)
	}
	defer fallback.db.Close()
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sendJSON(w, 200, Record{"Response": Record{"Error": Record{"Code": "AuthFailure.SecretIdNotFound", "Message": "secret should not leak"}, "RequestId": "mock-failure"}})
	}))
	defer bad.Close()
	fallback.ocr = &TencentOCR{secretID: "invalid", secretKey: "invalid-key", endpoint: bad.URL, client: bad.Client()}
	ok, e = fallback.importPDF(context.Background(), path, "invoice.pdf", "发票")
	if e != nil || !ok {
		t.Fatal(e)
	}
	fd, e := fallback.load()
	if e != nil {
		t.Fatal(e)
	}
	i = fd.Unclassified[0]
	if str(i, "recognition_provider") != "local" || str(i, "ocr_fallback_reason") == "" || cents(i, "amount") != 0 || strings.Contains(str(i, "ocr_fallback_reason"), "secret should") {
		t.Fatalf("fallback failed: %#v", i)
	}
	local, e := newTestStore(t)
	if e != nil {
		t.Fatal(e)
	}
	defer local.db.Close()
	local.ocr = &TencentOCR{endpoint: cloud.URL, client: cloud.Client()}
	before := calls.Load()
	if _, e = local.importPDF(context.Background(), path, "local.pdf", "发票"); e != nil {
		t.Fatal(e)
	}
	if calls.Load() != before {
		t.Fatal("unconfigured mode made network request")
	}
}
func TestOCRStatusAndSignature(t *testing.T) {
	var c *TencentOCR
	if c.configured() {
		t.Fatal("nil configured")
	}
	if yes(c.status(), "configured") {
		t.Fatal("nil status")
	}
	c = &TencentOCR{secretID: "sensitive-id", secretKey: "sensitive-key"}
	b, _ := json.Marshal(c.status())
	if strings.Contains(string(b), "sensitive") {
		t.Fatal("credentials leaked")
	}
	now := time.Unix(1551113065, 0)
	a := tcAuthorization("id", "key", "ocr.tencentcloudapi.com", "GeneralAccurateOCR", []byte(`{}`), now)
	b2 := tcAuthorization("id", "key", "ocr.tencentcloudapi.com", "GeneralAccurateOCR", []byte(`{"x":1}`), now)
	if a == b2 || !strings.Contains(a, "2019-02-25/ocr/tc3_request") {
		t.Fatal("signature does not bind payload and UTC date")
	}
}
