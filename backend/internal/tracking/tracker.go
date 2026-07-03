package tracking

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/zutemiss/dashboard-tracker/internal/captcha"
	"github.com/zutemiss/dashboard-tracker/internal/client"
	"github.com/zutemiss/dashboard-tracker/internal/idle"
)

const (
	HeartbeatInterval = 30 * time.Second
	AgentPairInterval = 10 * time.Minute
)

type Tracker struct {
	mu             sync.RWMutex
	captchaMu      sync.Mutex
	client         *client.Dashboard
	running        bool
	cancel         context.CancelFunc
	lastBeat       time.Time
	lastErr        string
	state          client.TrackingState
	solvingCaptcha bool
	lastToday      int
}

func New(c *client.Dashboard) *Tracker {
	return &Tracker{client: c}
}

func (t *Tracker) IsRunning() bool {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.running
}

func (t *Tracker) LastError() string {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.lastErr
}

func (t *Tracker) State() client.TrackingState {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.state
}

func (t *Tracker) SolvingCaptcha() bool {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.solvingCaptcha
}

func (t *Tracker) Start() error {
	t.Stop()
	runCtx, cancel := context.WithCancel(context.Background())
	t.mu.Lock()
	t.cancel = cancel
	t.running = true
	t.lastErr = ""
	t.mu.Unlock()

	slog.Info("tracker started", "deviceId", t.client.DeviceID())
	go t.loop(runCtx)
	return nil
}

func (t *Tracker) Stop() {
	t.mu.Lock()
	if t.cancel != nil {
		t.cancel()
		t.cancel = nil
	}
	t.running = false
	t.mu.Unlock()
	slog.Info("tracker stopped", "deviceId", t.client.DeviceID())
}

func (t *Tracker) loop(ctx context.Context) {
	heartbeat := time.NewTicker(HeartbeatInterval)
	agentPair := time.NewTicker(AgentPairInterval)
	defer heartbeat.Stop()
	defer agentPair.Stop()

	t.beat()

	for {
		select {
		case <-ctx.Done():
			return
		case <-heartbeat.C:
			t.beat()
		case <-agentPair.C:
			if err := t.client.PairAgent(); err != nil {
				slog.Warn("periodic agent pair failed", "error", err)
			} else {
				slog.Info("periodic agent pair ok")
			}
		}
	}
}

func (t *Tracker) beat() {
	isIdle := idle.IsIdle(60 * time.Second)
	resp, err := t.client.Heartbeat(isIdle)
	if err != nil && client.IsDeviceConflict(err) {
		slog.Warn("device conflict — taking over from Chrome/another tab")
		if takeErr := t.client.TakeOverTracking(); takeErr == nil {
			resp, err = t.client.Heartbeat(isIdle)
		} else {
			slog.Error("takeover failed", "error", takeErr)
		}
	}
	t.mu.Lock()
	t.lastBeat = time.Now()
	if err != nil {
		t.lastErr = err.Error()
		t.mu.Unlock()
		slog.Warn("heartbeat FAILED", "idle", isIdle, "error", err)
		return
	}
	st := resp.Tracking.State
	t.lastErr = ""
	t.state = st
	t.mu.Unlock()

	slog.Info("heartbeat OK",
		"active", st.Active,
		"idle", isIdle,
		"challenge", st.ChallengePending,
		"startedAt", st.StartedAt,
	)

	if dash, err := t.client.Dashboard(); err == nil {
		today := dash.Hours.TodaySeconds
		t.mu.Lock()
		if today > t.lastToday {
			slog.Info("time credited", "delta", today-t.lastToday, "todaySeconds", today)
			t.lastToday = today
		}
		t.mu.Unlock()
	}

	if st.ChallengePending {
		go t.solveCaptcha()
		return
	}

	if !st.Active {
		slog.Warn("session inactive — restarting")
		if _, err := t.client.StartTracking(nil, nil, nil); err != nil {
			t.mu.Lock()
			t.lastErr = "restart failed: " + err.Error()
			t.mu.Unlock()
			slog.Error("auto-restart failed", "error", err)
			return
		}
		slog.Info("session auto-restarted")
	}
}

func (t *Tracker) solveCaptcha() {
	if !t.captchaMu.TryLock() {
		return
	}
	defer t.captchaMu.Unlock()

	t.mu.Lock()
	t.solvingCaptcha = true
	t.mu.Unlock()
	defer func() {
		t.mu.Lock()
		t.solvingCaptcha = false
		t.mu.Unlock()
	}()

	slog.Info("captcha: solving")
	for attempt := 1; attempt <= 8; attempt++ {
		cap, err := t.client.FetchCaptcha()
		if err != nil {
			slog.Warn("captcha fetch", "attempt", attempt, "error", err)
			time.Sleep(2 * time.Second)
			continue
		}
		img, err := captcha.DecodeImage(cap.Image)
		if err != nil {
			continue
		}
		solution, err := captcha.Solve(img)
		if err != nil {
			slog.Warn("captcha OCR", "attempt", attempt, "error", err)
			continue
		}
		if err := t.client.VerifyCaptcha(cap.CaptchaID, solution); err != nil {
			slog.Warn("captcha verify rejected", "solution", solution, "error", err)
			time.Sleep(1500 * time.Millisecond)
			continue
		}
		slog.Info("captcha solved", "solution", solution)
		t.beat()
		return
	}
	msg := "captcha не решена"
	t.mu.Lock()
	t.lastErr = msg
	t.mu.Unlock()
	captcha.Notify("TS Tracker", msg)
}
