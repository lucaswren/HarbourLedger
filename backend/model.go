package main

import (
	"encoding/json"
	"fmt"
	"math"
	"math/big"
	"strings"
	"time"
)

// Record preserves the existing SQLite JSON schema and its audit metadata.
type Record map[string]any

func str(r Record, k string) string {
	if v, ok := r[k].(string); ok {
		return v
	}
	return ""
}
func num(r Record, k string) float64 {
	switch v := r[k].(type) {
	case float64:
		return v
	case int:
		return float64(v)
	case int64:
		return float64(v)
	case json.Number:
		f, _ := v.Float64()
		return f
	}
	return 0
}
func yes(r Record, k string) bool { v, _ := r[k].(bool); return v }
func arr(r Record, k string) []Record {
	out := []Record{}
	switch v := r[k].(type) {
	case []Record:
		return v
	case []any:
		for _, x := range v {
			if m, ok := x.(map[string]any); ok {
				out = append(out, Record(m))
			}
		}
	}
	return out
}
func texts(r Record, k string) []string {
	out := []string{}
	switch v := r[k].(type) {
	case []string:
		return v
	case []any:
		for _, s := range v {
			if s, ok := s.(string); ok {
				out = append(out, s)
			}
		}
	}
	return out
}
func clone(r Record) Record {
	b, _ := json.Marshal(r)
	var n Record
	_ = json.Unmarshal(b, &n)
	return n
}

// Money is rounded to cents with ties-to-even, matching Python Decimal's default.
func money(v any) (int64, error) {
	s := fmt.Sprint(v)
	r, ok := new(big.Rat).SetString(s)
	if !ok {
		return 0, fmt.Errorf("金额格式不正确")
	}
	r.Mul(r, big.NewRat(100, 1))
	q, rem := new(big.Int), new(big.Int)
	q.QuoRem(r.Num(), r.Denom(), rem)
	cmp := new(big.Int).Lsh(new(big.Int).Abs(rem), 1).Cmp(r.Denom())
	if cmp > 0 || cmp == 0 && q.Bit(0) == 1 {
		if r.Sign() < 0 {
			q.Sub(q, big.NewInt(1))
		} else {
			q.Add(q, big.NewInt(1))
		}
	}
	if !q.IsInt64() || math.Abs(float64(q.Int64())) > 9e15 {
		return 0, fmt.Errorf("金额超出范围")
	}
	return q.Int64(), nil
}
func cents(r Record, k string) int64 { v, _ := money(r[k]); return v }
func validDate(s string) bool {
	t, e := time.Parse("2006-01-02", s)
	return e == nil && t.Format("2006-01-02") == s
}
func months(start, end string) ([]string, error) {
	a, e := time.Parse("2006-01", start)
	if e != nil {
		return nil, fmt.Errorf("开始月份格式应为 YYYY-MM")
	}
	b, e := time.Parse("2006-01", end)
	if e != nil || a.After(b) || b.Sub(a) > 100*366*24*time.Hour {
		return nil, fmt.Errorf("结束月份无效或周期超过100年")
	}
	out := []string{}
	for !a.After(b) {
		out = append(out, a.Format("2006-01"))
		a = a.AddDate(0, 1, 0)
	}
	return out, nil
}
func accountMatch(i, t Record) bool {
	for _, a := range texts(i, "accounts") {
		if len(accountKey(a)) >= 6 && strings.Contains(accountKey(str(t, "details")), accountKey(a)) {
			return true
		}
	}
	return false
}

type Data struct {
	Advices      []Record `json:"advices"`
	Unclassified []Record `json:"unclassified"`
	Platforms    []Record `json:"platforms"`
	Sales        []Record `json:"sales"`
	Tx           []Record `json:"tx"`
	Checks       []Record `json:"checks"`
	Invoices     []Record `json:"invoices"`
	Matches      []Record `json:"matches"`
	Docs         []Record `json:"docs"`
	Issued       []Record `json:"issued"`
}

func emptyData() Data {
	return Data{Advices: []Record{}, Unclassified: []Record{}, Platforms: []Record{}, Sales: []Record{}, Tx: []Record{}, Checks: []Record{}, Invoices: []Record{}, Matches: []Record{}, Docs: []Record{}, Issued: []Record{}}
}
func verdicts(d *Data) {
	ts, is := map[string]Record{}, map[string]Record{}
	byi, byt := map[string]int64{}, map[string]int64{}
	bankLinks := map[string]int{}
	bankCurrencies := map[string]map[string]bool{}
	for _, t := range d.Tx {
		ts[str(t, "id")] = t
	}
	for _, i := range append(append([]Record{}, d.Invoices...), d.Sales...) {
		is[str(i, "id")] = i
	}
	for _, m := range d.Matches {
		if str(m, "status") == "已确认" {
			bankLinks[str(m, "tx")]++
			if bankCurrencies[str(m, "tx")] == nil {
				bankCurrencies[str(m, "tx")] = map[string]bool{}
			}
			bankCurrencies[str(m, "tx")][str(m, "ccy")] = true
			byi[str(m, "invoice")] += cents(m, "amount")
			byt[str(m, "tx")+str(m, "ccy")] += cents(m, "amount")
		}
	}
	for _, m := range d.Matches {
		t, i := ts[str(m, "tx")], is[str(m, "invoice")]
		m["bank_ccy"] = str(t, "ccy")
		m["bank_amount"] = float64(invoiceFlow(t, i)) / 100
		m["orig_ccy"] = str(t, "orig_ccy")
		m["orig_amount"] = t["orig_amount"]
		m["fx"] = t["fx"]
		m["fx_basis"] = ""
		if str(t, "ccy") != str(i, "ccy") && str(t, "orig_ccy") == str(i, "ccy") && flowOriginal(t) > 0 {
			m["fx_basis"] = "銀行原幣金額核對發票；實際扣賬保留銀行幣種及金額"
		} else if str(t, "ccy") != str(i, "ccy") {
			m["fx_basis"] = "銀行扣賬與票面金額分別保留，不推算匯率"
		}
		s := str(m, "status")
		capacity, platformError := platformCapacity(d, t, m)
		if str(m, "platform_id") == "" && str(m, "ccy") == str(t, "ccy") && str(m, "ccy") != str(t, "orig_ccy") {
			capacity = invoiceFlow(t, i)
		}
		m["bank_capacity"] = float64(capacity) / 100
		switch {
		case t == nil || i == nil:
			s = "关联编号失效"
		case str(i, "duplicate") != "":
			s = "重复发票禁止核销"
		case yes(m, "auto_confirmed") && !(validAutomaticExact(d, m) || validAutomaticSplit(d, m) || validAutomaticMerged(d, m)):
			s = "自動關聯條件已變更，需重新匹配"
		case isSale(i) && !yes(i, "verified") && !(yes(m, "auto_confirmed") && (validAutomaticExact(d, m) || validAutomaticSplit(d, m) || validAutomaticMerged(d, m))):
			s = "发票待核验"
		case str(m, "ccy") != str(i, "ccy"):
			s = "分配币种不符"
		case cents(m, "amount") <= 0:
			s = "分配金额无效"
		case str(i, "status") == "草稿":
			s = "草稿禁止核销"
		case s == "已确认":
			switch {
			case len(bankCurrencies[str(t, "id")]) > 1:
				s = "同一流水不可混用多種分配幣種"
			case str(m, "ccy") != str(t, "orig_ccy") && str(m, "ccy") != str(t, "ccy") && bankLinks[str(t, "id")] > 1:
				s = "未列原幣金額的跨幣流水不可重複分配"
			case strings.TrimSpace(str(m, "reviewer")) == "":
				s = "缺复核人"
			case str(m, "platform_id") != "" && (!isSale(i) || platformError != ""):
				s = "平台结算分配无效: " + platformError
			case invoiceFlow(t, i) <= 0:
				s = "收付款方向不符"
			case isSale(i) && str(m, "platform_id") == "" && !receiptEvidence(i, t) && !(yes(m, "auto_confirmed") && validAutomaticExact(d, m)) && strings.TrimSpace(str(m, "evidence")) == "":
				s = "缺收款归属或平台结算凭证"
			case !isSale(i) && len(texts(i, "accounts")) > 0 && !accountMatch(i, t) && strings.TrimSpace(str(m, "note")) == "":
				s = "收款账号不符，需说明"
			case byi[str(i, "id")] > cents(i, "amount"):
				s = "发票超额分配"
			case (str(m, "ccy") == str(t, "orig_ccy") || str(m, "ccy") == str(t, "ccy")) && byt[str(t, "id")+str(m, "ccy")] > capacity:
				s = "流水超额分配"
			default:
				s = "有效确认"
			}
		}
		m["validity"] = s
	}
	for _, t := range d.Tx {
		t["state"] = transactionState(t, d.Matches)
		if applyStripePayoutVerification(t, d) {
			continue
		}
		classifyDocumentReview(t, d)
	}
}
func transactionState(t Record, ms []Record) string {
	if yes(t, "transfer_verified") {
		return "银行凭证已具备"
	}
	var paid int64
	valid, candidate := false, false
	capacity := flowOriginal(t)
	for _, m := range ms {
		if str(m, "tx") != str(t, "id") {
			continue
		}
		if str(m, "status") != "不匹配" {
			candidate = true
		}
		if str(m, "validity") == "有效确认" {
			valid = true
			if str(m, "ccy") == str(t, "orig_ccy") || str(m, "ccy") == str(t, "ccy") {
				capacity = cents(m, "bank_capacity")
				paid += cents(m, "amount")
			}
		}
	}
	if valid {
		if paid > 0 && paid < capacity {
			return "部分付款已关联，仍需补凭证"
		}
		return "发票已关联（复核分类）"
	}
	if str(t, "need") == "银行记录" {
		return "银行凭证已具备"
	}
	if str(t, "internal_voucher") != "" {
		return "内部凭证已建，外部票据待补"
	}
	if str(t, "manual_category") != "" && str(t, "manual_note") != "" {
		return "已填写分类及说明，待会计师复核"
	}
	if candidate {
		return "待确认候选"
	}
	return "待补凭证或说明"
}

func isSale(i Record) bool { return str(i, "kind") == "销售发票" }
func invoiceFlow(t, i Record) int64 {
	if isSale(i) {
		return cents(t, "dep")
	}
	return cents(t, "wd")
}
func flowOriginal(t Record) int64 {
	if t["orig_amount"] != nil {
		return cents(t, "orig_amount")
	}
	return cents(t, "dep") + cents(t, "wd")
}
