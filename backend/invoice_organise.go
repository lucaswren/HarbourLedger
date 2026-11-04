package main

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// Enrich content metadata, preserving reviewed fields and stable identifiers.
func (s *Store) organiseInvoiceContents() error {
	d, e := s.load()
	if e != nil {
		return e
	}
	for _, doc := range d.Docs {
		kind := str(doc, "kind")
		if !invoiceKind(kind) {
			continue
		}
		sha := str(doc, "sha")
		path, e := s.documentPath(sha)
		if e != nil {
			return e
		}
		var raw string
		if e = s.db.QueryRow("SELECT meta FROM documents WHERE sha=?", sha).Scan(&raw); e != nil {
			return e
		}
		var m Record
		if e = json.Unmarshal([]byte(raw), &m); e != nil {
			return e
		}
		decision, e := s.detectInvoiceDirection(path, m)
		if e != nil {
			return e
		}
		if !yes(m, "verified") && str(decision, "seller") == "Google Asia Pacific Pte. Ltd." {
			parsed, err := parseInvoice(path, filepath.Base(path), sha)
			if err != nil {
				return err
			}
			if str(parsed, "currency_basis") != "" {
				if !yes(m, "currency_confirmed_by_user") {
					m["ccy"] = parsed["ccy"]
				}
				m["currency_basis"] = parsed["currency_basis"]
			}
			for _, key := range []string{"date", "number", "amount", "amount_basis", "document_type", "provider", "billing_id", "currency_basis"} {
				if v, ok := parsed[key]; ok {
					m[key] = v
				}
			}
		}
		for _, key := range []string{"seller", "buyer", "detected_direction", "direction_basis", "company_reference"} {
			m[key] = decision[key]
		}
		target := str(decision, "detected_direction")
		if !yes(m, "verified") && target == "销售发票" {
			parsed, err := parseInvoice(path, filepath.Base(path), sha)
			if err != nil {
				return err
			}
			if str(parsed, "currency_basis") != "" {
				m["ccy"] = parsed["ccy"]
				m["currency_basis"] = parsed["currency_basis"]
			}
			for _, key := range []string{"date", "number", "ccy", "amount", "amount_basis"} {
				if m[key] == nil || str(m, key) == "" || (key == "number" && str(m, key) == "Invoice") {
					if v, ok := parsed[key]; ok {
						m[key] = v
					}
				}
			}
			m["extraction_complete"] = validDate(str(m, "date")) && str(m, "number") != "" && str(m, "number") != "Invoice" && str(m, "ccy") != "" && cents(m, "amount") > 0 && str(decision, "buyer") != ""
		}
		linked := false
		for _, a := range d.Matches {
			if str(a, "invoice") == str(m, "id") && str(a, "status") == "已确认" {
				linked = true
			}
		}
		if target != "待分类发票" && target != kind && !yes(m, "verified") && !linked {
			id := str(m, "id")
			applyInvoiceDirection(m, decision, sha)
			m["id"] = id
			kind = target
			for _, a := range d.Matches {
				if str(a, "invoice") == id && str(a, "status") == "待确认" {
					a["status"] = "不匹配"
					a["note"] = str(a, "note") + "；發票方向已按內容更正，原候選待重新建立"
					if e = insertJSON(s.db, "allocations", str(a, "id"), a); e != nil {
						return e
					}
				}
			}
		} else if target != kind {
			m["direction_review_required"] = true
		} else if !yes(m, "verified") && !linked {
			m["direction_review_required"] = false
			if kind == "发票" && str(decision, "seller") != "" {
				m["vendor"] = decision["seller"]
			}
			if kind == "销售发票" && str(decision, "buyer") != "" {
				m["customer"] = decision["buyer"]
			}
		}
		if typ, err := detectPDFType(path); err == nil {
			annotatePDFType(m, typ)
		}
		b, e := os.ReadFile(path)
		if e != nil {
			return e
		}
		if e = s.saveCanonical(sha, kind, filepath.Base(path), m, b); e != nil {
			return e
		}
	}
	return nil
}
