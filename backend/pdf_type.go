package main

import (
	"fmt"
	pdf "github.com/ledongthuc/pdf"
	"strings"
	"unicode"
)

// Classify pages, not filenames. Blank/invalid pages alone do not authorize OCR.
func detectPDFType(path string) (kind string, err error) {
	defer func() {
		if recover() != nil {
			kind = ""
			err = fmt.Errorf("PDF类型检测失败，请检查原件")
		}
	}()
	lines, pages, e := readPDF(path)
	if e != nil {
		return "", e
	}
	counts := map[int]int{}
	for _, l := range lines {
		for _, c := range l.text(0, 10000) {
			if unicode.IsLetter(c) || unicode.IsDigit(c) {
				counts[l.page]++
			}
		}
	}
	f, r, e := pdf.Open(path)
	if e != nil {
		return "", e
	}
	defer f.Close()
	var hasImage func(pdf.Value, int) bool
	hasImage = func(resources pdf.Value, depth int) bool {
		if depth > 8 {
			return false
		}
		xs := resources.Key("XObject")
		for _, key := range xs.Keys() {
			v := xs.Key(key)
			if v.Key("Subtype").Name() == "Image" && v.Key("Width").Int64() >= 300 && v.Key("Height").Int64() >= 300 {
				return true
			}
			if v.Key("Subtype").Name() == "Form" && hasImage(v.Key("Resources"), depth+1) {
				return true
			}
		}
		return false
	}
	textPages, scanPages := 0, 0
	for p := 1; p <= pages; p++ {
		if counts[p] >= 20 {
			textPages++
		} else if hasImage(r.Page(p).Resources(), 0) {
			scanPages++
		} else if counts[p] > 0 {
			textPages++
		}
	}
	if scanPages > 0 {
		if textPages > 0 {
			return "mixed", nil
		}
		return "scanned", nil
	}
	if textPages > 0 {
		return "text", nil
	}
	return "unknown", nil
}
func needsCloudPDF(kind string) bool { return kind == "scanned" || kind == "mixed" }
func pdfTypeLabel(kind string) string {
	switch kind {
	case "text":
		return "文字型"
	case "scanned":
		return "扫描型"
	case "mixed":
		return "混合型（含扫描页）"
	}
	return "未检测到可用文字或扫描图像"
}

// Kept small so import and rebuild share exactly the same routing decision.
func annotatePDFType(m Record, kind string) {
	if m != nil {
		m["pdf_type"] = kind
		m["pdf_type_label"] = pdfTypeLabel(kind)
		if !needsCloudPDF(kind) {
			delete(m, "ocr_fallback_reason")
			delete(m, "ocr_request_id")
			delete(m, "ocr_request_ids")
			if strings.Contains(str(m, "recognition_provider"), "tencent") {
				m["recognition_provider"] = "local"
			}
		}
	}
}
