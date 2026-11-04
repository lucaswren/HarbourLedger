package main

import (
	"encoding/json"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type testOCRCredentials struct{ values map[string]string }

func (c *testOCRCredentials) read() (map[string]string, error) { return maps.Clone(c.values), nil }
func (c *testOCRCredentials) write(values map[string]string) error {
	c.values = maps.Clone(values)
	return nil
}

func TestConnectionSettingsKeepClearAndRedact(t *testing.T) {
	s, err := newTestStore(t)
	if err != nil {
		t.Fatal(err)
	}
	defer s.db.Close()
	vault := &testOCRCredentials{values: map[string]string{"TENCENTCLOUD_TOKEN": "token_do_not_echo"}}
	a := &server{s: s, credentials: vault}
	if err = a.saveConfig(Record{"values": Record{"TENCENTCLOUD_TOKEN": "", "TENCENTCLOUD_SECRET_ID": "id", "TENCENTCLOUD_SECRET_KEY": "test_secret_do_not_echo", ocrRegionKey: "ap-hongkong"}}); err != nil {
		t.Fatal(err)
	}
	if vault.values["TENCENTCLOUD_TOKEN"] != "token_do_not_echo" || !s.ocr.configured() {
		t.Fatal("keep/config reload failed")
	}
	settings, err := s.settings(nil)
	if err != nil || str(settings, "ocr_region") != "ap-hongkong" {
		t.Fatal("region did not persist", err)
	}
	status, _ := json.Marshal(a.configStatus())
	dbSettings, _ := json.Marshal(settings)
	for _, secret := range []string{"token_do_not_echo", "test_secret_do_not_echo"} {
		if strings.Contains(string(status), secret) || strings.Contains(string(dbSettings), secret) {
			t.Fatal("secret exposed")
		}
	}
	if err = a.saveConfig(Record{"clear": []string{"TENCENTCLOUD_TOKEN"}}); err != nil {
		t.Fatal(err)
	}
	if vault.values["TENCENTCLOUD_TOKEN"] != "" || s.ocr.token != "" {
		t.Fatal("clear not effective")
	}
}

func TestConnectionSettingsRejectInvalidWithoutChangingStorage(t *testing.T) {
	s, err := newTestStore(t)
	if err != nil {
		t.Fatal(err)
	}
	defer s.db.Close()
	vault := &testOCRCredentials{}
	a := &server{s: s, credentials: vault, configLocked: map[string]bool{ocrRegionKey: true}}
	for _, values := range []Record{{"TENCENTCLOUD_TOKEN": "bad\nKEY=oops"}, {"STRIPE_SECRET_KEY": "retired"}, {ocrRegionKey: "ap-hongkong"}, {"UNSUPPORTED": "x"}} {
		if err = a.saveConfig(Record{"values": values}); err == nil {
			t.Fatal("invalid config accepted")
		}
		if len(vault.values) > 0 {
			t.Fatal("invalid config modified vault")
		}
	}
}

func TestLegacyOCRMigrationPreservesOtherSettings(t *testing.T) {
	s, err := newTestStore(t)
	if err != nil {
		t.Fatal(err)
	}
	defer s.db.Close()
	a := &server{s: s, credentials: &testOCRCredentials{}}
	path := filepath.Join(t.TempDir(), ".env")
	if err = os.WriteFile(path, []byte("# keep\nOTHER=value\nTENCENTCLOUD_SECRET_ID=id\nTENCENTCLOUD_SECRET_KEY=secret\nTENCENTCLOUD_REGION=ap-test\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = a.migrateLegacyOCR(path); err != nil {
		t.Fatal(err)
	}
	remaining, err := os.ReadFile(path)
	if err != nil || string(remaining) != "# keep\nOTHER=value\n" || !s.ocr.configured() || s.ocr.region != "ap-test" {
		t.Fatal("migration did not preserve unrelated settings", err)
	}
}
