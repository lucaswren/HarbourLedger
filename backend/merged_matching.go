package main

import "time"

// A consolidated payment pays several whole invoices in the bank's original currency.
func mergedPaymentPiece(t, i Record) bool {
	if t == nil || i == nil || isSale(i) || !accountMatch(i, t) || cents(i, "amount") <= 0 || cents(i, "amount") >= flowOriginal(t) {
		return false
	}
	part := clone(t)
	part["orig_amount"] = i["amount"]
	return automaticPaymentPair(part, i)
}

func validAutomaticMerged(d *Data, m Record) bool {
	if !yes(m, "auto_merged") {
		return false
	}
	var bank Record
	for _, t := range d.Tx {
		if str(t, "id") == str(m, "tx") {
			bank = t
		}
	}
	var total int64
	count := 0
	seen := map[string]bool{}
	vendor := ""
	for _, a := range d.Matches {
		if str(a, "tx") != str(m, "tx") || str(a, "status") != "已确认" {
			continue
		}
		var invoice Record
		for _, i := range d.Invoices {
			if str(i, "id") == str(a, "invoice") {
				invoice = i
			}
		}
		if !yes(a, "auto_merged") || !mergedPaymentPiece(bank, invoice) || seen[str(a, "invoice")] || cents(a, "amount") != cents(invoice, "amount") || str(a, "ccy") != str(invoice, "ccy") {
			return false
		}
		party := companyIdentity(str(invoice, "vendor"))
		if party == "" || (vendor != "" && vendor != party) {
			return false
		}
		vendor = party
		seen[str(a, "invoice")] = true
		total += cents(a, "amount")
		count++
	}
	return count >= 2 && count <= 4 && total == flowOriginal(bank)
}

func (s *Store) autoMatchMergedPayments() error {
	d, e := s.load()
	if e != nil {
		return e
	}
	for _, bank := range d.Tx {
		occupied := false
		for _, m := range d.Matches {
			if str(m, "tx") == str(bank, "id") && str(m, "status") == "已确认" {
				occupied = true
			}
		}
		if occupied || cents(bank, "wd") <= 0 {
			continue
		}
		pool := []Record{}
		for _, i := range d.Invoices {
			if !mergedPaymentPiece(bank, i) {
				continue
			}
			used := false
			for _, m := range d.Matches {
				if str(m, "invoice") == str(i, "id") && (str(m, "status") == "已确认" || (str(m, "tx") == str(bank, "id") && str(m, "status") == "不匹配")) {
					used = true
				}
			}
			if !used {
				pool = append(pool, i)
			}
		}
		if len(pool) > 16 {
			continue
		}
		solutions := [][]Record{}
		var find func(int, []Record, int64, string)
		find = func(pos int, chosen []Record, total int64, vendor string) {
			if len(solutions) > 1 {
				return
			}
			if total == flowOriginal(bank) && len(chosen) >= 2 {
				solutions = append(solutions, append([]Record{}, chosen...))
				return
			}
			if len(chosen) == 4 || total >= flowOriginal(bank) {
				return
			}
			for n := pos; n < len(pool); n++ {
				party := companyIdentity(str(pool[n], "vendor"))
				if vendor != "" && party != vendor {
					continue
				}
				find(n+1, append(chosen, pool[n]), total+cents(pool[n], "amount"), party)
			}
		}
		find(0, nil, 0, "")
		if len(solutions) != 1 {
			continue
		}
		group := solutions[0]
		blocked := false
		for _, i := range group {
			if s.checkEditable(str(bank, "date"), str(i, "date")) != nil {
				blocked = true
			}
		}
		for _, other := range d.Tx {
			if str(other, "id") == str(bank, "id") || flowOriginal(other) != flowOriginal(bank) || str(other, "orig_ccy") != str(bank, "orig_ccy") {
				continue
			}
			all := true
			for _, i := range group {
				if !mergedPaymentPiece(other, i) {
					all = false
				}
			}
			if all {
				blocked = true
			}
		}
		if blocked {
			continue
		}
		next := d
		next.Matches = make([]Record, 0, len(d.Matches)+len(group))
		for _, m := range d.Matches {
			next.Matches = append(next.Matches, clone(m))
		}
		planned := []Record{}
		for _, i := range group {
			var match Record
			for _, m := range next.Matches {
				if str(m, "tx") == str(bank, "id") && str(m, "invoice") == str(i, "id") {
					match = m
				}
			}
			if match == nil {
				match = Record{"id": "MERGED-" + hashBytes([]byte(str(bank, "id") + "|" + str(i, "id")))[:16]}
				next.Matches = append(next.Matches, match)
			}
			match["tx"], match["invoice"], match["ccy"], match["amount"] = bank["id"], i["id"], i["ccy"], i["amount"]
			match["status"], match["auto_confirmed"], match["auto_merged"], match["reviewer"] = "已确认", true, true, "系統自動匹配"
			match["basis"] = "同供應商、同收款賬號、同原幣，多張發票合計等於一筆銀行付款；唯一組合整組匹配"
			match["confirmed_at"] = time.Now().UTC().Format(time.RFC3339)
			planned = append(planned, match)
		}
		verdicts(&next)
		valid := true
		for _, m := range planned {
			if str(m, "validity") != "有效确认" {
				valid = false
			}
		}
		if !valid {
			continue
		}
		tx, err := s.db.Begin()
		if err != nil {
			return err
		}
		for _, m := range planned {
			if err = insertJSON(tx, "allocations", str(m, "id"), m); err != nil {
				tx.Rollback()
				return err
			}
		}
		if err = tx.Commit(); err != nil {
			return err
		}
		d = next
	}
	return nil
}
