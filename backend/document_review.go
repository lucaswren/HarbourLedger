package main

import "strings"

// Missing originals, matching evidence and accounting classification are separate tasks.
func classifyDocumentReview(t Record, d *Data) {
	t["review_type"] = "已完成"
	t["review_reason"] = ""
	if str(t, "state") == "银行凭证已具备" || str(t, "state") == "发票已关联（复核分类）" {
		return
	}
	if str(t, "advice_sha") != "" && str(t, "advice_purpose") == "合作佣金" {
		t["review_type"] = "已有收款通知，待補佣金結算資料"
		t["review_reason"] = "已核對付款方、原款、銀行費用及淨入帳；另整理合作佣金結算資料"
		return
	}
	category := str(t, "manual_category")
	if category == "" {
		category = str(t, "category")
	}
	if strings.Contains(category, "董事") {
		t["review_type"] = "董事往來（交會計師分類）"
		t["review_reason"] = "已識別董事本人往來，保留銀行紀錄，款項性質交會計師分類，無需銷售或採購發票"
		return
	}
	if strings.Contains(category, "内部") {
		t["review_type"] = "轉賬／換匯核對"
		t["review_reason"] = "待核對是否為本公司資金調撥"
		return
	}
	for _, m := range d.Matches {
		if str(m, "tx") != str(t, "id") || str(m, "status") == "不匹配" {
			continue
		}
		found := false
		for _, i := range append(append([]Record{}, d.Invoices...), d.Sales...) {
			if str(i, "id") == str(m, "invoice") && str(i, "duplicate") == "" && str(i, "status") != "草稿" {
				found = true
				break
			}
		}
		if !found {
			continue
		}
		t["review_type"] = "已找到票據"
		t["review_reason"] = "已找到對應票據"
		nInvoice := 0
		for _, i := range append(append([]Record{}, d.Invoices...), d.Sales...) {
			if automaticCandidatePair(d, t, i) {
				nInvoice++
			}
		}
		if nInvoice > 1 {
			t["review_reason"] = "多張票據符合匹配條件，請選擇交易對應的發票"
		}

		if str(m, "ccy") != str(t, "orig_ccy") && nInvoice <= 1 {
			t["review_reason"] = "已找到發票；跨幣種有多個可能對應或資料不足，請選擇對應票據"
		}
		if str(t, "state") == "部分付款已关联，仍需补凭证" {
			t["review_reason"] = "部分金額已有票據，剩餘分配仍需核對"
		}
		return
	}
	if str(t, "gap_type") == "待補採購發票" {
		t["review_type"] = "未找到採購憑證"
		t["review_reason"] = "已導入文件中未找到對應採購發票或收據"
		return
	}
	if str(t, "gap_type") == "待補銷售發票" {
		t["review_type"] = "未找到銷售憑證"
		t["review_reason"] = "已導入文件中未找到對應銷售憑證；先核對收款性質"
		return
	}
	t["review_type"] = "用途說明"
}
