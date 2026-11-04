package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRebuildAllImportedDocuments(t *testing.T) {
	s := sampleLedger(t)
	before, e := s.load()
	if e != nil {
		t.Fatal(e)
	}
	first := before.Invoices[0]
	if e = s.saveInvoice(Record{"reviewer": "Test Reviewer", "id": str(first, "id"), "verified": true, "note": "保留人工复核备注"}); e != nil {
		t.Fatal(e)
	}
	txID := str(before.Tx[0], "id")
	if _, e = s.db.Exec("INSERT OR REPLACE INTO reviews VALUES(?,?,?)", txID, "人工分类", "保留流水复核说明"); e != nil {
		t.Fatal(e)
	}
	done := 0
	report, e := s.rebuildLedger(context.Background(), false, func(n, total int, name string) { done = n })
	if e != nil {
		t.Fatal(e)
	}
	if int(num(report, "total")) != len(before.Docs) || done != len(before.Docs) || str(report, "backup_url") == "" {
		t.Fatal("not all documents processed")
	}
	after, e := s.load()
	if e != nil {
		t.Fatal(e)
	}
	if len(after.Tx) != len(before.Tx) || len(after.Invoices) != len(before.Invoices) || len(after.Docs) != len(before.Docs) {
		t.Fatal("rebuild lost records")
	}
	for _, i := range after.Invoices {
		if str(i, "id") == str(first, "id") && (!yes(i, "verified") || str(i, "note") != "保留人工复核备注") {
			t.Fatal("reviewed invoice overwritten")
		}
	}
	for _, x := range after.Tx {
		if str(x, "id") == txID && (str(x, "manual_category") != "人工分类" || str(x, "manual_note") != "保留流水复核说明") {
			t.Fatal("transaction review overwritten")
		}
	}
	originalMatches := map[string]Record{}
	for _, m := range before.Matches {
		originalMatches[str(m, "id")] = m
	}
	for _, m := range after.Matches {
		if prior := originalMatches[str(m, "id")]; prior != nil && (str(m, "status") != str(prior, "status") || str(m, "reviewer") != str(prior, "reviewer")) {
			t.Fatal("allocation overwritten")
		}
	}
}

func TestRebuildJobExposesProgressAndRejectsDuplicate(t *testing.T) {
	s, e := newTestStore(t)
	if e != nil {
		t.Fatal(e)
	}
	defer s.db.Close()
	a := &server{s: s, assets: t.TempDir()}
	handler := a.routes()
	request := func(method, path, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Host = "localhost:8080"
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	a.mu.Lock()
	func() {
		defer a.mu.Unlock()
		if w := request(http.MethodPost, "/api/rebuild", `{"use_cloud":false}`); w.Code != 202 {
			t.Fatalf("start: %d", w.Code)
		}
		if w := request(http.MethodPost, "/api/rebuild", `{"use_cloud":false}`); w.Code != 409 {
			t.Fatalf("duplicate: %d", w.Code)
		}
		if w := request(http.MethodGet, "/api/rebuild/status", ""); w.Code != 200 || !strings.Contains(w.Body.String(), "queued") {
			t.Fatal("status blocked by job lock")
		}
	}()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		w := request(http.MethodGet, "/api/rebuild/status", "")
		var job Record
		json.Unmarshal(w.Body.Bytes(), &job)
		if str(job, "status") == "completed" {
			return
		}
		if str(job, "status") == "failed" {
			t.Fatalf("job failed: %s", str(job, "error"))
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("background job never completed")
}

func TestRebuildCorruptOriginalStopsBeforeChanges(t *testing.T) {
	s, e := newTestStore(t)
	if e != nil {
		t.Fatal(e)
	}
	defer s.db.Close()
	_, rel := storedContent(t, s, []byte("%PDF-fixture"), "original.pdf", Record{})
	var before string
	s.db.QueryRow("SELECT meta FROM documents").Scan(&before)
	os.WriteFile(filepath.Join(s.root, rel), []byte("changed"), 0644)
	if _, e = s.rebuildLedger(context.Background(), false, func(int, int, string) {}); e == nil {
		t.Fatal("corrupt original accepted")
	}
	var after string
	s.db.QueryRow("SELECT meta FROM documents").Scan(&after)
	if before != after {
		t.Fatal("metadata changed before integrity check")
	}
}
