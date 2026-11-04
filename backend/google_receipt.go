package main

import (
	"strings"
	"time"
)

func googleReceiptFields(text string) Record {
	if !rx(`(?i)Payment\s+Receipt`).MatchString(text) || !rx(`(?i)Google\s+Asia\s+Pacific\s+Pte\.?\s+Ltd\.?`).MatchString(text) {
		return nil
	}
	m := Record{"vendor": "Google Asia Pacific Pte. Ltd.", "document_type": "payment_receipt", "provider": "google", "number": capture(`(?i)Payment\s+number\s*[:：]?\s*([A-Z0-9]+)`, text), "amount_basis": "Payment amount（付款收據）", "currency_basis": "原件只列$，未明示USD/HKD；需核對Google賬戶結算幣種"}
	m["billing_id"] = capture(`\b([0-9]{4}-[0-9]{4}-[0-9]{4})\b`, text)
	date := capture(`(?i)Payment\s+date\s*[:：]?\s*([A-Za-z]+\s+\d{1,2},?\s+20\d{2})`, text)
	for _, layout := range []string{"Jan 2, 2006", "January 2, 2006", "Jan 2 2006"} {
		if t, e := time.Parse(layout, date); e == nil {
			m["date"] = t.Format("2006-01-02")
			break
		}
	}
	amount := capture(`(?i)Payment\s+amount\s*[:：]?\s*\$\s*([\d,]+\.\d{2})`, text)
	if v, e := money(strings.ReplaceAll(amount, ",", "")); e == nil && v > 0 {
		m["amount"] = float64(v) / 100
	}
	return m
}

// Currency is confirmed per billing account, never inferred from a dollar sign.
func (s *Store) applyConfirmedGoogleCurrency(m Record) error {
	if str(m, "provider") != "google" || str(m, "billing_id") == "" {
		return nil
	}
	cfg, e := s.settings(nil)
	if e != nil {
		return e
	}
	if str(cfg, "google_billing_id") != str(m, "billing_id") {
		return nil
	}
	ccy := str(cfg, "google_billing_currency")
	if ccy != "USD" && ccy != "HKD" {
		return nil
	}
	if str(m, "ccy") != "" && str(m, "ccy") != ccy {
		return nil
	}
	m["ccy"] = ccy
	m["currency_confirmed_by_user"] = true
	m["currency_basis"] = "用戶已確認此Google結算帳戶幣種：" + ccy
	m["extraction_complete"] = validDate(str(m, "date")) && str(m, "number") != "" && cents(m, "amount") > 0 && str(m, "vendor") != ""
	return nil
}
