package main

import (
	"fmt"
	"os"
	"strings"
)

func parseCreditAdvice(path, name, sha string) (Record, error) {
	lines, _, e := readPDF(path)
	if e != nil {
		return nil, e
	}
	full, left := []string{}, []string{}
	for _, l := range lines {
		full = append(full, l.text(0, 10000))
		left = append(left, l.text(0, 260))
	}
	return creditAdviceFields(strings.Join(full, "\n"), strings.Join(left, "\n"), sha)
}

func creditAdviceFields(text, left, sha string) (Record, error) {
	if !rx(`(?i)Credit\s+Advice`).MatchString(text) {
		return nil, fmt.Errorf("未識別為銀行收款通知，請核對文件類型")
	}
	ref := capture(`(?i)Transaction\s+reference\s*:\s*([A-Z0-9]+)`, text)
	date := parseDate(capture(`(?i)Transaction\s+date\s*:\s*(\d{1,2}\s+[A-Za-z]+\s+20\d{2})`, text))
	amount := func(label string) (string, float64) {
		m := rx(`(?i)` + label + `\s*:\s*(USD|HKD)\s*([\d,]+\.\d{2})`).FindStringSubmatch(text)
		if len(m) < 3 {
			return "", 0
		}
		v, _ := money(strings.ReplaceAll(m[2], ",", ""))
		return strings.ToUpper(m[1]), float64(v) / 100
	}
	ccy, gross := amount(`Payment\s+amount`)
	netCCY, net := amount(`Amount\s+credited`)
	feeCCY, fees := amount(`Total\s+charges`)
	localCCY, localFee := amount(`Local\s+charges`)
	payer := strings.TrimSpace(capture(`(?im)By\s+order\s+of\s*:\s*([^\n]+)`, left))
	account := capture(`(?i)Credited\s+account\s*:\s*(\d{3}-\d{6}-\d{3})`, text)
	if ref == "" || !validDate(date) || len(payer) < 4 || strings.EqualFold(payer, "Bank") || account == "" || gross <= 0 || net <= 0 || ccy != netCCY || ccy != feeCCY {
		return nil, fmt.Errorf("收款通知資料不完整或涉及尚未支援的跨幣結算，請核對原件")
	}
	m := Record{"id": "ADVICE-" + sha[:12], "kind": "银行通知", "document_type": "credit_advice", "date": date, "number": ref, "vendor": payer, "payer": payer, "account": account, "ccy": ccy, "amount": gross, "gross": gross, "fees": fees, "net": net, "local_fee_ccy": localCCY, "local_fee": localFee, "recognition_provider": "local", "recognition_method": "銀行收款通知文字本地解析"}
	if cents(m, "gross")-cents(m, "fees") != cents(m, "net") {
		return nil, fmt.Errorf("收款通知原款減費用不等於淨入帳，停止自動核對")
	}
	if rx(`(?i)PARTNER\s+COMMISSION`).MatchString(text) {
		m["purpose"] = "合作佣金"
	}
	return m, nil
}

func (s *Store) organiseBankAdviceContents() error {
	d, e := s.load()
	if e != nil {
		return e
	}
	for _, doc := range d.Docs {
		if str(doc, "kind") != "银行通知" {
			continue
		}
		sha := str(doc, "sha")
		path, e := s.documentPath(sha)
		if e != nil {
			return e
		}
		m, e := parseCreditAdvice(path, str(doc, "name"), sha)
		if e != nil {
			return e
		}
		b, e := os.ReadFile(path)
		if e != nil {
			return e
		}
		if e = s.saveCanonical(sha, "银行通知", str(doc, "name"), m, b); e != nil {
			return e
		}
	}
	return nil
}

func applyCreditAdvices(d *Data) {
	for _, a := range d.Advices {
		candidates := []Record{}
		for _, t := range d.Tx {
			if str(t, "date") == str(a, "date") && str(t, "ccy") == str(a, "ccy") && cents(t, "dep") == cents(a, "net") && accountKey(str(t, "bank_number")) == accountKey(str(a, "account")) {
				candidates = append(candidates, t)
			}
		}
		if len(candidates) != 1 {
			continue
		}
		t := candidates[0]
		t["party"] = a["payer"]
		t["advice_sha"] = a["document_sha"]
		t["advice_gross"], t["advice_fees"], t["advice_net"] = a["gross"], a["fees"], a["net"]
		if str(a, "purpose") == "合作佣金" {
			t["advice_purpose"] = "合作佣金"
			if str(t, "manual_category") == "" {
				t["category"] = "推薦佣金收入"
			}
		}
	}
}
