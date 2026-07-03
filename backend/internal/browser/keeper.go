package browser

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/go-rod/rod/lib/proto"
	"github.com/zutemiss/dashboard-tracker/internal/captcha"
	"github.com/zutemiss/dashboard-tracker/internal/client"
)

// Keeper runs the real dashboard in Chrome so official JS handles heartbeat,
// fingerprint, idle detection, agent pairing and captcha UI.
type Keeper struct {
	mu          sync.RWMutex
	client      *client.Dashboard
	running     bool
	cancel      context.CancelFunc
	browser     *rod.Browser
	page        *rod.Page
	lastError   string
	lastToday   int
	lastTodayAt time.Time
	stalled     bool
	tickCount   int
}

func NewKeeper(c *client.Dashboard) *Keeper {
	return &Keeper{client: c}
}

func (k *Keeper) IsRunning() bool {
	k.mu.RLock()
	defer k.mu.RUnlock()
	return k.running
}

func (k *Keeper) LastError() string {
	k.mu.RLock()
	defer k.mu.RUnlock()
	return k.lastError
}

func (k *Keeper) Stalled() bool {
	k.mu.RLock()
	defer k.mu.RUnlock()
	return k.stalled
}

func (k *Keeper) Start(ctx context.Context) error {
	k.mu.Lock()
	if k.running {
		k.mu.Unlock()
		slog.Warn("keeper already running")
		return nil
	}
	runCtx, cancel := context.WithCancel(ctx)
	k.cancel = cancel
	k.running = true
	k.lastError = ""
	k.stalled = false
	k.tickCount = 0
	k.mu.Unlock()

	slog.Info("keeper starting")
	go k.run(runCtx)
	return nil
}

func (k *Keeper) Stop() {
	slog.Info("keeper stopping")
	k.mu.Lock()
	cancel := k.cancel
	k.cancel = nil
	k.running = false
	page := k.page
	browser := k.browser
	k.page = nil
	k.browser = nil
	k.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	if page != nil {
		_ = page.Close()
	}
	if browser != nil {
		_ = browser.Close()
	}
}

func (k *Keeper) run(ctx context.Context) {
	defer func() {
		k.mu.Lock()
		k.running = false
		k.mu.Unlock()
		slog.Info("keeper stopped")
	}()

	if err := k.client.PairAgent(); err != nil {
		slog.Warn("keeper: agent pair failed", "error", err)
	}

	path, has := launcher.LookPath()
	if !has {
		k.setError("Chrome не найден")
		return
	}

	slog.Info("keeper: launching Chrome", "bin", path)
	u := launcher.New().Bin(path).
		Headless(false).
		Devtools(false).
		Set("window-size", "1280,800").
		MustLaunch()

	browser := rod.New().ControlURL(u).MustConnect()
	k.mu.Lock()
	k.browser = browser
	k.mu.Unlock()

	defer func() {
		slog.Info("keeper: closing Chrome")
		_ = browser.Close()
	}()

	page := browser.MustPage("about:blank")
	k.setupConsoleCapture(page)
	k.mu.Lock()
	k.page = page
	k.mu.Unlock()

	if err := k.injectCookies(page); err != nil {
		k.setError("cookies: " + err.Error())
		return
	}

	slog.Info("keeper: navigating to dashboard")
	page.MustNavigate(client.BaseURL + "/").MustWaitLoad()
	time.Sleep(2 * time.Second)

	k.logBrowserState(page, "after load")
	k.clickStartTracking(page)

	tick := time.NewTicker(25 * time.Second)
	defer tick.Stop()

	for {
		select {
		case <-ctx.Done():
			slog.Info("keeper: context cancelled")
			return
		case <-tick.C:
			k.tick(page)
		}
	}
}

func (k *Keeper) setupConsoleCapture(page *rod.Page) {
	go page.EachEvent(func(e *proto.RuntimeConsoleAPICalled) {
		args := make([]string, 0, len(e.Args))
		for _, a := range e.Args {
			args = append(args, a.Value.String())
		}
		slog.Info("chrome console", "type", e.Type, "text", fmt.Sprint(args))
	})()
}

func (k *Keeper) logBrowserState(page *rod.Page, label string) {
	info, err := page.Info()
	if err != nil {
		slog.Warn("keeper: page info", "label", label, "error", err)
		return
	}
	state, _ := page.Eval(`() => {
		const btns = [...document.querySelectorAll('button')].map(b => (b.textContent||'').trim()).filter(Boolean);
		return {buttons: btns.slice(0, 8), title: document.title};
	}`)
	slog.Info("keeper: browser state",
		"label", label,
		"url", info.URL,
		"state", state.Value.String(),
	)
}

func (k *Keeper) injectCookies(page *rod.Page) error {
	cookies := k.client.ExportCookies(client.BaseURL)
	slog.Info("keeper: injecting cookies", "count", len(cookies))
	if len(cookies) == 0 {
		return fmt.Errorf("нет cookies — войди заново")
	}
	var params []*proto.NetworkCookieParam
	names := make([]string, 0, len(cookies))
	for _, c := range cookies {
		names = append(names, c.Name)
		params = append(params, &proto.NetworkCookieParam{
			Name:     c.Name,
			Value:    c.Value,
			Domain:   "dashboard.tomorrow-school.ai",
			Path:     "/",
			Secure:   true,
			HTTPOnly: c.HttpOnly,
		})
	}
	slog.Debug("keeper: cookie names", "names", names)
	return page.SetCookies(params)
}

func (k *Keeper) clickStartTracking(page *rod.Page) {
	res, err := page.Eval(`() => {
		const btns = [...document.querySelectorAll('button')];
		const btn = btns.find(b => (b.textContent || '').includes('Запустить учёт'));
		if (btn) { btn.click(); return 'clicked'; }
		const active = btns.find(b => (b.textContent || '').includes('Учёт идёт'));
		if (active) return 'already_active';
		return 'button_not_found';
	}`)
	if err != nil {
		slog.Warn("keeper: click start failed", "error", err)
		return
	}
	slog.Info("keeper: start tracking click", "result", res.Value.String())
}

func (k *Keeper) tick(page *rod.Page) {
	k.mu.Lock()
	k.tickCount++
	tickN := k.tickCount
	k.mu.Unlock()

	dash, err := k.client.Dashboard()
	if err != nil {
		k.setError("dashboard: " + err.Error())
		return
	}

	today := dash.Hours.TodaySeconds
	k.mu.Lock()
	prev := k.lastToday
	prevAt := k.lastTodayAt
	k.mu.Unlock()

	slog.Info("keeper tick",
		"n", tickN,
		"tracking_active", dash.Tracking.Active,
		"tracking_startedAt", dash.Tracking.StartedAt,
		"challenge_pending", dash.Tracking.ChallengePending,
		"todaySeconds", today,
		"weekSeconds", dash.Hours.WeekSeconds,
		"prevToday", prev,
	)

	if today > prev {
		delta := today - prev
		slog.Info("keeper: time credited", "delta_seconds", delta, "todaySeconds", today)
		k.mu.Lock()
		k.lastToday = today
		k.lastTodayAt = time.Now()
		k.stalled = false
		k.lastError = ""
		k.mu.Unlock()
	} else if dash.Tracking.Active {
		if prevAt.IsZero() {
			k.mu.Lock()
			k.lastTodayAt = time.Now()
			k.mu.Unlock()
		} else if time.Since(prevAt) > 4*time.Minute {
			k.mu.Lock()
			k.stalled = true
			k.mu.Unlock()
			slog.Warn("keeper: time STALLED — not increasing",
				"todaySeconds", today,
				"stalled_minutes", time.Since(prevAt).Minutes(),
				"tracking_active", dash.Tracking.Active,
			)
			captcha.Notify("TS Tracker", "Время не растёт 4+ мин — смотри логи")
		}
	}

	if !dash.Tracking.Active {
		slog.Warn("keeper: tracking INACTIVE — restarting via browser only (no Go API to avoid device conflict)")
		k.clickStartTracking(page)
		k.logBrowserState(page, "after restart click")
		// НЕ вызываем client.StartTracking — другой deviceId ломает сессию Chrome
	}

	if dash.Tracking.ChallengePending {
		slog.Info("keeper: captcha challenge pending")
		go k.solveCaptchaInPage(page)
	}

	_, _ = page.Eval(`() => { document.dispatchEvent(new Event('visibilitychange')); window.dispatchEvent(new Event('focus')); }`)
}

func (k *Keeper) solveCaptchaInPage(page *rod.Page) {
	slog.Info("keeper: solving captcha")
	result, err := page.Eval(`async () => {
		const me = await fetch('/api/v1/auth/me', {credentials:'include'}).then(r=>r.json());
		if (!me.authenticated) return {error:'not auth'};
		const cap = await fetch('/api/v1/tracking/captcha', {
			credentials:'include',
			headers: {'X-CSRF-Token': me.csrfToken, 'Accept':'application/json'}
		}).then(r=>r.json());
		return {csrf: me.csrfToken, captchaId: cap.captchaId, image: cap.image ? cap.image.length : 0};
	}`)
	if err != nil {
		slog.Warn("keeper: captcha fetch in page failed", "error", err)
		return
	}
	slog.Info("keeper: captcha fetched", "meta", result.Value.String())

	result2, err := page.Eval(`async () => {
		const me = await fetch('/api/v1/auth/me', {credentials:'include'}).then(r=>r.json());
		const cap = await fetch('/api/v1/tracking/captcha', {
			credentials:'include',
			headers: {'X-CSRF-Token': me.csrfToken, 'Accept':'application/json'}
		}).then(r=>r.json());
		return {csrf: me.csrfToken, captchaId: cap.captchaId, image: cap.image};
	}`)
	if err != nil {
		return
	}

	data := result2.Value.Map()
	if data["error"].String() != "" {
		slog.Warn("keeper: captcha page not authenticated")
		return
	}
	csrf := data["csrf"].String()
	captchaID := data["captchaId"].String()
	image := data["image"].String()
	if captchaID == "" || image == "" {
		slog.Warn("keeper: empty captcha data")
		return
	}

	img, err := captcha.DecodeImage(image)
	if err != nil {
		slog.Warn("keeper: captcha decode failed", "error", err)
		return
	}
	solution, err := captcha.Solve(img)
	if err != nil {
		slog.Warn("keeper: captcha OCR failed", "error", err)
		captcha.Notify("TS Tracker — captcha", "Введи код в окне Chrome (5 сек)")
		_, _ = page.Activate()
		return
	}

	slog.Info("keeper: captcha OCR", "solution", solution, "captchaId", captchaID)

	verifyPayload, _ := json.Marshal(map[string]string{
		"captchaId": captchaID,
		"solution":  solution,
	})
	_, err = page.Eval(`async (csrf, payload) => {
		const r = await fetch('/api/v1/tracking/captcha/verify', {
			method:'POST',
			credentials:'include',
			headers: {'Content-Type':'application/json','X-CSRF-Token': csrf,'Accept':'application/json'},
			body: payload
		});
		if (!r.ok) throw new Error(await r.text());
		return true;
	}`, csrf, string(verifyPayload))
	if err != nil {
		slog.Warn("keeper: captcha verify rejected", "solution", solution, "error", err)
		return
	}
	slog.Info("keeper: captcha solved", "solution", solution)
}

func (k *Keeper) setError(msg string) {
	k.mu.Lock()
	k.lastError = msg
	k.mu.Unlock()
	slog.Error("keeper error", "msg", msg)
}
