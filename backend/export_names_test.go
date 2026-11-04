package main

import (
	"archive/zip"
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xuri/excelize/v2"
)

func TestReadableArchiveNamesNeverOverwrite(t *testing.T) {
	dir := t.TempDir()
	for n, want := range []string{"公司_会计师资料_2026-01.zip", "公司_会计师资料_2026-01（2）.zip"} {
		f, e := os.CreateTemp(dir, ".temp-")
		if e != nil {
			t.Fatal(e)
		}
		f.WriteString(strings.Repeat("x", n+1))
		f.Close()
		got, e := publishArchive(f.Name(), dir, "公司_会计师资料_2026-01")
		if e != nil || filepath.Base(got) != want {
			t.Fatalf("name %s: %v", got, e)
		}
	}
	b, e := os.ReadFile(filepath.Join(dir, "公司_会计师资料_2026-01.zip"))
	if e != nil || string(b) != "x" {
		t.Fatal("previous export overwritten")
	}
	used := map[string]bool{}
	doc := Record{"kind": "发票"}
	for _, tc := range []struct{ input, want string }{{"invoice.pdf", "invoice.pdf"}, {"INVOICE.pdf", "INVOICE（2）.pdf"}, {"待核验_发票_abcd1234.pdf", "待核验发票.pdf"}, {"待核验_发票_efgh5678.pdf", "待核验发票（2）.pdf"}} {
		if got := packagePDFName(doc, tc.input, used); got != tc.want {
			t.Fatalf("%s != %s", got, tc.want)
		}
	}
}

func TestExportReadableNamesAndWorkbookLinks(t *testing.T) {
	s := sampleLedger(t)
	path, e := s.export("2026-01", "2026-01", true, "测试公司")
	if e != nil {
		t.Fatal(e)
	}
	if filepath.Base(path) != "测试公司_會計師資料_2026-01.zip" {
		t.Fatal(path)
	}
	z, e := zip.OpenReader(path)
	if e != nil {
		t.Fatal(e)
	}
	defer z.Close()
	entries := map[string]bool{}
	for _, f := range z.File {
		entries[f.Name] = true
		if ext := strings.ToLower(filepath.Ext(f.Name)); ext != ".xlsx" && ext != ".pdf" {
			t.Fatalf("export contains non-XLSX/PDF material: %s", f.Name)
		}
		if strings.HasSuffix(f.Name, ".pdf") && rx(`^[a-f0-9]{8}_`).MatchString(filepath.Base(f.Name)) {
			t.Fatal("PDF still has hash prefix")
		}
	}
	links := 0
	for _, f := range z.File {
		if !strings.HasSuffix(f.Name, ".xlsx") {
			continue
		}
		if f.Name != "测试公司_賬務明細_2026-01.xlsx" {
			t.Fatal(f.Name)
		}
		r, e := f.Open()
		if e != nil {
			t.Fatal(e)
		}
		book, e := excelize.OpenReader(r)
		r.Close()
		if e != nil {
			t.Fatal(e)
		}
		if idx, _ := book.GetSheetIndex("帳戶資料"); idx >= 0 {
			t.Fatal("retired account details exported")
		}
		for _, sheet := range book.GetSheetList() {
			rows, e := book.GetRows(sheet)
			if e != nil {
				t.Fatal(e)
			}
			if len(rows) == 0 {
				continue
			}
			for col, label := range rows[0] {
				if !strings.Contains(label, "PDF") {
					continue
				}
				for row := 2; row <= len(rows); row++ {
					cell, _ := excelize.CoordinatesToCellName(col+1, row)
					ok, target, e := book.GetCellHyperLink(sheet, cell)
					if e != nil {
						t.Fatal(e)
					}
					if ok {
						links++
						if !entries[target] {
							t.Fatalf("broken PDF link: %s", target)
						}
					}
				}
			}
		}
		book.Close()
	}
	if links == 0 {
		t.Fatal("no workbook links found")
	}
}

func TestExportSelectedPlatformPDF(t *testing.T) {
	s := sampleLedger(t)
	path := duplicatePDF(t, "Platform balance summary")
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	sha := hashBytes(original)
	const name = "stripe-balance-summary.pdf"
	if added, err := s.importPDF(context.Background(), path, name, "平台结算"); err != nil || !added {
		t.Fatalf("archive platform report: added=%v err=%v", added, err)
	}
	if added, err := s.importPDF(context.Background(), path, name, "平台结算"); err != nil || added {
		t.Fatalf("archive duplicate: added=%v err=%v", added, err)
	}
	for _, selected := range []bool{false, true} {
		opts := Record{}
		if selected {
			opts["platform_pdf_shas"] = []string{sha}
		}
		exported, err := s.export("2026-01", "2026-01", true, "测试公司", opts)
		if err != nil {
			t.Fatal(err)
		}
		z, err := zip.OpenReader(exported)
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, entry := range z.File {
			if strings.HasPrefix(entry.Name, "平臺報告/") {
				b, err := fs.ReadFile(z, entry.Name)
				if err != nil || hashBytes(b) != sha {
					t.Errorf("export changed report bytes: %v", err)
				}
				found = true
			}
		}
		z.Close()
		if found != selected {
			t.Errorf("report included=%v, selected=%v", found, selected)
		}
	}
}
