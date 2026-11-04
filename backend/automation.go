package main

import (
	"strings"
)

func classifyKnownParty(t Record, profiles ...Record) {
	cfg := Record{}
	if len(profiles) > 0 {
		cfg = profiles[0]
	}
	t["party"] = strings.ReplaceAll(str(t, "party"), "（按2025凭证账号，待核实）", "")
	text := strings.ToUpper(str(t, "details"))
	compact := strings.Join(strings.Fields(text), "")
	compact = strings.ReplaceAll(compact, "-", "")
	bankKey := accountKey(str(t, "bank_number"))
	if len(bankKey) >= 9 && strings.Contains(compact, "TO"+bankKey[:9]) {
		t["category"] = "内部转账/换汇待确认"
		t["party"] = "本公司账户"
		t["need"] = "转账/换汇说明"
		t["documents"] = "本公司賬戶間轉賬／換匯，核對兩端流水，不要求採購或銷售發票"
		return
	}
	if strings.Contains(compact, "CASHREBATE") || strings.Contains(compact, "CHGFORAUDITCONF") {
		t["category"] = "银行费用"
		if strings.Contains(compact, "CASHREBATE") {
			t["category"] = "返现"
		}
		t["party"] = "汇丰银行"
		t["need"] = "银行记录"
		t["documents"] = "銀行返現／銀行詢證費，保留月結單及銀行通知，無需銷售或採購發票"
		return
	}
	interest := ""
	if cents(t, "dep") > 0 && (strings.Contains(compact, "CREDITINTEREST") || strings.Contains(compact, "INTERESTCREDIT") || strings.Contains(text, "存款利息") || strings.Contains(text, "利息收入")) {
		interest = "银行利息"
	}
	if cents(t, "wd") > 0 && (strings.Contains(compact, "DEBITINTEREST") || strings.Contains(compact, "OVERDRAFTINTEREST") || strings.Contains(text, "利息支出")) {
		interest = "银行利息支出"
	}
	if interest != "" {
		t["category"] = interest
		t["party"] = "汇丰银行"
		t["need"] = "银行记录"
		t["documents"] = "月结单已载明利息，无需供应商发票"
		return
	}
	for _, name := range profileNames(str(cfg, "director_names")) {
		if descriptionHasName(text, name) {
			t["category"] = "董事往来"
			t["party"] = name + "（董事本人）"
			t["need"] = "董事往来说明"
			t["documents"] = "按本帳簿設定的董事姓名識別；保留銀行紀錄並核實借款、還款、代墊或股本性質"
			return
		}
	}
	// An own-company name in a payment's remitter field is not its recipient.
	if strings.Contains(compact, "TO") && str(t, "need") == "转账/换汇说明" && cents(t, "wd") > 0 {
		t["category"] = "付款待分类"
		t["party"] = "待确认"
		t["need"] = "付款凭证待判定"
	}
	if cents(t, "dep") > 0 && ownCompanyDescription(text, cfg) && (strings.Contains(compact, "FROM") || strings.Contains(compact, "BIB")) {
		t["category"] = "内部转账/换汇待确认"
		t["party"] = "本公司账户"
		t["need"] = "转账/换汇说明"
	}
}

func classifyOwnTransferReceipts(txs []Record, profiles ...Record) {
	cfg := Record{}
	if len(profiles) > 0 {
		cfg = profiles[0]
	}
	for _, t := range txs {
		if cents(t, "dep") <= 0 || !ownCompanyDescription(str(t, "details"), cfg) {
			continue
		}
		ref := capture(`\b(N\d{8,})`, str(t, "details"))
		if ref == "" {
			continue
		}
		for _, other := range txs {
			if str(other, "date") == str(t, "date") && str(other, "ccy") == str(t, "ccy") && cents(other, "wd") == cents(t, "dep") && str(other, "category") == "内部转账/换汇待确认" && strings.Contains(str(other, "details"), ref) {
				t["category"] = "内部转账/换汇待确认"
				t["party"] = "本公司账户"
				t["need"] = "转账/换汇说明"
				t["documents"] = "同日同幣同額且銀行參考編號一致的本公司轉賬兩端，不要求發票"
				t["transfer_verified"], other["transfer_verified"] = true, true
				t["transfer_basis"], other["transfer_basis"] = "同日同幣同額及銀行參考編號一致", "同日同幣同額及銀行參考編號一致"
				break
			}
		}
	}
	for _, t := range txs {
		if cents(t, "wd") <= 0 || !strings.HasPrefix(str(t, "category"), "内部") || flowOriginal(t) <= 0 {
			continue
		}
		matches := []Record{}
		for _, other := range txs {
			if cents(other, "dep") <= 0 || str(other, "date") != str(t, "date") || str(other, "bank_number") != str(t, "bank_number") || !strings.HasPrefix(str(other, "category"), "内部") {
				continue
			}
			if str(t, "orig_ccy") == str(other, "ccy") && flowOriginal(t) == cents(other, "dep") {
				matches = append(matches, other)
			}
		}
		if len(matches) != 1 {
			continue
		}
		other := matches[0]
		reverse := 0
		for _, candidate := range txs {
			if cents(candidate, "wd") > 0 && str(candidate, "date") == str(other, "date") && str(candidate, "bank_number") == str(other, "bank_number") && strings.HasPrefix(str(candidate, "category"), "内部") && str(candidate, "orig_ccy") == str(other, "ccy") && flowOriginal(candidate) == cents(other, "dep") {
				reverse++
			}
		}
		if reverse == 1 {
			t["transfer_verified"], other["transfer_verified"] = true, true
			t["transfer_basis"], other["transfer_basis"] = "同公司賬戶、同日且銀行原幣與入賬金額雙向唯一一致", "同公司賬戶、同日且銀行原幣與入賬金額雙向唯一一致"
		}
	}
	// Some statements omit the dealt currency/amount but show both own-account
	// legs. Pair only a unique same-day cross-currency debit and own deposit.
	for _, t := range txs {
		if yes(t, "transfer_verified") || cents(t, "wd") <= 0 || !strings.HasPrefix(str(t, "category"), "内部") || !strings.Contains(strings.ToUpper(str(t, "details")), "EXCHANGE") || (str(t, "orig_ccy") != "" && str(t, "orig_ccy") != str(t, "ccy")) {
			continue
		}
		candidates := []Record{}
		for _, other := range txs {
			if !yes(other, "transfer_verified") && cents(other, "dep") > 0 && str(other, "date") == str(t, "date") && str(other, "bank_number") == str(t, "bank_number") && str(other, "ccy") != str(t, "ccy") && strings.HasPrefix(str(other, "category"), "内部") {
				candidates = append(candidates, other)
			}
		}
		if len(candidates) != 1 {
			continue
		}
		other := candidates[0]
		reverse := 0
		for _, a := range txs {
			if !yes(a, "transfer_verified") && cents(a, "wd") > 0 && str(a, "date") == str(other, "date") && str(a, "bank_number") == str(other, "bank_number") && str(a, "ccy") != str(other, "ccy") && strings.HasPrefix(str(a, "category"), "内部") && strings.Contains(strings.ToUpper(str(a, "details")), "EXCHANGE") {
				reverse++
			}
		}
		if reverse == 1 {
			t["transfer_verified"], other["transfer_verified"] = true, true
			t["transfer_basis"], other["transfer_basis"] = "本公司同日換匯兩端、雙向唯一；保留各幣金額，不推算成交匯率", "本公司同日換匯兩端、雙向唯一；保留各幣金額，不推算成交匯率"
		}
	}
	for _, t := range txs {
		if yes(t, "transfer_verified") {
			t["category"] = "内部转账/换汇"
			t["documents"] = "銀行兩端已核對，無需採購或銷售發票"
		}
	}
}

// Matching is a proposal only. Unmatched documents are evidence, never garbage.
func (s *Store) applyAutomation() error { return nil }
