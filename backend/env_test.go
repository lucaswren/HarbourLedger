package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnvFile(t *testing.T) {
	for k := range ocrEnvKeys {
		t.Setenv(k, "")
		os.Unsetenv(k)
	}
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	text := "\ufeff# comment\r\nTENCENTCLOUD_SECRET_ID='file-id'\r\nexport TENCENTCLOUD_SECRET_KEY=\"file=#key\" # note\r\nTENCENTCLOUD_REGION=ap-test # note\r\nIGNORED_KEY=value\r\n"
	if e := os.WriteFile(path, []byte(text), 0600); e != nil {
		t.Fatal(e)
	}
	t.Setenv("TENCENTCLOUD_SECRET_ID", "system-id")
	if e := loadEnvFile(path, false); e != nil {
		t.Fatal(e)
	}
	if os.Getenv("TENCENTCLOUD_SECRET_ID") != "system-id" || os.Getenv("TENCENTCLOUD_SECRET_KEY") != "file=#key" || os.Getenv("TENCENTCLOUD_REGION") != "ap-test" {
		t.Fatal("env precedence or parsing failed")
	}
	t.Setenv("TENCENTCLOUD_TOKEN", "")
	os.WriteFile(path, []byte("TENCENTCLOUD_TOKEN=file-token\n"), 0600)
	if e := loadEnvFile(path, false); e != nil || os.Getenv("TENCENTCLOUD_TOKEN") != "" {
		t.Fatal("explicit empty system value must win")
	}
	if e := loadEnvFile(filepath.Join(dir, "missing"), true); e != nil {
		t.Fatal(e)
	}
	if e := loadEnvFile(filepath.Join(dir, "missing"), false); e == nil {
		t.Fatal("explicit missing config accepted")
	}
}

func TestEnvErrorsDoNotLeakOrPartiallyApply(t *testing.T) {
	for k := range ocrEnvKeys {
		t.Setenv(k, "")
		os.Unsetenv(k)
	}
	path := filepath.Join(t.TempDir(), ".env")
	os.WriteFile(path, []byte("TENCENTCLOUD_SECRET_ID=do-not-print\nTENCENTCLOUD_SECRET_KEY='do-not-print\n"), 0600)
	e := loadEnvFile(path, false)
	if e == nil || strings.Contains(e.Error(), "do-not-print") {
		t.Fatal("invalid env error or credential leak")
	}
	if _, ok := os.LookupEnv("TENCENTCLOUD_SECRET_ID"); ok {
		t.Fatal("invalid file partially applied")
	}
	for _, text := range []string{"bad line", "1BAD=x", "TENCENTCLOUD_SECRET_KEY='x' unexpected"} {
		if _, e := parseEnv(strings.NewReader(text)); e == nil {
			t.Fatal("invalid syntax accepted")
		}
	}
}

func TestConfigNotServed(t *testing.T) {
	s, e := newTestStore(t)
	if e != nil {
		t.Fatal(e)
	}
	defer s.db.Close()
	assets := t.TempDir()
	for _, name := range []string{".env", "custom-config.txt"} {
		path := filepath.Join(assets, name)
		os.WriteFile(path, []byte("private-credential"), 0600)
		a := &server{s: s, assets: assets, envFile: path}
		r := httptest.NewRequest(http.MethodGet, "/"+name, nil)
		r.Host = "localhost:8080"
		w := httptest.NewRecorder()
		a.routes().ServeHTTP(w, r)
		if w.Code != 404 || strings.Contains(w.Body.String(), "private-credential") {
			t.Fatal("config was exposed")
		}
	}
}
