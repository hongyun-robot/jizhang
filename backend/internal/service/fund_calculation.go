package service

import (
	"errors"
	"github.com/shopspring/decimal"
	"jizhang/internal/fuyao"
	"jizhang/internal/model"
	"regexp"
	"sort"
	"strings"
	"time"
)

var (
	ErrFundInvalid  = errors.New("基金资料、金额、份额或日期无效")
	ErrFundMissing  = errors.New("基金账户或持仓不存在")
	ErrFundCode     = errors.New("请选择已确认的人民币场外净值型基金代码")
	ErrFundPrice    = errors.New("尚无有效基金净值，请保留资料并稍后重试")
	ErrFundConflict = errors.New("请求已使用或该基金事件已登记，请刷新核对")
	ErrFundManaged  = errors.New("基金资金记录请到基金详情统一处理")
	ErrFundHistory  = errors.New("已有后续基金事件，请先处理后续记录")
)
var fundNumberPattern = regexp.MustCompile(`^[0-9]+(?:\.[0-9]{1,8})?$`)
var fundCodePattern = regexp.MustCompile(`^[0-9]{6}(?:\.OF)?$`)

// fundDay normalizes SQL DATE values without applying a second timezone conversion.
func fundDay(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}
func fundToday(t time.Time) time.Time { return fundDay(t.In(fuyao.Shanghai)) }
func fundDate(s string, now time.Time) (time.Time, error) {
	d, e := time.Parse("2006-01-02", s)
	if e != nil || d.Format("2006-01-02") != s || d.Year() < 2000 || d.Year() > 2100 || d.After(fundToday(now)) {
		return time.Time{}, ErrFundInvalid
	}
	return d, nil
}
func fundNumber(s string, zero bool, places int) (decimal.Decimal, error) {
	s = strings.TrimSpace(s)
	if s == "" && zero {
		s = "0"
	}
	if !fundNumberPattern.MatchString(s) {
		return decimal.Zero, ErrFundInvalid
	}
	d, e := decimal.NewFromString(s)
	if e != nil || d.GreaterThan(maxAmount) || !d.Round(int32(places)).Equal(d) || (!zero && !d.IsPositive()) {
		return decimal.Zero, ErrFundInvalid
	}
	return d, nil
}
func fundCanonicalCode(code string) (string, error) {
	code = strings.ToUpper(strings.TrimSpace(code))
	if !fundCodePattern.MatchString(code) {
		return "", ErrFundCode
	}
	if !strings.HasSuffix(code, ".OF") {
		code += ".OF"
	}
	return code, nil
}

type fundState struct {
	Shares    decimal.Decimal
	Cost      decimal.Decimal
	Cash      decimal.Decimal
	Realized  decimal.Decimal
	Dividends decimal.Decimal
	Fees      decimal.Decimal
	BasisDate *time.Time
	Imported  bool
}

// replayFund applies economic effects separately from bank settlement effects.
// No write or external quote request is performed during valuation.
func replayFund(events []model.FundEvent, at time.Time) (fundState, error) {
	at = fundDay(at)
	s := fundState{}
	rows := append([]model.FundEvent(nil), events...)
	sort.SliceStable(rows, func(i, j int) bool {
		a, b := fundDay(rows[i].EffectiveDate), fundDay(rows[j].EffectiveDate)
		if a.Equal(b) {
			return rows[i].ID < rows[j].ID
		}
		return a.Before(b)
	})
	parents := map[int64]model.FundEvent{}
	for _, e := range rows {
		parents[e.ID] = e
	}
	for _, e := range rows {
		if e.VoidedAt != nil {
			continue
		}
		effective := fundDay(e.EffectiveDate)
		active := !effective.After(at)
		if (e.Kind == "opening" || e.Kind == "buy") && (s.BasisDate == nil || effective.Before(*s.BasisDate)) {
			d := effective
			s.BasisDate = &d
			s.Imported = e.Kind == "opening"
		}
		rights := effective
		if e.RightsDate != nil {
			rights = fundDay(*e.RightsDate)
		}
		cashActive := e.CashDate != nil && !fundDay(*e.CashDate).After(at)
		switch e.Kind {
		case "opening":
			if active {
				s.Shares = s.Shares.Add(e.Shares)
				s.Cost = s.Cost.Add(e.Amount)
			}
		case "buy":
			if cashActive {
				s.Cash = s.Cash.Add(e.Amount)
			}
			if active {
				s.Shares = s.Shares.Add(e.Shares)
				s.Cost = s.Cost.Add(e.Amount)
				s.Cash = s.Cash.Sub(e.Amount)
				s.Fees = s.Fees.Add(e.Fee)
			}
		case "redeem":
			net := e.Amount.Sub(e.Fee)
			if active {
				if !e.Shares.IsPositive() || e.Shares.GreaterThan(s.Shares) {
					return s, ErrFundInvalid
				}
				cost := s.Cost
				if !e.Shares.Equal(s.Shares) {
					cost = s.Cost.Mul(e.Shares).DivRound(s.Shares, 16)
				}
				s.Shares = s.Shares.Sub(e.Shares)
				s.Cost = s.Cost.Sub(cost)
				if s.Shares.IsZero() {
					s.Cost = decimal.Zero
				}
				s.Realized = s.Realized.Add(net.Sub(cost))
				s.Fees = s.Fees.Add(e.Fee)
				s.Cash = s.Cash.Add(net)
			}
			if cashActive {
				s.Cash = s.Cash.Sub(net)
			}
		case "cash_dividend":
			if !rights.After(at) {
				s.Cash = s.Cash.Add(e.Amount)
				s.Dividends = s.Dividends.Add(e.Amount)
			}
			if cashActive {
				s.Cash = s.Cash.Sub(e.Amount)
			}
		case "reinvest":
			if !rights.After(at) {
				s.Cash = s.Cash.Add(e.Amount)
				s.Dividends = s.Dividends.Add(e.Amount)
			}
			if active {
				s.Shares = s.Shares.Add(e.Shares)
				s.Cost = s.Cost.Add(e.Amount)
				s.Cash = s.Cash.Sub(e.Amount)
				s.Fees = s.Fees.Add(e.Fee)
			}
		case "settlement":
			if e.SourceEventID == nil {
				return s, ErrFundInvalid
			}
			p, ok := parents[*e.SourceEventID]
			if !ok || p.VoidedAt != nil {
				return s, ErrFundInvalid
			}
			if active {
				if p.Kind == "buy" {
					s.Cash = s.Cash.Add(e.Amount)
				} else {
					s.Cash = s.Cash.Sub(e.Amount)
				}
			}
		default:
			return s, ErrFundInvalid
		}
		if s.Shares.IsNegative() || s.Cost.IsNegative() || s.Cost.GreaterThan(maxAmount) {
			return s, ErrFundInvalid
		}
	}
	return s, nil
}

func fundEventsFor(rows []model.FundEvent, id int64) []model.FundEvent {
	out := []model.FundEvent{}
	for _, e := range rows {
		if e.PositionID == id {
			out = append(out, e)
		}
	}
	return out
}
func fundNAVsFor(rows []model.FundNAV, id int64) []model.FundNAV {
	out := []model.FundNAV{}
	for _, n := range rows {
		if n.InstrumentID == id {
			n.NAVDate = fundDay(n.NAVDate)
			out = append(out, n)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].NAVDate.Before(out[j].NAVDate) })
	return out
}
func fundPriceAt(rows []model.FundNAV, at time.Time, shares decimal.Decimal) (*model.FundNAV, bool) {
	var found *model.FundNAV
	fallback := false
	for i := len(rows) - 1; i >= 0; i-- {
		if rows[i].NAVDate.After(fundDay(at)) {
			continue
		}
		if shares.Mul(rows[i].UnitNAV).GreaterThan(maxAmount) {
			fallback = true
			continue
		}
		n := rows[i]
		found = &n
		break
	}
	return found, fallback
}
func fundDaily(events []model.FundEvent, navs []model.FundNAV, index int, dividends []model.FundDividend) (*decimal.Decimal, string, error) {
	if index <= 0 {
		return nil, "no_history", nil
	}
	cur, prev := navs[index], navs[index-1]
	a, e := replayFund(events, prev.NAVDate)
	if e != nil {
		return nil, "invalid", e
	}
	b, e := replayFund(events, cur.NAVDate)
	if e != nil {
		return nil, "invalid", e
	}
	if b.BasisDate == nil || b.BasisDate.After(cur.NAVDate) || (b.Imported && b.BasisDate.After(prev.NAVDate)) {
		return nil, "no_history", nil
	}
	for _, d := range dividends {
		ex := fundDay(d.ExDate)
		if !ex.After(prev.NAVDate) || ex.After(cur.NAVDate) {
			continue
		}
		reg, _ := replayFund(events, fundDay(d.RegistrationDate))
		entitled := reg.Shares.IsPositive() || (b.Imported && b.BasisDate.After(fundDay(d.RegistrationDate)) && !b.BasisDate.After(ex) && b.Shares.IsPositive())
		matched := false
		for _, v := range events {
			if v.VoidedAt == nil && (v.Kind == "cash_dividend" || v.Kind == "reinvest") && v.RightsDate != nil && fundDay(*v.RightsDate).Equal(ex) {
				matched = true
				break
			}
		}
		if entitled && !matched {
			return nil, "dividend_review", nil
		}
	}
	pnl := b.Shares.Mul(cur.UnitNAV).Sub(a.Shares.Mul(prev.UnitNAV))
	within := func(d time.Time) bool { d = fundDay(d); return d.After(prev.NAVDate) && !d.After(cur.NAVDate) }
	for _, v := range events {
		if v.VoidedAt != nil {
			continue
		}
		if within(v.EffectiveDate) {
			switch v.Kind {
			case "buy", "reinvest":
				pnl = pnl.Sub(v.Amount)
			case "redeem":
				pnl = pnl.Add(v.Amount.Sub(v.Fee))
			}
		}
		if (v.Kind == "cash_dividend" || v.Kind == "reinvest") && v.RightsDate != nil && within(*v.RightsDate) {
			pnl = pnl.Add(v.Amount)
		}
	}
	pnl = pnl.Round(2)
	return &pnl, "ready", nil
}
