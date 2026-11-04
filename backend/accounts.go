package main

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/google/uuid"
)

func accountKey(number string) string {
	return strings.ReplaceAll(strings.Join(strings.Fields(number), ""), "-", "")
}

func (s *Store) accounts() ([]Record, error) {
	stored := []Record{}
	rows, err := s.db.Query("SELECT meta FROM accounts ORDER BY id")
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var raw string
		var v Record
		if err = rows.Scan(&raw); err == nil {
			err = json.Unmarshal([]byte(raw), &v)
		}
		if err != nil {
			rows.Close()
			return nil, err
		}
		stored = append(stored, v)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	d, err := s.load()
	if err != nil {
		return nil, err
	}
	groups := map[string]Record{}
	legacy := map[string][]Record{}
	ensure := func(number string) Record {
		key := accountKey(number)
		if groups[key] == nil {
			groups[key] = Record{"id": "", "bank_name": "", "bank_number": number, "account": "综合账户", "currencies": []string{}, "opened_on": "", "closed_on": "", "status": "待确认", "note": "", "subaccounts": []Record{}, "first_statement_period": "", "first_statement_sha": ""}
		}
		return groups[key]
	}
	for _, v := range stored {
		if accountKey(str(v, "bank_number")) == "" {
			continue
		}
		legacy[accountKey(str(v, "bank_number"))] = append(legacy[accountKey(str(v, "bank_number"))], v)
		g := ensure(str(v, "bank_number"))
		if str(g, "id") == "" || yes(v, "main_account") {
			for _, k := range []string{"id", "bank_name", "bank_number", "opened_on", "closed_on", "status", "note"} {
				g[k] = v[k]
			}
			if yes(v, "main_account") {
				g["account"] = str(v, "account")
				g["main_account"] = true
			}
		}
		for _, ccy := range append(texts(v, "currencies"), str(v, "ccy")) {
			if ccy == "HKD" || ccy == "USD" {
				g["currencies"] = append(texts(g, "currencies"), ccy)
			}
		}
	}
	seen := map[string]bool{}
	for _, t := range append(append([]Record{}, d.Tx...), d.Checks...) {
		if accountKey(str(t, "bank_number")) == "" {
			continue
		}
		g := ensure(str(t, "bank_number"))
		key := accountKey(str(t, "bank_number")) + "|" + str(t, "account") + "|" + str(t, "ccy")
		g["currencies"] = append(texts(g, "currencies"), str(t, "ccy"))
		if !seen[key] {
			seen[key] = true
			g["subaccounts"] = append(arr(g, "subaccounts"), Record{"account": str(t, "account"), "ccy": str(t, "ccy")})
		}
	}
	for _, c := range d.Checks {
		if accountKey(str(c, "bank_number")) == "" {
			continue
		}
		g := ensure(str(c, "bank_number"))
		period := str(c, "period")
		if period != "" && (str(g, "first_statement_period") == "" || period < str(g, "first_statement_period")) {
			g["first_statement_period"] = period
			g["first_statement_sha"] = str(c, "document_sha")
		}
	}
	out := []Record{}
	for key, g := range groups {
		unique := map[string]bool{}
		currencies := []string{}
		for _, ccy := range texts(g, "currencies") {
			if ccy != "" && !unique[ccy] {
				unique[ccy] = true
				currencies = append(currencies, ccy)
			}
		}
		sort.Strings(currencies)
		g["currencies"] = currencies
		g["ccy"] = strings.Join(currencies, " / ")
		if !yes(g, "main_account") && len(currencies) > 1 {
			g["account"] = "多币种综合账户"
		}
		g["metadata_conflicts"] = []string{}
		if len(legacy[key]) > 1 {
			g["legacy_records"] = legacy[key]
			if !yes(g, "main_account") {
				for _, field := range []string{"bank_name", "opened_on", "closed_on", "status", "note"} {
					values := map[string]bool{}
					value := ""
					for _, v := range legacy[key] {
						if str(v, field) != "" {
							values[str(v, field)] = true
							value = str(v, field)
						}
					}
					if len(values) > 1 {
						g["metadata_conflicts"] = append(texts(g, "metadata_conflicts"), field)
						if field == "status" {
							g[field] = "待确认"
						} else {
							g[field] = ""
						}
					} else if len(values) == 1 {
						g[field] = value
					}
				}
			}
		}
		out = append(out, g)
	}
	sort.Slice(out, func(i, j int) bool { return str(out[i], "bank_number") < str(out[j], "bank_number") })
	return out, nil
}

func (s *Store) saveAccount(input Record) (Record, error) {
	v := Record{}
	for _, k := range []string{"id", "bank_name", "bank_number", "account", "opened_on", "closed_on", "status", "note"} {
		value := strings.TrimSpace(str(input, k))
		if len(value) > 4000 {
			return nil, fmt.Errorf("账户信息过长")
		}
		v[k] = value
	}
	if str(v, "bank_name") == "" || str(v, "bank_number") == "" || str(v, "account") == "" {
		return nil, fmt.Errorf("请填写银行名称、账号及账户类型")
	}
	if accountKey(str(v, "bank_number")) == "" {
		return nil, fmt.Errorf("银行账号无效")
	}
	currencies := texts(input, "currencies")
	if len(currencies) == 0 && str(input, "ccy") != "" {
		currencies = []string{str(input, "ccy")}
	}
	unique := map[string]bool{}
	valid := []string{}
	for _, ccy := range currencies {
		if ccy != "HKD" && ccy != "USD" {
			return nil, fmt.Errorf("账户币种应为HKD或USD")
		}
		if !unique[ccy] {
			unique[ccy] = true
			valid = append(valid, ccy)
		}
	}
	if len(valid) == 0 {
		return nil, fmt.Errorf("请至少选择一个账户币种")
	}
	sort.Strings(valid)
	v["currencies"] = valid
	v["main_account"] = true
	for _, k := range []string{"opened_on", "closed_on"} {
		if str(v, k) != "" && !validDate(str(v, k)) {
			return nil, fmt.Errorf("开户/销户日期格式应为YYYY-MM-DD")
		}
	}
	if str(v, "opened_on") != "" && str(v, "closed_on") != "" && str(v, "closed_on") < str(v, "opened_on") {
		return nil, fmt.Errorf("销户时间不能早于开户时间")
	}
	switch str(v, "status") {
	case "待确认", "正常", "停用", "已销户":
	default:
		return nil, fmt.Errorf("账户状态无效")
	}
	if str(v, "status") == "已销户" && str(v, "closed_on") == "" {
		return nil, fmt.Errorf("已销户账户请填写销户日期")
	}
	all, err := s.accounts()
	if err != nil {
		return nil, err
	}
	found := str(v, "id") == ""
	for _, a := range all {
		if accountKey(str(a, "bank_number")) == accountKey(str(v, "bank_number")) && str(a, "first_statement_period") != "" && str(v, "opened_on") != "" && str(v, "opened_on")[:7] > str(a, "first_statement_period") {
			return nil, fmt.Errorf("开通日期晚于首份月结单月份，请核对银行资料")
		}
		if str(a, "id") == str(v, "id") && str(a, "id") != "" {
			found = true
		}
		if str(a, "id") != "" && str(a, "id") != str(v, "id") && accountKey(str(a, "bank_number")) == accountKey(str(v, "bank_number")) {
			return nil, fmt.Errorf("该主账户已存在，请编辑原记录")
		}
	}
	if !found {
		return nil, fmt.Errorf("账户不存在，请刷新后重试")
	}
	if str(v, "id") == "" {
		v["id"] = uuid.NewString()
	}
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	_, err = s.db.Exec("INSERT INTO accounts(id,meta) VALUES(?,?) ON CONFLICT(id) DO UPDATE SET meta=excluded.meta", str(v, "id"), string(b))
	return v, err
}
