package service

import (
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"jizhang/internal/repository"
)

func d(t *testing.T, s string) time.Time {
	t.Helper()
	v, err := time.ParseInLocation("2006-01-02", s, time.Local)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func dec(s string) decimal.Decimal { return decimal.RequireFromString(s) }

// TestPrevMonthWindow 覆盖环比区间计算：
// statistics spec 场景 2026-09-26~10-25 → 2026-08-26~09-25，
// 自然月窗口、跨年、月末收敛（2/28、闰年 2/29）。
func TestPrevMonthWindow(t *testing.T) {
	cases := []struct {
		start, end         string
		wantStart, wantEnd string
	}{
		{"2026-09-26", "2026-10-25", "2026-08-26", "2026-09-25"}, // spec 场景
		{"2026-10-01", "2026-10-31", "2026-09-01", "2026-09-30"}, // 自然月
		{"2026-01-01", "2026-01-31", "2025-12-01", "2025-12-31"}, // 跨年
		{"2026-03-31", "2026-03-31", "2026-02-28", "2026-02-28"}, // 2026 非闰年收敛
		{"2024-03-31", "2024-03-31", "2024-02-29", "2024-02-29"}, // 闰年收敛
		{"2026-12-15", "2026-12-20", "2026-11-15", "2026-11-20"},
	}
	for _, c := range cases {
		gs, ge := PrevMonthWindow(d(t, c.start), d(t, c.end))
		if gs.Format("2006-01-02") != c.wantStart || ge.Format("2006-01-02") != c.wantEnd {
			t.Errorf("PrevMonthWindow(%s, %s) = %s~%s, want %s~%s",
				c.start, c.end,
				gs.Format("2006-01-02"), ge.Format("2006-01-02"),
				c.wantStart, c.wantEnd)
		}
	}
}

// TestBucketDailyDay 趋势-按天：零填充且金额归位。
func TestBucketDailyDay(t *testing.T) {
	rows := []repository.DailyRow{{
		D:       d(t, "2026-10-02"),
		Income:  dec("100"),
		Expense: dec("30"),
	}}
	pts := bucketDaily(rows, d(t, "2026-10-01"), d(t, "2026-10-03"), "day")
	if len(pts) != 3 {
		t.Fatalf("day 点数 = %d, want 3", len(pts))
	}
	if !pts[0].Income.IsZero() || !pts[2].Expense.IsZero() {
		t.Error("空日应为零填充")
	}
	if pts[1].Key != "2026-10-02" || !pts[1].Balance.Equal(dec("70")) {
		t.Errorf("10-02 = %s/%s/%s, want 100/30/70",
			pts[1].Income, pts[1].Expense, pts[1].Balance)
	}
}

// TestBucketDailyWeek 趋势-按周：周一对齐、首尾桶裁剪到区间内。
func TestBucketDailyWeek(t *testing.T) {
	rows := []repository.DailyRow{
		{D: d(t, "2026-09-30"), Income: dec("100")}, // 周三
		{D: d(t, "2026-10-04"), Expense: dec("50")}, // 周日（同周）
		{D: d(t, "2026-10-06"), Expense: dec("20")}, // 下周二
	}
	// 2026-09-28 是周一（2026-W40），区间 09-30~10-07
	pts := bucketDaily(rows, d(t, "2026-09-30"), d(t, "2026-10-07"), "week")
	if len(pts) != 2 {
		t.Fatalf("week 点数 = %d, want 2", len(pts))
	}
	if pts[0].Key != "2026-09-28" || pts[0].Label != "2026-W40" {
		t.Errorf("首桶 = %s(%s), want 2026-09-28(2026-W40)", pts[0].Key, pts[0].Label)
	}
	if !pts[0].Income.Equal(dec("100")) || !pts[0].Expense.Equal(dec("50")) || !pts[0].Balance.Equal(dec("50")) {
		t.Errorf("首桶合计 = %s/%s/%s, want 100/50/50", pts[0].Income, pts[0].Expense, pts[0].Balance)
	}
	if pts[1].Key != "2026-10-05" || !pts[1].Expense.Equal(dec("20")) {
		t.Errorf("次桶 = %s, expense %s, want 2026-10-05/20", pts[1].Key, pts[1].Expense)
	}
}

// TestBucketDailyMonth 趋势-按月：跨月归桶。
func TestBucketDailyMonth(t *testing.T) {
	rows := []repository.DailyRow{
		{D: d(t, "2026-09-20"), Income: dec("500")},
		{D: d(t, "2026-10-05"), Income: dec("700"), Expense: dec("100")},
	}
	pts := bucketDaily(rows, d(t, "2026-09-15"), d(t, "2026-10-10"), "month")
	if len(pts) != 2 {
		t.Fatalf("month 点数 = %d, want 2", len(pts))
	}
	if pts[0].Key != "2026-09" || pts[0].Label != "2026年9月" || !pts[0].Income.Equal(dec("500")) {
		t.Errorf("9月桶 = %s(%s) income=%s, want 2026-09(2026年9月)/500", pts[0].Key, pts[0].Label, pts[0].Income)
	}
	if pts[1].Key != "2026-10" || !pts[1].Balance.Equal(dec("600")) {
		t.Errorf("10月桶 = %s balance=%s, want 2026-10/600", pts[1].Key, pts[1].Balance)
	}
}

// TestPercentOf 环比幅度：上期 0 → nil；负基数取绝对值。
func TestPercentOf(t *testing.T) {
	if p := percentOf(dec("100"), dec("0")); p != nil {
		t.Errorf("上期为 0 应返回 nil, got %v", *p)
	}
	p := percentOf(dec("110"), dec("100"))
	if p == nil || *p != 10 {
		t.Errorf("110 vs 100 = %v, want 10", p)
	}
	// 亏损收窄：-50 相对 -100 → +50%
	p = percentOf(dec("-50"), dec("-100"))
	if p == nil || *p != 50 {
		t.Errorf("-50 vs -100 = %v, want 50", p)
	}
	// 支出下降：80 相对 100 → -20%
	p = percentOf(dec("80"), dec("100"))
	if p == nil || *p != -20 {
		t.Errorf("80 vs 100 = %v, want -20", p)
	}
}
