package accounts

import (
	"context"
	"log/slog"
	"time"
)

// StartMidnightWatcher запускает фоновую задачу на полночь (локальное время Mac).
func (m *Manager) StartMidnightWatcher(ctx context.Context) {
	go m.midnightWatcherLoop(ctx)
}

func (m *Manager) midnightWatcherLoop(ctx context.Context) {
	for {
		now := time.Now()
		// +5 сек после полуночи — даём школе обнулить todaySeconds
		next := time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 5, 0, now.Location())
		wait := time.Until(next)
		slog.Info("midnight watcher scheduled", "next", next.Format(time.RFC3339), "in", wait.Round(time.Second))

		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
			m.onNewDay("midnight watcher", false)
		}
	}
}

func (m *Manager) waitUntilNextDay(ctx context.Context) bool {
	now := time.Now()
	next := time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 5, 0, now.Location())
	wait := time.Until(next)
	if wait <= 0 {
		m.onNewDay("rotation day rollover", true)
		return ctx.Err() == nil
	}

	slog.Info("rotation: waiting for new day", "until", next.Format(time.RFC3339), "in", wait.Round(time.Second))
	timer := time.NewTimer(wait)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		m.onNewDay("rotation day rollover", true)
		return true
	}
}

// onNewDay — новый календарный день: перераспределить график и при необходимости возобновить учёт.
// fromRotation=true когда вызов из цикла ротации (не трогаем rotCancel).
func (m *Manager) onNewDay(source string, fromRotation bool) {
	today := time.Now().Format("2006-01-02")

	m.midnightMu.Lock()
	if m.lastHandledDay == today {
		m.midnightMu.Unlock()
		return
	}
	m.lastHandledDay = today
	m.midnightMu.Unlock()

	slog.Info("new day", "source", source, "date", today)

	m.mu.RLock()
	mode := m.mode
	weekHours := m.weekHours
	accountCount := len(m.byLogin)
	m.mu.RUnlock()

	m.stopAllKeepers()

	if mode != ModeSchedule || accountCount == 0 {
		slog.Info("new day: skip auto-resume", "mode", mode, "accounts", accountCount)
		return
	}

	if err := m.DistributeSchedule(weekHours); err != nil {
		slog.Warn("new day: redistribute failed", "error", err)
		return
	}
	slog.Info("new day: schedule redistributed", "weekHours", weekHours)

	if fromRotation {
		// Ротация уже ждёт — просто продолжим цикл после refresh.
		return
	}

	if m.RotationActive() {
		slog.Info("new day: rotation already active")
		return
	}

	if err := m.StartRotation(); err != nil {
		slog.Warn("new day: failed to start rotation", "error", err)
		return
	}
	slog.Info("new day: rotation started")
}
