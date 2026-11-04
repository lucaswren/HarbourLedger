package main

import (
	"encoding/json"
	"strings"
	"time"
)

// New imports can make an earlier unique link ambiguous. Reopen those links
// together before refreshing; one stale link must not block every other match.
func (s *Store) reopenChangedAutomaticLinks() error {
	d, e := s.load()
	if e != nil {
		return e
	}
	changed := []Record{}
	for _, m := range d.Matches {
		if yes(m, "auto_confirmed") && str(m, "status") == "已确认" && str(m, "validity") != "有效确认" {
			next := clone(m)
			next["previous_confirmation"] = clone(m)
			next["status"] = "待确认"
			next["auto_confirmed"] = false
			next["reviewer"] = ""
			next["note"] = "新增資料令自動關聯條件改變，請選擇對應票據"
			changed = append(changed, next)
		}
	}
	if len(changed) == 0 {
		return nil
	}
	if _, e = s.backup(); e != nil {
		return e
	}
	tx, e := s.db.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()
	for _, m := range changed {
		b, err := json.Marshal(m)
		if err != nil {
			return err
		}
		if _, e = tx.Exec("UPDATE allocations SET meta=? WHERE id=?", string(b), str(m, "id")); e != nil {
			return e
		}
	}
	return tx.Commit()
}

func automaticPaymentPair(t, i Record) bool {
	if isSale(i) {
		return automaticReceiptPair(t, i)
	}
	if t == nil || i == nil || isSale(i) || str(i, "vendor") == "" || str(i, "number") == "" || str(i, "duplicate") != "" || yes(i, "direction_review_required") || str(i, "status") == "草稿" {
		return false
	}
	if automaticFXAssociation(t, i) {
		return true
	}
	if cents(t, "wd") <= 0 || str(t, "orig_ccy") != str(i, "ccy") || flowOriginal(t) != cents(i, "amount") || cents(i, "amount") <= 0 {
		return false
	}
	serviceEvidence(t)
	if str(t, "gap_type") != "待補採購發票" {
		return false
	}
	a, ea := time.Parse("2006-01-02", str(t, "date"))
	b, eb := time.Parse("2006-01-02", str(i, "date"))
	if ea != nil || eb != nil {
		return false
	}
	if str(i, "provider") == "google" && str(i, "document_type") == "payment_receipt" && yes(i, "currency_confirmed_by_user") {
		return strings.Contains(strings.ToUpper(str(t, "details")), "GOOGLE") && a.Sub(b).Hours() >= 0 && a.Sub(b).Hours() <= 7*24
	}
	return a.Sub(b).Hours() >= -120*24 && a.Sub(b).Hours() <= 120*24
}

func (s *Store) autoMatchExactPayments() error {
	cfg, e := s.settings(nil)
	if e != nil || str(cfg, "auto_match_exact") != "true" {
		return e
	}
	d, e := s.load()
	if e != nil {
		return e
	}
	for _, m := range d.Matches {
		if str(m, "status") != "待确认" {
			continue
		}
		var t, i Record
		for _, row := range d.Tx {
			if str(row, "id") == str(m, "tx") {
				t = row
			}
		}
		for _, row := range append(append([]Record{}, d.Invoices...), d.Sales...) {
			if str(row, "id") == str(m, "invoice") {
				i = row
			}
		}
		if !automaticCandidatePair(&d, t, i) {
			continue
		}
		// Require a unique pair in both directions, regardless of candidate ranking.
		nInvoice, nBank := 0, 0
		for _, row := range append(append([]Record{}, d.Invoices...), d.Sales...) {
			if automaticCandidatePair(&d, t, row) {
				nInvoice++
			}
		}
		for _, row := range d.Tx {
			if automaticCandidatePair(&d, row, i) {
				nBank++
			}
		}
		if nInvoice != 1 || nBank != 1 {
			continue
		}
		occupied := false
		for _, old := range d.Matches {
			if str(old, "id") != str(m, "id") && str(old, "status") == "已确认" && (str(old, "tx") == str(m, "tx") || str(old, "invoice") == str(m, "invoice")) {
				occupied = true
			}
		}
		if occupied {
			continue
		}
		if e = s.checkEditable(str(t, "date"), str(i, "date")); e != nil {
			continue
		}
		next := clone(m)
		next["auto_confirmed"] = true
		next["status"] = "已确认"
		next["reviewer"] = "系統自動匹配"
		next["basis"] = "採購付款同幣同額、日期範圍內雙向唯一；系統自動匹配"
		if isSale(i) {
			next["basis"] = "銷售收款同幣同額、日期範圍內雙向唯一；系統自動匹配"
			next["note"] = "按金額自動關聯；銀行未列付款方時，不推定銀行已核實客戶身份"
		}
		if automaticFXAssociation(t, i) {
			next["basis"] = "收款賬號一致、45日內雙向唯一；跨幣種自動關聯"
			next["note"] = "銀行未列原幣金額；僅確認票據歸屬，不推算匯率或宣稱金額相等"
		}
		if str(i, "provider") == "google" {
			next["basis"] = "Google商戶、已確認結算幣種、原幣同額、7日內扣款、雙向唯一；系統自動匹配"
			next["note"] = "Google結算帳戶幣種已由用戶確認"
		}
		next["confirmed_at"] = time.Now().UTC().Format(time.RFC3339)
		if e = s.saveMatch(next); e != nil {
			return e
		}
		m["status"] = "已确认"
	}
	if e = s.autoMatchSplitPayments(); e != nil {
		return e
	}
	return s.autoMatchMergedPayments()
}

func automaticReceiptPair(t, i Record) bool {
	if t == nil || i == nil || !isSale(i) || str(i, "number") == "" || str(i, "customer") == "" || str(i, "duplicate") != "" || yes(i, "direction_review_required") || str(i, "status") == "草稿" || (!yes(i, "verified") && !yes(i, "extraction_complete")) || cents(t, "dep") <= 0 || str(t, "orig_ccy") != str(i, "ccy") || flowOriginal(t) != cents(i, "amount") || (!receiptEvidence(i, t) && !anonymousReceipt(t)) {
		return false
	}
	a, e := time.Parse("2006-01-02", str(t, "date"))
	b, f := time.Parse("2006-01-02", str(i, "date"))
	return e == nil && f == nil && a.Sub(b).Hours() >= -120*24 && a.Sub(b).Hours() <= 120*24
}

// Amount-based links are operational associations, not assertions of payer identity.
func anonymousReceipt(t Record) bool {
	details := strings.ToUpper(strings.ReplaceAll(str(t, "details"), " ", ""))
	category := str(t, "category")
	return !yes(t, "transfer_verified") && !strings.Contains(category, "董事") && !strings.Contains(category, "内部") && !strings.Contains(category, "內部") && (details == "DEPOSIT" || details == "B/FBALANCEDEPOSIT")
}

func automaticFXAssociation(t, i Record) bool {
	if t == nil || i == nil || isSale(i) || str(i, "duplicate") != "" || yes(i, "direction_review_required") || str(i, "status") == "草稿" || str(i, "number") == "" || str(i, "vendor") == "" || cents(i, "amount") <= 0 || cents(t, "wd") <= 0 || yes(t, "transfer_verified") || str(t, "orig_ccy") != "" || t["orig_amount"] != nil || str(t, "ccy") == str(i, "ccy") || !accountMatch(i, t) {
		return false
	}
	category := str(t, "category")
	if strings.Contains(category, "董事") || strings.Contains(category, "内部") || strings.Contains(category, "內部") {
		return false
	}
	a, e := time.Parse("2006-01-02", str(t, "date"))
	b, f := time.Parse("2006-01-02", str(i, "date"))
	return e == nil && f == nil && a.Sub(b).Hours() >= -45*24 && a.Sub(b).Hours() <= 45*24
}

func validAutomaticExact(d *Data, m Record) bool {
	var t, i Record
	for _, r := range d.Tx {
		if str(r, "id") == str(m, "tx") {
			t = r
		}
	}
	invoices := append(append([]Record{}, d.Invoices...), d.Sales...)
	for _, r := range invoices {
		if str(r, "id") == str(m, "invoice") {
			i = r
		}
	}
	if !automaticCandidatePair(d, t, i) || str(m, "ccy") != str(i, "ccy") || cents(m, "amount") != cents(i, "amount") {
		return false
	}
	nInvoice, nBank := 0, 0
	for _, r := range invoices {
		if automaticCandidatePair(d, t, r) {
			nInvoice++
		}
	}
	for _, r := range d.Tx {
		if automaticCandidatePair(d, r, i) {
			nBank++
		}
	}
	return nInvoice == 1 && nBank == 1
}

// Exact original-currency payments take precedence over account/date-only FX links.
func automaticCandidatePair(d *Data, t, i Record) bool {
	if !automaticPaymentPair(t, i) {
		return false
	}
	if !automaticFXAssociation(t, i) {
		return true
	}
	for _, bank := range d.Tx {
		if !automaticFXAssociation(bank, i) && automaticPaymentPair(bank, i) {
			return false
		}
	}
	for _, m := range d.Matches {
		if str(m, "status") != "已确认" {
			continue
		}
		if str(m, "invoice") == str(i, "id") && str(m, "tx") != str(t, "id") && (!yes(m, "auto_confirmed") || yes(m, "auto_split") || yes(m, "auto_merged")) {
			return false
		}
	}
	return true
}
