package service

import (
	"testing"
	"time"
)

type memSettings struct{ m map[string]string }

func (s *memSettings) Get(k string) (string, error) { return s.m[k], nil }
func (s *memSettings) Set(k, v string) error        { s.m[k] = v; return nil }

func mustDate(t *testing.T, s string) time.Time {
	t.Helper()
	d, err := time.ParseInLocation("2006-01-02", s, time.Local)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

// TestKeyForDateNaturalMonth 结算日=1（自然月）。
func TestKeyForDateNaturalMonth(t *testing.T) {
	ps := NewPeriodService(&memSettings{m: map[string]string{}})
	cases := map[string]string{
		"2026-01-01": "2026-01",
		"2026-01-31": "2026-01",
		"2026-12-31": "2026-12",
	}
	for in, want := range cases {
		if got := ps.KeyForDate(mustDate(t, in)); got != want {
			t.Errorf("自然月 KeyForDate(%s) = %s, want %s", in, got, want)
		}
	}
}

// TestKeyForDateSettlement15 结算日=15 的跨期归属（结束月键）：
// 15 日起归下一账期（键=结束月），15 日前在 [上月15, 本月15) 期内（键=本月）。
func TestKeyForDateSettlement15(t *testing.T) {
	ps := NewPeriodService(&memSettings{m: map[string]string{"settlement_day": "15"}})
	cases := map[string]string{
		"2026-03-15": "2026-04", // 正好 15 日 → 新期 [3/15,4/15)，结束 4/14 → 键 4 月
		"2026-03-20": "2026-04",
		"2026-03-14": "2026-03", // 15 日前 → [2/15,3/15)，结束 3/14 → 键 3 月
		"2026-03-01": "2026-03",
		"2026-01-14": "2026-01", // 跨年
		"2026-01-15": "2026-02",
	}
	for in, want := range cases {
		if got := ps.KeyForDate(mustDate(t, in)); got != want {
			t.Errorf("结算日15 KeyForDate(%s) = %s, want %s", in, got, want)
		}
	}
}

// TestRangeHalfOpen 结算日=15 的账期区间（键=结束月）：key 2026-04 = [3/15, 4/15)。
func TestRangeHalfOpen(t *testing.T) {
	ps := NewPeriodService(&memSettings{m: map[string]string{"settlement_day": "15"}})
	start, end, ok := ps.Range("2026-04")
	if !ok {
		t.Fatal("Range 解析失败")
	}
	if start != mustDate(t, "2026-03-15") {
		t.Errorf("start = %v, want 2026-03-15", start)
	}
	if end != mustDate(t, "2026-04-15") {
		t.Errorf("end = %v, want 2026-04-15", end)
	}
	if _, _, ok := ps.Range("bad-key"); ok {
		t.Error("非法 key 应返回 false")
	}
}

// TestSettlementDay26Boundary 结算日 26 的跨期边界（结束月键）：
// 26 日前在 [上月26, 本月26) 期（键=本月），26 日起在新期（键=次月）。
func TestSettlementDay26Boundary(t *testing.T) {
	ps := NewPeriodService(&memSettings{m: map[string]string{"settlement_day": "26"}})
	cases := map[string]string{
		"2026-09-25": "2026-09", // 26 日之前 → 期 [8/26,9/25]，结束 9/25 → 键 9 月
		"2026-09-26": "2026-10", // 正好 26 日 → 新期 [9/26,10/25]，结束 10/25 → 键 10 月
		"2026-09-01": "2026-09",
		"2026-10-25": "2026-10",
		"2026-10-26": "2026-11",
	}
	for in, want := range cases {
		if got := ps.KeyForDate(mustDate(t, in)); got != want {
			t.Errorf("结算日26 KeyForDate(%s) = %s, want %s", in, got, want)
		}
	}
}

// TestSpecScenario26CurrentPeriod specs/billing-periods「结算日为 26 日」：
// 当前 2026-10-10 → 本期 2026-09-26 至 2026-10-25，上期 2026-08-26 至 2026-09-25；
// 及「跨期日期归期」：2026-09-25 归 8/26-9/25 期，而非 9/26 起新期（任务 7.1）。
func TestSpecScenario26CurrentPeriod(t *testing.T) {
	ps := NewPeriodService(&memSettings{m: map[string]string{"settlement_day": "26"}})

	cur := ps.CurrentKey(mustDate(t, "2026-10-10"))
	start, end, ok := ps.Range(cur)
	if !ok {
		t.Fatal("本期 Range 解析失败")
	}
	if start != mustDate(t, "2026-09-26") || end.AddDate(0, 0, -1) != mustDate(t, "2026-10-25") {
		t.Errorf("本期区间 = %v~%v（末日）, want 2026-09-26~2026-10-25", start, end.AddDate(0, 0, -1))
	}

	prev, ok := ps.PrevKey(cur)
	if !ok {
		t.Fatal("上期键解析失败")
	}
	pStart, pEnd, ok := ps.Range(prev)
	if !ok {
		t.Fatal("上期 Range 解析失败")
	}
	if pStart != mustDate(t, "2026-08-26") || pEnd.AddDate(0, 0, -1) != mustDate(t, "2026-09-25") {
		t.Errorf("上期区间 = %v~%v（末日）, want 2026-08-26~2026-09-25", pStart, pEnd.AddDate(0, 0, -1))
	}

	// 跨期日期归期：9/25 与 9/26 必须落在不同账期
	if k := ps.KeyForDate(mustDate(t, "2026-09-25")); k == ps.KeyForDate(mustDate(t, "2026-09-26")) {
		t.Errorf("9/25 与 9/26 不应同归 %s", k)
	}
}

// TestSettlementDayFallback 非法配置回退自然月；写入校验 1-28。
func TestSettlementDayFallback(t *testing.T) {
	st := &memSettings{m: map[string]string{"settlement_day": "99"}}
	ps := NewPeriodService(st)
	if ps.SettlementDay() != 1 {
		t.Errorf("非法结算日应回退 1，got %d", ps.SettlementDay())
	}
	if err := ps.SetSettlementDay(0); err == nil {
		t.Error("结算日 0 应被拒绝")
	}
	if err := ps.SetSettlementDay(28); err != nil {
		t.Errorf("结算日 28 应被接受: %v", err)
	}
	if ps.SettlementDay() != 28 {
		t.Errorf("写入后读回 = %d, want 28", ps.SettlementDay())
	}
}

// TestPrevKey 跨年上期。
func TestPrevKey(t *testing.T) {
	ps := NewPeriodService(&memSettings{m: map[string]string{}})
	if got, ok := ps.PrevKey("2026-01"); !ok || got != "2025-12" {
		t.Errorf("PrevKey(2026-01) = %s,%v; want 2025-12,true", got, ok)
	}
}
