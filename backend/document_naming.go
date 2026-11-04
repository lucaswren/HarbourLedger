package main

import "strings"

func canonicalParty(name string) string {
	return safeName(strings.TrimSpace(name))
}

func documentNameLabel(kind string, m Record) string {
	if kind == "银行通知" {
		return "收款通知"
	}
	if kind == "银行" {
		return "銀行月結單"
	}
	if invoiceKind(kind) {
		label := "採購"
		if kind == "销售发票" || isSale(m) {
			label = "銷售"
		}
		if kind == "待分类发票" {
			label = "待分類"
		}
		if str(m, "document_type") == "payment_receipt" {
			return label + "收據"
		}
		return label + "發票"
	}
	return traditional(safeName(kind))
}
