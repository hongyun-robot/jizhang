package repository

import (
	"errors"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"jizhang/internal/model"
	"time"
)

type VoucherRepo struct{ db *gorm.DB }

func NewVoucherRepo(db *gorm.DB) *VoucherRepo { return &VoucherRepo{db: db} }
func (r *VoucherRepo) DB() *gorm.DB           { return r.db }

func (r *VoucherRepo) Get(id int64, lock bool) (*model.Voucher, error) {
	q := r.db
	if lock {
		q = q.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	var v model.Voucher
	err := q.First(&v, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &v, err
}
func (r *VoucherRepo) ByCreateKey(actor int64, key string) (*model.Voucher, error) {
	var v model.Voucher
	err := r.db.Unscoped().Where("created_by = ? AND create_request_key = ?", actor, key).First(&v).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &v, err
}
func (r *VoucherRepo) Create(v *model.Voucher) error { return r.db.Create(v).Error }
func (r *VoucherRepo) Save(v *model.Voucher) error   { return r.db.Save(v).Error }
func (r *VoucherRepo) Delete(v *model.Voucher) error { return r.db.Delete(v).Error }
func (r *VoucherRepo) List(keyword string) ([]model.Voucher, error) {
	q := r.db.Model(&model.Voucher{})
	if keyword != "" {
		q = q.Where("name LIKE ? OR platform LIKE ?", "%"+keyword+"%", "%"+keyword+"%")
	}
	var rows []model.Voucher
	err := q.Order("purchase_date DESC, id DESC").Find(&rows).Error
	return rows, err
}
func (r *VoucherRepo) Due(today string) ([]model.Voucher, error) {
	var rows []model.Voucher
	err := r.db.Where("auto_refund_enabled = ? AND auto_refund_suspended = ? AND expires_on < ?", true, false, today).Order("expires_on, id").Find(&rows).Error
	return rows, err
}

type VoucherSummary struct {
	VoucherID        int64
	UsedUnits        int64
	RefundedUnits    int64
	WrittenOffUnits  int64
	UsedAmount       decimal.Decimal
	RefundedAmount   decimal.Decimal
	WrittenOffAmount decimal.Decimal
}

func (r *VoucherRepo) Summaries(ids []int64) (map[int64]VoucherSummary, error) {
	out := make(map[int64]VoucherSummary, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	var rows []VoucherSummary
	err := r.db.Model(&model.VoucherEvent{}).Select(`voucher_id,
SUM(CASE WHEN action='redeem' THEN units ELSE 0 END) AS used_units,
SUM(CASE WHEN action='refund' THEN units ELSE 0 END) AS refunded_units,
SUM(CASE WHEN action='writeoff' THEN units ELSE 0 END) AS written_off_units,
SUM(CASE WHEN action='redeem' THEN amount ELSE 0 END) AS used_amount,
SUM(CASE WHEN action='refund' THEN amount ELSE 0 END) AS refunded_amount,
SUM(CASE WHEN action='writeoff' THEN amount ELSE 0 END) AS written_off_amount`).
		Where("voucher_id IN ? AND voided_at IS NULL", ids).Group("voucher_id").Scan(&rows).Error
	for _, row := range rows {
		out[row.VoucherID] = row
	}
	return out, err
}
func (r *VoucherRepo) Events(id int64) ([]model.VoucherEvent, error) {
	var rows []model.VoucherEvent
	err := r.db.Where("voucher_id = ?", id).Order("id").Find(&rows).Error
	return rows, err
}
func (r *VoucherRepo) EventByKey(id int64, key string) (*model.VoucherEvent, error) {
	var e model.VoucherEvent
	err := r.db.Where("voucher_id = ? AND request_key = ?", id, key).First(&e).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &e, err
}
func (r *VoucherRepo) CreateEvent(e *model.VoucherEvent) error { return r.db.Create(e).Error }
func (r *VoucherRepo) SaveEvent(e *model.VoucherEvent) error   { return r.db.Save(e).Error }

// Failure 的 revision 校验防止失败扫描覆盖随后成功的核销、配置修改或撤销。
func (r *VoucherRepo) Failure(id, revision int64, now time.Time, message string) error {
	msg := []rune(message)
	if len(msg) > 255 {
		message = string(msg[:255])
	}
	return r.db.Model(&model.Voucher{}).Where("id = ? AND revision = ?", id, revision).
		Updates(map[string]any{"last_error": message, "last_attempt_at": now}).Error
}
