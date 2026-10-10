package liability

import (
	"encoding/json"
	"fmt"
	"gorm.io/gorm"
	"jizhang/internal/model"
	"time"
)

func Configure(db *gorm.DB, account int64, kind string, day, due, offset int, actor int64, now time.Time) (model.LiabilityBillRule, error) {
	var rule model.LiabilityBillRule
	if ValidateRule(day, due, offset) != nil {
		return rule, ErrInvalid
	}
	if e := ValidateAccountType(db, kind); e != nil {
		return rule, e
	}
	var a model.Account
	if e := db.First(&a, account).Error; e != nil {
		return rule, e
	}
	if a.Type != model.AccountLiability {
		return rule, ErrInvalid
	}
	var rules []model.LiabilityBillRule
	if e := db.Where("account_id = ?", account).Order("version").Find(&rules).Error; e != nil {
		return rule, e
	}
	start := Date(now)
	version := 1
	if len(rules) == 0 {
		var count int64
		if e := db.Model(&model.Transaction{}).Where("account_id = ? OR transfer_account_id = ?", account, account).Count(&count).Error; e != nil {
			return rule, e
		}
		if count != 0 || !a.OpeningBalance.IsZero() {
			return rule, ErrSetup
		}
		template := model.LiabilityBillRule{StatementDay: day, RepaymentDay: due, RepaymentMonthOffset: offset, EffectiveFrom: "2000-01-01"}
		x, _, _, e := Range(template, start)
		if e != nil {
			return rule, e
		}
		start = x
	} else {
		active, e := RuleForDate(db, account, Date(now))
		if e != nil {
			return rule, e
		}
		_, end, _, e := Range(active, Date(now))
		if e != nil {
			return rule, e
		}
		close, e := Parse(end)
		if e != nil {
			return rule, e
		}
		start = Date(close.AddDate(0, 0, 1))
		version = rules[len(rules)-1].Version + 1
		// Replace a not-yet-effective rule only while it has no frozen statements.
		for _, old := range rules {
			if old.EffectiveFrom >= start {
				var n int64
				if e := db.Model(&model.LiabilityStatement{}).Where("rule_id = ? AND issued_at IS NOT NULL", old.ID).Count(&n).Error; e != nil {
					return rule, e
				}
				if n > 0 {
					return rule, ErrConflict
				}
			}
		}
	}
	rule = model.LiabilityBillRule{AccountID: account, Version: version, StatementDay: day, RepaymentDay: due, RepaymentMonthOffset: offset, EffectiveFrom: start}
	if e := db.Create(&rule).Error; e != nil {
		return rule, e
	}
	var future []model.LiabilityStatement
	if e := db.Where("account_id = ? AND issued_at IS NULL AND cycle_end >= ?", account, start).Order("cycle_end").Find(&future).Error; e != nil {
		return rule, e
	}
	for _, b := range future {
		reference := b.CycleStart
		if reference < start {
			reference = start
		}
		from, end, pay, e := Range(rule, reference)
		if e != nil {
			return rule, e
		}
		if e := db.Model(&b).Updates(map[string]any{"rule_id": rule.ID, "cycle_start": from, "cycle_end": end, "due_date": pay, "bill_key": fmt.Sprintf("monthly:%d:%s", account, end), "reconciled_at": nil}).Error; e != nil {
			return rule, e
		}
	}
	dueDisplay := due
	var ordinary []model.LiabilityDebtItem
	if e := db.Where("account_id = ? AND active = ? AND plan_id IS NULL AND manual_assignment = ? AND source_txn_id IS NOT NULL", account, true, false).Find(&ordinary).Error; e != nil {
		return rule, e
	}
	for _, item := range ordinary {
		var source model.Transaction
		if e := db.First(&source, *item.SourceTxnID).Error; e != nil {
			return rule, e
		}
		if Date(source.EntryDate) < start {
			continue
		}
		bill, e := StatementFor(db, account, Date(source.EntryDate))
		if e != nil {
			return rule, e
		}
		if e := db.Model(&item).Update("statement_id", bill.ID).Error; e != nil {
			return rule, e
		}
	}
	if len(rules) > 0 && a.RepaymentDay != nil {
		dueDisplay = *a.RepaymentDay
	}
	if e := db.Model(&a).Updates(map[string]any{"billing_enabled": true, "liability_type": kind, "repayment_day": dueDisplay}).Error; e != nil {
		return rule, e
	}
	var changedStatements []model.LiabilityStatement
	if e := db.Where("account_id = ? AND issued_at IS NULL AND cycle_end >= ?", account, start).Order("cycle_end").Find(&changedStatements).Error; e != nil {
		return rule, e
	}
	result, _ := json.Marshal(map[string]any{"rule": rule, "statements": changedStatements})
	before, _ := json.Marshal(map[string]any{"rules": rules, "statements": future})
	op := model.LiabilityOperation{ActorID: actor, RequestKey: fmt.Sprintf("rule:%d:%d", account, version), Kind: "settings", AccountID: account, Reason: "账单规则配置", BeforeJSON: string(before), ResultJSON: string(result)}
	if e := db.Create(&op).Error; e != nil {
		return rule, e
	}
	return rule, nil
}
