package service

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
	"jizhang/internal/liability"
	"jizhang/internal/model"
	"jizhang/internal/repository"
	"strings"
	"time"
)

type LiabilityService struct {
	db           *gorm.DB
	txns         *TransactionService
	Now          func() time.Time
	BeforeEnsure func(time.Time) error
}

func NewLiabilityService(db *gorm.DB, txns *TransactionService) *LiabilityService {
	s := &LiabilityService{db: db, txns: txns, Now: time.Now}
	txns.liabilityNow = func() time.Time { return s.Now() }
	return s
}

type LiabilityResult struct {
	OperationID      int64                   `json:"operation_id"`
	PlanID           int64                   `json:"plan_id,omitempty"`
	Version          int                     `json:"version,omitempty"`
	TransactionIDs   []int64                 `json:"transaction_ids"`
	CreditSignatures map[int64]string        `json:"credit_signatures,omitempty"`
	ChangedPeriods   []string                `json:"-"`
	Assignments      []LiabilityAssignment   `json:"assignments,omitempty"`
	PlanSnapshot     *model.InstallmentPlan  `json:"plan_snapshot,omitempty"`
	TermIDs          []int64                 `json:"term_ids,omitempty"`
	PlanIDs          []int64                 `json:"plan_ids,omitempty"`
	PlanSnapshots    []model.InstallmentPlan `json:"plan_snapshots,omitempty"`
	ManagedSourceIDs []int64                 `json:"managed_source_ids,omitempty"`
	PostingSnapshots []model.Transaction     `json:"transaction_snapshots,omitempty"`
}
type LiabilityAssignment struct {
	ItemID   int64           `json:"item_id"`
	SourceID int64           `json:"source_id"`
	From     int64           `json:"from_statement_id"`
	To       int64           `json:"to_statement_id"`
	Amount   decimal.Decimal `json:"amount"`
}

func (s *LiabilityService) List(actor *model.User) ([]liability.Summary, error) {
	if !CanView(actor) {
		return nil, ErrTxnForbidden
	}
	accounts, e := NewAccountService(repository.NewAccountRepo(s.db).ForType(model.AccountLiability)).ListFor(actor)
	if e != nil {
		return nil, e
	}
	selected := []model.Account{}
	for _, a := range accounts {
		if a.Type == model.AccountLiability {
			selected = append(selected, a)
		}
	}
	states, e := liability.Load(s.db, selected, s.Now())
	if e != nil {
		return nil, e
	}
	result := []liability.Summary{}
	for _, a := range selected {
		result = append(result, states[a.ID].Summary)
	}
	return result, nil
}
func detailAt(db *gorm.DB, id int64, now time.Time) (*liability.Detail, error) {
	accounts, e := repository.NewAccountRepo(db).ForType(model.AccountLiability).List()
	if e != nil {
		return nil, e
	}
	for _, a := range accounts {
		if a.ID == id && a.Type == model.AccountLiability {
			states, e := liability.Load(db, []model.Account{a}, now)
			if e != nil {
				return nil, e
			}
			return states[id], nil
		}
	}
	return nil, ErrAccountNotFound
}
func (s *LiabilityService) Detail(actor *model.User, id int64) (*liability.Detail, error) {
	if !CanView(actor) {
		return nil, ErrTxnForbidden
	}
	if e := checkAccountOwnership(s.db, actor, id); e != nil {
		return nil, e
	}
	detail, e := detailAt(s.db, id, s.Now())
	if e != nil {
		return nil, e
	}
	if e = s.txns.redactHistory(actor, detail.Transactions); e != nil {
		return nil, e
	}
	return detail, nil
}
func (s *LiabilityService) Plan(actor *model.User, id int64) (model.InstallmentPlan, error) {
	var p model.InstallmentPlan
	if !CanView(actor) {
		return p, ErrTxnForbidden
	}
	p, e := repository.NewLiabilityRepo(s.db).Plan(id)
	if errors.Is(e, gorm.ErrRecordNotFound) {
		return p, ErrTxnNotFound
	}
	if e != nil {
		return p, e
	}
	if e := checkAccountOwnership(s.db, actor, p.AccountID); e != nil {
		return p, e
	}
	d, e := detailAt(s.db, p.AccountID, s.Now())
	if e != nil {
		return p, e
	}
	for _, plan := range d.Plans {
		if plan.ID == id {
			return plan, nil
		}
	}
	return p, ErrTxnNotFound
}

func (s *LiabilityService) operation(actor *model.User, account int64, kind, key, reason string, input any, fn func(*gorm.DB, *model.LiabilityOperation) (LiabilityResult, error)) (result LiabilityResult, err error) {
	if !CanCreateTransaction(actor) {
		return result, ErrTxnForbidden
	}
	if len(key) < 8 || len(key) > 100 || strings.TrimSpace(key) != key || len([]rune(reason)) > 255 {
		return result, liability.ErrInvalid
	}
	raw, e := json.Marshal(input)
	if e != nil {
		return result, e
	}
	sum := sha256.Sum256(append([]byte(kind+":"), raw...))
	digest := hex.EncodeToString(sum[:])
	dirty := false
	periods := map[string]bool{}
	err = repository.LedgerWrite(s.db, func(tx *gorm.DB) error {
		if e := checkAccountOwnership(tx, actor, account); e != nil {
			return e
		}
		var prior model.LiabilityOperation
		e := tx.Where("actor_id = ? AND request_key = ?", actor.ID, key).First(&prior).Error
		if e == nil {
			if prior.Digest != digest || prior.AccountID != account || prior.Kind != kind {
				return liability.ErrConflict
			}
			return json.Unmarshal([]byte(prior.ResultJSON), &result)
		}
		if !errors.Is(e, gorm.ErrRecordNotFound) {
			return e
		}
		before, e := detailAt(tx, account, s.Now())
		if e != nil {
			return e
		}
		if !before.Account.BillingEnabled {
			return liability.ErrSetup
		}
		beforeJSON, e := json.Marshal(operationSnapshot(before))
		if e != nil {
			return e
		}
		op := model.LiabilityOperation{ActorID: actor.ID, RequestKey: key, Digest: digest, Kind: kind, AccountID: account, Reason: reason, BeforeJSON: string(beforeJSON), ResultJSON: "{}"}
		if e := tx.Create(&op).Error; e != nil {
			return e
		}
		result, e = fn(tx, &op)
		if e != nil {
			return e
		}
		result.OperationID = op.ID
		if result.PlanID > 0 {
			op.PlanID = &result.PlanID
			var p model.InstallmentPlan
			if e := tx.First(&p, result.PlanID).Error; e != nil {
				return e
			}
			result.Version = p.Version
		}
		if e := liability.Reallocate(tx, account); e != nil {
			return e
		}
		var a model.Account
		if e := tx.First(&a, account).Error; e != nil {
			return e
		}
		if e := liability.CheckInvariant(tx, a); e != nil {
			return e
		}
		if e := liability.RefreshReminders(tx, s.Now(), false); e != nil {
			return e
		}
		if result.PlanID > 0 {
			var p model.InstallmentPlan
			if e := tx.First(&p, result.PlanID).Error; e != nil {
				return e
			}
			if e := tx.Where("plan_id = ?", p.ID).Order("number").Find(&p.Terms).Error; e != nil {
				return e
			}
			result.PlanSnapshot = &p
		}
		for _, id := range result.TransactionIDs {
			var t model.Transaction
			if e := tx.First(&t, id).Error; e != nil {
				return e
			}
			result.PostingSnapshots = append(result.PostingSnapshots, t)
			periods[t.PeriodKey] = true
			dirty = true
		}
		for _, key := range result.ChangedPeriods {
			periods[key] = true
			dirty = true
		}
		encoded, e := json.Marshal(result)
		if e != nil {
			return e
		}
		op.ResultJSON = string(encoded)
		return tx.Save(&op).Error
	})
	if err == nil && dirty {
		s.txns.fireWrite()
		keys := []string{}
		for key := range periods {
			keys = append(keys, key)
		}
		s.txns.firePeriods(keys...)
	}
	return
}

// Prior operations are already durable audit rows. Do not recursively embed
// their complete before/result JSON in every subsequent financial snapshot.
func operationSnapshot(detail *liability.Detail) *liability.Detail {
	snapshot := *detail
	snapshot.Operations = []model.LiabilityOperation{}
	return &snapshot
}

type LiabilitySettingsInput struct {
	LiabilityType        string `json:"liability_type"`
	StatementDay         int    `json:"statement_day"`
	RepaymentDay         int    `json:"repayment_day"`
	RepaymentMonthOffset int    `json:"repayment_month_offset"`
}

func (s *LiabilityService) Settings(actor *model.User, id int64, in LiabilitySettingsInput) (out model.LiabilityBillRule, err error) {
	if !CanView(actor) {
		return out, ErrTxnForbidden
	}
	err = repository.LedgerWrite(s.db, func(tx *gorm.DB) error {
		if e := checkAccountOwnership(tx, actor, id); e != nil {
			return e
		}
		var e error
		out, e = liability.Configure(tx, id, in.LiabilityType, in.StatementDay, in.RepaymentDay, in.RepaymentMonthOffset, actor.ID, s.Now())
		if e != nil {
			return e
		}
		if e := liability.Reallocate(tx, id); e != nil {
			return e
		}
		return liability.RefreshReminders(tx, s.Now(), false)
	})
	return
}

type LiabilityRepaymentInput struct {
	AccountID     int64              `json:"account_id"`
	FromAccountID int64              `json:"from_account_id"`
	Amount        decimal.Decimal    `json:"amount"`
	EntryDate     string             `json:"entry_date"`
	RequestKey    string             `json:"request_key"`
	Mode          string             `json:"mode"`
	Targets       []liability.Target `json:"targets"`
	Reason        string             `json:"reason"`
}

func (s *LiabilityService) Repay(actor *model.User, in LiabilityRepaymentInput) (LiabilityResult, error) {
	return s.operation(actor, in.AccountID, "repayment", in.RequestKey, in.Reason, in, func(tx *gorm.DB, op *model.LiabilityOperation) (LiabilityResult, error) {
		date, e := liability.Parse(in.EntryDate)
		if e != nil || !liability.ValidAmount(in.Amount, false) {
			return LiabilityResult{}, liability.ErrInvalid
		}
		mode := in.Mode
		if mode == "" {
			mode = "auto"
		}
		if mode != "auto" && mode != "manual" {
			return LiabilityResult{}, requestConflict("不支持暂不分配，还款资金必须当次完整使用")
		}
		if mode != "manual" && len(in.Targets) > 0 {
			return LiabilityResult{}, liability.ErrInvalid
		}
		inner := s.txns.bound(tx)
		var to model.Account
		if e := tx.First(&to, in.AccountID).Error; e != nil {
			return LiabilityResult{}, e
		}
		var from model.Account
		if e := checkRepaymentAccount(tx, actor, in.FromAccountID); e != nil {
			return LiabilityResult{}, e
		}
		if e := tx.First(&from, in.FromAccountID).Error; e != nil {
			return LiabilityResult{}, e
		}
		if !from.IsCash() || from.Status != model.StatusActive || !to.BillingEnabled || from.ID == to.ID {
			return LiabilityResult{}, liability.ErrInvalid
		}
		period := inner.deps.Periods.KeyForDate(date)
		if e := inner.guardOpen(period); e != nil {
			return LiabilityResult{}, e
		}
		prepared, e := s.prepareOrdinaryRepayment(tx, actor, to.ID, from.ID, in.Amount, in.EntryDate, mode, in.Targets, op)
		if e != nil {
			return LiabilityResult{}, e
		}
		targetJSON := "[]"
		if prepared == nil {
			targetJSON, e = liability.TargetsJSON(tx, in.AccountID, in.Amount, in.Targets, false)
			if e != nil {
				return LiabilityResult{}, e
			}
		}
		t := model.Transaction{Kind: model.TxnTransfer, Amount: in.Amount, AccountID: from.ID, TransferAccountID: &to.ID, EntryDate: date, PeriodKey: period, CreatedBy: actor.ID, Source: model.SourceManual, Note: in.Reason}
		if prepared != nil {
			t.LiabilityOperationID = &op.ID
		}
		if e := repository.NewTransactionRepo(tx).Create(&t); e != nil {
			return LiabilityResult{}, e
		}
		if prepared != nil {
			return prepared.finish(tx, actor, &t, true)
		}
		if e := tx.Model(&model.LiabilityCreditEvent{}).Where("account_id = ? AND source_id = ?", to.ID, t.ID).Updates(map[string]any{"mode": mode, "targets": targetJSON}).Error; e != nil {
			return LiabilityResult{}, e
		}
		return LiabilityResult{TransactionIDs: []int64{t.ID}}, nil
	})
}

type LiabilityAllocationInput struct {
	RequestKey string             `json:"request_key"`
	Reason     string             `json:"reason"`
	Mode       string             `json:"mode"`
	Targets    []liability.Target `json:"targets"`
}

func (s *LiabilityService) Allocate(actor *model.User, txnID int64, in LiabilityAllocationInput) (LiabilityResult, error) {
	var t model.Transaction
	if e := s.db.First(&t, txnID).Error; e != nil {
		return LiabilityResult{}, e
	}
	if t.Kind != model.TxnTransfer || t.TransferAccountID == nil {
		return LiabilityResult{}, liability.ErrInvalid
	}
	return s.operation(actor, *t.TransferAccountID, "allocation", in.RequestKey, in.Reason, in, func(tx *gorm.DB, op *model.LiabilityOperation) (LiabilityResult, error) {
		var source model.Transaction
		if e := tx.First(&source, txnID).Error; e != nil {
			return LiabilityResult{}, e
		}
		if !CanModifyTransaction(actor, source.CreatedBy) {
			return LiabilityResult{}, ErrTxnForbidden
		}
		if e := s.txns.bound(tx).checkPostingOwnership(actor, &source); e != nil {
			return LiabilityResult{}, e
		}
		if e := s.txns.bound(tx).guardOpen(source.PeriodKey); e != nil {
			return LiabilityResult{}, e
		}
		if source.LiabilityOperationID != nil {
			return LiabilityResult{}, liability.ErrManaged
		}
		var c model.LiabilityCreditEvent
		if e := tx.Where("source_id = ? AND account_id = ? AND active = ?", txnID, *t.TransferAccountID, true).First(&c).Error; e != nil {
			return LiabilityResult{}, e
		}
		if c.Mode == "settlement" {
			return LiabilityResult{}, liability.ErrManaged
		}
		if e := tx.Model(&model.LiabilityAllocation{}).Where("credit_event_id = ? AND active = ?", c.ID, true).Updates(map[string]any{"active": false, "voided_at": s.Now()}).Error; e != nil {
			return LiabilityResult{}, e
		}
		mode := in.Mode
		if mode != "auto" && mode != "manual" {
			return LiabilityResult{}, requestConflict("不支持暂不分配，还款资金必须当次完整使用")
		}
		if mode != "manual" && len(in.Targets) > 0 {
			return LiabilityResult{}, liability.ErrInvalid
		}
		prepared, e := s.prepareOrdinaryRepayment(tx, actor, c.AccountID, source.AccountID, c.Amount, liability.Date(source.EntryDate), mode, in.Targets, op)
		if e != nil {
			return LiabilityResult{}, e
		}
		if prepared != nil {
			return prepared.finish(tx, actor, &source, false)
		}
		targets, e := liability.TargetsJSON(tx, c.AccountID, c.Amount, in.Targets, false)
		if e != nil {
			return LiabilityResult{}, e
		}
		return LiabilityResult{}, tx.Model(&c).Updates(map[string]any{"mode": mode, "targets": targets}).Error
	})
}

type LiabilityReconcileInput struct {
	RequestKey      string           `json:"request_key"`
	Reason          string           `json:"reason"`
	ReferenceAmount *decimal.Decimal `json:"reference_amount"`
	Complete        bool             `json:"complete"`
	Moves           []struct {
		ItemID      int64 `json:"item_id"`
		StatementID int64 `json:"statement_id"`
	} `json:"moves"`
}

func (s *LiabilityService) Reconcile(actor *model.User, id int64, in LiabilityReconcileInput) (LiabilityResult, error) {
	var bill model.LiabilityStatement
	if e := s.db.First(&bill, id).Error; e != nil {
		return LiabilityResult{}, e
	}
	return s.operation(actor, bill.AccountID, "reconcile", in.RequestKey, in.Reason, in, func(tx *gorm.DB, op *model.LiabilityOperation) (LiabilityResult, error) {
		result := LiabilityResult{Assignments: []LiabilityAssignment{}}
		if len(in.Moves) > 0 && strings.TrimSpace(in.Reason) == "" {
			return LiabilityResult{}, liability.ErrInvalid
		}
		if in.ReferenceAmount != nil && !liability.ValidAmount(*in.ReferenceAmount, true) {
			return LiabilityResult{}, liability.ErrInvalid
		}
		affected := map[int64]bool{id: true}
		for _, move := range in.Moves {
			var d model.LiabilityDebtItem
			var b model.LiabilityStatement
			if e := tx.First(&d, move.ItemID).Error; e != nil {
				return LiabilityResult{}, e
			}
			if e := tx.First(&b, move.StatementID).Error; e != nil {
				return LiabilityResult{}, e
			}
			if d.AccountID != bill.AccountID || b.AccountID != bill.AccountID || !d.Active || d.PlanID != nil {
				return LiabilityResult{}, liability.ErrInvalid
			}
			var source model.Transaction
			if d.SourceTxnID == nil {
				return LiabilityResult{}, liability.ErrConflict
			}
			if e := tx.First(&source, *d.SourceTxnID).Error; e != nil {
				return LiabilityResult{}, e
			}
			if !CanModifyTransaction(actor, source.CreatedBy) {
				return LiabilityResult{}, ErrTxnForbidden
			}
			affected[d.StatementID] = true
			affected[b.ID] = true
			result.Assignments = append(result.Assignments, LiabilityAssignment{ItemID: d.ID, SourceID: d.SourceID, From: d.StatementID, To: b.ID, Amount: d.Amount})
			if e := tx.Model(&d).Updates(map[string]any{"statement_id": b.ID, "manual_assignment": true}).Error; e != nil {
				return LiabilityResult{}, e
			}
		}
		for b := range affected {
			if e := tx.Model(&model.LiabilityStatement{}).Where("id = ?", b).Update("reconciled_at", nil).Error; e != nil {
				return LiabilityResult{}, e
			}
		}
		if e := tx.Model(&model.LiabilityStatement{}).Where("id = ?", id).Update("reference_amount", in.ReferenceAmount).Error; e != nil {
			return LiabilityResult{}, e
		}
		if e := liability.Reallocate(tx, bill.AccountID); e != nil {
			return LiabilityResult{}, e
		}
		if in.Complete {
			detail, e := detailAt(tx, bill.AccountID, s.Now())
			if e != nil {
				return LiabilityResult{}, e
			}
			for _, p := range detail.Plans {
				if p.Status == "needs_reconcile" {
					return LiabilityResult{}, liability.ErrConflict
				}
				for _, term := range p.Terms {
					if term.StatementID == id && term.FeeStatus == "blocked" {
						return LiabilityResult{}, liability.ErrConflict
					}
				}
			}
			for _, b := range detail.Statements {
				if b.ID == id && in.ReferenceAmount != nil && !in.ReferenceAmount.Equal(b.Amount) {
					return LiabilityResult{}, liability.ErrConflict
				}
			}
			if e := tx.Model(&model.LiabilityStatement{}).Where("id = ?", id).Update("reconciled_at", s.Now()).Error; e != nil {
				return LiabilityResult{}, e
			}
		}
		return result, nil
	})
}

func (s *LiabilityService) Statement(actor *model.User, id int64) (*liability.Detail, error) {
	if !CanView(actor) {
		return nil, ErrTxnForbidden
	}
	var b model.LiabilityStatement
	if e := s.db.First(&b, id).Error; e != nil {
		return nil, e
	}
	return s.Detail(actor, b.AccountID)
}

func (s *LiabilityService) EnsureDue(now time.Time) (generated int, err error) {
	if s.BeforeEnsure != nil {
		if e := s.BeforeEnsure(now); e != nil {
			return 0, e
		}
	}
	n, e := s.ensureFees(now)
	if e != nil {
		return n, e
	}
	generated += n
	var accounts []model.Account
	if e := s.db.Where("billing_enabled = ?", true).Find(&accounts).Error; e != nil {
		return generated, e
	}
	for _, a := range accounts {
		var first model.LiabilityBillRule
		if e := s.db.Where("account_id = ?", a.ID).Order("effective_from,version").First(&first).Error; e != nil {
			return generated, e
		}
		cursor := first.EffectiveFrom
		var last model.LiabilityStatement
		e := s.db.Where("account_id = ? AND kind = ? AND issued_at IS NOT NULL", a.ID, "monthly").Order("cycle_end DESC").First(&last).Error
		if e == nil {
			d, _ := liability.Parse(last.CycleEnd)
			cursor = liability.Date(d.AddDate(0, 0, 1))
		} else if !errors.Is(e, gorm.ErrRecordNotFound) {
			return generated, e
		}
		for step := 0; step < 1212 && cursor <= liability.Date(now); step++ {
			var end string
			changed := false
			e := repository.LedgerWrite(s.db, func(tx *gorm.DB) error {
				b, e := liability.StatementFor(tx, a.ID, cursor)
				if e != nil {
					return e
				}
				end = b.CycleEnd
				if b.CycleEnd >= liability.Date(now) || b.IssuedAt != nil {
					return nil
				}
				var items []model.LiabilityDebtItem
				if e := tx.Where("statement_id = ? AND active = ?", b.ID, true).Find(&items).Error; e != nil {
					return e
				}
				gross := decimal.Zero
				for _, d := range items {
					gross = gross.Add(d.Amount)
				}
				if e := tx.Model(&b).Updates(map[string]any{"issued_at": now, "snapshot_amount": gross}).Error; e != nil {
					return e
				}
				changed = true
				return liability.Reallocate(tx, a.ID)
			})
			if e != nil {
				return generated, e
			}
			if changed {
				generated++
			}
			if end >= liability.Date(now) {
				break
			}
			d, e := liability.Parse(end)
			if e != nil {
				return generated, e
			}
			cursor = liability.Date(d.AddDate(0, 0, 1))
		}
		// Explicitly entered backdated sources may create an older unissued bill.
		var overdueDrafts []model.LiabilityStatement
		if e := s.db.Where("account_id = ? AND issued_at IS NULL AND cycle_end < ?", a.ID, liability.Date(now)).Find(&overdueDrafts).Error; e != nil {
			return generated, e
		}
		for _, draft := range overdueDrafts {
			changed := false
			e := repository.LedgerWrite(s.db, func(tx *gorm.DB) error {
				var b model.LiabilityStatement
				if e := tx.First(&b, draft.ID).Error; e != nil {
					return e
				}
				if b.IssuedAt != nil {
					return nil
				}
				var items []model.LiabilityDebtItem
				if e := tx.Where("statement_id = ? AND active = ?", b.ID, true).Find(&items).Error; e != nil {
					return e
				}
				gross := decimal.Zero
				for _, d := range items {
					gross = gross.Add(d.Amount)
				}
				if e := tx.Model(&b).Updates(map[string]any{"issued_at": now, "snapshot_amount": gross}).Error; e != nil {
					return e
				}
				changed = true
				return liability.Reallocate(tx, a.ID)
			})
			if e != nil {
				return generated, e
			}
			if changed {
				generated++
			}
		}
		rule, e := liability.RuleForDate(s.db, a.ID, liability.Date(now))
		if e != nil {
			return generated, e
		}
		if e := s.db.Model(&a).Update("repayment_day", rule.RepaymentDay).Error; e != nil {
			return generated, e
		}
	}
	err = repository.LedgerWrite(s.db, func(tx *gorm.DB) error { return liability.RefreshReminders(tx, now, true) })
	return
}

func jsonText(v any) string { b, _ := json.Marshal(v); return string(b) }
func requestConflict(message string) error {
	return fmt.Errorf("%w：%s", liability.ErrConflict, message)
}
