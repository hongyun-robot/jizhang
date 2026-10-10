package service

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	_ "time/tzdata"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
	"jizhang/internal/model"
	"jizhang/internal/repository"
)

var (
	ErrVoucherNotFound    = errors.New("券或操作不存在")
	ErrVoucherInvalid     = errors.New("券信息无效")
	ErrVoucherSettled     = errors.New("券已结清或剩余次数不足")
	ErrVoucherDependent   = errors.New("存在后续有效操作，请先撤销后续操作")
	ErrVoucherKeyConflict = errors.New("这次操作已记录，请刷新核对后再发起新操作")
	ErrVoucherUsed        = errors.New("券已有有效操作，请先撤销后再修改购买信息或删除")
)

var voucherLocation = func() *time.Location {
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		panic(err)
	}
	return loc
}()

func ParseVoucherDate(value string) (time.Time, error) {
	return time.ParseInLocation("2006-01-02", value, voucherLocation)
}
func voucherDay(d time.Time) string { return d.Format("2006-01-02") }

// DATE 是日历日期，匹配项目 DSN 的 Local，避免驱动把北京时间零点换算到前一天。
func voucherStorageDate(d time.Time) time.Time {
	return time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, time.Local)
}
func validVoucherDate(d time.Time) bool { return !d.IsZero() && d.Year() >= 2000 && d.Year() <= 2100 }

type VoucherInput struct {
	Name              string
	Platform          string
	Note              string
	PaidAmount        decimal.Decimal
	PurchaseDate      time.Time
	AccountID         int64
	CategoryID        int64
	TotalUnits        int64
	ExpiresOn         *time.Time
	AutoRefundEnabled bool
	ResumeAutoRefund  bool
	RequestKey        string
}
type VoucherOperation struct {
	EntryDate  time.Time
	Units      int64
	RequestKey string
}

type VoucherService struct {
	db   *gorm.DB
	txns *TransactionService
	now  func() time.Time
}

func NewVoucherService(db *gorm.DB, txns *TransactionService) *VoucherService {
	return &VoucherService{db: db, txns: txns, now: time.Now}
}

func voucherHash(values ...any) string {
	b, _ := json.Marshal(values)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}
func voucherManualKey(key string) (string, error) {
	u, err := uuid.Parse(key)
	if err != nil || u == uuid.Nil {
		return "", fmt.Errorf("%w：请求标识须为 UUID", ErrVoucherInvalid)
	}
	return "manual:" + u.String(), nil
}
func expiryString(d *time.Time) string {
	if d == nil {
		return ""
	}
	return voucherDay(*d)
}
func (in *VoucherInput) normalize() error {
	in.PurchaseDate = voucherStorageDate(in.PurchaseDate)
	if in.ExpiresOn != nil {
		date := voucherStorageDate(*in.ExpiresOn)
		in.ExpiresOn = &date
	}
	in.Name = strings.TrimSpace(in.Name)
	platform, err := normalizeVoucherPlatform(in.Platform, false)
	if err != nil {
		return err
	}
	in.Platform = platform
	in.Note = strings.TrimSpace(in.Note)
	if in.Name == "" || utf8.RuneCountInString(in.Name) > 128 || utf8.RuneCountInString(in.Platform) > 128 || utf8.RuneCountInString(in.Note) > 255 {
		return fmt.Errorf("%w：名称必填，名称和平台最多 128 字，备注最多 255 字", ErrVoucherInvalid)
	}
	if !in.PaidAmount.IsPositive() || in.PaidAmount.GreaterThan(maxAmount) || !in.PaidAmount.Round(2).Equal(in.PaidAmount) {
		return ErrTxnAmountInvalid
	}
	if in.TotalUnits < 1 || in.TotalUnits > 2147483647 {
		return fmt.Errorf("%w：次数须为正整数", ErrVoucherInvalid)
	}
	if !validVoucherDate(in.PurchaseDate) {
		return ErrTxnDateInvalid
	}
	if in.ExpiresOn != nil && (!validVoucherDate(*in.ExpiresOn) || voucherDay(*in.ExpiresOn) < voucherDay(in.PurchaseDate)) {
		return fmt.Errorf("%w：有效期不能早于购买日", ErrVoucherInvalid)
	}
	if in.AutoRefundEnabled && in.ExpiresOn == nil {
		return fmt.Errorf("%w：自动退款需要有效期", ErrVoucherInvalid)
	}
	if in.ResumeAutoRefund && !in.AutoRefundEnabled {
		return fmt.Errorf("%w：重新启用需要打开自动退款", ErrVoucherInvalid)
	}
	return nil
}
func (in VoucherInput) hash() string {
	return voucherHash(in.Name, in.Platform, in.Note, in.PaidAmount.StringFixed(2), voucherDay(in.PurchaseDate), in.AccountID, in.CategoryID, in.TotalUnits, expiryString(in.ExpiresOn), in.AutoRefundEnabled)
}

func (s *VoucherService) afterWrite(keys ...string) {
	s.txns.fireWrite()
	if len(keys) > 0 {
		s.txns.firePeriods(keys...)
	}
}

func (s *VoucherService) Create(actor *model.User, in VoucherInput) (*model.Voucher, error) {
	if !CanCreateTransaction(actor) {
		return nil, ErrTxnForbidden
	}
	if err := in.normalize(); err != nil {
		return nil, err
	}
	key, err := voucherManualKey(in.RequestKey)
	if err != nil {
		return nil, err
	}
	hash := in.hash()
	var out *model.Voucher
	created := false
	err = repository.LedgerWrite(s.db, func(tx *gorm.DB) error {
		if e := checkAccountOwnership(tx, actor, in.AccountID); e != nil {
			return e
		}
		r := repository.NewVoucherRepo(tx)
		existing, e := r.ByCreateKey(actor.ID, key)
		if e != nil {
			return e
		}
		if existing != nil {
			if existing.CreatePayloadHash != hash || existing.DeletedAt.Valid {
				return ErrVoucherKeyConflict
			}
			out = existing
			return ensureVoucherPlatform(tx, existing.Platform)
		}
		inner := s.txns.bound(tx)
		input := TxnInput{Kind: model.TxnExpense, Amount: in.PaidAmount, AccountID: in.AccountID, CategoryID: in.CategoryID, EntryDate: in.PurchaseDate}
		if e := inner.validate(&input); e != nil {
			return e
		}
		period := inner.deps.Periods.KeyForDate(in.PurchaseDate)
		if e := inner.guardOpen(period); e != nil {
			return e
		}
		v := &model.Voucher{Name: in.Name, Platform: in.Platform, Note: in.Note, PaidAmount: in.PaidAmount, PurchaseDate: in.PurchaseDate, AccountID: in.AccountID, CategoryID: in.CategoryID, TotalUnits: in.TotalUnits, ExpiresOn: in.ExpiresOn, AutoRefundEnabled: in.AutoRefundEnabled, ScheduleVersion: 1, Revision: 1, CreatedBy: actor.ID, CreateRequestKey: key, CreatePayloadHash: hash}
		if e := r.Create(v); e != nil {
			return e
		}
		event := &model.VoucherEvent{VoucherID: v.ID, Action: model.VoucherPurchase, Units: v.TotalUnits, Amount: v.PaidAmount, EntryDate: v.PurchaseDate, PeriodKey: period, AccountID: v.AccountID, CategoryID: v.CategoryID, CreatedBy: actor.ID, Source: "manual", RequestKey: key, PayloadHash: hash}
		if e := r.CreateEvent(event); e != nil {
			return e
		}
		if e := s.project(tx, v, event); e != nil {
			return e
		}
		if e := ensureVoucherPlatform(tx, v.Platform); e != nil {
			return e
		}
		out = v
		created = true
		return nil
	})
	if err == nil && created {
		s.afterWrite()
	}
	return out, err
}

func (s *VoucherService) project(tx *gorm.DB, v *model.Voucher, e *model.VoucherEvent) error {
	if e.Amount.IsZero() {
		return nil
	}
	var purpose model.TxnPurpose
	kind := model.TxnExpense
	switch e.Action {
	case model.VoucherPurchase:
		purpose = model.PurposeVoucherPurchase
	case model.VoucherRedeem:
		purpose = model.PurposeVoucherRedemption
	case model.VoucherRefund:
		purpose = model.PurposeVoucherRefund
		kind = model.TxnRefund
	case model.VoucherWriteoff:
		purpose = model.PurposeVoucherWriteoff
	default:
		return ErrVoucherInvalid
	}
	source := model.SourceManual
	if e.Source == "auto" {
		source = model.SourceVoucherAuto
	}
	cat := e.CategoryID
	t := model.Transaction{Kind: kind, Amount: e.Amount, AccountID: e.AccountID, CategoryID: &cat, EntryDate: e.EntryDate, PeriodKey: e.PeriodKey, Note: v.Name, CreatedBy: e.CreatedBy, Source: source, Purpose: purpose, VoucherEventID: &e.ID}
	if e.Action == model.VoucherRefund {
		var purchase model.Transaction
		if err := tx.Joins("JOIN voucher_events e ON e.id = transactions.voucher_event_id").Where("e.voucher_id = ? AND e.action = ? AND e.voided_at IS NULL", v.ID, model.VoucherPurchase).First(&purchase).Error; err != nil {
			return err
		}
		t.RelatedTxnID = &purchase.ID
	}
	return repository.NewTransactionRepo(tx).Create(&t)
}

func (s *VoucherService) Operate(actor *model.User, id int64, action model.VoucherAction, in VoucherOperation) (*model.VoucherEvent, error) {
	if !CanCreateTransaction(actor) {
		return nil, ErrTxnForbidden
	}
	v, err := repository.NewVoucherRepo(s.db).Get(id, false)
	if err != nil {
		return nil, err
	}
	if v == nil {
		return nil, ErrVoucherNotFound
	}
	if e := checkAccountOwnership(s.db, actor, v.AccountID); e != nil {
		return nil, e
	}
	key, err := voucherManualKey(in.RequestKey)
	if err != nil {
		return nil, err
	}
	e, created, err := s.operate(id, action, in, key, actor.ID, nil)
	if err == nil && created {
		if action == model.VoucherRedeem || action == model.VoucherWriteoff {
			s.afterWrite(e.PeriodKey)
		} else {
			s.afterWrite()
		}
	}
	return e, err
}

func (s *VoucherService) operate(id int64, action model.VoucherAction, in VoucherOperation, key string, actorID int64, autoVersion *int64) (out *model.VoucherEvent, created bool, err error) {
	err = repository.LedgerWrite(s.db, func(tx *gorm.DB) error {
		r := repository.NewVoucherRepo(tx)
		v, e := r.Get(id, true)
		if e != nil {
			return e
		}
		if v == nil {
			if autoVersion != nil {
				return nil
			}
			return ErrVoucherNotFound
		}
		if autoVersion != nil {
			if v.ScheduleVersion != *autoVersion || !v.AutoRefundEnabled || v.AutoRefundSuspended || v.ExpiresOn == nil {
				return nil
			}
			in.EntryDate = v.ExpiresOn.AddDate(0, 0, 1)
			actorID = v.CreatedBy
		}
		in.EntryDate = voucherStorageDate(in.EntryDate)
		hash := voucherHash(action, voucherDay(in.EntryDate), in.Units)
		existing, e := r.EventByKey(id, key)
		if e != nil {
			return e
		}
		if existing != nil {
			if existing.PayloadHash != hash {
				return ErrVoucherKeyConflict
			}
			out = existing
			return nil
		}
		if action != model.VoucherRedeem && action != model.VoucherRefund && action != model.VoucherWriteoff {
			return ErrVoucherInvalid
		}
		if (!validVoucherDate(in.EntryDate) && autoVersion == nil) || voucherDay(in.EntryDate) < voucherDay(v.PurchaseDate) {
			return ErrTxnDateInvalid
		}
		if action == model.VoucherRedeem && v.ExpiresOn != nil && voucherDay(in.EntryDate) > voucherDay(*v.ExpiresOn) {
			return fmt.Errorf("%w：核销日期不能超过有效期", ErrVoucherInvalid)
		}
		summaries, e := r.Summaries([]int64{id})
		if e != nil {
			return e
		}
		sum := summaries[id]
		settled := sum.UsedUnits + sum.RefundedUnits + sum.WrittenOffUnits
		remaining := v.TotalUnits - settled
		if remaining <= 0 {
			if autoVersion != nil {
				return nil
			}
			return ErrVoucherSettled
		}
		units := in.Units
		if action == model.VoucherRedeem {
			if units < 1 || units > remaining {
				return ErrVoucherSettled
			}
		} else {
			if in.Units != 0 {
				return fmt.Errorf("%w：退款和作废由系统结算全部剩余份额", ErrVoucherInvalid)
			}
			units = remaining
		}
		inner := s.txns.bound(tx)
		period := inner.deps.Periods.KeyForDate(in.EntryDate)
		if e := inner.guardOpen(period); e != nil {
			return e
		}
		if action == model.VoucherRefund {
			account, e := repository.NewAccountRepo(tx).GetByID(v.AccountID)
			if e != nil {
				return e
			}
			if account == nil || account.Status != model.StatusActive {
				return ErrTxnAccountInvalid
			}
		}
		source := "manual"
		if autoVersion != nil {
			source = "auto"
		}
		event := &model.VoucherEvent{VoucherID: id, Action: action, Units: units, Amount: model.VoucherCost(v.PaidAmount, v.TotalUnits, settled, units), EntryDate: in.EntryDate, PeriodKey: period, AccountID: v.AccountID, CategoryID: v.CategoryID, CreatedBy: actorID, Source: source, RequestKey: key, PayloadHash: hash, ScheduleVersion: autoVersion}
		if e := r.CreateEvent(event); e != nil {
			return e
		}
		if e := s.project(tx, v, event); e != nil {
			return e
		}
		v.Revision++
		v.LastError = ""
		if e := r.Save(v); e != nil {
			return e
		}
		out = event
		created = true
		return nil
	})
	return
}

func (s *VoucherService) Undo(actor *model.User, id, eventID int64) error {
	if !CanCreateTransaction(actor) {
		return ErrTxnForbidden
	}
	v, err := repository.NewVoucherRepo(s.db).Get(id, false)
	if err != nil {
		return err
	}
	if v == nil {
		return ErrVoucherNotFound
	}
	if e := checkAccountOwnership(s.db, actor, v.AccountID); e != nil {
		return e
	}
	changed := false
	var period string
	err = repository.LedgerWrite(s.db, func(tx *gorm.DB) error {
		r := repository.NewVoucherRepo(tx)
		v, e := r.Get(id, true)
		if e != nil {
			return e
		}
		if v == nil {
			return ErrVoucherNotFound
		}
		events, e := r.Events(id)
		if e != nil {
			return e
		}
		var target *model.VoucherEvent
		var last int64
		for i := range events {
			if events[i].ID == eventID {
				target = &events[i]
			}
			if events[i].VoidedAt == nil {
				last = events[i].ID
			}
		}
		if target == nil {
			return ErrVoucherNotFound
		}
		if !CanModifyTransaction(actor, target.CreatedBy) {
			return ErrTxnForbidden
		}
		if target.Action == model.VoucherPurchase {
			return fmt.Errorf("%w：购买记录请使用删除券操作", ErrVoucherInvalid)
		}
		if target.VoidedAt != nil {
			return nil
		}
		if last != target.ID {
			return ErrVoucherDependent
		}
		if e := s.txns.bound(tx).guardOpen(target.PeriodKey); e != nil {
			return e
		}
		now := s.now()
		target.VoidedAt = &now
		target.VoidedBy = &actor.ID
		if e := r.SaveEvent(target); e != nil {
			return e
		}
		var projections []model.Transaction
		if e := tx.Where("voucher_event_id = ?", target.ID).Find(&projections).Error; e != nil {
			return e
		}
		for _, projection := range projections {
			if e := repository.NewTransactionRepo(tx).WithActor(actor.ID).Delete(projection.ID); e != nil {
				return e
			}
		}
		if target.Source == "auto" {
			v.AutoRefundSuspended = true
		}
		v.Revision++
		v.LastError = ""
		if e := r.Save(v); e != nil {
			return e
		}
		changed = true
		if target.Action == model.VoucherRedeem || target.Action == model.VoucherWriteoff {
			period = target.PeriodKey
		}
		return nil
	})
	if err == nil && changed {
		if period != "" {
			s.afterWrite(period)
		} else {
			s.afterWrite()
		}
	}
	return err
}

func (s *VoucherService) Update(actor *model.User, id int64, in VoucherInput) (*model.Voucher, error) {
	if !CanCreateTransaction(actor) {
		return nil, ErrTxnForbidden
	}
	if err := in.normalize(); err != nil {
		return nil, err
	}
	v, err := repository.NewVoucherRepo(s.db).Get(id, false)
	if err != nil {
		return nil, err
	}
	if v == nil {
		return nil, ErrVoucherNotFound
	}
	if e := checkAccountOwnership(s.db, actor, v.AccountID, in.AccountID); e != nil {
		return nil, e
	}
	var out *model.Voucher
	err = repository.LedgerWrite(s.db, func(tx *gorm.DB) error {
		r := repository.NewVoucherRepo(tx)
		v, e := r.Get(id, true)
		if e != nil {
			return e
		}
		if v == nil {
			return ErrVoucherNotFound
		}
		if !CanModifyTransaction(actor, v.CreatedBy) {
			return ErrTxnForbidden
		}
		events, e := r.Events(id)
		if e != nil {
			return e
		}
		var purchase *model.VoucherEvent
		hasLater := false
		for i := range events {
			if events[i].VoidedAt != nil {
				continue
			}
			if events[i].Action == model.VoucherPurchase {
				purchase = &events[i]
			} else {
				hasLater = true
			}
			if events[i].Action == model.VoucherRedeem && in.ExpiresOn != nil && voucherDay(events[i].EntryDate) > voucherDay(*in.ExpiresOn) {
				return fmt.Errorf("%w：有效期不能否定已有核销日期", ErrVoucherInvalid)
			}
		}
		if purchase == nil {
			return ErrVoucherNotFound
		}
		financial := !v.PaidAmount.Equal(in.PaidAmount) || voucherDay(v.PurchaseDate) != voucherDay(in.PurchaseDate) || v.AccountID != in.AccountID || v.CategoryID != in.CategoryID || v.TotalUnits != in.TotalUnits
		inner := s.txns.bound(tx)
		if financial {
			if hasLater {
				return ErrVoucherUsed
			}
			if e := inner.guardOpen(purchase.PeriodKey); e != nil {
				return e
			}
			period := inner.deps.Periods.KeyForDate(in.PurchaseDate)
			if e := inner.guardOpen(period); e != nil {
				return e
			}
			input := TxnInput{Kind: model.TxnExpense, Amount: in.PaidAmount, AccountID: in.AccountID, CategoryID: in.CategoryID, EntryDate: in.PurchaseDate}
			if e := inner.validate(&input); e != nil {
				return e
			}
			purchase.Amount = in.PaidAmount
			purchase.Units = in.TotalUnits
			purchase.EntryDate = in.PurchaseDate
			purchase.PeriodKey = period
			purchase.AccountID = in.AccountID
			purchase.CategoryID = in.CategoryID
			if e := r.SaveEvent(purchase); e != nil {
				return e
			}
			var projection model.Transaction
			if e := tx.Where("voucher_event_id = ?", purchase.ID).First(&projection).Error; e != nil {
				return e
			}
			projection.Amount = in.PaidAmount
			projection.EntryDate = in.PurchaseDate
			projection.PeriodKey = period
			projection.AccountID = in.AccountID
			projection.CategoryID = &in.CategoryID
			projection.Note = in.Name
			if e := repository.NewTransactionRepo(tx).WithActor(actor.ID).Update(&projection); e != nil {
				return e
			}
		}
		if expiryString(v.ExpiresOn) != expiryString(in.ExpiresOn) || v.AutoRefundEnabled != in.AutoRefundEnabled || in.ResumeAutoRefund {
			v.ScheduleVersion++
			v.LastError = ""
			if in.ResumeAutoRefund || (!v.AutoRefundEnabled && in.AutoRefundEnabled) {
				v.AutoRefundSuspended = false
			}
		}
		v.Name = in.Name
		v.Platform = in.Platform
		v.Note = in.Note
		v.PaidAmount = in.PaidAmount
		v.PurchaseDate = in.PurchaseDate
		v.AccountID = in.AccountID
		v.CategoryID = in.CategoryID
		v.TotalUnits = in.TotalUnits
		v.ExpiresOn = in.ExpiresOn
		v.AutoRefundEnabled = in.AutoRefundEnabled
		v.Revision++
		if e := r.Save(v); e != nil {
			return e
		}
		out = v
		return ensureVoucherPlatform(tx, v.Platform)
	})
	if err == nil {
		s.afterWrite()
	}
	return out, err
}

func (s *VoucherService) Delete(actor *model.User, id int64) error {
	if !CanCreateTransaction(actor) {
		return ErrTxnForbidden
	}
	v, err := repository.NewVoucherRepo(s.db).Get(id, false)
	if err != nil {
		return err
	}
	if v == nil {
		return ErrVoucherNotFound
	}
	if e := checkAccountOwnership(s.db, actor, v.AccountID); e != nil {
		return e
	}
	err = repository.LedgerWrite(s.db, func(tx *gorm.DB) error {
		r := repository.NewVoucherRepo(tx)
		v, e := r.Get(id, true)
		if e != nil {
			return e
		}
		if v == nil {
			return ErrVoucherNotFound
		}
		if !CanModifyTransaction(actor, v.CreatedBy) {
			return ErrTxnForbidden
		}
		events, e := r.Events(id)
		if e != nil {
			return e
		}
		var purchase *model.VoucherEvent
		for i := range events {
			if events[i].VoidedAt != nil {
				continue
			}
			if events[i].Action != model.VoucherPurchase {
				return ErrVoucherUsed
			}
			purchase = &events[i]
		}
		if purchase == nil {
			return ErrVoucherNotFound
		}
		if e := s.txns.bound(tx).guardOpen(purchase.PeriodKey); e != nil {
			return e
		}
		now := s.now()
		purchase.VoidedAt = &now
		purchase.VoidedBy = &actor.ID
		if e := r.SaveEvent(purchase); e != nil {
			return e
		}
		var projections []model.Transaction
		if e := tx.Where("voucher_event_id = ?", purchase.ID).Find(&projections).Error; e != nil {
			return e
		}
		for _, projection := range projections {
			if e := repository.NewTransactionRepo(tx).WithActor(actor.ID).Delete(projection.ID); e != nil {
				return e
			}
		}
		return r.Delete(v)
	})
	if err == nil {
		s.afterWrite()
	}
	return err
}

type VoucherEnsureResult struct {
	Generated int `json:"generated"`
	Failed    int `json:"failed"`
}

func (s *VoucherService) EnsureDue(now time.Time) (VoucherEnsureResult, error) {
	result := VoucherEnsureResult{}
	r := repository.NewVoucherRepo(s.db)
	rows, err := r.Due(voucherDay(now.In(voucherLocation)))
	if err != nil {
		return result, err
	}
	var failureErr error
	for _, v := range rows {
		version := v.ScheduleVersion
		_, created, e := s.operate(v.ID, model.VoucherRefund, VoucherOperation{}, fmt.Sprintf("auto:%d", version), v.CreatedBy, &version)
		if e != nil {
			result.Failed++
			if updateErr := r.Failure(v.ID, v.Revision, now, e.Error()); updateErr != nil {
				failureErr = errors.Join(failureErr, updateErr)
			}
			continue
		}
		if created {
			result.Generated++
			s.afterWrite()
		}
	}
	return result, failureErr
}
