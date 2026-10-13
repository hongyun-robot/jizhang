package service

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/shopspring/decimal"
	"gorm.io/gorm"

	"jizhang/internal/liability"
	"jizhang/internal/model"
	"jizhang/internal/repository"
)

type InstallmentRepaymentInput struct {
	RequestKey     string           `json:"request_key"`
	Reason         string           `json:"reason"`
	Version        int              `json:"version"`
	TermIDs        []int64          `json:"term_ids"`
	AllRemaining   bool             `json:"all_remaining"`
	FromAccountID  int64            `json:"from_account_id"`
	EntryDate      string           `json:"entry_date"`
	QuoteSignature string           `json:"quote_signature"`
	TotalAmount    *decimal.Decimal `json:"total_amount,omitempty"`
}

// Old fields remain decodable so old requests fail explicitly and completed
// historical operations can still be replayed; new writes cannot use them.
type SettlementInput struct {
	RequestKey     string           `json:"request_key"`
	Reason         string           `json:"reason"`
	Version        int              `json:"version"`
	TermIDs        []int64          `json:"term_ids"`
	AllRemaining   bool             `json:"all_remaining"`
	FromAccountID  int64            `json:"from_account_id"`
	EntryDate      string           `json:"entry_date"`
	QuoteSignature string           `json:"quote_signature"`
	TotalAmount    *decimal.Decimal `json:"total_amount,omitempty"`
	Principal      decimal.Decimal  `json:"principal,omitempty"`
	FeeAmount      decimal.Decimal  `json:"fee_amount,omitempty"`
	FeeAccountID   int64            `json:"fee_account_id,omitempty"`
	FeeCategoryID  int64            `json:"fee_category_id,omitempty"`
}

type InstallmentRepaymentTerm struct {
	ID              int64           `json:"id"`
	Number          int             `json:"number"`
	Principal       decimal.Decimal `json:"principal"`
	OutstandingFees decimal.Decimal `json:"outstanding_fees"`
	ScheduledFees   decimal.Decimal `json:"scheduled_fees"`
	TotalAmount     decimal.Decimal `json:"total_amount"`
}

type SettlementPreview struct {
	PlanID          int64                      `json:"plan_id"`
	Version         int                        `json:"version"`
	Terms           []InstallmentRepaymentTerm `json:"terms"`
	Principal       decimal.Decimal            `json:"principal"`
	PaidPrincipal   decimal.Decimal            `json:"paid_principal"`
	OutstandingFees decimal.Decimal            `json:"outstanding_fees"`
	ScheduledFees   decimal.Decimal            `json:"scheduled_fees"`
	TotalAmount     decimal.Decimal            `json:"total_amount"`
	TransferAmount  decimal.Decimal            `json:"transfer_amount"`
	QuoteSignature  string                     `json:"quote_signature"`
	CancelledFees   decimal.Decimal            `json:"-"`
	PendingCredit   decimal.Decimal            `json:"-"`
	FeeAmount       decimal.Decimal            `json:"-"`
}

func settlementRepayment(in SettlementInput) (InstallmentRepaymentInput, error) {
	if !in.Principal.IsZero() || !in.FeeAmount.IsZero() || in.FeeAccountID != 0 || in.FeeCategoryID != 0 {
		return InstallmentRepaymentInput{}, requestConflict("结清规则已更新，请重新核对完整期次及全部未付手续费")
	}
	if len(in.TermIDs) > 0 {
		return InstallmentRepaymentInput{}, liability.ErrInvalid
	}
	return InstallmentRepaymentInput{RequestKey: in.RequestKey, Reason: in.Reason, Version: in.Version, AllRemaining: true, FromAccountID: in.FromAccountID, EntryDate: in.EntryDate, QuoteSignature: in.QuoteSignature, TotalAmount: in.TotalAmount}, nil
}

func (s *LiabilityService) SettlementPreview(actor *model.User, id int64, in SettlementInput) (SettlementPreview, error) {
	input, err := settlementRepayment(in)
	if err != nil {
		return SettlementPreview{}, err
	}
	return s.InstallmentRepaymentPreview(actor, id, input)
}

func (s *LiabilityService) Settle(actor *model.User, id int64, in SettlementInput) (LiabilityResult, error) {
	if !CanCreateTransaction(actor) {
		return LiabilityResult{}, ErrTxnForbidden
	}
	// Preserve the exact old digest for an already completed historical request.
	if in.RequestKey != "" {
		var p model.InstallmentPlan
		if err := s.db.First(&p, id).Error; err != nil {
			return LiabilityResult{}, err
		}
		var prior model.LiabilityOperation
		if err := s.db.Where("actor_id = ? AND request_key = ? AND kind = ?", actor.ID, in.RequestKey, "settle").First(&prior).Error; err == nil {
			var recorded LiabilityResult
			if err := json.Unmarshal([]byte(prior.ResultJSON), &recorded); err != nil {
				return LiabilityResult{}, err
			}
			if len(recorded.TermIDs) == 0 {
				legacy := struct {
					RequestKey    string          `json:"request_key"`
					Reason        string          `json:"reason"`
					Version       int             `json:"version"`
					Principal     decimal.Decimal `json:"principal"`
					FeeAmount     decimal.Decimal `json:"fee_amount"`
					FeeAccountID  int64           `json:"fee_account_id"`
					FeeCategoryID int64           `json:"fee_category_id"`
					FromAccountID int64           `json:"from_account_id"`
					EntryDate     string          `json:"entry_date"`
				}{in.RequestKey, in.Reason, in.Version, in.Principal, in.FeeAmount, in.FeeAccountID, in.FeeCategoryID, in.FromAccountID, in.EntryDate}
				return s.operation(actor, p.AccountID, "settle", in.RequestKey, in.Reason, legacy, func(*gorm.DB, *model.LiabilityOperation) (LiabilityResult, error) {
					return LiabilityResult{}, liability.ErrConflict
				})
			}
		}
	}
	input, err := settlementRepayment(in)
	if err != nil {
		return LiabilityResult{}, err
	}
	return s.repayInstallment(actor, id, input, "settle")
}

func termQuote(d *liability.Detail, term model.InstallmentTerm) InstallmentRepaymentTerm {
	out := InstallmentRepaymentTerm{ID: term.ID, Number: term.Number, Principal: term.Remaining}
	for _, item := range d.Items {
		if item.TermID != nil && *item.TermID == term.ID && item.Kind == "fee" {
			out.OutstandingFees = out.OutstandingFees.Add(item.Remaining)
		}
	}
	if term.FeeTxnID == nil && term.FeeStatus != "cancelled" {
		out.ScheduledFees = term.FeeAmount
	}
	out.TotalAmount = out.Principal.Add(out.OutstandingFees).Add(out.ScheduledFees)
	return out
}

func selectRepaymentTerms(d *liability.Detail, p model.InstallmentPlan, in InstallmentRepaymentInput) ([]InstallmentRepaymentTerm, error) {
	if p.Status == "settled" || p.Status == "needs_reconcile" {
		return nil, requestConflict("计划已结清或退款期次尚未核对")
	}
	if in.AllRemaining == (len(in.TermIDs) > 0) {
		return nil, liability.ErrInvalid
	}
	selected := map[int64]bool{}
	for _, id := range in.TermIDs {
		if id <= 0 || selected[id] {
			return nil, liability.ErrInvalid
		}
		selected[id] = true
	}
	terms := append([]model.InstallmentTerm(nil), p.Terms...)
	sort.Slice(terms, func(i, j int) bool { return terms[i].Number < terms[j].Number })
	result := []InstallmentRepaymentTerm{}
	gap := false
	for _, t := range terms {
		quote := termQuote(d, t)
		if !quote.TotalAmount.IsPositive() {
			if selected[t.ID] {
				return nil, requestConflict("不能选择已经还清的期次")
			}
			continue
		}
		if !in.AllRemaining && !selected[t.ID] {
			gap = true
			continue
		}
		if gap {
			return nil, requestConflict("请从最早未还期开始选择连续期数")
		}
		if !quote.Principal.IsPositive() {
			return nil, requestConflict("该期只有未付手续费，请先核对记录，不支持单独还手续费")
		}
		result = append(result, quote)
		delete(selected, t.ID)
	}
	if len(selected) != 0 || len(result) == 0 {
		return nil, liability.ErrInvalid
	}
	return result, nil
}

func (s *LiabilityService) repaymentPreviewAt(tx *gorm.DB, actor *model.User, id int64, in InstallmentRepaymentInput) (SettlementPreview, *liability.Detail, model.InstallmentPlan, error) {
	out := SettlementPreview{}
	var p model.InstallmentPlan
	if !CanCreateTransaction(actor) {
		return out, nil, p, ErrTxnForbidden
	}
	if err := tx.First(&p, id).Error; err != nil {
		return out, nil, p, err
	}
	if err := checkAccountOwnership(tx, actor, p.AccountID); err != nil {
		return out, nil, p, err
	}
	if !CanModifyTransaction(actor, p.CreatedBy) {
		return out, nil, p, ErrTxnForbidden
	}
	date, err := liability.Parse(in.EntryDate)
	if err != nil || in.EntryDate > liability.Date(s.Now()) {
		return out, nil, p, requestConflict("还款日期须为今天或过去的实际日期")
	}
	var source model.Transaction
	if err := tx.First(&source, p.SourceTxnID).Error; err != nil {
		return out, nil, p, err
	}
	if in.EntryDate < liability.Date(source.EntryDate) {
		return out, nil, p, requestConflict("还款日期不能早于原消费")
	}
	if err := checkRepaymentAccount(tx, actor, in.FromAccountID); err != nil {
		return out, nil, p, err
	}
	var from model.Account
	if err := tx.First(&from, in.FromAccountID).Error; err != nil {
		return out, nil, p, err
	}
	if !from.IsCash() || from.Status != model.StatusActive {
		return out, nil, p, ErrTxnAccountInvalid
	}
	if err := s.txns.bound(tx).guardOpen(s.txns.bound(tx).deps.Periods.KeyForDate(date)); err != nil {
		return out, nil, p, err
	}
	d, err := detailAt(tx, p.AccountID, s.Now())
	if err != nil {
		return out, nil, p, err
	}
	for _, plan := range d.Plans {
		if plan.ID == p.ID {
			p = plan
		}
	}
	terms, err := selectRepaymentTerms(d, p, in)
	if err != nil {
		return out, nil, p, err
	}
	selected := map[int64]bool{}
	for _, t := range terms {
		selected[t.ID] = true
		out.Principal = out.Principal.Add(t.Principal)
		out.OutstandingFees = out.OutstandingFees.Add(t.OutstandingFees)
		out.ScheduledFees = out.ScheduledFees.Add(t.ScheduledFees)
	}
	for _, t := range p.Terms {
		if selected[t.ID] && (t.FeeStatus == "blocked" || (t.FeeTxnID == nil && t.FeeAmount.IsPositive() && t.FeeStatus != "cancelled" && t.FeeDate <= liability.Date(s.Now()))) {
			return out, nil, p, requestConflict("到期手续费尚未入账，请先补齐或解决封账及配置问题")
		}
	}
	if out.ScheduledFees.IsPositive() {
		if err := checkCategoryOwnership(tx, actor, p.FeeCategoryID); err != nil {
			return out, nil, p, err
		}
		var category model.Category
		var children int64
		if err := tx.First(&category, p.FeeCategoryID).Error; err != nil {
			return out, nil, p, err
		}
		if err := tx.Model(&model.Category{}).Where("parent_id = ?", category.ID).Count(&children).Error; err != nil {
			return out, nil, p, err
		}
		if category.Kind != model.KindExpense || children != 0 {
			return out, nil, p, ErrTxnCategoryInvalid
		}
	}
	out.PlanID, out.Version, out.Terms = p.ID, p.Version, terms
	principalItems := map[int64]bool{}
	creditSources := map[int64]int64{}
	sourceKinds := map[int64]model.TxnKind{}
	for _, item := range d.Items {
		principalItems[item.ID] = item.PlanID != nil && *item.PlanID == p.ID && item.Kind == "principal"
	}
	for _, credit := range d.Credits {
		creditSources[credit.ID] = credit.SourceID
	}
	for _, transaction := range d.Transactions {
		sourceKinds[transaction.ID] = transaction.Kind
	}
	for _, allocation := range d.Allocations {
		if principalItems[allocation.DebtItemID] && sourceKinds[creditSources[allocation.CreditEventID]] != model.TxnRefund {
			out.PaidPrincipal = out.PaidPrincipal.Add(allocation.Amount)
		}
	}
	out.TotalAmount = out.Principal.Add(out.OutstandingFees).Add(out.ScheduledFees)
	out.TransferAmount = out.TotalAmount
	if !liability.ValidAmount(out.TotalAmount, false) {
		return out, nil, p, liability.ErrInvalid
	}
	items := []model.LiabilityDebtItem{}
	allocations := []model.LiabilityAllocation{}
	itemIDs := map[int64]bool{}
	for _, item := range d.Items {
		if item.TermID != nil && selected[*item.TermID] {
			items = append(items, item)
			itemIDs[item.ID] = true
		}
	}
	for _, a := range d.Allocations {
		if itemIDs[a.DebtItemID] {
			allocations = append(allocations, a)
		}
	}
	sort.Slice(allocations, func(i, j int) bool { return allocations[i].ID < allocations[j].ID })
	encoded, err := json.Marshal(struct {
		Quote       SettlementPreview
		Terms       []model.InstallmentTerm
		Items       []model.LiabilityDebtItem
		Allocations []model.LiabilityAllocation
		From        int64
		Date        string
	}{out, p.Terms, items, allocations, in.FromAccountID, in.EntryDate})
	if err != nil {
		return out, nil, p, err
	}
	signature := sha256.Sum256(encoded)
	out.QuoteSignature = hex.EncodeToString(signature[:])
	return out, d, p, nil
}

func (s *LiabilityService) InstallmentRepaymentPreview(actor *model.User, id int64, in InstallmentRepaymentInput) (SettlementPreview, error) {
	if err := liability.CheckRepaymentCutover(s.db); err != nil {
		return SettlementPreview{}, err
	}
	out, _, _, err := s.repaymentPreviewAt(s.db, actor, id, in)
	return out, err
}

func (s *LiabilityService) RepayInstallment(actor *model.User, id int64, in InstallmentRepaymentInput) (LiabilityResult, error) {
	return s.repayInstallment(actor, id, in, "installment_repayment")
}

func (s *LiabilityService) repayInstallment(actor *model.User, id int64, in InstallmentRepaymentInput, kind string) (LiabilityResult, error) {
	var p model.InstallmentPlan
	if err := s.db.First(&p, id).Error; err != nil {
		return LiabilityResult{}, err
	}
	return s.operation(actor, p.AccountID, kind, in.RequestKey, in.Reason, in, func(tx *gorm.DB, op *model.LiabilityOperation) (LiabilityResult, error) {
		quote, d, plan, err := s.repaymentPreviewAt(tx, actor, id, in)
		if err != nil {
			return LiabilityResult{}, err
		}
		if in.Version != quote.Version || in.QuoteSignature == "" || in.QuoteSignature != quote.QuoteSignature || (in.TotalAmount != nil && !in.TotalAmount.Equal(quote.TotalAmount)) {
			return LiabilityResult{}, requestConflict("还款预览已变化，请重新核对期次和全部未付手续费")
		}
		op.PlanID = &plan.ID
		if err := tx.Model(op).Update("plan_id", plan.ID).Error; err != nil {
			return LiabilityResult{}, err
		}
		return s.payQuotedTerms(tx, actor, op, d, plan, quote, in.FromAccountID, in.EntryDate, nil)
	})
}

func (s *LiabilityService) payQuotedTerms(tx *gorm.DB, actor *model.User, op *model.LiabilityOperation, before *liability.Detail, p model.InstallmentPlan, quote SettlementPreview, from int64, entryDate string, existing *model.Transaction) (LiabilityResult, error) {
	result := LiabilityResult{PlanID: p.ID, TransactionIDs: []int64{}, TermIDs: []int64{}, CreditSignatures: map[int64]string{}}
	selected := map[int64]bool{}
	for _, t := range quote.Terms {
		selected[t.ID] = true
		result.TermIDs = append(result.TermIDs, t.ID)
	}
	if err := s.fixRepaymentSources(tx, actor, before, selected, &result); err != nil {
		return result, err
	}
	targets := []liability.Target{}
	for _, item := range before.Items {
		if item.TermID != nil && selected[*item.TermID] && item.Remaining.IsPositive() {
			targets = append(targets, liability.Target{DebtItemID: item.ID, Amount: item.Remaining})
		}
	}
	for _, term := range p.Terms {
		if !selected[term.ID] || term.FeeTxnID != nil || term.FeeStatus == "cancelled" || !term.FeeAmount.IsPositive() {
			continue
		}
		feeID, err := s.postFeeAt(tx, &p, &term, op.ID, entryDate, p.AccountID)
		if err != nil {
			return result, err
		}
		result.TransactionIDs = append(result.TransactionIDs, feeID)
		var item model.LiabilityDebtItem
		if err := tx.Where("source_id = ? AND account_id = ? AND active = ?", feeID, p.AccountID, true).First(&item).Error; err != nil {
			return result, err
		}
		targets = append(targets, liability.Target{DebtItemID: item.ID, Amount: term.FeeAmount})
	}
	date, _ := liability.Parse(entryDate)
	if existing == nil {
		existing = &model.Transaction{Kind: model.TxnTransfer, AccountID: from, TransferAccountID: &p.AccountID, Amount: quote.TotalAmount, EntryDate: date, PeriodKey: s.txns.bound(tx).deps.Periods.KeyForDate(date), CreatedBy: actor.ID, Source: model.SourceManual, LiabilityOperationID: &op.ID, Note: "分期整期还款：本金及手续费"}
		if err := repository.NewTransactionRepo(tx).WithActor(actor.ID).Create(existing); err != nil {
			return result, err
		}
	} else {
		if err := tx.Model(existing).Update("liability_operation_id", op.ID).Error; err != nil {
			return result, err
		}
		existing.LiabilityOperationID = &op.ID
	}
	result.TransactionIDs = append(result.TransactionIDs, existing.ID)
	if err := tx.Model(&model.LiabilityCreditEvent{}).Where("account_id = ? AND source_id = ? AND active = ?", p.AccountID, existing.ID, true).Updates(map[string]any{"mode": "settlement", "targets": jsonText(targets)}).Error; err != nil {
		return result, err
	}
	var credit model.LiabilityCreditEvent
	if err := tx.Where("account_id = ? AND source_id = ? AND active = ?", p.AccountID, existing.ID, true).First(&credit).Error; err != nil {
		return result, err
	}
	result.CreditSignatures[credit.ID] = credit.Mode + ":" + credit.Targets
	if err := s.movePrepaidItems(tx, op, before, selected, entryDate); err != nil {
		return result, err
	}
	if err := tx.Model(&model.InstallmentTerm{}).Where("plan_id = ? AND id IN ?", p.ID, result.TermIDs).Update("status", "settled").Error; err != nil {
		return result, err
	}
	if err := liability.Reallocate(tx, p.AccountID); err != nil {
		return result, err
	}
	d, err := detailAt(tx, p.AccountID, s.Now())
	if err != nil {
		return result, err
	}
	remaining := decimal.Zero
	for _, plan := range d.Plans {
		if plan.ID != p.ID {
			continue
		}
		for _, term := range plan.Terms {
			x := termQuote(d, term)
			if selected[term.ID] && !x.TotalAmount.IsZero() {
				return result, liability.ErrInvariant
			}
			remaining = remaining.Add(x.TotalAmount)
		}
	}
	status := "active"
	if remaining.IsZero() {
		status = "settled"
	}
	if err := tx.Model(&model.InstallmentPlan{}).Where("id = ?", p.ID).Updates(map[string]any{"status": status, "version": gorm.Expr("version + 1")}).Error; err != nil {
		return result, err
	}
	var stored model.InstallmentPlan
	if err := tx.First(&stored, p.ID).Error; err != nil {
		return result, err
	}
	if err := tx.Where("plan_id = ?", p.ID).Order("number").Find(&stored.Terms).Error; err != nil {
		return result, err
	}
	result.PlanSnapshot = &stored
	return result, nil
}

func (s *LiabilityService) fixRepaymentSources(tx *gorm.DB, actor *model.User, d *liability.Detail, selected map[int64]bool, result *LiabilityResult) error {
	items := map[int64]model.LiabilityDebtItem{}
	for _, item := range d.Items {
		items[item.ID] = item
	}
	for _, credit := range d.Credits {
		if credit.Mode == "settlement" {
			continue
		}
		targets := []liability.Target{}
		affected := false
		for _, a := range d.Allocations {
			if a.CreditEventID != credit.ID {
				continue
			}
			targets = append(targets, liability.Target{DebtItemID: a.DebtItemID, Amount: a.Amount})
			item := items[a.DebtItemID]
			affected = affected || (item.TermID != nil && selected[*item.TermID])
		}
		if !affected {
			continue
		}
		var source model.Transaction
		if err := tx.First(&source, credit.SourceID).Error; err != nil {
			return err
		}
		if !CanModifyTransaction(actor, source.CreatedBy) {
			return ErrTxnForbidden
		}
		inner := s.txns.bound(tx)
		if err := inner.checkPostingOwnership(actor, &source); err != nil {
			return err
		}
		if err := inner.guardOpen(source.PeriodKey); err != nil {
			return err
		}
		if !credit.Remaining.IsZero() {
			return requestConflict("存在历史未分配资金，请先核对")
		}
		encoded := jsonText(targets)
		if err := tx.Model(&credit).Updates(map[string]any{"mode": "settlement", "targets": encoded}).Error; err != nil {
			return err
		}
		result.CreditSignatures[credit.ID] = "settlement:" + encoded
	}
	return nil
}

func (s *LiabilityService) movePrepaidItems(tx *gorm.DB, op *model.LiabilityOperation, d *liability.Detail, selected map[int64]bool, date string) error {
	bills := map[int64]model.LiabilityStatement{}
	for _, b := range d.Statements {
		bills[b.ID] = b
	}
	ids := []int64{}
	amount := decimal.Zero
	var items []model.LiabilityDebtItem
	termIDs := []int64{}
	for id := range selected {
		termIDs = append(termIDs, id)
	}
	if err := tx.Where("account_id = ? AND term_id IN ? AND active = ?", op.AccountID, termIDs, true).Find(&items).Error; err != nil {
		return err
	}
	for _, item := range items {
		if bills[item.StatementID].IssuedAt == nil {
			ids = append(ids, item.ID)
			amount = amount.Add(item.Amount)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	rule, err := liability.RuleForDate(tx, op.AccountID, date)
	if err != nil {
		return err
	}
	now := s.Now()
	bill := model.LiabilityStatement{AccountID: op.AccountID, RuleID: rule.ID, Kind: "settlement", BillKey: fmt.Sprintf("settlement:%d", op.ID), CycleStart: date, CycleEnd: date, DueDate: date, IssuedAt: &now, SnapshotAmount: amount}
	if err := tx.Create(&bill).Error; err != nil {
		return err
	}
	return tx.Model(&model.LiabilityDebtItem{}).Where("id IN ?", ids).Update("statement_id", bill.ID).Error
}
