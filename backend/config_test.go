package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConnectionSettingsKeepClearAndRedact(t *testing.T) {
	for k := range ocrEnvKeys {
		t.Setenv(k, "")
	}
	t.Setenv("TENCENTCLOUD_TOKEN", "token_do_not_echo")
	s, e := newTestStore(t)
	if e != nil {
		t.Fatal(e)
	}
	defer s.db.Close()
	path := filepath.Join(t.TempDir(), ".env")
	os.WriteFile(path, []byte("# keep comment\nOTHER=value\nTENCENTCLOUD_TOKEN=\"token_do_not_echo\"\n"), 0600)
	a := &server{s: s, envFile: path}
	if e = a.saveConfig(Record{"values": Record{"TENCENTCLOUD_TOKEN": "", "TENCENTCLOUD_SECRET_ID": "id", "TENCENTCLOUD_SECRET_KEY": "secret"}}); e != nil {
		t.Fatal(e)
	}
	b, _ := os.ReadFile(path)
	if !strings.Contains(string(b), "OTHER=value") || os.Getenv("TENCENTCLOUD_TOKEN") != "token_do_not_echo" || !a.s.ocr.configured() {
		t.Fatal("keep/config reload failed")
	}
	status, _ := json.Marshal(a.configStatus())
	if strings.Contains(string(status), "token_do_not_echo") || strings.Contains(string(status), "\"secret\"") {
		t.Fatal("secret echoed")
	}
	if e = a.saveConfig(Record{"clear": []string{"TENCENTCLOUD_TOKEN"}}); e != nil {
		t.Fatal(e)
	}
	if os.Getenv("TENCENTCLOUD_TOKEN") != "" {
		t.Fatal("clear not effective")
	}
	b, _ = os.ReadFile(path)
	if strings.Contains(string(b), "token_do_not_echo") {
		t.Fatal("old secret retained")
	}
}

func TestConnectionSettingsRejectInvalidWithoutChangingFile(t *testing.T) {
	for k := range ocrEnvKeys {
		t.Setenv(k, "")
	}
	s, e := newTestStore(t)
	if e != nil {
		t.Fatal(e)
	}
	defer s.db.Close()
	path := filepath.Join(t.TempDir(), ".env")
	os.WriteFile(path, []byte("# original\n"), 0600)
	a := &server{s: s, envFile: path, configLocked: map[string]bool{"TENCENTCLOUD_REGION": true}}
	for _, values := range []Record{{"TENCENTCLOUD_TOKEN": "bad\nKEY=oops"}, {"STRIPE_SECRET_KEY": "retired"}, {"TENCENTCLOUD_REGION": "ap-hongkong"}, {"UNSUPPORTED": "x"}} {
		if e = a.saveConfig(Record{"values": values}); e == nil {
			t.Fatal("invalid config accepted", values)
		}
		b, _ := os.ReadFile(path)
		if string(b) != "# original\n" {
			t.Fatal("invalid config modified file")
		}
	}
}
