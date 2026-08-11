package accounts

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"time"
)

// StartRotation запускает очередь: один Chrome, аккаунты по графику до квоты дня.
func (m *Manager) StartRotation() error {
	if m.Mode() != ModeSchedule {
		return fmt.Errorf("авто-ротация доступна только в режиме «По графику»")
	}

	m.rotMu.Lock()
	if m.rotating {
		m.rotMu.Unlock()
		return fmt.Errorf("ротация уже запущена")
	}
	ctx, cancel := context.WithCancel(context.Background())
	m.rotCancel = cancel
	m.rotating = true
	m.rotLogin = ""
	m.rotMu.Unlock()

	m.mu.Lock()
	m.autoRotation = true
	m.mu.Unlock()
	_ = m.saveSettings()

	m.stopAllKeepers()

	go m.rotationLoop(ctx)
	slog.Info("rotation started")
	return nil
}

func (m *Manager) StopRotation() {
	m.rotMu.Lock()
	if m.rotCancel != nil {
		m.rotCancel()
		m.rotCancel = nil
	}
	wasRotating := m.rotating
	m.rotating = false
	m.rotLogin = ""
	m.rotMu.Unlock()

	if wasRotating {
		m.stopAllKeepers()
		m.mu.Lock()
		m.autoRotation = false
		m.mu.Unlock()
		_ = m.saveSettings()
		slog.Info("rotation stopped")
	}
}

func (m *Manager) RotationActive() bool {
	m.rotMu.Lock()
	defer m.rotMu.Unlock()
	return m.rotating
}

func (m *Manager) RotationLogin() string {
	m.rotMu.Lock()
	defer m.rotMu.Unlock()
	return m.rotLogin
}

func (m *Manager) rotationLoop(ctx context.Context) {
	defer func() {
		m.rotMu.Lock()
		m.rotating = false
		m.rotLogin = ""
		if m.rotCancel != nil {
			m.rotCancel = nil
		}
		m.rotMu.Unlock()
		slog.Info("rotation loop finished")
	}()

	skipped := make(map[string]bool)

	for {
		if ctx.Err() != nil {
			return
		}

		login, err := m.nextRotationAccount(skipped)
		if err != nil {
			slog.Warn("rotation: cannot pick account", "error", err)
			return
		}
		if login == "" {
			slog.Info("rotation: all daily quotas done for today")
			if !m.waitUntilNextDay(ctx) {
				return
			}
			skipped = make(map[string]bool)
			continue
		}

		m.rotMu.Lock()
		m.rotLogin = login
		m.rotMu.Unlock()

		if err := m.startKeeper(login); err != nil {
			slog.Warn("rotation: failed to start account", "login", login, "error", err)
			skipped[login] = true
			continue
		}

		m.waitKeeperDone(ctx, login)

		if ctx.Err() != nil {
			return
		}

		m.rotMu.Lock()
		m.rotLogin = ""
		m.rotMu.Unlock()

		select {
		case <-ctx.Done():
			return
		case <-time.After(3 * time.Second):
		}
	}
}

func (m *Manager) nextRotationAccount(skipped map[string]bool) (string, error) {
	m.mu.RLock()
	logins := make([]string, 0, len(m.byLogin))
	for login := range m.byLogin {
		logins = append(logins, login)
	}
	m.mu.RUnlock()
	if len(logins) == 0 {
		return "", fmt.Errorf("нет аккаунтов")
	}
	sort.Strings(logins)

	now := time.Now()
	for _, login := range logins {
		if skipped[login] {
			continue
		}
		m.mu.RLock()
		st := m.byLogin[login]
		m.mu.RUnlock()
		if st == nil || st.schedule.IsEmpty() {
			continue
		}
		quota := m.todayQuotaFor(st, now)
		if quota <= 0 {
			continue
		}
		dash, err := st.client.Dashboard()
		if err != nil {
			return "", err
		}
		if dash.Hours.TodaySeconds >= quota {
			continue
		}
		return login, nil
	}
	return "", nil
}

func (m *Manager) waitKeeperDone(ctx context.Context, login string) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.mu.RLock()
			st := m.byLogin[login]
			m.mu.RUnlock()
			if st == nil || !st.keeper.IsRunning() {
				slog.Info("rotation: keeper stopped", "login", login)
				return
			}
			if st.keeper.QuotaReached() {
				slog.Info("rotation: quota reached", "login", login)
				return
			}
		}
	}
}
