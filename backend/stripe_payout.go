package main

func applyStripePayoutVerification(t Record, d *Data) bool {
	delete(t, "stripe_payout_verified")
	delete(t, "stripe_payout_id")
	for _, p := range d.Platforms {
		if str(p, "settlement_kind") != "Stripe提现" || str(p, "status") != "已核验" || !yes(p, "source_valid") || str(p, "tx") != str(t, "id") || str(p, "ccy") != str(t, "ccy") || cents(p, "net") != cents(t, "dep") {
			continue
		}
		t["stripe_payout_verified"] = true
		t["stripe_payout_id"] = str(p, "payout_id")
		t["state"] = "银行凭证已具备"
		t["category"] = "Stripe提现到账"
		t["gap_type"] = ""
		t["need"] = "Stripe提现及银行记录"
		t["review_type"] = "提现到账已核对"
		t["review_reason"] = "提现与银行到账已核对；客户收款和手续费按相关凭证核对，无需以提现净额另开销售发票"
		return true
	}
	return false
}
