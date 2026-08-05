package accounts

import (
	"math"
	"testing"
)

func TestGenerateScheduleSumsToWeekHours(t *testing.T) {
	for _, week := range []float64{15, 20, 25} {
		for i := 0; i < 6; i++ {
			sch := generateSchedule(week, i)
			sum := sch.WeekTotal()
			if math.Abs(sum-week) > 0.01 {
				t.Fatalf("seed %d week %v: sum=%v schedule=%+v", i, week, sum, sch)
			}
			for _, h := range sch.AsSlice() {
				if h < 0.5 || h > 8 {
					t.Fatalf("hour out of range: %v in %+v", h, sch)
				}
			}
		}
	}
}

func TestGenerateScheduleVariesBySeed(t *testing.T) {
	a := generateSchedule(20, 0)
	b := generateSchedule(20, 1)
	if a == b {
		t.Fatalf("expected different schedules for different seeds: %+v", a)
	}
}
