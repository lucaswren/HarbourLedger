package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDefaultPeriodFirstStatementThroughCurrentMonth(t *testing.T) {
	d := emptyData()
	d.Checks = []Record{{"period": "2026-09"}, {"period": "2025-03"}, {"period": "2025-04"}}
	// Shanghai is already November while UTC is still October.
	start, end := defaultPeriod(d, time.Date(2026, 10, 31, 18, 0, 0, 0, time.UTC))
	if start != "2025-03" || end != "2026-10" {
		t.Fatal(start, end)
	}
	start, end = defaultPeriod(emptyData(), time.Date(2026, 10, 16, 0, 0, 0, 0, time.UTC))
	if start != "2026-09" || end != "2026-09" {
		t.Fatal("empty ledger default", start, end)
	}
}

func TestYearArchiveMigrationPreservesOriginals(t *testing.T) {
	s, e := newTestStore(t)
	if e != nil {
		t.Fatal(e)
	}
	defer s.db.Close()
	sha, old := storedContent(t, s, []byte("%PDF-year-2025"), "source-a.pdf", Record{"date": "2025-04-09", "vendor": "Vendor", "number": "A1", "amount": 10.0, "ccy": "USD"})
	other, _ := storedContent(t, s, []byte("%PDF-year-2026"), "source-b.pdf", Record{"date": "2026-02-01", "vendor": "Vendor", "number": "B1", "amount": 20.0, "ccy": "USD"})
	unknown, _ := storedContent(t, s, []byte("%PDF-no-date"), "2020-misleading.pdf", Record{})
	report, e := s.organizeDocuments()
	if e != nil {
		t.Fatal(e)
	}
	if num(report, "renamed") != 3 {
		t.Fatal("migration missed documents")
	}
	for h, year := range map[string]string{sha: "2025", other: "2026", unknown: "待核對日期"} {
		path, e := s.documentPath(h)
		if e != nil {
			t.Fatal(e)
		}
		if !strings.Contains(path, filepath.Join("憑證", year, "採購憑證")) {
			t.Fatal("wrong year", path)
		}
		b, e := os.ReadFile(path)
		if e != nil || hashBytes(b) != h {
			t.Fatal("original changed")
		}
	}
	if _, e = os.Stat(filepath.Dir(filepath.Join(s.root, old))); !os.IsNotExist(e) {
		t.Fatal("obsolete hash directory remains")
	}
	if _, e = s.organizeDocuments(); e != nil {
		t.Fatal("second organize failed", e)
	}
	if archiveYear("银行", Record{"statement_date": "2025-12-31"}) != "2025" {
		t.Fatal("bank statement date ignored")
	}
}

func TestCalendarYearArchiveIndependentOfFiscalPeriod(t *testing.T) {
	s, e := newTestStore(t)
	if e != nil {
		t.Fatal(e)
	}
	defer s.db.Close()
	if _, e = s.settings(Record{"report_start": "2025-04-01", "report_end": "2026-03-31"}); e != nil {
		t.Fatal(e)
	}
	for date, want := range map[string]string{"2025-03-31": "2025", "2025-04-01": "2025", "2026-03-31": "2026", "2026-04-01": "2026"} {
		if got := s.archivePeriod("发票", Record{"date": date}); got != want {
			t.Fatal(date, got)
		}
	}
	if got := s.archivePeriod("银行", Record{"statement_date": "2026-06-30"}); got != "2026" {
		t.Fatal(got)
	}
}
