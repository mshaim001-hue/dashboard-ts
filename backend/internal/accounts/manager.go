package accounts

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/zutemiss/dashboard-tracker/internal/browser"
	"github.com/zutemiss/dashboard-tracker/internal/client"
	"github.com/zutemiss/dashboard-tracker/internal/device"
)

const (
	ModeManual   = "manual"
	ModeSchedule = "schedule"
)

type Account struct {
	Login             string        `json:"login"`
	DisplayName       string        `json:"displayName"`
	Tracking          bool          `json:"tracking"`
	LastError         string        `json:"lastError,omitempty"`
	Stalled           bool          `json:"stalled"`
	TodaySeconds      int           `json:"todaySeconds"`
	WeekSeconds       int           `json:"weekSeconds"`
	SessionActive     bool          `json:"sessionActive"`
	StartedAt         string        `json:"startedAt,omitempty"`
	ChallengePending  bool          `json:"challengePending"`
	VirtualHostname   string        `json:"virtualHostname,omitempty"`
	Schedule          *WeekSchedule `json:"schedule,omitempty"`
	QuotaTodaySeconds int           `json:"quotaTodaySeconds,omitempty"`
	QuotaReached      bool          `json:"quotaReached,omitempty"`
	WeekRemainingSec  int            `json:"weekRemainingSeconds,omitempty"`
	WeekDaysActual    map[string]int `json:"weekDaysActual,omitempty"`

	client  *client.Dashboard
	keeper  *browser.Keeper
	dir     string
}

type Manager struct {
	mu        sync.RWMutex
	dataDir   string
	chromeDir string
	byLogin   map[string]*accountState
	active    string
	mode      string
	weekHours float64

	rotMu     sync.Mutex
	rotCancel context.CancelFunc
	rotating     bool
	rotLogin     string
	autoRotation bool

	midnightMu      sync.Mutex
	lastHandledDay  string
}

type accountState struct {
	login       string
	displayName string
	hostname    string
	client      *client.Dashboard
	keeper      *browser.Keeper
	dir         string
	lastToday   int
	lastTodayAt time.Time
	stalled        bool
	schedule       WeekSchedule
	weekDays       WeekDaysActual
	weekDaysDirty  bool
}

type storedAccount struct {
	Login       string         `json:"login"`
	DisplayName string         `json:"displayName"`
	DeviceID    string         `json:"deviceId"`
	Hostname    string         `json:"hostname,omitempty"`
	Cookies     []cookieRecord `json:"cookies"`
	Schedule    *WeekSchedule   `json:"schedule,omitempty"`
	WeekDays    *WeekDaysActual `json:"weekDays,omitempty"`
}

type settingsFile struct {
	Mode          string  `json:"mode"`
	WeekHours     float64 `json:"weekHours"`
	AutoRotation  bool    `json:"autoRotation,omitempty"`
}

type cookieRecord struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

func NewManager(dataDir string) *Manager {
	m := &Manager{
		dataDir:   dataDir,
		chromeDir: filepath.Join(dataDir, "chrome-keeper"),
		byLogin:   make(map[string]*accountState),
		mode:      ModeManual,
		weekHours: 20,
	}
	m.loadSettings()
	m.loadAll()
	go m.StartMidnightWatcher(context.Background())
	m.maybeResumeRotation()
	return m
}

func (m *Manager) maybeResumeRotation() {
	m.mu.RLock()
	mode := m.mode
	auto := m.autoRotation
	n := len(m.byLogin)
	m.mu.RUnlock()
	if mode != ModeSchedule || !auto || n == 0 || !isWeekday(time.Now()) {
		return
	}
	if err := m.StartRotation(); err != nil {
		slog.Warn("auto-resume rotation failed", "error", err)
	}
}

func (m *Manager) loadAll() {
	dir := filepath.Join(m.dataDir, "accounts")
	entries, err := os.ReadDir(dir)
	if err != nil {
		m.migrateOldCookies()
		return
	}

	var stored []storedAccount
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		path := filepath.Join(dir, e.Name(), "account.json")
		b, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var sa storedAccount
		if json.Unmarshal(b, &sa) != nil || sa.Login == "" {
			continue
		}
		stored = append(stored, sa)
	}

	usedHosts := make(map[string]bool)
	for i := range stored {
		migrated := m.ensureStoredHostname(&stored[i], usedHosts)
		st := m.buildState(stored[i])
		m.byLogin[stored[i].Login] = st
		if m.active == "" {
			m.active = stored[i].Login
		}
		if migrated {
			if err := m.saveAccount(st, cookiesFromStored(stored[i])); err != nil {
				slog.Warn("save migrated hostname failed", "login", stored[i].Login, "error", err)
			} else {
				slog.Info("virtual hostname assigned", "login", stored[i].Login, "hostname", st.hostname)
			}
		}
		slog.Info("account loaded", "login", stored[i].Login, "hostname", st.hostname)
	}

	if len(m.byLogin) == 0 {
		m.migrateOldCookies()
	}
}

func (m *Manager) migrateOldCookies() {
	path := filepath.Join(m.dataDir, "cookies.json")
	b, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var records []cookieRecord
	if json.Unmarshal(b, &records) != nil || len(records) == 0 {
		return
	}
	var cookies []*http.Cookie
	for _, r := range records {
		cookies = append(cookies, &http.Cookie{Name: r.Name, Value: r.Value})
	}
	if _, err := m.AddFromCookies(cookies); err != nil {
		slog.Warn("migrate old cookies failed", "error", err)
		return
	}
	_ = os.Rename(path, path+".bak")
	slog.Info("migrated old cookies.json to accounts")
}

func (m *Manager) buildState(sa storedAccount) *accountState {
	acctDir := filepath.Join(m.dataDir, "accounts", sa.Login)
	fp := client.Fingerprint(acctDir, sa.Login, sa.Hostname)
	c := client.New(sa.DeviceID, device.DeviceName(sa.Hostname), fp)
	var cookies []*http.Cookie
	for _, r := range sa.Cookies {
		cookies = append(cookies, &http.Cookie{Name: r.Name, Value: r.Value})
	}
	c.ImportCookies(client.BaseURL, cookies)
	if _, err := c.AuthMe(); err != nil {
		slog.Warn("account csrf refresh failed", "login", sa.Login, "error", err)
	}
	st := &accountState{
		login:       sa.Login,
		displayName: sa.DisplayName,
		hostname:    sa.Hostname,
		client:      c,
		keeper:      browser.NewKeeper(c, m.chromeDir),
		dir:         acctDir,
	}
	if sa.Schedule != nil {
		st.schedule = *sa.Schedule
	}
	if sa.WeekDays != nil {
		st.weekDays = *sa.WeekDays
	}
	return st
}

func (m *Manager) AddFromCookies(cookies []*http.Cookie) (*Account, error) {
	tmpDir := filepath.Join(m.dataDir, "_tmp_auth")
	_ = os.MkdirAll(tmpDir, 0o700)
	fp := client.Fingerprint(tmpDir, "", "tmp")
	c := client.New(generateUUID(), device.DeviceName("tmp"), fp)
	c.ImportCookies(client.BaseURL, cookies)

	me, err := c.AuthMe()
	if err != nil || !me.Authenticated || me.User == nil {
		return nil, fmt.Errorf("сессия недействительна")
	}

	login := me.User.Login
	acctDir := filepath.Join(m.dataDir, "accounts", login)
	_ = os.MkdirAll(acctDir, 0o700)

	deviceIDPath := filepath.Join(acctDir, "device_id")
	deviceID := loadOrCreateID(deviceIDPath)

	hostname := m.loadOrGenerateHostname(acctDir)

	fp = client.Fingerprint(acctDir, login, hostname)
	c = client.New(deviceID, device.DeviceName(hostname), fp)
	c.ImportCookies(client.BaseURL, cookies)

	st := &accountState{
		login:       login,
		displayName: me.User.DisplayName,
		hostname:    hostname,
		client:      c,
		keeper:      browser.NewKeeper(c, m.chromeDir),
		dir:         acctDir,
	}

	if err := m.saveAccount(st, cookies); err != nil {
		return nil, err
	}

	m.mu.Lock()
	m.byLogin[login] = st
	m.active = login
	m.mu.Unlock()

	slog.Info("account added", "login", login, "hostname", hostname)
	return m.snapshot(st, m.Mode()), nil
}

func (m *Manager) saveAccount(st *accountState, cookies []*http.Cookie) error {
	records := make([]cookieRecord, 0, len(cookies))
	for _, c := range cookies {
		records = append(records, cookieRecord{Name: c.Name, Value: c.Value})
	}
	sa := storedAccount{
		Login:       st.login,
		DisplayName: st.displayName,
		DeviceID:    st.client.DeviceID(),
		Hostname:    st.hostname,
		Cookies:     records,
	}
	if !st.schedule.IsEmpty() {
		sch := st.schedule
		sa.Schedule = &sch
	}
	if st.weekDays.Days != nil {
		wd := st.weekDays
		sa.WeekDays = &wd
	}
	st.weekDaysDirty = false
	b, err := json.MarshalIndent(sa, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(st.dir, "account.json"), b, 0o600)
}

func (m *Manager) Mode() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.mode
}

func (m *Manager) WeekHours() float64 {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.weekHours
}

func (m *Manager) SetMode(mode string) error {
	if mode != ModeManual && mode != ModeSchedule {
		return fmt.Errorf("режим должен быть manual или schedule")
	}
	m.mu.Lock()
	m.mode = mode
	m.mu.Unlock()
	if err := m.saveSettings(); err != nil {
		return err
	}
	// В ручном режиме снимаем лимиты у всех keeper.
	if mode == ModeManual {
		m.mu.RLock()
		defer m.mu.RUnlock()
		for _, st := range m.byLogin {
			st.keeper.SetDailyQuota(0)
		}
	}
	return nil
}

func (m *Manager) SetWeekHours(h float64) error {
	if h < 1 || h > 60 {
		return fmt.Errorf("недельная цель должна быть от 1 до 60 часов")
	}
	m.mu.Lock()
	m.weekHours = h
	m.mu.Unlock()
	return m.saveSettings()
}

// DistributeSchedule раздаёт разный Пн–Пт график всем аккаунтам.
func (m *Manager) DistributeSchedule(weekHours float64) error {
	if weekHours <= 0 {
		m.mu.RLock()
		weekHours = m.weekHours
		m.mu.RUnlock()
	}
	if weekHours < 1 {
		weekHours = 20
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	logins := make([]string, 0, len(m.byLogin))
	for login := range m.byLogin {
		logins = append(logins, login)
	}
	if len(logins) == 0 {
		return fmt.Errorf("нет аккаунтов")
	}
	// Стабильный порядок, чтобы seedIndex был предсказуемым при повторных вызовах.
	sort.Strings(logins)

	m.weekHours = weekHours
	m.mode = ModeSchedule

	for i, login := range logins {
		st := m.byLogin[login]
		st.schedule = generateSchedule(weekHours, i)
		cookies := m.readCookies(st)
		if err := m.saveAccount(st, cookies); err != nil {
			return fmt.Errorf("%s: %w", login, err)
		}
		// Если уже крутится — обновить квоту на сегодня (динамически).
		if st.keeper.IsRunning() {
			st.keeper.SetDailyQuota(m.todayQuotaFor(st, time.Now()))
		}
		slog.Info("schedule assigned",
			"login", login,
			"week", st.schedule.WeekTotal(),
			"mon", st.schedule.Mon,
			"tue", st.schedule.Tue,
			"wed", st.schedule.Wed,
			"thu", st.schedule.Thu,
			"fri", st.schedule.Fri,
		)
	}

	if err := m.saveSettingsLocked(); err != nil {
		return err
	}
	return nil
}

func (m *Manager) readCookies(st *accountState) []*http.Cookie {
	path := filepath.Join(st.dir, "account.json")
	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var sa storedAccount
	if json.Unmarshal(b, &sa) != nil {
		return nil
	}
	return cookiesFromStored(sa)
}

func (m *Manager) loadSettings() {
	path := filepath.Join(m.dataDir, "settings.json")
	b, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var s settingsFile
	if json.Unmarshal(b, &s) != nil {
		return
	}
	if s.Mode == ModeManual || s.Mode == ModeSchedule {
		m.mode = s.Mode
	}
	if s.WeekHours >= 1 {
		m.weekHours = s.WeekHours
	}
	m.autoRotation = s.AutoRotation
}

func (m *Manager) saveSettings() error {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.saveSettingsLocked()
}

func (m *Manager) saveSettingsLocked() error {
	s := settingsFile{Mode: m.mode, WeekHours: m.weekHours, AutoRotation: m.autoRotation}
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	_ = os.MkdirAll(m.dataDir, 0o700)
	return os.WriteFile(filepath.Join(m.dataDir, "settings.json"), b, 0o600)
}

func (m *Manager) List() []*Account {
	m.mu.RLock()
	mode := m.mode
	logins := make([]string, 0, len(m.byLogin))
	for login := range m.byLogin {
		logins = append(logins, login)
	}
	m.mu.RUnlock()
	sort.Strings(logins)

	out := make([]*Account, 0, len(logins))
	for _, login := range logins {
		m.mu.RLock()
		st := m.byLogin[login]
		m.mu.RUnlock()
		if st == nil {
			continue
		}
		out = append(out, m.snapshot(st, mode))
	}
	return out
}

func (m *Manager) Active() *Account {
	m.mu.RLock()
	login := m.active
	st := m.byLogin[login]
	mode := m.mode
	m.mu.RUnlock()
	if st == nil {
		return nil
	}
	return m.snapshot(st, mode)
}

func (m *Manager) Select(login string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.byLogin[login]; !ok {
		return fmt.Errorf("аккаунт не найден")
	}
	m.active = login
	return nil
}

func (m *Manager) Remove(login string) error {
	m.mu.Lock()
	st, ok := m.byLogin[login]
	if !ok {
		m.mu.Unlock()
		return fmt.Errorf("аккаунт не найден")
	}
	st.keeper.Stop()
	delete(m.byLogin, login)
	if m.active == login {
		m.active = ""
		for k := range m.byLogin {
			m.active = k
			break
		}
	}
	m.mu.Unlock()
	return os.RemoveAll(st.dir)
}

func (m *Manager) StartTracking(login string) error {
	if m.RotationActive() {
		return fmt.Errorf("идёт авто-ротация — сначала останови её")
	}
	return m.startKeeper(login)
}

func (m *Manager) startKeeper(login string) error {
	m.mu.RLock()
	st, ok := m.byLogin[login]
	mode := m.mode
	m.mu.RUnlock()
	if !ok {
		return fmt.Errorf("аккаунт не найден")
	}

	slog.Info("start keeper", "login", login, "mode", mode)
	if _, err := st.client.AuthMe(); err != nil {
		return fmt.Errorf("сессия истекла — войди снова: %w", err)
	}

	quotaSec := 0
	if mode == ModeSchedule {
		if st.schedule.IsEmpty() {
			return fmt.Errorf("сначала распредели график")
		}
		now := time.Now()
		quotaSec = m.todayQuotaFor(st, now)
		if quotaSec <= 0 && st.schedule.TodayHours(now) <= 0 {
			return fmt.Errorf("сегодня выходной по графику (сб/вс)")
		}
		dash, _ := st.client.Dashboard()
		if dash != nil {
			if WeekRemainingSeconds(m.WeekHours(), dash.Hours.WeekSeconds) <= 0 {
				return fmt.Errorf("недельная цель уже выполнена")
			}
			if dash.Hours.TodaySeconds >= quotaSec {
				return fmt.Errorf("квота на сегодня уже выполнена (%s)", formatHoursShort(quotaSec))
			}
		}
	}

	m.stopAllKeepersExcept(login)

	st.keeper.SetDailyQuota(quotaSec)
	if mode == ModeSchedule {
		st.keeper.SetQuotaRefresh(func() int {
			m.mu.RLock()
			st2 := m.byLogin[login]
			m.mu.RUnlock()
			if st2 == nil || st2.schedule.IsEmpty() {
				return 0
			}
			return m.todayQuotaFor(st2, time.Now())
		})
	} else {
		st.keeper.SetQuotaRefresh(nil)
	}
	m.mu.Lock()
	m.active = login
	m.mu.Unlock()

	return st.keeper.Start(context.Background())
}

func formatHoursShort(seconds int) string {
	h := seconds / 3600
	m := (seconds % 3600) / 60
	if h == 0 {
		return fmt.Sprintf("%dм", m)
	}
	if m == 0 {
		return fmt.Sprintf("%dч", h)
	}
	return fmt.Sprintf("%dч %dм", h, m)
}

func (m *Manager) StopTracking(login string) {
	if login == "" {
		m.stopAllKeepers()
		return
	}
	m.mu.RLock()
	st, ok := m.byLogin[login]
	m.mu.RUnlock()
	if !ok {
		return
	}
	st.keeper.Stop()
}

func (m *Manager) stopAllKeepers() {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, st := range m.byLogin {
		st.keeper.Stop()
	}
}

func (m *Manager) todayQuotaFor(st *accountState, now time.Time) int {
	m.mu.RLock()
	weekHours := m.weekHours
	m.mu.RUnlock()

	weekSec, todaySec := 0, 0
	if dash, err := st.client.Dashboard(); err == nil {
		weekSec = dash.Hours.WeekSeconds
		todaySec = dash.Hours.TodaySeconds
	}
	return DynamicTodayQuota(st.schedule, weekHours, weekSec, todaySec, now)
}

func (m *Manager) stopAllKeepersExcept(login string) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for l, st := range m.byLogin {
		if l != login {
			st.keeper.Stop()
		}
	}
}

func (m *Manager) snapshot(st *accountState, mode string) *Account {
	ac := &Account{
		Login:           st.login,
		DisplayName:     st.displayName,
		Tracking:        st.keeper.IsRunning(),
		LastError:       st.keeper.LastError(),
		Stalled:         st.keeper.Stalled(),
		VirtualHostname: st.hostname,
		QuotaReached:    st.keeper.QuotaReached(),
	}
	if !st.schedule.IsEmpty() {
		sch := st.schedule
		ac.Schedule = &sch
		if mode == ModeSchedule {
			now := time.Now()
			ac.QuotaTodaySeconds = m.todayQuotaFor(st, now)
		}
	}
	if dash, err := st.client.Dashboard(); err == nil {
		ac.TodaySeconds = dash.Hours.TodaySeconds
		ac.WeekSeconds = dash.Hours.WeekSeconds
		ac.WeekRemainingSec = WeekRemainingSeconds(m.WeekHours(), dash.Hours.WeekSeconds)
		ac.SessionActive = dash.Tracking.Active
		ac.StartedAt = dash.Tracking.StartedAt
		ac.ChallengePending = dash.Tracking.ChallengePending
		if dash.Hours.TodaySeconds > st.lastToday {
			st.lastToday = dash.Hours.TodaySeconds
			st.lastTodayAt = time.Now()
			st.stalled = false
		} else if st.keeper.IsRunning() && !st.lastTodayAt.IsZero() &&
			time.Since(st.lastTodayAt) > 4*time.Minute {
			st.stalled = true
		}
		ac.Stalled = st.stalled || st.keeper.Stalled()
		if ac.QuotaTodaySeconds > 0 && dash.Hours.TodaySeconds >= ac.QuotaTodaySeconds {
			ac.QuotaReached = true
		}
		m.recordDayActual(st, dash.Hours.TodaySeconds)
		now := time.Now()
		m.backfillPastDays(st, dash.Hours.TodaySeconds, dash.Hours.WeekSeconds, now)
		if len(st.weekDays.Days) > 0 {
			ac.WeekDaysActual = make(map[string]int, len(st.weekDays.Days))
			for k, v := range st.weekDays.Days {
				ac.WeekDaysActual[k] = v
			}
		}
		if st.weekDaysDirty {
			go func(st *accountState) {
				m.mu.RLock()
				cookies := m.readCookies(st)
				m.mu.RUnlock()
				if err := m.saveAccount(st, cookies); err != nil {
					slog.Warn("save week days failed", "login", st.login, "error", err)
				}
			}(st)
		}
	}
	if ac.StartedAt == "" && st.keeper.IsRunning() {
		if dash, err := st.client.Dashboard(); err == nil {
			ac.StartedAt = dash.Tracking.StartedAt
			ac.ChallengePending = dash.Tracking.ChallengePending
		}
	}
	return ac
}

func (m *Manager) ImportCookiesToActive(cookies []*http.Cookie) (*Account, error) {
	return m.AddFromCookies(cookies)
}

func (m *Manager) ensureStoredHostname(sa *storedAccount, usedHosts map[string]bool) bool {
	if sa.Hostname != "" {
		usedHosts[sa.Hostname] = true
		return false
	}
	sa.Hostname = device.UniqueHostname(usedHosts)
	usedHosts[sa.Hostname] = true
	return true
}

func (m *Manager) loadOrGenerateHostname(acctDir string) string {
	path := filepath.Join(acctDir, "account.json")
	if b, err := os.ReadFile(path); err == nil {
		var sa storedAccount
		if json.Unmarshal(b, &sa) == nil && sa.Hostname != "" {
			return sa.Hostname
		}
	}
	usedH := m.usedHostnames()
	return device.UniqueHostname(usedH)
}

func (m *Manager) usedHostnames() map[string]bool {
	hosts := make(map[string]bool)
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, st := range m.byLogin {
		if st.hostname != "" {
			hosts[st.hostname] = true
		}
	}
	return hosts
}

func cookiesFromStored(sa storedAccount) []*http.Cookie {
	var cookies []*http.Cookie
	for _, r := range sa.Cookies {
		cookies = append(cookies, &http.Cookie{Name: r.Name, Value: r.Value})
	}
	return cookies
}

func loadOrCreateID(path string) string {
	if b, err := os.ReadFile(path); err == nil && len(b) > 0 {
		return string(b)
	}
	id := generateUUID()
	_ = os.WriteFile(path, []byte(id), 0o600)
	return id
}

func generateUUID() string {
	b := make([]byte, 16)
	if f, err := os.Open("/dev/urandom"); err == nil {
		_, _ = f.Read(b)
		_ = f.Close()
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	const hexdigits = "0123456789abcdef"
	out := make([]byte, 36)
	j := 0
	for i, v := range b {
		if i == 4 || i == 6 || i == 8 || i == 10 {
			out[j] = '-'
			j++
		}
		out[j] = hexdigits[v>>4]
		j++
		out[j] = hexdigits[v&0x0f]
		j++
	}
	return string(out)
}
