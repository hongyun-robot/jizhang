package liability

import (
	"errors"
	"github.com/shopspring/decimal"
	"jizhang/internal/model"
	"time"
)

var (
	ErrInvalid   = errors.New("负债配置或金额无效")
	ErrSetup     = errors.New("请先配置负债账户的账单规则；账单管理仅支持零余额启用")
	ErrConflict  = errors.New("账单或分期已变化，请重新核对")
	ErrManaged   = errors.New("分期和结清关联流水请到负债详情处理")
	ErrInvariant = errors.New("负债分解与资金余额不一致，操作未提交")
)

var Location = time.FixedZone("Asia/Shanghai", 8*3600)

func Date(t time.Time) string { return t.In(Location).Format("2006-01-02") }
func Parse(s string) (time.Time, error) {
	t, e := time.ParseInLocation("2006-01-02", s, Location)
	if e != nil || t.Year() < 2000 || t.Year() > 2100 {
		return time.Time{}, ErrInvalid
	}
	return t, nil
}
func monthDay(y int, m time.Month, day int) time.Time {
	last := time.Date(y, m+1, 0, 0, 0, 0, 0, Location).Day()
	if day > last {
		day = last
	}
	return time.Date(y, m, day, 0, 0, 0, 0, Location)
}
func ValidateRule(day, due, offset int) error {
	if day < 1 || day > 31 || due < 1 || due > 31 || offset < 0 || offset > 1 {
		return ErrInvalid
	}
	// The combination must work in every short month, not only the current month.
	for m := time.January; m <= time.December; m++ {
		c := monthDay(2027, m, day)
		d := monthDay(2027, m+time.Month(offset), due)
		if !d.After(c) {
			return ErrInvalid
		}
	}
	return nil
}
func Range(rule model.LiabilityBillRule, date string) (string, string, string, error) {
	d, e := Parse(date)
	if e != nil {
		return "", "", "", e
	}
	c := monthDay(d.Year(), d.Month(), rule.StatementDay)
	if d.After(c) {
		c = monthDay(d.Year(), d.Month()+1, rule.StatementDay)
	}
	prev := monthDay(c.Year(), c.Month()-1, rule.StatementDay)
	start := prev.AddDate(0, 0, 1)
	// A rule transition starts immediately after the last old-rule cycle.
	effective, e := Parse(rule.EffectiveFrom)
	if e != nil {
		return "", "", "", e
	}
	if effective.After(start) {
		start = effective
	}
	due := monthDay(c.Year(), c.Month()+time.Month(rule.RepaymentMonthOffset), rule.RepaymentDay)
	if !due.After(c) {
		return "", "", "", ErrInvalid
	}
	return Date(start), Date(c), Date(due), nil
}
func ValidAmount(v decimal.Decimal, zero bool) bool {
	return !v.IsNegative() && (zero || v.IsPositive()) && v.LessThanOrEqual(decimal.RequireFromString("9999999999.99")) && v.Round(2).Equal(v)
}
func Spread(total decimal.Decimal, n int, positive bool) ([]decimal.Decimal, error) {
	if n < 1 || n > 120 || !ValidAmount(total, !positive) {
		return nil, ErrInvalid
	}
	cents := total.Mul(decimal.NewFromInt(100)).IntPart()
	base := cents / int64(n)
	if positive && base < 1 {
		return nil, ErrInvalid
	}
	out := make([]decimal.Decimal, n)
	for i := range out {
		c := base
		if i == n-1 {
			c = cents - base*int64(n-1)
		}
		out[i] = decimal.NewFromInt(c).Div(decimal.NewFromInt(100))
	}
	return out, nil
}
