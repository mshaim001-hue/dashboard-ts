package accounts

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/zutemiss/dashboard-tracker/internal/client"
	"github.com/zutemiss/dashboard-tracker/internal/tracking"
)

type Account struct {
	Login            string `json:"login"`
	DisplayName      string `json:"displayName"`
	Tracking         bool   `json:"tracking"`
	LastError        string `json:"lastError,omitempty"`
	Stalled          bool   `json:"stalled"`
	TodaySeconds     int    `json:"todaySeconds"`
	WeekSeconds      int    `json:"weekSeconds"`
	SessionActive    bool   `json:"sessionActive"`
	StartedAt        string `json:"startedAt,omitempty"`
	ChallengePending bool   `json:"challengePending"`

	client  *client.Dashboard
	tracker *tracking.Tracker
	dir     string
}

type Manager struct {
	mu      sync.RWMutex
	dataDir string
	byLogin map[string]*accountState
	active  string
}

type accountState struct {
	login       string
	displayName string
	client      *client.Dashboard
	tracker     *tracking.Tracker
	dir         string
	lastToday   int
	lastTodayAt time.Time
	stalled     bool
}

type storedAccount struct {
	Login       string         `json:"login"`
	DisplayName string         `json:"displayName"`
	DeviceID    string         `json:"deviceId"`
	Cookies     []cookieRecord `json:"cookies"`
}

type cookieRecord struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

func NewManager(dataDir string) *Manager {
	m := &Manager{
		dataDir: dataDir,
		byLogin: make(map[string]*accountState),
	}
	m.loadAll()
	return m
}

func (m *Manager) loadAll() {
	dir := filepath.Join(m.dataDir, "accounts")
	entries, err := os.ReadDir(dir)
	if err != nil {
		m.migrateOldCookies()
		return
	}
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
		st := m.buildState(sa)
		m.byLogin[sa.Login] = st
		if m.active == "" {
			m.active = sa.Login
		}
		slog.Info("account loaded", "login", sa.Login)
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
	fp := client.Fingerprint(acctDir, sa.Login)
	hostname, _ := os.Hostname()
	c := client.New(sa.DeviceID, hostname+" · ts-tracker", fp)
	var cookies []*http.Cookie
	for _, r := range sa.Cookies {
		cookies = append(cookies, &http.Cookie{Name: r.Name, Value: r.Value})
	}
	c.ImportCookies(client.BaseURL, cookies)
	if _, err := c.AuthMe(); err != nil {
		slog.Warn("account csrf refresh failed", "login", sa.Login, "error", err)
	}
	return &accountState{
		login:       sa.Login,
		displayName: sa.DisplayName,
		client:      c,
		tracker:     tracking.New(c),
		dir:         acctDir,
	}
}

func (m *Manager) AddFromCookies(cookies []*http.Cookie) (*Account, error) {
	tmpDir := filepath.Join(m.dataDir, "_tmp_auth")
	_ = os.MkdirAll(tmpDir, 0o700)
	fp := client.Fingerprint(tmpDir, "")
	hostname, _ := os.Hostname()
	c := client.New(generateUUID(), hostname+" · ts-tracker", fp)
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

	fp = client.Fingerprint(acctDir, login)
	c = client.New(deviceID, hostname+" · ts-tracker", fp)
	c.ImportCookies(client.BaseURL, cookies)

	st := &accountState{
		login:       login,
		displayName: me.User.DisplayName,
		client:      c,
		tracker:     tracking.New(c),
		dir:         acctDir,
	}

	if err := m.saveAccount(st, cookies); err != nil {
		return nil, err
	}

	m.mu.Lock()
	m.byLogin[login] = st
	m.active = login
	m.mu.Unlock()

	slog.Info("account added", "login", login)
	return m.snapshot(st), nil
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
		Cookies:     records,
	}
	b, err := json.MarshalIndent(sa, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(st.dir, "account.json"), b, 0o600)
}

func (m *Manager) List() []*Account {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*Account, 0, len(m.byLogin))
	for _, st := range m.byLogin {
		out = append(out, m.snapshot(st))
	}
	return out
}

func (m *Manager) Active() *Account {
	m.mu.RLock()
	login := m.active
	st := m.byLogin[login]
	m.mu.RUnlock()
	if st == nil {
		return nil
	}
	return m.snapshot(st)
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
	st.tracker.Stop()
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
	m.mu.RLock()
	st, ok := m.byLogin[login]
	m.mu.RUnlock()
	if !ok {
		return fmt.Errorf("аккаунт не найден")
	}

	slog.Info("start tracking", "login", login)
	if _, err := st.client.AuthMe(); err != nil {
		return fmt.Errorf("сессия истекла — войди снова: %w", err)
	}
	if err := st.client.PairAgent(); err != nil {
		slog.Warn("agent pair failed", "login", login, "error", err)
	}

	dash, _ := st.client.Dashboard()
	if dash != nil && dash.Tracking.Active {
		slog.Info("clearing existing session before start", "login", login)
	}
	_ = st.client.StopTracking()
	if _, err := st.client.StartTracking(nil, nil, nil); err != nil {
		return fmt.Errorf("не удалось начать учёт: %w", err)
	}
	return st.tracker.Start()
}

func (m *Manager) StopTracking(login string) {
	m.mu.RLock()
	st, ok := m.byLogin[login]
	m.mu.RUnlock()
	if !ok {
		return
	}
	st.tracker.Stop()
	_ = st.client.StopTracking()
}

func (m *Manager) snapshot(st *accountState) *Account {
	ac := &Account{
		Login:       st.login,
		DisplayName: st.displayName,
		Tracking:    st.tracker.IsRunning(),
		LastError:   st.tracker.LastError(),
		Stalled:     st.stalled,
	}
	if dash, err := st.client.Dashboard(); err == nil {
		ac.TodaySeconds = dash.Hours.TodaySeconds
		ac.WeekSeconds = dash.Hours.WeekSeconds
		ac.SessionActive = dash.Tracking.Active
		ac.StartedAt = dash.Tracking.StartedAt
		ac.ChallengePending = dash.Tracking.ChallengePending
		if dash.Hours.TodaySeconds > st.lastToday {
			st.lastToday = dash.Hours.TodaySeconds
			st.lastTodayAt = time.Now()
			st.stalled = false
		} else if st.tracker.IsRunning() && !st.lastTodayAt.IsZero() &&
			time.Since(st.lastTodayAt) > 4*time.Minute {
			st.stalled = true
		}
		ac.Stalled = st.stalled
	}
	if ac.StartedAt == "" && st.tracker.IsRunning() {
		ts := st.tracker.State()
		ac.StartedAt = ts.StartedAt
		ac.ChallengePending = ts.ChallengePending
	}
	return ac
}

func (m *Manager) ImportCookiesToActive(cookies []*http.Cookie) (*Account, error) {
	return m.AddFromCookies(cookies)
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
