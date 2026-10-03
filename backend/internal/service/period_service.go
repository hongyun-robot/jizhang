package service

import (
	"errors"
	"strconv"
	"time"
)

// ErrInvalidSettlementDay 结算日非法。
var ErrInvalidSettlementDay = errors.New("结算日须为 1-28")

// PeriodService 账期口径的唯一实现（设计 D4/D7）：
//   - 结算日 S（1-28，缺省 1 = 自然月）
//   - 账期 [S 日, 次月 S-1 日]，period_key = 结束日所在月 YYYY-MM
//     （设计示例：S=26 时 2026-09-26～10-25 记为 2026-10；S=1 时与自然月一致）
//   - 写入时物化 period_key，改结算日不回溯历史
//
// 预算、统计、封账、流水归期全部调用本服务，禁止各自换算（任务 7.1）。
type PeriodService struct {
	settings SettingsStore
}

// SettingsStore 配置读取（SettingRepo 满足）。
type SettingsStore interface {
	Get(key string) (string, error)
	Set(key, value string) error
}

func NewPeriodService(settings SettingsStore) *PeriodService {
	return &PeriodService{settings: settings}
}

// SettlementDay 当前结算日；未配置或非法时回退 1（自然月）。
func (s *PeriodService) SettlementDay() int {
	v, err := s.settings.Get("settlement_day")
	if err != nil || v == "" {
		return 1
	}
	day, err := strconv.Atoi(v)
	if err != nil || day < 1 || day > 28 {
		return 1
	}
	return day
}

// SetSettlementDay 写入结算日（1-28）。
func (s *PeriodService) SetSettlementDay(day int) error {
	if day < 1 || day > 28 {
		return ErrInvalidSettlementDay
	}
	return s.settings.Set("settlement_day", strconv.Itoa(day))
}

// KeyForDate 计算某笔发生日期归属的 period_key（结束月 YYYY-MM，设计 D7）：
// 所在账期为 [S 日, 次月 S-1 日]，key 取账期结束日所在月。
// S=1（自然月）时即发生日期所在月。
func (s *PeriodService) KeyForDate(d time.Time) string {
	sDay := s.SettlementDay()
	y, m := d.Year(), int(d.Month())
	if d.Day() >= sDay {
		m++
		if m > 12 {
			m, y = 1, y+1
		}
	}
	// end = 账期半开右端（次月 S 日），结束日 = end - 1 天
	end := time.Date(y, time.Month(m), sDay, 0, 0, 0, 0, time.Local)
	return end.AddDate(0, 0, -1).Format("2006-01")
}

// Range 返回某 period_key 对应的 [start, end) 半开区间（key = 结束月）。
func (s *PeriodService) Range(periodKey string) (time.Time, time.Time, bool) {
	t, err := time.ParseInLocation("2006-01", periodKey, time.Local)
	if err != nil {
		return time.Time{}, time.Time{}, false
	}
	sDay := s.SettlementDay()
	if sDay == 1 {
		// 自然月：key 所在月整月
		start := time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.Local)
		return start, start.AddDate(0, 1, 0), true
	}
	// 结束月 key：右端 = key 月 S 日，左端 = 前一月 S 日
	end := time.Date(t.Year(), t.Month(), sDay, 0, 0, 0, 0, time.Local)
	return end.AddDate(0, -1, 0), end, true
}

// PrevKey 上一个账期 key；key 非法返回 "",false。
func (s *PeriodService) PrevKey(periodKey string) (string, bool) {
	t, err := time.ParseInLocation("2006-01", periodKey, time.Local)
	if err != nil {
		return "", false
	}
	return t.AddDate(0, -1, 0).Format("2006-01"), true
}

// CurrentKey 当前所处账期。
func (s *PeriodService) CurrentKey(now time.Time) string {
	return s.KeyForDate(now)
}
