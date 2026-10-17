package service

import (
	"jizhang/internal/wealthdata"
	"time"
)

// wealthSlots returns due Shanghai slots after the durable cursor. A first
// startup recovers just the latest slot, including the previous day's 22:00.
func wealthSlots(now time.Time, last *time.Time) []time.Time {
	now = now.In(wealthdata.Shanghai)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, wealthdata.Shanghai)
	if last == nil {
		for _, hour := range []int{22, 18, 12, 8} {
			at := today.Add(time.Duration(hour) * time.Hour)
			if !at.After(now) {
				return []time.Time{at}
			}
		}
		return []time.Time{today.Add(-2 * time.Hour)}
	}
	start := last.In(wealthdata.Shanghai)
	start = time.Date(start.Year(), start.Month(), start.Day(), 0, 0, 0, 0, wealthdata.Shanghai)
	result := []time.Time{}
	for day := start; !day.After(today); day = day.AddDate(0, 0, 1) {
		for _, hour := range []int{8, 12, 18, 22} {
			at := day.Add(time.Duration(hour) * time.Hour)
			if at.After(*last) && !at.After(now) {
				result = append(result, at)
			}
		}
	}
	return result
}
