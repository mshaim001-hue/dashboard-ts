package accounts

import (
	"testing"
	"time"
)

func TestBackfillPastDaysSingleMissingMonday(t *testing.T) {
	m := &Manager{}
	st := &accountState{
		schedule: WeekSchedule{Mon: 5, Tue: 3.5},
		weekDays: WeekDaysActual{
			WeekStart: "2026-08-10",
			Days:      map[string]int{"tue": 7380},
		},
	}
	tuesday := time.Date(2026, 8, 11, 12, 0, 0, 0, time.Local)
	m.backfillPastDays(st, 7380, 46800+7380, tuesday)
	if got := st.weekDays.Days["mon"]; got != 46800 {
		t.Fatalf("mon backfill = %d, want 46800 (13h)", got)
	}
}

func TestBackfillPastDaysDoesNotOverwrite(t *testing.T) {
	m := &Manager{}
	st := &accountState{
		schedule: WeekSchedule{Mon: 5, Tue: 3.5},
		weekDays: WeekDaysActual{
			Days: map[string]int{"mon": 40000, "tue": 7380},
		},
	}
	tuesday := time.Date(2026, 8, 11, 12, 0, 0, 0, time.Local)
	m.backfillPastDays(st, 7380, 47380, tuesday)
	if got := st.weekDays.Days["mon"]; got != 40000 {
		t.Fatalf("mon overwritten: %d", got)
	}
}
