package main

import "strings"

// Legacy bank parsing used the debit as a fallback original amount. On an
// exchange debit this does not establish the currency/amount paid abroad.
func normalizeUnlistedOriginalAmount(t Record) {
	if strings.Contains(strings.ToUpper(str(t, "details")), "EXCHANGE") && !rx(`(?i)/OCMT/`).MatchString(str(t, "details")) && str(t, "orig_ccy") == str(t, "ccy") {
		t["orig_ccy"] = ""
		t["orig_amount"] = nil
		t["original_amount_status"] = "未列示"
	}
}

// Service delivery is electronic. Cash movements alone do not prove revenue/cost.
func serviceEvidence(t Record) {
	t["business_model"] = "一般業務"
	t["gap_type"] = "用途待確認"
	need := str(t, "need")
	category := str(t, "manual_category")
	if category == "" {
		category = str(t, "category")
	}
	if str(t, "manual_category") != "" {
		switch category {
		case "银行费用", "银行利息", "銀行費用", "銀行利息":
			need = "银行记录"
		case "董事往来", "董事往來":
			need = "董事往来说明"
		case "内部转账/换汇", "內部轉賬/換匯":
			need = "银行记录"
			t["documents"] = "本公司資金調撥，保留銀行紀錄，無需銷售或採購發票"
		default:
			// A manual reclassification must not inherit an old bank-only/director exemption.
			if cents(t, "wd") > 0 {
				need = "采购/服务发票"
			} else if cents(t, "dep") > 0 {
				need = "收款来源及销售凭证"
			}
		}
		t["need"] = need
	}
	if yes(t, "transfer_verified") {
		need = "银行记录"
		t["need"] = need
	}
	if need == "银行记录" {
		t["gap_type"] = "無需發票"
	} else if !strings.Contains(category, "内部") && !strings.Contains(category, "轉賬") && !strings.Contains(category, "董事") && !strings.Contains(need, "资金来源") && !strings.Contains(need, "董事") && !strings.Contains(need, "换汇") {
		if cents(t, "wd") > 0 {
			t["gap_type"] = "待補採購發票"
			t["documents"] = "核對採購發票或收據、供應商、交易內容及付款；按實際業務保留交付依據"
		} else if cents(t, "dep") > 0 {
			t["gap_type"] = "待補銷售發票"
			t["documents"] = "核對銷售憑證、客戶、交易內容及收款；銀行到賬與銷售金額分別記錄"
		}
	}
	t["fx_status"] = "同幣收付款"
	if str(t, "orig_ccy") != "" && str(t, "orig_ccy") != str(t, "ccy") && flowOriginal(t) > 0 {
		t["fx_status"] = "按銀行原幣金額匹配"
	} else if strings.Contains(strings.ToUpper(str(t, "details")), "EXCHANGE") && !strings.Contains(category, "内部") {
		t["fx_status"] = "銀行扣賬與票面原幣分開記錄"
		if str(t, "payment_ccy") == "USD" {
			t["fx_status"] = "USD付款，HKD扣賬"
		}
	}
}
