package main

import (
	"encoding/json"
	"github.com/google/uuid"
	"strings"
	"time"
)

func (s *Store) refreshCandidates() error {
	if e := s.reopenChangedAutomaticLinks(); e != nil {
		return e
	}
	d, e := s.load()
	if e != nil {
		return e
	}
	existing := map[string]bool{}
	has := map[string]bool{}
	for _, m := range d.Matches {
		existing[str(m, "tx")+"|"+str(m, "invoice")] = true
		has[str(m, "invoice")] = true
	}
	pending := []Record{}
	add := func(t, i Record, value float64, basis, note string) {
		key := str(t, "id") + "|" + str(i, "id")
		if existing[key] {
			return
		}
		existing[key] = true
		has[str(i, "id")] = true
		pending = append(pending, Record{"id": uuid.NewString(), "tx": str(t, "id"), "invoice": str(i, "id"), "ccy": str(i, "ccy"), "amount": value, "basis": basis, "status": "待确认", "reviewer": "", "note": note, "evidence": ""})
	}
	for _, i := range d.Invoices {
		if str(i, "duplicate") != "" || cents(i, "amount") <= 0 || !validDate(str(i, "date")) {
			continue
		}
		idate, _ := time.Parse("2006-01-02", str(i, "date"))
		if str(i, "provider") == "google" && str(i, "document_type") == "payment_receipt" && str(i, "ccy") == "" {
			for _, t := range d.Tx {
				td, err := time.Parse("2006-01-02", str(t, "date"))
				days := td.Sub(idate).Hours() / 24
				if err == nil && days >= 0 && days <= 7 && cents(t, "wd") > 0 && flowOriginal(t) == cents(i, "amount") && strings.Contains(strings.ToUpper(str(t, "details")), "GOOGLE") {
					before := len(pending)
					add(t, i, num(i, "amount"), "Google付款收據、扣款對象及金額相符，銀行入賬日相差不超過7日", "原件只列$，發票幣種待確認；此為付款收據，未冒充正式發票")
					if len(pending) > before {
						pending[len(pending)-1]["ccy"] = str(t, "orig_ccy")
					}
				}
			}
		}
		near := []Record{}
		for _, t := range d.Tx {
			td, _ := time.Parse("2006-01-02", str(t, "date"))
			days := td.Sub(idate).Hours() / 24
			if cents(t, "wd") > 0 && !strings.HasPrefix(str(t, "category"), "内部") && days >= -120 && days <= 120 {
				near = append(near, t)
				if str(t, "orig_ccy") == str(i, "ccy") && cents(t, "orig_amount") == cents(i, "amount") {
					basis, note := "同币同额；收款对象待核验", ""
					if accountMatch(i, t) {
						basis = "同币同额及收款账号"
					}
					if str(t, "ccy") != str(i, "ccy") {
						basis = "銀行原幣同幣同額，銀行扣賬幣種不同；收款對象待核驗"
						if accountMatch(i, t) {
							basis = "銀行原幣同幣同額及收款賬號，銀行扣賬幣種不同"
						}
					}
					if days < 0 {
						note = "付款早于发票日"
					}
					add(t, i, num(i, "amount"), basis, note)
				}
			}
		}
		if !has[str(i, "id")] {
			pool := []Record{}
			for _, t := range near {
				if str(t, "orig_ccy") == str(i, "ccy") && cents(t, "orig_amount") < cents(i, "amount") && accountMatch(i, t) {
					pool = append(pool, t)
				}
			}
			solutions := [][]Record{}
			if len(pool) <= 16 {
				var find func(int, []Record, int64)
				find = func(pos int, chosen []Record, total int64) {
					if len(solutions) > 1 {
						return
					}
					if len(chosen) >= 2 && total == cents(i, "amount") {
						solutions = append(solutions, append([]Record{}, chosen...))
						return
					}
					if len(chosen) == 4 || total >= cents(i, "amount") {
						return
					}
					for n := pos; n < len(pool); n++ {
						find(n+1, append(chosen, pool[n]), total+cents(pool[n], "orig_amount"))
					}
				}
				find(0, nil, 0)
			}
			if len(solutions) == 1 {
				for _, t := range solutions[0] {
					add(t, i, num(t, "orig_amount"), "同账号同币种，拆分付款合计等于发票金额", "")
				}
			}
		}
		if !has[str(i, "id")] {
			for _, t := range near {
				td, _ := time.Parse("2006-01-02", str(t, "date"))
				days := td.Sub(idate).Hours() / 24
				if str(t, "ccy") != str(i, "ccy") && t["fx"] == nil && accountMatch(i, t) && days >= -45 && days <= 45 {
					add(t, i, num(i, "amount"), "同賬號及鄰近日期找到發票", "銀行扣賬與票面金額分開記錄")
				}
			}
		}
	}
	tx, e := s.db.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()
	for _, m := range pending {
		b, _ := json.Marshal(m)
		if _, e = tx.Exec("INSERT INTO allocations VALUES(?,?)", str(m, "id"), string(b)); e != nil {
			return e
		}
	}
	if e = tx.Commit(); e != nil {
		return e
	}
	if e = s.refreshSalesCandidates(); e != nil {
		return e
	}
	return s.autoMatchExactPayments()
}
