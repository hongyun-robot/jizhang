package repository

// SQL 片段只接受包内静态别名，不接受请求参数。
func balanceEffectSQL(alias string) string {
	p := alias
	if p != "" {
		p += "."
	}
	return "CASE WHEN " + p + "purpose IN ('voucher_redemption','voucher_writeoff') THEN 0 " +
		"WHEN " + p + "kind IN ('income','refund') THEN " + p + "amount " +
		"WHEN " + p + "kind IN ('expense','transfer') THEN -" + p + "amount ELSE 0 END"
}

func expenseEffectSQL(alias string) string {
	p := alias
	if p != "" {
		p += "."
	}
	return "CASE WHEN " + p + "purpose IN ('voucher_purchase','voucher_refund','wealth_cash_yield') THEN 0 " +
		"WHEN " + p + "kind = 'expense' THEN " + p + "amount " +
		"WHEN " + p + "kind = 'refund' THEN -" + p + "amount ELSE 0 END"
}
