package accounts

import (
	"math"
	"math/rand"
	"time"
)

// WeekSchedule — квоты часов Пн–Пт (сб/вс = 0).
type WeekSchedule struct {
	Mon float64 `json:"mon"`
	Tue float64 `json:"tue"`
	Wed float64 `json:"wed"`
	Thu float64 `json:"thu"`
	Fri float64 `json:"fri"`
}

func (w WeekSchedule) IsEmpty() bool {
	return w.Mon == 0 && w.Tue == 0 && w.Wed == 0 && w.Thu == 0 && w.Fri == 0
}

func (w WeekSchedule) TodayHours(now time.Time) float64 {
	switch now.Weekday() {
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

func (w WeekSchedule) TodaySeconds(now time.Time) int {
	return int(math.Round(w.TodayHours(now) * 3600))
}

func (w WeekSchedule) WeekTotal() float64 {
	return w.Mon + w.Tue + w.Wed + w.Thu + w.Fri
}

func (w WeekSchedule) AsSlice() []float64 {
	return []float64{w.Mon, w.Tue, w.Wed, w.Thu, w.Fri}
}

func scheduleFromSlice(hours []float64) WeekSchedule {
	if len(hours) < 5 {
		hours = append(hours, make([]float64, 5-len(hours))...)
	}
	return WeekSchedule{
		Mon: hours[0],
		Tue: hours[1],
		Wed: hours[2],
		Thu: hours[3],
		Fri: hours[4],
	}
}

// generateSchedule разбивает weekHours на 5 разных дней.
// seedIndex сдвигает «тяжёлые» дни между аккаунтами.
func generateSchedule(weekHours float64, seedIndex int) WeekSchedule {
	if weekHours <= 0 {
		weekHours = 20
	}

	rng := rand.New(rand.NewSource(time.Now().UnixNano() + int64(seedIndex)*9973))

	// Базовые веса с пиком, сдвинутым по аккаунту — чтобы не все сидели одинаково.
	weights := make([]float64, 5)
	peak := seedIndex % 5
	for i := 0; i < 5; i++ {
		dist := absInt(i - peak)
		if dist > 2 {
			dist = 5 - dist
		}
		weights[i] = 1.2 + float64(2-dist)*0.55 + rng.Float64()*0.7
	}

	raw := make([]float64, 5)
	var sumW float64
	for _, w := range weights {
		sumW += w
	}
	for i := range raw {
		raw[i] = weekHours * weights[i] / sumW
	}

	// Округляем до 0.5ч — выглядит естественнее целых.
	hours := make([]float64, 5)
	var sum float64
	for i, v := range raw {
		hours[i] = math.Round(v*2) / 2
		if hours[i] < 0.5 {
			hours[i] = 0.5
		}
		if hours[i] > 8 {
			hours[i] = 8
		}
		sum += hours[i]
	}

	// Подгоняем сумму к weekHours шагами 0.5ч.
	target := math.Round(weekHours*2) / 2
	for sum > target+0.01 {
		idx := maxDayIndex(hours, rng)
		if hours[idx] > 0.5 {
			hours[idx] -= 0.5
			sum -= 0.5
		} else {
			break
		}
	}
	for sum < target-0.01 {
		idx := minDayIndex(hours, rng)
		if hours[idx] < 8 {
			hours[idx] += 0.5
			sum += 0.5
		} else {
			break
		}
	}

	return scheduleFromSlice(hours)
}

func absInt(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

func maxDayIndex(hours []float64, rng *rand.Rand) int {
	best := 0
	for i := 1; i < len(hours); i++ {
		if hours[i] > hours[best] || (hours[i] == hours[best] && rng.Float64() < 0.5) {
			best = i
		}
	}
	return best
}

func minDayIndex(hours []float64, rng *rand.Rand) int {
	best := 0
	for i := 1; i < len(hours); i++ {
		if hours[i] < hours[best] || (hours[i] == hours[best] && rng.Float64() < 0.5) {
			best = i
		}
	}
	return best
}
