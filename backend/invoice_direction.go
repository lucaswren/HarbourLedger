package main

import (
	"golang.org/x/text/unicode/norm"
	"strings"
	"unicode"
)

func invoiceKind(kind string) bool {
	return kind == "发票" || kind == "销售发票" || kind == "自动发票" || kind == "待分类发票"
}
func companyIdentity(s string) string {
	s = strings.ToUpper(norm.NFKC.String(s))
	return strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return r
		}
		return -1
	}, s)
}
func (s *Store) detectInvoiceDirection(path string, m Record) (Record, error) {
	cfg, e := s.settings(nil)
	if e != nil {
		return nil, e
	}
	company := str(cfg, "company")
	own := func(name string) bool {
		key := companyIdentity(name)
		if key == "" {
			return false
		}
		for _, alias := range companyNames(cfg) {
			if key == companyIdentity(alias) {
				return true
			}
		}
		return false
	}
	seller, buyer := str(m, "seller"), str(m, "customer")
	if seller == "" {
		seller = str(m, "vendor")
	}
	lines, _, e := readPDF(path)
	if e != nil {
		return nil, e
	}
	ss := []string{}
	for _, line := range lines {
		ss = append(ss, norm.NFKC.String(line.text(0, 10000)))
	}
	text := strings.Join(ss, "\n")
	// Two-column invoices put the issuer and customer on the same text row.
	columnSeller, columnBuyer := "", ""
	for n, line := range lines {
		if !rx(`(?i)Billed\s+to`).MatchString(line.text(0, 10000)) {
			continue
		}
		for _, w := range line.words {
			if !strings.EqualFold(w.s, "Billed") {
				continue
			}
			for k := n + 1; k < len(lines) && k <= n+12; k++ {
				left, right := strings.TrimSpace(lines[k].text(0, w.x)), strings.TrimSpace(lines[k].text(w.x, 10000))
				if rx(`(?i)Invoice\s+Details|發票明細|发票明细`).MatchString(right) {
					break
				}
				if own(left) {
					columnSeller = left
				}
				if columnBuyer == "" && right != "" {
					columnBuyer = right
				}
				if columnSeller != "" && columnBuyer != "" {
					break
				}
			}
		}
	}
	party := func(pattern string) string { return strings.TrimSpace(capture(pattern, text)) }
	if v := party(`(?im)(?:Bill\s*To|Sold\s*To|Buyer(?:\s*Name)?|Customer(?:\s*Name)?|客戶名稱|客户名称|買方(?:名稱)?|买方(?:名称)?|購買方(?:名稱)?|购买方(?:名称)?)\s*[:：]?\s*([^\n]+)`); v != "" {
		buyer = v
		// Explicit buyer content replaces seller guesses from earlier parsing.
		if own(seller) {
			seller = ""
		}
	}
	if v := party(`(?im)(?:Seller(?:\s*Name)?|Supplier(?:\s*Name)?|Vendor(?:\s*Name)?|銷售方(?:名稱)?|销售方(?:名称)?|供應商(?:名稱)?|供应商(?:名称)?|開票方(?:名稱)?|开票方(?:名称)?)\s*[:：]\s*([^\n]+)`); v != "" {
		seller = v
	} else {
		for n, line := range ss {
			if n > 14 || rx(`(?i)Bill\s*To|Sold\s*To|Customer|Buyer|客戶|客户|買方|买方|購買方|购买方`).MatchString(line) {
				break
			}
			if rx(`(?i)有限公司|\bLIMITED\b|\bLTD\.?$`).MatchString(line) {
				seller = strings.TrimSpace(line)
				break
			}
		}
	}
	if seller == "" && own(buyer) {
		seller = party(`(?im)Beneficiary['’]?s\s*Name\s*[:：]\s*([^\n]+)`)
	}
	if receipt := googleReceiptFields(text); receipt != nil {
		seller = str(receipt, "vendor")
		m["billing_id"] = receipt["billing_id"]
		if e := s.applyConfirmedGoogleCurrency(m); e != nil {
			return nil, e
		}
		for _, line := range ss {
			if own(strings.TrimSpace(line)) {
				buyer = strings.TrimSpace(line)
				break
			}
		}
	}
	if columnSeller != "" && columnBuyer != "" {
		seller, buyer = columnSeller, columnBuyer
	}
	direction, basis := "待分类发票", "未能確認本公司在發票中的交易角色，請核對開票方及買方"
	switch {
	case own(seller) && !own(buyer):
		direction = "销售发票"
		basis = "開票方與本公司名稱或已設定別名一致"
	case own(buyer) && !own(seller):
		direction = "发票"
		basis = "買方與本公司名稱或已設定別名一致"
	case own(seller) && own(buyer):
		basis = "開票方與買方均指向本公司，請核對是否為內部文件"
	}
	return Record{"seller": seller, "buyer": buyer, "detected_direction": direction, "direction_basis": basis, "company_reference": company, "is_draft": rx(`(?i)\bDRAFT\b|草稿`).MatchString(text)}, nil
}
func applyInvoiceDirection(m, decision Record, sha string) {
	for _, k := range []string{"seller", "buyer", "detected_direction", "direction_basis", "company_reference"} {
		m[k] = decision[k]
	}
	m["customer"] = decision["buyer"]
	kind := str(decision, "detected_direction")
	m["kind"] = kind
	m["direction_review_required"] = kind == "待分类发票"
	m["id"] = "INV-" + sha[:12]
	if kind == "销售发票" {
		m["id"] = "SALE-" + sha[:12]
		m["customer"] = decision["buyer"]
		m["party"] = m["customer"]
		m["vendor"] = m["customer"]
		m["accounts"] = []string{}
	} else {
		m["vendor"] = decision["seller"]
	}
	if yes(decision, "is_draft") {
		m["status"] = "草稿"
		m["verified"] = false
	}
	m["origin"] = "imported"
}
