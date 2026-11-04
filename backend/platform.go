package main

import (
	"encoding/json"
	"fmt"
	"github.com/google/uuid"
	"strings"
)

func (s *Store) platformList() ([]Record, error) {
	rows, e := s.db.Query("SELECT meta FROM platform_reports ORDER BY id")
	if e != nil {
		return nil, e
	}
	out := []Record{}
	for rows.Next() {
		var raw string
		if e = rows.Scan(&raw); e != nil {
			rows.Close()
			return nil, e
		}
		var m Record
		if e = json.Unmarshal([]byte(raw), &m); e != nil {
			rows.Close()
			return nil, e
		}
		out = append(out, m)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return nil, e
	}
	for _, p := range out {
		if str(p, "settlement_kind") != "Stripe提现" {
			continue
		}
		var raw string
		p["source_valid"] = false
		if err := s.db.QueryRow("SELECT meta FROM stripe_records WHERE scope=? AND id=? AND kind='payout'", str(p, "scope"), str(p, "payout_id")).Scan(&raw); err == nil {
			var source Record
			if json.Unmarshal([]byte(raw), &source) == nil {
				p["source_valid"] = str(source, "status") == "paid" && strings.EqualFold(str(source, "currency"), str(p, "ccy")) && int64(num(source, "amount")) == cents(p, "net")
			}
		}
	}
	return out, nil
}
func platformNet(m Record) int64 {
	return cents(m, "gross") - cents(m, "fees") - cents(m, "refunds") + cents(m, "adjustments")
}
func (s *Store) savePlatform(m Record) error {
	d, e := s.load()
	if e != nil {
		return e
	}
	var bank Record
	for _, t := range d.Tx {
		if str(t, "id") == str(m, "tx") {
			bank = t
		}
	}
	if bank == nil || cents(bank, "dep") <= 0 {
		return fmt.Errorf("请选择实际银行收款流水")
	}
	if e = s.checkEditable(str(bank, "date")); e != nil {
		return e
	}
	for _, k := range []string{"gross", "fees", "refunds", "adjustments"} {
		v, err := money(m[k])
		if err != nil || k != "adjustments" && v < 0 {
			return fmt.Errorf("平台金额无效: %s", k)
		}
		m[k] = float64(v) / 100
	}
	if str(m, "ccy") != str(bank, "ccy") || str(bank, "orig_ccy") != str(bank, "ccy") {
		return fmt.Errorf("平台对账暂限银行同币种结算，跨币请保留凭证交人工核对")
	}
	if platformNet(m) != cents(bank, "dep") {
		return fmt.Errorf("销售总额－手续费－退款＋调整必须等于银行净到账")
	}
	if str(m, "settlement_kind") == "跨境匯款" && str(m, "status") == "已核验" && cents(m, "fees") > 0 && strings.TrimSpace(str(m, "fee_basis")) == "" {
		return fmt.Errorf("請填寫手續費依據，不能只將發票與入賬差額推定為銀行費用")
	}
	m["net"] = bank["dep"]
	if str(m, "payout_id") == "" || strings.TrimSpace(str(m, "reviewer")) == "" {
		return fmt.Errorf("请填写平台结算编号及复核人")
	}
	if str(m, "status") != "待复核" && str(m, "status") != "已核验" {
		return fmt.Errorf("平台核验状态无效")
	}
	sha := str(m, "evidence_sha")
	if sha == "" {
		return fmt.Errorf("请选择平台结算报告原件")
	}
	if _, e = s.documentPath(sha); e != nil {
		return e
	}
	if str(m, "id") == "" {
		m["id"] = "PLAT-" + uuid.NewString()
	}
	next := []Record{}
	for _, p := range d.Platforms {
		if str(p, "id") == str(m, "id") {
			for _, t := range d.Tx {
				if str(t, "id") == str(p, "tx") {
					if e = s.checkEditable(str(t, "date")); e != nil {
						return e
					}
				}
			}
			continue
		}
		if str(p, "tx") == str(m, "tx") && str(p, "status") == "已核验" && str(m, "status") == "已核验" {
			return fmt.Errorf("同一银行到账已有已核验平台结算，避免重复核销")
		}
		next = append(next, p)
	}
	d.Platforms = append(next, m)
	verdicts(&d)
	for _, a := range d.Matches {
		if str(a, "status") == "已确认" && str(a, "validity") != "有效确认" {
			return fmt.Errorf("修改将使已确认分配失效: %s", str(a, "validity"))
		}
	}
	b, _ := json.Marshal(m)
	_, e = s.db.Exec("INSERT INTO platform_reports VALUES(?,?) ON CONFLICT(id) DO UPDATE SET meta=excluded.meta", str(m, "id"), string(b))
	return e
}
func platformCapacity(d *Data, t, m Record) (int64, string) {
	id := str(m, "platform_id")
	if id == "" {
		return flowOriginal(t), ""
	}
	for _, p := range d.Platforms {
		if str(p, "id") != id {
			continue
		}
		if str(p, "status") != "已核验" || str(p, "tx") != str(t, "id") || str(p, "ccy") != str(m, "ccy") || platformNet(p) != cents(t, "dep") {
			return 0, "平台结算未核验或与流水不符"
		}
		if cents(p, "refunds") != 0 || cents(p, "adjustments") != 0 {
			return 0, "含退款或调整的结算须另核对贷项，禁止直接核销销售发票"
		}
		for _, other := range d.Matches {
			if str(other, "tx") == str(t, "id") && str(other, "status") == "已确认" && str(other, "platform_id") != id {
				return 0, "同一到账不能混用平台总额与直接收款分配"
			}
		}
		return cents(p, "gross"), ""
	}
	return 0, "平台结算不存在"
}
