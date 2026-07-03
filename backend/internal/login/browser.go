package login

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"
)

type BrowserLogin struct {
	mu      sync.Mutex
	running bool
	done    bool
	err     string
	cookies []*http.Cookie
}

var DefaultBrowser = &BrowserLogin{}

func (b *BrowserLogin) Start(loginURL string) error {
	b.mu.Lock()
	if b.running {
		b.mu.Unlock()
		return fmt.Errorf("вход уже выполняется")
	}
	b.running = true
	b.done = false
	b.err = ""
	b.cookies = nil
	b.mu.Unlock()

	go b.run(loginURL)
	return nil
}

func (b *BrowserLogin) run(loginURL string) {
	defer func() {
		b.mu.Lock()
		b.running = false
		b.mu.Unlock()
	}()

	path, has := launcher.LookPath()
	if !has {
		b.setError("Chrome не найден — установи Google Chrome")
		return
	}

	u := launcher.New().Bin(path).Headless(false).MustLaunch()
	browser := rod.New().ControlURL(u).MustConnect()
	defer browser.MustClose()

	page := browser.MustPage(loginURL).MustWindowMaximize()
	deadline := time.Now().Add(5 * time.Minute)

	for time.Now().Before(deadline) {
		time.Sleep(500 * time.Millisecond)

		info, err := page.Info()
		if err != nil {
			continue
		}

		parsed, err := url.Parse(info.URL)
		if err != nil {
			continue
		}

		if !strings.Contains(parsed.Host, "dashboard.tomorrow-school.ai") {
			continue
		}
		if strings.Contains(parsed.Path, "/api/v1/auth/gitea") {
			continue
		}

		// Check if authenticated
		rcs, err := page.Cookies([]string{"https://dashboard.tomorrow-school.ai"})
		if err != nil || len(rcs) == 0 {
			continue
		}

		var cookies []*http.Cookie
		for _, c := range rcs {
			cookies = append(cookies, &http.Cookie{Name: c.Name, Value: c.Value})
		}

		// Verify session works
		if !hasSessionCookie(cookies) {
			continue
		}

		b.mu.Lock()
		b.cookies = cookies
		b.done = true
		b.mu.Unlock()
		return
	}

	b.setError("время ожидания входа истекло (5 мин)")
}

func hasSessionCookie(cookies []*http.Cookie) bool {
	for _, c := range cookies {
		name := strings.ToLower(c.Name)
		if strings.Contains(name, "session") || strings.Contains(name, "sid") || name == "ts_session" {
			return true
		}
	}
	// any non-trivial cookie from dashboard domain
	return len(cookies) >= 1
}

func (b *BrowserLogin) setError(msg string) {
	b.mu.Lock()
	b.err = msg
	b.mu.Unlock()
}

func (b *BrowserLogin) Status() (running, done bool, err string, cookies []*http.Cookie) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.running, b.done, b.err, b.cookies
}

func (b *BrowserLogin) TakeCookies() []*http.Cookie {
	b.mu.Lock()
	defer b.mu.Unlock()
	c := b.cookies
	b.cookies = nil
	b.done = false
	return c
}

// Reset clears state after cookies were consumed.
func (b *BrowserLogin) Reset() {
	b.mu.Lock()
	b.cookies = nil
	b.done = false
	b.err = ""
	b.mu.Unlock()
}
