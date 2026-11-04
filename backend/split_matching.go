package main

import "time"

func splitPaymentPiece(t, i Record) bool {
	if t == nil || i == nil || isSale(i) || str(i, "duplicate") != "" || yes(i, "direction_review_required") || str(i, "status") == "草稿" || (!yes(i, "verified") && !yes(i, "extraction_complete")) || !accountMatch(i, t) || str(t, "orig_ccy") != str(i, "ccy") || cents(t, "wd") <= 0 || flowOriginal(t) <= 0 || flowOriginal(t) >= cents(i, "amount") {
		return false
	}
	a, e := time.Parse("2006-01-02", str(t, "date"))
	b, f := time.Parse("2006-01-02", str(i, "date"))
	return e == nil && f == nil && a.Sub(b).Hours() >= -120*24 && a.Sub(b).Hours() <= 120*24
}

// A split is valid only as a complete set; every bank amount remains unchanged.
func validAutomaticSplit(d *Data, m Record) bool {
	if !yes(m, "auto_split") {
		return false
	}
	var invoice Record
	for _, i := range d.Invoices {
		if str(i, "id") == str(m, "invoice") {
			invoice = i
		}
	}
	var total int64
	count := 0
	seen := map[string]bool{}
	for _, a := range d.Matches {
		if str(a, "invoice") != str(m, "invoice") || str(a, "status") != "已确认" {
			continue
		}
		var bank Record
		for _, t := range d.Tx {
			if str(t, "id") == str(a, "tx") {
				bank = t
			}
		}
		if !yes(a, "auto_split") || !splitPaymentPiece(bank, invoice) || seen[str(a, "tx")] || cents(a, "amount") != flowOriginal(bank) || str(a, "ccy") != str(invoice, "ccy") {
			return false
		}
		seen[str(a, "tx")] = true
		total += cents(a, "amount")
		count++
	}
	return count >= 2 && count <= 4 && total == cents(invoice, "amount")
}

func (s *Store) autoMatchSplitPayments() error {
	d, e := s.load()
	if e != nil {
		return e
	}
	for _, i := range d.Invoices {
		group := []Record{}
		var total int64
		used := map[string]bool{}
		for _, m := range d.Matches {
			if str(m, "invoice") != str(i, "id") || str(m, "status") != "待确认" {
				continue
			}
			for _, t := range d.Tx {
				if str(t, "id") == str(m, "tx") && splitPaymentPiece(t, i) && cents(m, "amount") == flowOriginal(t) && !used[str(t, "id")] {
					group = append(group, m)
					total += cents(m, "amount")
					used[str(t, "id")] = true
				}
			}
		}
		if len(group) < 2 || len(group) > 4 || total != cents(i, "amount") {
			continue
		}
		blocked := false
		for _, m := range d.Matches {
			if str(m, "status") == "已确认" && (used[str(m, "tx")] || str(m, "invoice") == str(i, "id")) {
				blocked = true
			}
			if used[str(m, "tx")] && str(m, "invoice") != str(i, "id") && str(m, "status") != "不匹配" {
				blocked = true
			}
		}
		for _, t := range d.Tx {
			if used[str(t, "id")] {
				if s.checkEditable(str(t, "date"), str(i, "date")) != nil {
					blocked = true
				}
			}
		}
		if blocked {
			continue
		}
		for _, m := range group {
			m["status"] = "已确认"
			m["auto_confirmed"] = true
			m["auto_split"] = true
			m["reviewer"] = "系統自動匹配"
			m["basis"] = "同收款賬號、同原幣，數筆付款合計等於一張發票；整組核對"
			m["confirmed_at"] = time.Now().UTC().Format(time.RFC3339)
		}
		verdicts(&d)
		for _, m := range group {
			if str(m, "validity") != "有效确认" {
				return nil
			}
		}
		tx, err := s.db.Begin()
		if err != nil {
			return err
		}
		for _, m := range group {
			if err = insertJSON(tx, "allocations", str(m, "id"), m); err != nil {
				tx.Rollback()
				return err
			}
		}
		if err = tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}
