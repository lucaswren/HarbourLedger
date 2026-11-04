package main

import (
	"net/http/httptest"
	"testing"
)

func TestReferenceQuestionModuleRetired(t *testing.T) {
	s, e := openStore(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	defer s.db.Close()
	if _, e = s.db.Exec(`CREATE TABLE issues(id TEXT PRIMARY KEY,meta TEXT);INSERT INTO issues VALUES('Q','{"title":"reference only"}');INSERT INTO audit_log(at,entity,record_id,action,after_json) VALUES('now','issues','Q','INSERT','{"title":"reference only"}')`); e != nil {
		t.Fatal(e)
	}
	if e = s.retireReferenceIssues(); e != nil {
		t.Fatal(e)
	}
	var n int
	s.db.QueryRow("SELECT count(*) FROM sqlite_master WHERE type='table' AND name='issues'").Scan(&n)
	if n != 0 {
		t.Fatal("reference questions remain")
	}
	logs, e := s.auditHistory(true)
	if e != nil || len(logs) != 0 {
		t.Fatal("reference questions leaked to export history", logs, e)
	}
	a := &server{s: s, assets: t.TempDir()}
	r := httptest.NewRequest("GET", "/api/issues", nil)
	r.Host = "localhost:8080"
	out := httptest.NewRecorder()
	a.routes().ServeHTTP(out, r)
	if out.Code != 404 {
		t.Fatal(out.Code)
	}
}
