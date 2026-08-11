package accounts

import (
	"testing"
	"time"
)

func TestDynamicTodayQuotaMidWeek(t *testing.T) {
	sched := WeekSchedule{Mon: 4, Tue: 4, Wed: 4, Thu: 4, Fri: 4}
	// Вторник 10:00, за неделю уже 8ч, цель 20ч → осталось 12ч на 4 дня → сегодня ~3ч
	tue := time.Date(2026, 8, 11, 10, 0, 0, 0, time.Local)
	quota := DynamicTodayQuota(sched, 20, 8*3600, 0, tue)
	if quota < 2*3600 || quota > 4*3600 {
		t.Fatalf("expected ~3h today, got %d sec (%vh)", quota, float64(quota)/3600)
	}
}

func TestDynamicTodayQuotaWeekDone(t *testing.T) {
	sched := WeekSchedule{Mon: 4, Tue: 4, Wed: 4, Thu: 4, Fri: 4}
	wed := time.Date(2026, 8, 12, 14, 0, 0, 0, time.Local)
	quota := DynamicTodayQuota(sched, 20, 20*3600, 3600, wed)
	if quota != 3600 {
		t.Fatalf("week done: quota should equal todaySeconds, got %d", quota)
	}
}

func TestDynamicTodayQuotaCappedByMidnight(t *testing.T) {
	sched := WeekSchedule{Mon: 5, Tue: 5, Wed: 5, Thu: 5, Fri: 0}
	// Пятница 23:00, много осталось — но до полуночи только 1ч
	fri := time.Date(2026, 8, 14, 23, 0, 0, 0, time.Local)
	quota := DynamicTodayQuota(sched, 20, 0, 0, fri)
	if quota > 3600 {
		t.Fatalf("expected cap ~1h until midnight, got %d sec", quota)
	}
}

func TestDynamicTodayQuotaWeekend(t *testing.T) {
	sched := WeekSchedule{Mon: 4, Tue: 4, Wed: 4, Thu: 4, Fri: 4}
	sat := time.Date(2026, 8, 15, 12, 0, 0, 0, time.Local)
	if q := DynamicTodayQuota(sched, 20, 0, 0, sat); q != 0 {
		t.Fatalf("weekend quota should be 0, got %d", q)
	}
}
