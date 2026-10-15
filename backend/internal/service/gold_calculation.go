package service

import (
	"errors"
	"github.com/shopspring/decimal"
	"jizhang/internal/ccb"
	"jizhang/internal/model"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

var (
	ErrGoldInvalid  = errors.New("黄金克数、金额、费用或日期无效；仅支持同日已完成交易")
	ErrGoldMissing  = errors.New("黄金账户或事件不存在")
	ErrGoldHistory  = errors.New("存在后续黄金事件或封账影响，请先处理后续记录")
	ErrGoldConflict = errors.New("黄金请求已使用或记录已更改，请刷新核对")
	ErrGoldManaged  = errors.New("黄金资金记录请到黄金详情统一撤销")
)

type GoldInput struct {
	RequestKey    string  `json:"request_key"`
	Kind          string  `json:"kind"`
	Grams         string  `json:"grams"`
	CashAmount    string  `json:"cash_amount"`
	Fee           *string `json:"fee"`
	TradeDate     string  `json:"trade_date"`
	CashDate      string  `json:"cash_date,omitempty"`
	CashAccountID int64   `json:"cash_account_id"`
	CostMode      string  `json:"cost_mode"`
	Cost          string  `json:"cost"`
	Note          string  `json:"note"`
}
type goldState struct {
	Grams, Cost, Realized, Fees decimal.Decimal
	Imported                    bool
}

func goldNumber(value string, zero bool, places int) (decimal.Decimal, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return decimal.Zero, ErrGoldInvalid
	}
	parts := strings.Split(value, ".")
	if len(parts) > 1 && len(parts[1]) > places {
		return decimal.Zero, ErrGoldInvalid
	}
	n, e := fundNumber(value, zero, places)
	if e != nil {
		return decimal.Zero, ErrGoldInvalid
	}
	return n, nil
}

func normalizeGold(in GoldInput, now time.Time) (model.GoldEvent, error) {
	var e model.GoldEvent
	if strings.TrimSpace(in.RequestKey) == "" || len(in.RequestKey) > 128 || utf8.RuneCountInString(in.Note) > 255 {
		return e, ErrGoldInvalid
	}
	if in.Kind != "opening" && in.Kind != "buy" && in.Kind != "sell" {
		return e, ErrGoldInvalid
	}
	if in.TradeDate == "" {
		in.TradeDate = now.In(ccb.Shanghai).Format("2006-01-02")
	}
	d, err := fundDate(in.TradeDate, now)
	if err != nil || in.CashDate != "" && in.CashDate != in.TradeDate {
		return e, ErrGoldInvalid
	}
	g, err := goldNumber(in.Grams, false, 8)
	if err != nil {
		return e, ErrGoldInvalid
	}
	var amount decimal.Decimal
	if in.Kind == "opening" {
		if in.CashAccountID != 0 || in.Fee != nil || strings.TrimSpace(in.Cost) == "" {
			return e, ErrGoldInvalid
		}
		places := 2
		if in.CostMode == "unit" {
			places = 8
		} else if in.CostMode != "total" {
			return e, ErrGoldInvalid
		}
		amount, err = goldNumber(in.Cost, true, places)
		if err != nil {
			return e, ErrGoldInvalid
		}
		if in.CostMode == "unit" {
			amount = amount.Mul(g).Round(2)
		}
	} else {
		amount, err = goldNumber(in.CashAmount, false, 2)
		if err != nil || in.CashAccountID <= 0 {
			return e, ErrGoldInvalid
		}
		e.CashAccountID = &in.CashAccountID
	}
	if amount.GreaterThan(maxAmount) {
		return e, ErrGoldInvalid
	}
	if in.Fee != nil {
		f, err := goldNumber(*in.Fee, true, 2)
		if err != nil || in.Kind == "buy" && !f.LessThan(amount) || amount.Add(f).GreaterThan(maxAmount) {
			return e, ErrGoldInvalid
		}
		e.Fee = &f
	}
	e.Kind = in.Kind
	e.Grams = g
	e.CashAmount = amount
	e.TradeDate = d
	e.Note = strings.TrimSpace(in.Note)
	return e, nil
}
func replayGold(events []model.GoldEvent, at time.Time) (goldState, error) {
	s := goldState{}
	rows := append([]model.GoldEvent(nil), events...)
	sort.SliceStable(rows, func(i, j int) bool {
		a, b := fundDay(rows[i].TradeDate), fundDay(rows[j].TradeDate)
		if a.Equal(b) {
			return rows[i].ID < rows[j].ID
		}
		return a.Before(b)
	})
	seen := false
	for _, e := range rows {
		if e.VoidedAt != nil || fundDay(e.TradeDate).After(fundDay(at)) {
			continue
		}
		switch e.Kind {
		case "opening":
			if seen {
				return s, ErrGoldHistory
			}
			s.Imported = true
			s.Grams = e.Grams
			s.Cost = e.CashAmount
		case "buy":
			s.Grams = s.Grams.Add(e.Grams)
			s.Cost = s.Cost.Add(e.CashAmount)
		case "sell":
			if e.Grams.GreaterThan(s.Grams) || !e.Grams.IsPositive() {
				return s, ErrGoldInvalid
			}
			cost := s.Cost
			if !e.Grams.Equal(s.Grams) {
				cost = s.Cost.Mul(e.Grams).DivRound(s.Grams, 16)
			}
			s.Grams = s.Grams.Sub(e.Grams)
			s.Cost = s.Cost.Sub(cost)
			s.Realized = s.Realized.Add(e.CashAmount.Sub(cost))
			if s.Grams.IsZero() {
				s.Cost = decimal.Zero
			}
		default:
			return s, ErrGoldInvalid
		}
		if e.Fee != nil {
			s.Fees = s.Fees.Add(*e.Fee)
		}
		seen = true
		if s.Cost.IsNegative() || s.Cost.GreaterThan(maxAmount) || s.Grams.IsNegative() || s.Grams.GreaterThan(maxAmount) || s.Realized.Abs().GreaterThan(maxAmount) {
			return s, ErrGoldInvalid
		}
	}
	return s, nil
}
