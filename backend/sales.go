package main

import (
	"context"
	"fmt"
	"strings"
	"time"
)

func (s *Store) parseSalesInvoice(ctx context.Context, path, name, sha, pdfType string, cloud Record) (Record, error) {
	lines, pages, e := readPDF(path)
	if e != nil {
		return nil, e
	}
	m, e := parseInvoiceLines(lines, pages, name, sha)
	if e != nil {
		return nil, e
	}
	parts := []string{}
	for _, l := range lines {
		parts = append(parts, l.text(0, 10000))
	}
	text := strings.Join(parts, "\n")
	customer := capture(`(?im)(?:Bill\s*To|Sold\s*To|Customer(?:\s*Name)?|客戶名稱|客户名称|買方|买方)\s*[:：]?\s*([^\n]+)`, text)
	if cloud != nil {
		m = cloud
		if str(cloud, "customer") != "" {
			customer = str(cloud, "customer")
		}
	}
	m["id"] = "SALE-" + sha[:12]
	m["kind"] = "销售发票"
	m["origin"] = "imported"
	m["customer"] = strings.TrimSpace(customer)
	m["vendor"] = m["customer"]
	m["party"] = m["customer"]
	m["accounts"] = []string{}
	m["verified"] = false
	m["status"] = "待核验"
	m["verification_source"] = ""
	m["method"] = "销售发票内容识别，待人工核验"
	if str(m, "recognition_provider") == "" {
		m["recognition_provider"] = "local"
	}
	annotatePDFType(m, pdfType)
	m["due_date"] = parseDate(capture(`(?im)(?:Due\s*Date|到期日期)\s*[:：]?\s*([^\n]+)`, text))
	m["extraction_complete"] = validDate(str(m, "date")) && customer != "" && str(m, "number") != "" && cents(m, "amount") > 0 && str(m, "ccy") != ""
	return m, nil
}
func markSalesDuplicates(sales []Record) {
	copies := []Record{}
	for _, s := range sales {
		m := clone(s)
		m["vendor"] = str(s, "customer")
		copies = append(copies, m)
	}
	markDuplicates(copies)
	byID := map[string]Record{}
	for _, m := range copies {
		byID[str(m, "id")] = m
	}
	for _, s := range sales {
		s["duplicate"] = str(byID[str(s, "id")], "duplicate")
	}
}
func receiptEvidence(i, t Record) bool {
	if strings.Contains(strings.ToUpper(str(t, "details")), "STRIPE") {
		return false
	}
	customer := strings.ToUpper(strings.Join(strings.Fields(str(i, "customer")), ""))
	details := strings.ToUpper(strings.Join(strings.Fields(str(t, "details")), ""))
	return len(customer) >= 4 && strings.Contains(details, customer)
}
func (s *Store) refreshSalesCandidates() error {
	d, e := s.load()
	if e != nil {
		return e
	}
	existing := map[string]bool{}
	for _, m := range d.Matches {
		existing[str(m, "tx")+"|"+str(m, "invoice")] = true
	}
	tx, e := s.db.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()
	for _, i := range d.Sales {
		if str(i, "status") == "草稿" || str(i, "duplicate") != "" || !validDate(str(i, "date")) || cents(i, "amount") <= 0 {
			continue
		}
		date, _ := time.Parse("2006-01-02", str(i, "date"))
		for _, t := range d.Tx {
			td, _ := time.Parse("2006-01-02", str(t, "date"))
			days := td.Sub(date).Hours() / 24
			key := str(t, "id") + "|" + str(i, "id")
			if existing[key] || cents(t, "dep") <= 0 || strings.HasPrefix(str(t, "category"), "内部") || str(t, "category") == "董事往来" || str(t, "category") == "银行利息" || days < -120 || days > 120 || str(t, "orig_ccy") != str(i, "ccy") || flowOriginal(t) != cents(i, "amount") {
				continue
			}
			note := "同币同额仅为候选，请补付款方/收款通知后人工确认"
			if strings.Contains(strings.ToUpper(str(t, "details")), "STRIPE") {
				note = "Stripe净到账不能直接认定销售额；需销售订单、结算、手续费及退款明细"
			}
			m := Record{"id": "RCPT-" + hashBytes([]byte(key))[:16], "tx": str(t, "id"), "invoice": str(i, "id"), "direction": "收款", "ccy": str(i, "ccy"), "amount": num(i, "amount"), "status": "待确认", "basis": "销售发票同原币同额", "note": note, "reviewer": "", "evidence": ""}
			if e = insertJSON(tx, "allocations", str(m, "id"), m); e != nil {
				return e
			}
		}
	}
	return tx.Commit()
}

// This is an invoice settlement schedule, not a general-ledger balance.
func settlementSchedule(d Data, asOf string) []Record {
	out := []Record{}
	date, _ := time.Parse("2006-01-02", asOf)
	txs := map[string]Record{}
	for _, t := range d.Tx {
		txs[str(t, "id")] = t
	}
	for _, i := range append(append([]Record{}, d.Invoices...), d.Sales...) {
		if str(i, "duplicate") != "" || str(i, "status") == "草稿" {
			continue
		}
		r := clone(i)
		r["as_of"] = asOf
		r["direction"] = "应付"
		r["counterparty"] = str(i, "vendor")
		if isSale(i) {
			r["direction"] = "应收"
			r["counterparty"] = str(i, "customer")
		}
		paid := int64(0)
		for _, m := range d.Matches {
			if str(m, "invoice") == str(i, "id") && str(m, "validity") == "有效确认" && validDate(str(txs[str(m, "tx")], "date")) && str(txs[str(m, "tx")], "date") <= asOf {
				paid += cents(m, "amount")
			}
		}
		if !validDate(str(i, "date")) || i["amount"] == nil {
			r["settlement_state"] = "字段待补全"
			r["remaining_amount"] = nil
			out = append(out, r)
			continue
		}
		if str(i, "date") > asOf {
			continue
		}
		r["settled_amount"] = float64(paid) / 100
		r["remaining_amount"] = float64(cents(i, "amount")-paid) / 100
		r["settlement_state"] = "未关联"
		if paid > 0 {
			r["settlement_state"] = "部分关联"
		}
		if paid == cents(i, "amount") {
			r["settlement_state"] = "已结清"
		}
		if !yes(i, "verified") {
			r["settlement_state"] = "待核验（不作为确认余额）"
		}
		issued, _ := time.Parse("2006-01-02", str(i, "date"))
		days := int(date.Sub(issued).Hours() / 24)
		r["age_days"] = days
		r["age_basis"] = "开票日"
		r["overdue_days"] = nil
		if validDate(str(i, "due_date")) {
			due, _ := time.Parse("2006-01-02", str(i, "due_date"))
			overdue := int(date.Sub(due).Hours() / 24)
			if overdue < 0 {
				overdue = 0
			}
			r["overdue_days"] = overdue
		}
		bucket := "0–30天"
		if days > 90 {
			bucket = "90天以上"
		} else if days > 60 {
			bucket = "61–90天"
		} else if days > 30 {
			bucket = "31–60天"
		}
		r["age_bucket"] = bucket
		out = append(out, r)
	}
	return out
}
func validReportSettings(v Record) error {
	start, end, after := str(v, "report_start"), str(v, "report_end"), str(v, "subsequent_end")
	if start == "" && end == "" && after == "" {
		return nil
	}
	if !validDate(start) || !validDate(end) || start > end {
		return fmt.Errorf("请填写有效且顺序正确的会计年度起止日期")
	}
	if after != "" && (!validDate(after) || after < end) {
		return fmt.Errorf("期后核查截止日不得早于财报截止日")
	}
	return nil
}
