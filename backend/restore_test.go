package main

import (
	"archive/zip"
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestBackupRestoreRoundTripAndRejectNonemptyLedger(t *testing.T) {
	s, err := newTestStore(t)
	if err != nil {
		t.Fatal(err)
	}
	defer s.db.Close()
	p := rolePDF(t, "Example Supplier Limited", "EXAMPLE SERVICES LIMITED")
	if _, err = s.importPDF(context.Background(), p, "purchase.pdf", "自动发票"); err != nil {
		t.Fatal(err)
	}
	s.settings(Record{"director_names": "JANE DOE", "company_aliases": "EXAMPLE"})
	if _, err = s.db.Exec(`INSERT INTO stripe_records VALUES ('acct_example/test', 'ch_example', 'charge', '{"amount":10000}')`); err != nil {
		t.Fatal(err)
	}
	draft := filepath.Join(s.root, "董事往來草稿", "draft.pdf")
	os.MkdirAll(filepath.Dir(draft), 0700)
	os.WriteFile(draft, []byte("%PDF-draft"), 0600)
	archive, err := s.backup()
	if err != nil {
		t.Fatal(err)
	}
	target, err := openStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer target.db.Close()
	result, err := target.restoreBackup(context.Background(), archive)
	if err != nil || num(result, "documents") != 1 || num(result, "director_drafts") != 1 {
		t.Fatal(result, err)
	}
	got, err := target.load()
	if err != nil || len(got.Invoices) != 1 {
		t.Fatal(got, err)
	}
	path, err := target.documentPath(str(got.Docs[0], "sha"))
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil || hashBytes(b) != str(got.Docs[0], "sha") {
		t.Fatal("restored evidence differs", err)
	}
	cfg, _ := target.settings(nil)
	if str(cfg, "director_names") != "JANE DOE" {
		t.Fatal(cfg)
	}
	var sourceHistory, targetHistory, stripeCount int
	s.db.QueryRow("SELECT count(*) FROM audit_log").Scan(&sourceHistory)
	target.db.QueryRow("SELECT count(*) FROM audit_log").Scan(&targetHistory)
	target.db.QueryRow("SELECT count(*) FROM stripe_records").Scan(&stripeCount)
	if sourceHistory != targetHistory || stripeCount != 1 {
		t.Fatal("history or platform records lost")
	}
	if _, err = target.restoreBackup(context.Background(), archive); err == nil {
		t.Fatal("nonempty ledger overwritten")
	}
	if _, err = target.db.Exec("DELETE FROM audit_log"); err == nil {
		t.Fatal("audit protection not restored")
	}
	target.settings(Record{"company": "Edited Company"})
	result, err = target.restoreBackup(context.Background(), archive, true)
	if err != nil || str(result, "previous_backup") == "" {
		t.Fatal("confirmed replacement did not preserve previous backup", result, err)
	}
	if _, err = os.Stat(filepath.Join(target.root, "备份", str(result, "previous_backup"))); err != nil {
		t.Fatal(err)
	}
	cfg, _ = target.settings(nil)
	if str(cfg, "company") != "EXAMPLE SERVICES LIMITED" {
		t.Fatal("company settings not restored", cfg)
	}
	// A restored ledger must itself produce a restorable backup.
	again, err := target.backup()
	if err != nil {
		t.Fatal(err)
	}
	third, err := openStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer third.db.Close()
	if _, err = third.restoreBackup(context.Background(), again); err != nil {
		t.Fatal(err)
	}
}

func TestRestoreRejectsTraversalAndCorruptOriginal(t *testing.T) {
	s, err := newTestStore(t)
	if err != nil {
		t.Fatal(err)
	}
	defer s.db.Close()
	p := rolePDF(t, "Supplier Limited", "EXAMPLE SERVICES LIMITED")
	if _, err = s.importPDF(context.Background(), p, "purchase.pdf", "自动发票"); err != nil {
		t.Fatal(err)
	}
	archive, err := s.backup()
	if err != nil {
		t.Fatal(err)
	}
	for _, attack := range []string{"traversal", "corrupt", "duplicate"} {
		t.Run(attack, func(t *testing.T) {
			z, err := zip.OpenReader(archive)
			if err != nil {
				t.Fatal(err)
			}
			defer z.Close()
			bad := filepath.Join(t.TempDir(), "bad.zip")
			f, _ := os.Create(bad)
			out := zip.NewWriter(f)
			for _, entry := range z.File {
				if attack == "corrupt" && filepath.Ext(entry.Name) == ".pdf" {
					w, _ := out.Create(entry.Name)
					w.Write([]byte("corrupted"))
				} else {
					if err := out.Copy(entry); err != nil {
						t.Fatal(err)
					}
				}
			}
			if attack == "traversal" {
				w, _ := out.Create("../outside.pdf")
				w.Write([]byte("bad"))
			}
			if attack == "duplicate" {
				w, _ := out.Create("账务.sqlite3")
				w.Write([]byte("bad"))
			}
			out.Close()
			f.Close()
			target, err := openStore(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			defer target.db.Close()
			if _, err = target.restoreBackup(context.Background(), bad); err == nil {
				t.Fatal("invalid backup accepted")
			}
			if err = target.emptyForRestore(); err != nil {
				t.Fatal("failed restore changed ledger", err)
			}
		})
	}
}
