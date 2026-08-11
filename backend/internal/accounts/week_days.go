package accounts

import "time"

var weekdayKeys = []string{"mon", "tue", "wed", "thu", "fri"}

// WeekDaysActual — фактические секунды по дням текущей недели (Пн–Пт).
type WeekDaysActual struct {
	WeekStart string         `json:"weekStart"`
	Days      map[string]int `json:"days"`
}

func (w WeekSchedule) hoursForKey(key string) float64 {
	switch key {
	case "mon":
		return w.Mon
	case "tue":
		return w.Tue
	case "wed":
		return w.Wed
	case "thu":
		return w.Thu
	case "fri":
		return w.Fri
	default:
		return 0
	}
}

func mondayOf(t time.Time) time.Time {
	wd := int(t.Weekday())
	if wd == 0 {
		wd = 7
	}
	d := t.AddDate(0, 0, -(wd - 1))
	return time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, t.Location())
}

func weekdayKey(w time.Weekday) string {
	switch w {
	case time.Monday:
		return "mon"
	case time.Tuesday:
		return "tue"
	case time.Wednesday:
		return "wed"
	case time.Thursday:
		return "thu"
	case time.Friday:
		return "fri"
	default:
		return ""
	}
}

func weekdayIndex(w time.Weekday) int {
	switch w {
	case time.Monday:
		return 0
	case time.Tuesday:
		return 1
	case time.Wednesday:
		return 2
	case time.Thursday:
		return 3
	case time.Friday:
		return 4
	default:
		return -1
	}
}

func isWeekday(t time.Time) bool {
	wd := t.Weekday()
	return wd >= time.Monday && wd <= time.Friday
}

func (m *Manager) recordDayActual(st *accountState, todaySec int) {
	now := time.Now()
	key := weekdayKey(now.Weekday())
	if key == "" {
		return
	}

	weekStart := mondayOf(now).Format("2006-01-02")
	if st.weekDays.WeekStart != weekStart || st.weekDays.Days == nil {
		st.weekDays = WeekDaysActual{
			WeekStart: weekStart,
			Days:      make(map[string]int),
		}
		st.weekDaysDirty = true
	}

	if st.weekDays.Days[key] != todaySec {
		st.weekDays.Days[key] = todaySec
		st.weekDaysDirty = true
	}
}

// backfillPastDays восстанавливает пропущенные прошлые дни из weekSeconds школы.
// weekSeconds = сумма Пн..сегодня; todaySec = только сегодня.
func (m *Manager) backfillPastDays(st *accountState, todaySec, weekSec int, now time.Time) {
	if weekSec <= 0 || st.weekDays.Days == nil {
		return
	}
	idx := weekdayIndex(now.Weekday())
	if idx <= 0 {
		return
	}

	knownPast := 0
	var missing []string
	for i := 0; i < idx; i++ {
		k := weekdayKeys[i]
		if v, ok := st.weekDays.Days[k]; ok && v > 0 {
			knownPast += v
		} else {
			missing = append(missing, k)
		}
	}
	if len(missing) == 0 {
		return
	}

	remainder := weekSec - todaySec - knownPast
	if remainder <= 0 {
		return
	}

	if len(missing) == 1 {
		st.weekDays.Days[missing[0]] = remainder
		st.weekDaysDirty = true
		return
	}

	var weightSum float64
	weights := make([]float64, len(missing))
	for i, k := range missing {
		w := st.schedule.hoursForKey(k)
		if w <= 0 {
			w = 1
		}
		weights[i] = w
		weightSum += w
	}
	left := remainder
	for i, k := range missing {
		var share int
		if i == len(missing)-1 {
			share = left
		} else {
			share = int(float64(remainder) * weights[i] / weightSum)
			left -= share
		}
		if share > 0 {
			st.weekDays.Days[k] = share
			st.weekDaysDirty = true
		}
	}
}
