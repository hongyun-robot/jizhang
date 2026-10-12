package service

import (
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
	"jizhang/internal/model"
	"jizhang/internal/repository"
	"strings"
)

type VoucherView struct {
	model.Voucher
	UsedUnits        int64           `json:"used_units"`
	RefundedUnits    int64           `json:"refunded_units"`
	WrittenOffUnits  int64           `json:"written_off_units"`
	RemainingUnits   int64           `json:"remaining_units"`
	UsedAmount       decimal.Decimal `json:"used_amount"`
	RefundedAmount   decimal.Decimal `json:"refunded_amount"`
	WrittenOffAmount decimal.Decimal `json:"written_off_amount"`
	RemainingAmount  decimal.Decimal `json:"remaining_amount"`
	NextUnitAmount   decimal.Decimal `json:"next_unit_amount"`
	Status           string          `json:"status"`
	Settlement       string          `json:"settlement"`
	CanEdit          bool            `json:"can_edit"`
	CanEditPurchase  bool            `json:"can_edit_purchase"`
	CanDelete        bool            `json:"can_delete"`
	CanOperate       bool            `json:"can_operate"`
}
type VoucherEventView struct {
	model.VoucherEvent
	CanUndo bool `json:"can_undo"`
}
type VoucherDetail struct {
	VoucherView
	Events []VoucherEventView `json:"events"`
}
type VoucherList struct {
	Items        []VoucherView   `json:"items"`
	Total        int             `json:"total"`
	Page         int             `json:"page"`
	PageSize     int             `json:"page_size"`
	UnusedAmount decimal.Decimal `json:"unused_amount"`
}

func (s *VoucherService) view(v model.Voucher, sum repository.VoucherSummary, actor *model.User, closed bool) VoucherView {
	settled := sum.UsedUnits + sum.RefundedUnits + sum.WrittenOffUnits
	remaining := v.PaidAmount.Sub(sum.UsedAmount).Sub(sum.RefundedAmount).Sub(sum.WrittenOffAmount)
	state := "pending"
	settlement := ""
	if v.TotalUnits == settled {
		state = "settled"
		if sum.RefundedUnits > 0 {
			settlement = "refunded"
		} else if sum.WrittenOffUnits > 0 {
			settlement = "written_off"
		} else {
			settlement = "used"
		}
	} else if v.ExpiresOn != nil && voucherDay(*v.ExpiresOn) < voucherDay(s.now().In(voucherLocation)) {
		state = "expired"
		if v.LastError != "" {
			state = "failed"
		}
	}
	editable := CanModifyTransaction(actor, v.CreatedBy)
	return VoucherView{Voucher: v, UsedUnits: sum.UsedUnits, RefundedUnits: sum.RefundedUnits, WrittenOffUnits: sum.WrittenOffUnits, RemainingUnits: v.TotalUnits - settled, UsedAmount: sum.UsedAmount, RefundedAmount: sum.RefundedAmount, WrittenOffAmount: sum.WrittenOffAmount, RemainingAmount: remaining, NextUnitAmount: model.VoucherCost(v.PaidAmount, v.TotalUnits, settled, 1), Status: state, Settlement: settlement, CanEdit: editable, CanEditPurchase: editable && settled == 0 && !closed, CanDelete: editable && settled == 0 && !closed, CanOperate: CanCreateTransaction(actor) && v.TotalUnits > settled}
}

func (s *VoucherService) List(actor *model.User, keyword, status string, page, size int) (VoucherList, error) {
	if !CanView(actor) {
		return VoucherList{}, ErrTxnForbidden
	}
	if page < 1 {
		page = 1
	}
	if size < 1 || size > 100 {
		size = 20
	}
	out := VoucherList{Items: []VoucherView{}, Page: page, PageSize: size}
	err := s.db.Transaction(func(tx *gorm.DB) error {
		r := repository.NewVoucherRepo(tx)
		rows, e := r.List("")
		if e != nil {
			return e
		}
		var accounts []model.Account
		if e := tx.Find(&accounts).Error; e != nil {
			return e
		}
		visible := map[int64]bool{}
		for _, a := range accounts {
			visible[a.ID] = ownsAccount(actor, a)
		}
		selected := []model.Voucher{}
		for _, v := range rows {
			if visible[v.AccountID] {
				selected = append(selected, v)
			}
		}
		rows = selected
		ids := make([]int64, 0, len(rows))
		for _, v := range rows {
			ids = append(ids, v.ID)
		}
		sums, e := r.Summaries(ids)
		if e != nil {
			return e
		}
		closings, e := repository.NewClosingRepo(tx).List()
		if e != nil {
			return e
		}
		closed := map[string]bool{}
		for _, c := range closings {
			closed[c.PeriodKey] = true
		}
		purchasePeriods := map[int64]string{}
		if len(ids) > 0 {
			var events []model.VoucherEvent
			if e := tx.Select("voucher_id, period_key").Where("voucher_id IN ? AND action = ? AND voided_at IS NULL", ids, model.VoucherPurchase).Find(&events).Error; e != nil {
				return e
			}
			for _, event := range events {
				purchasePeriods[event.VoucherID] = event.PeriodKey
			}
		}
		key := strings.ToLower(strings.TrimSpace(keyword))
		for _, v := range rows {
			view := s.view(v, sums[v.ID], actor, closed[purchasePeriods[v.ID]])
			out.UnusedAmount = out.UnusedAmount.Add(view.RemainingAmount)
			if key != "" && !strings.Contains(strings.ToLower(v.Name+" "+v.Platform), key) {
				continue
			}
			if status != "" && status != view.Status {
				continue
			}
			out.Total++
			if out.Total > (page-1)*size && len(out.Items) < size {
				out.Items = append(out.Items, view)
			}
		}
		return nil
	})
	return out, err
}

func (s *VoucherService) Detail(actor *model.User, id int64) (VoucherDetail, error) {
	if !CanView(actor) {
		return VoucherDetail{}, ErrTxnForbidden
	}
	var out VoucherDetail
	err := s.db.Transaction(func(tx *gorm.DB) error {
		r := repository.NewVoucherRepo(tx)
		v, e := r.Get(id, false)
		if e != nil {
			return e
		}
		if v == nil {
			return ErrVoucherNotFound
		}
		if e := checkAccountOwnership(tx, actor, v.AccountID); e != nil {
			return e
		}
		sums, e := r.Summaries([]int64{id})
		if e != nil {
			return e
		}
		events, e := r.Events(id)
		if e != nil {
			return e
		}
		var last int64
		var purchasePeriod string
		for _, event := range events {
			if event.VoidedAt == nil {
				last = event.ID
				if event.Action == model.VoucherPurchase {
					purchasePeriod = event.PeriodKey
				}
			}
		}
		closed, e := repository.NewClosingRepo(tx).IsClosed(purchasePeriod)
		if e != nil {
			return e
		}
		out.VoucherView = s.view(*v, sums[id], actor, closed)
		out.Events = []VoucherEventView{}
		for _, event := range events {
			canUndo := event.Action != model.VoucherPurchase && event.VoidedAt == nil && event.ID == last && CanModifyTransaction(actor, event.CreatedBy)
			if canUndo {
				closed, e := repository.NewClosingRepo(tx).IsClosed(event.PeriodKey)
				if e != nil {
					return e
				}
				canUndo = !closed
			}
			out.Events = append(out.Events, VoucherEventView{VoucherEvent: event, CanUndo: canUndo})
		}
		return nil
	})
	return out, err
}
