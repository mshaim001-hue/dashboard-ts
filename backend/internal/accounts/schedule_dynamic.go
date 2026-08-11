package accounts

import (
	"math"
	"time"
)

// DynamicTodayQuota — сколько секунд нужно набрать СЕГОДНЯ (абсолютный todaySeconds),
// с учётом недельной цели, уже засчитанных часов за неделю и оставшихся будней.
// График Пн–Пт используется как веса распределения, не как жёсткие квоты.
func DynamicTodayQuota(sched WeekSchedule, weekTargetHours float64, weekSeconds, todaySeconds int, now time.Time) int {
	if sched.IsEmpty() || weekTargetHours <= 0 {
		return 0
	}

	wd := now.Weekday()
	if wd == time.Saturday || wd == time.Sunday {
		return 0
	}

	weekTargetSec := int(math.Round(weekTargetHours * 3600))
	remainingWeek := weekTargetSec - weekSeconds
	if remainingWeek <= 0 {
		// Неделя закрыта — сегодня больше не нужно.
		return todaySeconds
	}

	weights := sched.remainingWeightsFrom(wd)
	if len(weights) == 0 {
		return 0
	}

	var sumW float64
	for _, w := range weights {
		sumW += w
	}
	if sumW <= 0 {
		sumW = float64(len(weights))
		for i := range weights {
			weights[i] = 1
		}
	}

	todayShare := float64(remainingWeek) * weights[0] / sumW
	todayQuota := roundSecondsToHalfHour(int(math.Round(todayShare)))

	// Не больше, чем физически успеть до полуночи.
	maxToday := todaySeconds + secondsUntilMidnight(now)
	if todayQuota > maxToday {
		todayQuota = maxToday
	}

	if todayQuota < todaySeconds {
		return todaySeconds
	}
	return todayQuota
}

// WeekRemainingSeconds — сколько секунд осталось до недельной цели.
func WeekRemainingSeconds(weekTargetHours float64, weekSeconds int) int {
	target := int(math.Round(weekTargetHours * 3600))
	return max(0, target-weekSeconds)
}

func (w WeekSchedule) remainingWeightsFrom(from time.Weekday) []float64 {
	if from == time.Saturday || from == time.Sunday {
		return nil
	}
	var out []float64
	for d := from; d <= time.Friday; d++ {
		out = append(out, w.hoursForWeekday(d))
	}
	return out
}

func (w WeekSchedule) hoursForWeekday(d time.Weekday) float64 {
	switch d {
	case time.Monday:
		return w.Mon
	case time.Tuesday:
		return w.Tue
	case time.Wednesday:
		return w.Wed
	case time.Thursday:
		return w.Thu
	case time.Friday:
		return w.Fri
	default:
		return 0
	}
}

func secondsUntilMidnight(now time.Time) int {
	end := time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, now.Location())
	sec := int(end.Sub(now).Seconds())
	if sec < 0 {
		return 0
	}
	return sec
}

func roundSecondsToHalfHour(sec int) int {
	const half = 1800
	if sec <= 0 {
		return 0
	}
	return int(math.Round(float64(sec)/half)) * half
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
