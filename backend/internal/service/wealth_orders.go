package service

import (
	"encoding/json"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/shopspring/decimal"
	"gorm.io/gorm"
	"jizhang/internal/model"
	"jizhang/internal/wealthdata"
)

// Order facts are append-only event metadata. Estimates never mutate a bank fact.
type WealthAllocation struct {
	SourceID int64           `json:"source_id"`
	Amount   decimal.Decimal `json:"amount"`
	Shares   decimal.Decimal `json:"shares"`
}
type WealthOrderData struct {
	OpeningNAV               string             `json:"opening_nav,omitempty"`
	TradeDate                string             `json:"trade_date,omitempty"`
	NAVDate                  string             `json:"nav_date,omitempty"`
	ExpectedDate             string             `json:"expected_date,omitempty"`
	ApplicationTime          string             `json:"application_time,omitempty"`
	PreviousTradeDate        string             `json:"previous_trade_date,omitempty"`
	Reason                   string             `json:"reason,omitempty"`
	NAVSource                string             `json:"nav_source,omitempty"`
	Allocations              []WealthAllocation `json:"allocations,omitempty"`
	FailedPrincipal          decimal.Decimal    `json:"failed_principal"`
	InsufficientPolicy       string             `json:"insufficient_policy,omitempty"`
	CancelledForInsufficient bool               `json:"cancelled_for_insufficient,omitempty"`
	AllocatedAmount          decimal.Decimal    `json:"allocated_amount"`
}
type WealthOrderView struct {
	EventID         int64           `json:"event_id"`
	Kind            string          `json:"kind"`
	Status          string          `json:"status"`
	Amount          decimal.Decimal `json:"amount"`
	RefundDue       decimal.Decimal `json:"refund_due"`
	ReservedAmount  decimal.Decimal `json:"reserved_amount"`
	SuggestedShares decimal.Decimal `json:"suggested_shares"`
	Ready           bool            `json:"ready"`
	Data            WealthOrderData `json:"data"`
}

func wealthOrderKind(kind string) bool {
	switch kind {
	case "subscribe", "confirm_buy", "cancel_buy", "refund_buy", "request_redeem", "confirm_redeem", "cancel_redeem":
		return true
	}
	return false
}
func orderData(e model.WealthEvent) WealthOrderData {
	var d WealthOrderData
	if e.OrderJSON != "" {
		_ = json.Unmarshal([]byte(e.OrderJSON), &d)
	}
	return d
}
func putOrderData(e *model.WealthEvent, d WealthOrderData) {
	b, _ := json.Marshal(d)
	e.OrderJSON = string(b)
}
func orderDate(raw string, future bool, now time.Time) (time.Time, error) {
	d, e := time.Parse("2006-01-02", raw)
	if e != nil || d.Format("2006-01-02") != raw || d.Year() < 2000 || d.Year() > 2100 || (!future && d.After(wealthToday(now))) {
		return d, ErrWealthInvalid
	}
	return d, nil
}
func normalizeWealthOrder(in WealthInput, now time.Time) (WealthInput, model.WealthEvent, error) {
	e := model.WealthEvent{Kind: in.Kind, Note: in.Note}
	fail := func() (WealthInput, model.WealthEvent, error) { return in, e, ErrWealthInvalid }
	if utf8.RuneCountInString(in.AdjustmentReason) > 255 || in.RightsDate != "" || in.RegistrationDate != "" || in.CostPrice != "" {
		return fail()
	}
	d, err := wealthDate(in.EffectiveDate, now)
	if err != nil {
		return fail()
	}
	e.EffectiveDate = d
	data := WealthOrderData{TradeDate: in.TradeDate, NAVDate: in.NAVDate, ExpectedDate: in.ExpectedDate, PreviousTradeDate: in.PreviousTradeDate, ApplicationTime: in.ApplicationTime, Reason: strings.TrimSpace(in.AdjustmentReason)}
	if in.Kind == "request_redeem" {
		if in.InsufficientPolicy == "" {
			in.InsufficientPolicy = "cancel"
		}
		if in.InsufficientPolicy != "cancel" && in.InsufficientPolicy != "all" {
			return fail()
		}
		data.InsufficientPolicy = in.InsufficientPolicy
	}
	for _, raw := range []string{data.TradeDate, data.NAVDate, data.PreviousTradeDate} {
		if raw != "" {
			if _, err = orderDate(raw, in.Kind == "subscribe", now); err != nil {
				return fail()
			}
		}
	}
	if data.ExpectedDate != "" {
		expected, err := orderDate(data.ExpectedDate, true, now)
		if err != nil || expected.Before(d) {
			return fail()
		}
	}
	if in.Kind == "subscribe" || in.Kind == "request_redeem" {
		if in.SourceEventID != 0 {
			return fail()
		}
	} else {
		if in.SourceEventID <= 0 {
			return fail()
		}
		e.SourceEventID = &in.SourceEventID
	}
	if in.Kind == "subscribe" || in.Kind == "refund_buy" {
		if in.CashDate != "" && in.CashDate != in.EffectiveDate {
			return fail()
		}
		if in.CashAccountID <= 0 {
			return fail()
		}
		e.CashDate = &e.EffectiveDate
		e.CashAccountID = &in.CashAccountID
	} else if in.CashDate != "" || in.CashAccountID != 0 {
		return fail()
	}
	amountZero := in.Kind != "subscribe" && in.Kind != "refund_buy" && in.Kind != "request_redeem" && in.Kind != "confirm_redeem"
	e.Amount, err = wealthNumber(in.Amount, amountZero, 2)
	if err != nil {
		return fail()
	}
	e.Shares, err = wealthNumber(in.Shares, in.Kind != "confirm_buy", 8)
	if err != nil {
		return fail()
	}
	e.Fee, err = wealthNumber(in.Fee, true, 2)
	if err != nil {
		return fail()
	}
	e.TradeNAV, err = wealthNumber(in.TradeNAV, true, 8)
	if err != nil {
		return fail()
	}
	if in.Kind != "confirm_buy" && in.Kind != "confirm_redeem" && !e.Fee.IsZero() {
		return fail()
	}
	if in.Kind == "subscribe" {
		clock, err := time.Parse("15:04:05", data.ApplicationTime)
		if err != nil || clock.Format("15:04:05") != data.ApplicationTime {
			return fail()
		}
		if data.TradeDate == "" {
			return fail()
		}
		td, _ := orderDate(data.TradeDate, true, now)
		if td.Before(d) {
			return fail()
		}
		if !e.Shares.IsZero() || !e.TradeNAV.IsZero() {
			return fail()
		}
	}
	if (in.Kind == "cancel_buy" || in.Kind == "cancel_redeem") && (!e.Amount.IsZero() || !e.Shares.IsZero() || !e.TradeNAV.IsZero()) {
		return fail()
	}
	if (in.Kind == "request_redeem" || in.Kind == "refund_buy") && !e.Shares.IsZero() {
		return fail()
	}
	if in.Kind == "refund_buy" && !e.TradeNAV.IsZero() {
		return fail()
	}
	putOrderData(&e, data)
	in.Amount = e.Amount.String()
	in.Shares = e.Shares.String()
	in.Fee = e.Fee.String()
	in.TradeNAV = e.TradeNAV.String()
	return in, e, nil
}
func activeOrderRows(rows []model.WealthEvent) []model.WealthEvent {
	out := []model.WealthEvent{}
	for _, e := range rows {
		if e.VoidedAt == nil {
			out = append(out, e)
		}
	}
	return out
}
func orderChild(rows []model.WealthEvent, id int64, kinds ...string) *model.WealthEvent {
	for i := range rows {
		e := &rows[i]
		if e.VoidedAt != nil || e.SourceEventID == nil || *e.SourceEventID != id {
			continue
		}
		for _, kind := range kinds {
			if e.Kind == kind {
				return e
			}
		}
	}
	return nil
}
func orderEvent(rows []model.WealthEvent, id int64) *model.WealthEvent {
	for i := range rows {
		if rows[i].ID == id && rows[i].VoidedAt == nil {
			return &rows[i]
		}
	}
	return nil
}
func orderRequestActive(rows []model.WealthEvent, e model.WealthEvent) bool {
	return e.Kind == "request_redeem" && e.VoidedAt == nil && !orderData(e).CancelledForInsufficient && orderChild(rows, e.ID, "cancel_redeem") == nil
}
func orderReservedAmount(rows []model.WealthEvent, source int64) decimal.Decimal {
	sum := decimal.Zero
	for _, e := range rows {
		if !orderRequestActive(rows, e) {
			continue
		}
		for _, a := range orderData(e).Allocations {
			if a.SourceID == source {
				sum = sum.Add(a.Amount)
			}
		}
	}
	return sum
}

// A failed subscription still funds its reserved principal transfer. Only the
// unreserved remainder becomes refundable, until that transfer is cancelled.
func orderRefundDue(rows []model.WealthEvent, root model.WealthEvent) decimal.Decimal {
	cancel := orderChild(rows, root.ID, "cancel_buy")
	if cancel == nil {
		return decimal.Zero
	}
	due := root.Amount.Sub(orderReservedAmount(rows, root.ID))
	for _, e := range rows {
		if e.VoidedAt == nil && e.Kind == "refund_buy" && e.SourceEventID != nil && *e.SourceEventID == cancel.ID {
			due = due.Sub(e.Amount)
		}
	}
	return due
}
func resolvedAllocations(rows []model.WealthEvent, request model.WealthEvent) ([]WealthAllocation, decimal.Decimal, bool) {
	result := []WealthAllocation{}
	failed := decimal.Zero
	ready := true
	for _, a := range orderData(request).Allocations {
		if a.SourceID > 0 {
			root := orderEvent(rows, a.SourceID)
			if root == nil {
				return nil, failed, false
			}
			confirm := orderChild(rows, root.ID, "confirm_buy")
			if confirm != nil {
				a.Shares = a.Amount.DivRound(root.Amount, 16).Mul(confirm.Shares).Round(2)
				used := decimal.Zero
				others := append([]model.WealthEvent(nil), rows...)
				sort.Slice(others, func(i, j int) bool { return others[i].ID < others[j].ID })
				for _, other := range others {
					if !orderRequestActive(rows, other) || other.ID >= request.ID {
						continue
					}
					allocations := orderData(other).Allocations
					finished := orderChild(rows, other.ID, "confirm_redeem")
					if finished != nil {
						allocations = orderData(*finished).Allocations
					}
					for _, prior := range allocations {
						if prior.SourceID == root.ID {
							n := prior.Shares
							if finished == nil {
								n = prior.Amount.DivRound(root.Amount, 16).Mul(confirm.Shares).Round(2)
							}
							used = decimal.Min(confirm.Shares, used.Add(n))
						}
					}
				}
				a.Shares = decimal.Min(a.Shares, decimal.Max(decimal.Zero, confirm.Shares.Sub(used)))
			} else if orderChild(rows, root.ID, "cancel_buy") != nil {
				a.Shares = decimal.Zero
				failed = failed.Add(a.Amount)
			} else {
				ready = false
			}
		}
		result = append(result, a)
	}
	return result, failed, ready
}
func reservedShares(rows []model.WealthEvent) decimal.Decimal {
	sum := decimal.Zero
	for _, e := range rows {
		if !orderRequestActive(rows, e) || orderChild(rows, e.ID, "confirm_redeem") != nil {
			continue
		}
		allocs, _, _ := resolvedAllocations(rows, e)
		for _, a := range allocs {
			sum = sum.Add(a.Shares)
		}
	}
	return sum
}
func validateWealthReservations(rows []model.WealthEvent, at time.Time) error {
	visible := []model.WealthEvent{}
	for _, e := range rows {
		if e.VoidedAt == nil && !wealthDay(e.EffectiveDate).After(wealthDay(at)) {
			visible = append(visible, e)
		}
	}
	state, err := replayWealth(visible, at)
	if err != nil {
		return err
	}
	if reservedShares(visible).GreaterThan(state.Shares) {
		return ErrWealthConflict
	}
	for _, e := range visible {
		if e.Kind == "subscribe" && orderReservedAmount(visible, e.ID).GreaterThan(e.Amount) {
			return ErrWealthConflict
		}
		if e.Kind == "subscribe" && orderRefundDue(visible, e).IsNegative() {
			return ErrWealthConflict
		}
	}
	return nil
}
func (s *WealthService) prepareWealthOrder(tx *gorm.DB, actor *model.User, p *model.WealthPosition, in WealthInput, e *model.WealthEvent, rows []model.WealthEvent) error {
	if !wealthOrderKind(e.Kind) {
		return nil
	}
	if e.Kind == "request_redeem" {
		product, err := s.repo.At(tx).Product(p.ProductID, false)
		if err != nil {
			return err
		}
		if product == nil {
			return ErrWealthInvalid
		}
		identity, known := wealthdata.Catalog(product.Code)
		if !known || identity.OrderPolicy != "zhoulibao_20250402" {
			return ErrWealthInvalid
		}
	}
	data := orderData(*e)
	if e.Kind == "subscribe" {
		return nil
	}
	var source *model.WealthEvent
	if e.SourceEventID != nil {
		source = orderEvent(rows, *e.SourceEventID)
		if source == nil {
			if err := s.foreignEvent(tx, actor, *e.SourceEventID); err != nil {
				return err
			}
			return ErrWealthMissing
		}
		if wealthDay(e.EffectiveDate).Before(wealthDay(source.EffectiveDate)) {
			return ErrWealthInvalid
		}
	}
	switch e.Kind {
	case "confirm_buy", "cancel_buy":
		if source.Kind != "subscribe" || orderChild(rows, source.ID, "confirm_buy", "cancel_buy") != nil {
			return ErrWealthConflict
		}
		if e.Kind == "cancel_buy" {
			e.Amount = source.Amount
			data = orderData(*source)
			data.Reason = in.AdjustmentReason
			break
		}
		rootData := orderData(*source)
		if data.TradeDate == "" {
			data.TradeDate = rootData.TradeDate
		}
		if data.NAVDate == "" {
			data.NAVDate = data.TradeDate
		}
		navDay, err := wealthDate(data.NAVDate, s.Now())
		if err != nil || navDay.After(e.EffectiveDate) || data.NAVDate != data.TradeDate {
			return ErrWealthInvalid
		}
		if data.TradeDate != rootData.TradeDate && data.Reason == "" {
			return ErrWealthInvalid
		}
		e.Amount = source.Amount
		if !e.Amount.Sub(e.Fee).IsPositive() {
			return ErrWealthInvalid
		}
		if err = s.orderNAV(tx, actor, p, e, &data); err != nil {
			return err
		}
		suggestion := e.Amount.Sub(e.Fee).DivRound(e.TradeNAV, 16).Round(2)
		if !e.Shares.Equal(suggestion) && data.Reason == "" {
			return ErrWealthInvalid
		}
	case "refund_buy":
		if source.Kind != "cancel_buy" || source.SourceEventID == nil {
			return ErrWealthInvalid
		}
		root := orderEvent(rows, *source.SourceEventID)
		if root == nil || e.Amount.GreaterThan(orderRefundDue(rows, *root)) {
			return ErrWealthConflict
		}
	case "request_redeem":
		if data.TradeDate == "" || data.PreviousTradeDate == "" || data.TradeDate != e.EffectiveDate.Format("2006-01-02") || data.PreviousTradeDate >= data.TradeDate {
			return ErrWealthInvalid
		}
		if data.NAVDate == "" {
			data.NAVDate = data.PreviousTradeDate
		}
		navDay, err := wealthDate(data.NAVDate, s.Now())
		if err != nil || navDay.After(e.EffectiveDate) {
			return ErrWealthInvalid
		}
		// Pending previous-trading-day applications are consumed newest first.
		pending := []model.WealthEvent{}
		for _, root := range rows {
			if root.Kind == "subscribe" && orderData(root).TradeDate == data.PreviousTradeDate && orderChild(rows, root.ID, "confirm_buy", "cancel_buy") == nil {
				pending = append(pending, root)
			}
		}
		sort.Slice(pending, func(i, j int) bool {
			if pending[i].EffectiveDate.Equal(pending[j].EffectiveDate) {
				if orderData(pending[i]).ApplicationTime != orderData(pending[j]).ApplicationTime {
					return orderData(pending[i]).ApplicationTime > orderData(pending[j]).ApplicationTime
				}
				return pending[i].ID > pending[j].ID
			}
			return pending[i].EffectiveDate.After(pending[j].EffectiveDate)
		})
		remaining := e.Amount
		for _, root := range pending {
			available := root.Amount.Sub(orderReservedAmount(rows, root.ID))
			take := decimal.Min(available, remaining)
			if take.IsPositive() {
				data.Allocations = append(data.Allocations, WealthAllocation{SourceID: root.ID, Amount: take})
				remaining = remaining.Sub(take)
			}
		}
		if remaining.IsPositive() {
			state, err := replayWealth(rows, e.EffectiveDate)
			if err != nil {
				return err
			}
			available := state.Shares.Sub(reservedShares(rows))
			if !available.IsPositive() {
				if data.InsufficientPolicy == "cancel" || len(data.Allocations) == 0 {
					data.Allocations = nil
					data.CancelledForInsufficient = true
					putOrderData(e, data)
					return nil
				}
			} else {
				if err = s.orderNAV(tx, actor, p, e, &data); err != nil {
					return err
				}
				shares := remaining.DivRound(e.TradeNAV, 16).Round(2)
				if !shares.IsPositive() || shares.GreaterThan(available) {
					if data.InsufficientPolicy == "cancel" || (!available.IsPositive() && len(data.Allocations) == 0) {
						data.Allocations = nil
						data.CancelledForInsufficient = true
						putOrderData(e, data)
						return nil
					}
					shares = available
					remaining = shares.Mul(e.TradeNAV).Round(2)
				}
				if shares.IsPositive() {
					data.Allocations = append(data.Allocations, WealthAllocation{Amount: remaining, Shares: shares})
				}
			}
		}
		for _, allocation := range data.Allocations {
			data.AllocatedAmount = data.AllocatedAmount.Add(allocation.Amount)
		}
	case "confirm_redeem":
		if source.Kind != "request_redeem" || orderData(*source).CancelledForInsufficient || orderChild(rows, source.ID, "confirm_redeem", "cancel_redeem") != nil {
			return ErrWealthConflict
		}
		allocs, failed, ready := resolvedAllocations(rows, *source)
		if !ready {
			return ErrWealthConflict
		}
		expected := decimal.Zero
		for _, a := range allocs {
			expected = expected.Add(a.Shares)
		}
		// A transfer funded only by a failed application has no confirmed shares.
		if !e.Shares.Equal(expected) && data.Reason == "" {
			return ErrWealthInvalid
		}
		if e.Shares.IsZero() && !expected.IsZero() {
			return ErrWealthInvalid
		}
		if e.Shares.IsZero() && !e.Amount.Sub(e.Fee).Equal(failed) {
			return ErrWealthInvalid
		}
		state, err := replayWealth(rows, e.EffectiveDate)
		if err != nil {
			return err
		}
		available := state.Shares.Sub(reservedShares(rows)).Add(expected)
		if e.Shares.GreaterThan(available) || e.Fee.GreaterThan(e.Amount) || e.Amount.Sub(e.Fee).LessThan(failed) {
			return ErrWealthInvalid
		}
		if !e.Shares.Equal(expected) {
			delta := e.Shares.Sub(expected)
			for i := len(allocs) - 1; i >= 0 && !delta.IsZero(); i-- {
				a := &allocs[i]
				if a.Shares.IsZero() && (delta.IsNegative() || (a.SourceID > 0 && orderChild(rows, a.SourceID, "confirm_buy") == nil)) {
					continue
				}
				if delta.IsNegative() {
					take := decimal.Min(a.Shares, delta.Abs())
					a.Shares = a.Shares.Sub(take)
					delta = delta.Add(take)
				} else if a.SourceID == 0 {
					a.Shares = a.Shares.Add(delta)
					delta = decimal.Zero
				} else {
					root := orderEvent(rows, a.SourceID)
					confirmed := orderChild(rows, root.ID, "confirm_buy")
					used := decimal.Zero
					for _, other := range rows {
						if !orderRequestActive(rows, other) || other.ID == source.ID {
							continue
						}
						otherAlloc, _, _ := resolvedAllocations(rows, other)
						if finished := orderChild(rows, other.ID, "confirm_redeem"); finished != nil {
							otherAlloc = orderData(*finished).Allocations
						}
						for _, allocation := range otherAlloc {
							if allocation.SourceID == root.ID {
								used = used.Add(allocation.Shares)
							}
						}
					}
					room := decimal.Max(decimal.Zero, confirmed.Shares.Sub(used).Sub(a.Shares))
					take := decimal.Min(room, delta)
					a.Shares = a.Shares.Add(take)
					delta = delta.Sub(take)
				}
			}
			if !delta.IsZero() {
				return ErrWealthConflict
			}
		}
		data.Allocations = allocs
		data.FailedPrincipal = failed
		data.TradeDate = orderData(*source).TradeDate
		if data.NAVDate != "" {
			d, err := wealthDate(data.NAVDate, s.Now())
			if err != nil || d.After(e.EffectiveDate) {
				return ErrWealthInvalid
			}
		}
	case "cancel_redeem":
		if source.Kind != "request_redeem" || orderData(*source).CancelledForInsufficient || orderChild(rows, source.ID, "confirm_redeem", "cancel_redeem") != nil {
			return ErrWealthConflict
		}
	}
	putOrderData(e, data)
	return nil
}
func (s *WealthService) orderNAV(tx *gorm.DB, actor *model.User, p *model.WealthPosition, e *model.WealthEvent, data *WealthOrderData) error {
	loaded, err := s.repo.At(tx).LoadAccounts([]int64{p.AccountID})
	if err != nil {
		return err
	}
	for _, product := range loaded.Products {
		if product.ID == p.ProductID && data.NAVDate < product.InceptionDate.Format("2006-01-02") {
			return ErrWealthInvalid
		}
	}
	var quote *model.WealthQuote
	for _, q := range wealthQuotes(loaded, actor.ID, p.ProductID) {
		if q.NAVDate.Format("2006-01-02") == data.NAVDate {
			copy := q
			quote = &copy
			break
		}
	}
	if e.TradeNAV.IsZero() {
		if quote == nil {
			return ErrWealthPrice
		}
		e.TradeNAV = quote.UnitNAV
	}
	data.NAVSource = "bank_confirmation"
	if quote != nil && e.TradeNAV.Equal(quote.UnitNAV) {
		data.NAVSource = quote.Source
	} else if data.Reason == "" {
		return ErrWealthInvalid
	}
	return nil
}
func wealthOrderViews(rows []model.WealthEvent) []WealthOrderView {
	rows = activeOrderRows(rows)
	out := []WealthOrderView{}
	for _, e := range rows {
		if e.Kind != "subscribe" && e.Kind != "request_redeem" {
			continue
		}
		v := WealthOrderView{EventID: e.ID, Kind: e.Kind, Status: "pending", Amount: e.Amount, Data: orderData(e)}
		if e.Kind == "subscribe" {
			v.ReservedAmount = orderReservedAmount(rows, e.ID)
			if child := orderChild(rows, e.ID, "confirm_buy", "cancel_buy"); child != nil {
				if child.Kind == "confirm_buy" {
					v.Status = "confirmed"
				} else {
					v.Status = "cancelled"
					v.RefundDue = orderRefundDue(rows, e)
					if v.RefundDue.IsZero() {
						v.Status = "refunded"
						if v.ReservedAmount.IsPositive() {
							v.Status = "offset"
						}
					}
				}
			}
		} else {
			allocs, failed, ready := resolvedAllocations(rows, e)
			v.Data.Allocations = allocs
			v.Data.FailedPrincipal = failed
			v.Ready = ready
			if orderData(e).CancelledForInsufficient {
				v.Status = "cancelled"
			}
			for _, a := range allocs {
				v.SuggestedShares = v.SuggestedShares.Add(a.Shares)
			}
			if child := orderChild(rows, e.ID, "confirm_redeem", "cancel_redeem"); child != nil {
				if child.Kind == "confirm_redeem" {
					v.Status = "confirmed"
					v.Data.Allocations = orderData(*child).Allocations
					v.SuggestedShares = child.Shares
				} else {
					v.Status = "cancelled"
				}
			}
		}
		out = append(out, v)
	}
	return out
}
func wealthOrderDepends(e model.WealthEvent, id int64) bool {
	for _, a := range orderData(e).Allocations {
		if a.SourceID == id {
			return true
		}
	}
	return false
}

// Preview reads only cached quotes and order facts. The real write rebuilds the
// allocation under the same product/position locks as all financial events.
func (s *WealthService) PreviewOrder(actor *model.User, id int64, in WealthInput) (model.WealthEvent, error) {
	p, err := s.position(s.repo.DB(), actor, id, false)
	if err != nil {
		return model.WealthEvent{}, err
	}
	if !CanCreateTransaction(actor) {
		return model.WealthEvent{}, ErrTxnForbidden
	}
	if in.Kind != "request_redeem" {
		return model.WealthEvent{}, ErrWealthInvalid
	}
	in.RequestKey = "preview"
	normal, e, err := normalizeWealthOrder(in, s.Now())
	if err != nil {
		return e, err
	}
	rows, err := s.repo.Events(actor.ID, id)
	if err != nil {
		return e, err
	}
	e.ID = 1 << 62
	err = s.prepareWealthOrder(s.repo.DB(), actor, p, normal, &e, activeOrderRows(rows))
	return e, err
}
