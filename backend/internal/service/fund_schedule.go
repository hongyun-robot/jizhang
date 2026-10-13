package service

import (
	"time"

	"jizhang/internal/fuyao"
)

const (
	FundEveningCron     = "0,30 19-22 * * 1-5"
	FundLastEveningCron = "0 23 * * 1-5"
	FundMorningCron     = "0 4 * * *"
)

type fundRun struct {
	Morning    bool
	TargetDate time.Time
	SlotStart  time.Time
}

func fundWeekday(t time.Time) bool {
	return t.Weekday() != time.Saturday && t.Weekday() != time.Sunday
}

// A startup can resume the current slot, but persisted claims prevent another
// fetch within that slot. The morning recovery window lasts until 04:30.
func fundRunAt(now time.Time) (fundRun, bool) {
	local := now.In(fuyao.Shanghai)
	if local.Hour() == 4 && local.Minute() < 30 && fundWeekday(local.AddDate(0, 0, -1)) {
		return fundRun{
			Morning:    true,
			TargetDate: fundDay(local.AddDate(0, 0, -1)),
			SlotStart:  time.Date(local.Year(), local.Month(), local.Day(), 4, 0, 0, 0, fuyao.Shanghai),
		}, true
	}
	if fundWeekday(local) && ((local.Hour() >= 19 && local.Hour() < 23) || (local.Hour() == 23 && local.Minute() == 0)) {
		return fundRun{
			TargetDate: fundDay(local),
			SlotStart:  time.Date(local.Year(), local.Month(), local.Day(), local.Hour(), local.Minute()/30*30, 0, 0, fuyao.Shanghai),
		}, true
	}
	return fundRun{}, false
}

func fundSameDay(date *time.Time, target time.Time) bool {
	return date != nil && fundDay(*date).Equal(target)
}
