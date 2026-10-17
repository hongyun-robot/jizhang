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

const unbilledOrdinary = "unbilled_ordinary"

type OrdinaryRepaymentBill struct {
	StatementID     int64           `json:"statement_id"`
	CycleStart      string          `json:"cycle_start"`
	CycleEnd        string          `json:"cycle_end"`
	DueDate         string          `json:"due_date"`
	AvailableAmount decimal.Decimal `json:"available_amount"`
}
type OrdinaryRepaymentPreview struct {
	Scope           string                        `json:"scope"`
	AvailableAmount decimal.Decimal               `json:"available_amount"`
	Amount          decimal.Decimal               `json:"amount"`
	Bills           []OrdinaryRepaymentBill       `json:"bills"`
	Allocations     []liability.AdvanceAllocation `json:"allocations"`
	QuoteSignature  string                        `json:"quote_signature"`
}

func (s *LiabilityService) ordinaryPreviewAt(tx *gorm.DB, actor *model.User, in LiabilityRepaymentInput) (OrdinaryRepaymentPreview, error) {
	out := OrdinaryRepaymentPreview{Scope: unbilledOrdinary, Amount: in.Amount, Bills: []OrdinaryRepaymentBill{}, Allocations: []liability.AdvanceAllocation{}}
	if !CanCreateTransaction(actor) {
		return out, ErrTxnForbidden
	}
	if in.Scope != unbilledOrdinary || !liability.ValidAmount(in.Amount, false) {
		return out, liability.ErrInvalid
	}
	if err := checkAccountOwnership(tx, actor, in.AccountID); err != nil {
		return out, err
	}
	if err := checkRepaymentAccount(tx, actor, in.FromAccountID); err != nil {
		return out, err
	}
	date, err := liability.Parse(in.EntryDate)
	if err != nil || in.EntryDate > liability.Date(s.Now()) {
		return out, liability.ErrInvalid
	}
	if err := s.txns.bound(tx).guardOpen(s.txns.bound(tx).deps.Periods.KeyForDate(date)); err != nil {
		return out, err
	}
	d, err := detailAt(tx, in.AccountID, s.Now())
	if err != nil {
		return out, err
	}
	if !d.Account.BillingEnabled {
		return out, liability.ErrSetup
	}
	items := liability.OrdinaryCandidates(d, in.EntryDate)
	bills := map[int64]model.LiabilityStatement{}
	capacity := map[int64]decimal.Decimal{}
	for _, b := range d.Statements {
		bills[b.ID] = b
	}
	for _, item := range items {
		capacity[item.StatementID] = capacity[item.StatementID].Add(item.Remaining)
		out.AvailableAmount = out.AvailableAmount.Add(item.Remaining)
	}
	seenBills := map[int64]bool{}
	for _, item := range items {
		if seenBills[item.StatementID] {
			continue
		}
		seenBills[item.StatementID] = true
		b := bills[item.StatementID]
		out.Bills = append(out.Bills, OrdinaryRepaymentBill{b.ID, b.CycleStart, b.CycleEnd, b.DueDate, capacity[b.ID]})
	}
	mode := in.Mode
	if mode == "" {
		mode = "auto"
	}
	if mode != "auto" && mode != "manual" {
		return out, liability.ErrInvalid
	}
	budgets := []liability.Target{{Amount: in.Amount}}
	if mode == "manual" {
		budgets = append([]liability.Target{}, in.Targets...)
		sum := decimal.Zero
		seen := map[int64]bool{}
		for _, target := range budgets {
			if target.StatementID == 0 || target.DebtItemID != 0 || seen[target.StatementID] || !liability.ValidAmount(target.Amount, false) {
				return out, liability.ErrInvalid
			}
			seen[target.StatementID] = true
			if target.Amount.GreaterThan(capacity[target.StatementID]) {
				return out, requestConflict(fmt.Sprintf("该账单普通可还金额为 %s，请重新核对", capacity[target.StatementID].StringFixed(2)))
			}
			sum = sum.Add(target.Amount)
		}
		if !sum.Equal(in.Amount) {
			return out, requestConflict("手动分配须完整使用本次还款金额")
		}
		sort.Slice(budgets, func(i, j int) bool { return budgets[i].StatementID < budgets[j].StatementID })
	} else if len(in.Targets) != 0 {
		return out, liability.ErrInvalid
	}
	if in.Amount.GreaterThan(out.AvailableAmount) {
		return out, requestConflict(fmt.Sprintf("未出账普通消费可还金额为 %s，请重新核对", out.AvailableAmount.StringFixed(2)))
	}
	for _, budget := range budgets {
		left := budget.Amount
		for _, item := range items {
			if !left.IsPositive() || (budget.StatementID != 0 && budget.StatementID != item.StatementID) {
				continue
			}
			take := decimal.Min(left, item.Remaining)
			out.Allocations = append(out.Allocations, liability.AdvanceAllocation{StatementID: item.StatementID, DebtItemID: item.ID, SourceID: item.SourceID, Amount: take})
			left = left.Sub(take)
		}
		if !left.IsZero() {
			return out, liability.ErrConflict
		}
	}
	sources := []model.Transaction{}
	for _, item := range items {
		for _, source := range d.Transactions {
			if item.SourceID == source.ID {
				sources = append(sources, source)
				break
			}
		}
	}
	encoded, err := json.Marshal(struct {
		Actor   int64
		From    int64
		Account int64
		Date    string
		Mode    string
		Targets []liability.Target
		Items   []model.LiabilityDebtItem
		Sources []model.Transaction
		Preview OrdinaryRepaymentPreview
	}{actor.ID, in.FromAccountID, in.AccountID, in.EntryDate, mode, budgets, items, sources, out})
	if err != nil {
		return out, err
	}
	sum := sha256.Sum256(encoded)
	out.QuoteSignature = hex.EncodeToString(sum[:])
	return out, nil
}

func (s *LiabilityService) OrdinaryRepaymentPreview(actor *model.User, in LiabilityRepaymentInput) (OrdinaryRepaymentPreview, error) {
	// A read transaction gives the preview a coherent snapshot without LedgerWrite.
	var out OrdinaryRepaymentPreview
	err := s.db.Transaction(func(tx *gorm.DB) error {
		var err error
		out, err = s.ordinaryPreviewAt(tx, actor, in)
		return err
	})
	return out, err
}

func (s *LiabilityService) repayOrdinaryAdvance(tx *gorm.DB, actor *model.User, in LiabilityRepaymentInput, op *model.LiabilityOperation) (LiabilityResult, error) {
	preview, err := s.ordinaryPreviewAt(tx, actor, in)
	if err != nil {
		return LiabilityResult{}, err
	}
	if in.QuoteSignature == "" || preview.QuoteSignature != in.QuoteSignature {
		return LiabilityResult{}, requestConflict("欠款或账单已变化，请重新核对还款金额")
	}
	targets := []liability.Target{}
	for _, a := range preview.Allocations {
		targets = append(targets, liability.Target{DebtItemID: a.DebtItemID, Amount: a.Amount})
	}
	raw, err := json.Marshal(targets)
	if err != nil {
		return LiabilityResult{}, err
	}
	date, _ := liability.Parse(in.EntryDate)
	t := model.Transaction{Kind: model.TxnTransfer, AccountID: in.FromAccountID, TransferAccountID: &in.AccountID, Amount: in.Amount, EntryDate: date, PeriodKey: s.txns.bound(tx).deps.Periods.KeyForDate(date), CreatedBy: actor.ID, Source: model.SourceManual, Note: in.Reason, LiabilityOperationID: &op.ID}
	if err := repository.NewTransactionRepo(tx).Create(&t); err != nil {
		return LiabilityResult{}, err
	}
	if err := tx.Model(&model.LiabilityCreditEvent{}).Where("account_id = ? AND source_id = ?", in.AccountID, t.ID).Updates(map[string]any{"mode": liability.OrdinaryAdvance, "targets": string(raw)}).Error; err != nil {
		return LiabilityResult{}, err
	}
	return LiabilityResult{TransactionIDs: []int64{t.ID}, RepaymentScope: unbilledOrdinary, AllocationMode: in.Mode, RepaymentTargets: preview.Allocations}, nil
}
