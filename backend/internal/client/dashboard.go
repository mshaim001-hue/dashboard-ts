package client

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"sync"
	"time"
)

const BaseURL = "https://dashboard.tomorrow-school.ai"

type Dashboard struct {
	mu          sync.RWMutex
	http        *http.Client
	csrfToken   string
	deviceID    string
	deviceName  string
	fingerprint string
}

func New(deviceID, deviceName, fingerprint string) *Dashboard {
	jar, _ := cookiejar.New(nil)
	fp := fingerprint
	if fp == "" {
		fp = "ts-tracker-go"
	}
	return &Dashboard{
		http: &http.Client{
			Jar:     jar,
			Timeout: 30 * time.Second,
		},
		deviceID:   deviceID,
		deviceName: deviceName,
		fingerprint: fp,
	}
}

type AuthMeResponse struct {
	Authenticated bool `json:"authenticated"`
	CSRFToken     string `json:"csrfToken"`
	User          *struct {
		Login       string `json:"login"`
		DisplayName string `json:"displayName"`
	} `json:"user"`
}

type DashboardResponse struct {
	Hours struct {
		TodaySeconds int `json:"todaySeconds"`
		WeekSeconds  int `json:"weekSeconds"`
		TotalSeconds int `json:"totalSeconds"`
	} `json:"hours"`
	Tracking TrackingState `json:"tracking"`
}

type TrackingState struct {
	Active           bool   `json:"active"`
	StartedAt        string `json:"startedAt"`
	ChallengePending bool   `json:"challengePending"`
}

type TrackingResponse struct {
	Tracking TrackingState `json:"tracking"`
}

type HeartbeatResponse struct {
	Tracking struct {
		State TrackingState `json:"state"`
	} `json:"tracking"`
}

type CaptchaResponse struct {
	CaptchaID string `json:"captchaId"`
	Image     string `json:"image"`
}

func (d *Dashboard) LoginURL(returnTo string) string {
	return fmt.Sprintf("%s/api/v1/auth/gitea/start?returnTo=%s", BaseURL, url.QueryEscape(returnTo))
}

func (d *Dashboard) ImportCookies(rawURL string, cookies []*http.Cookie) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return
	}
	d.http.Jar.SetCookies(u, cookies)
}

func (d *Dashboard) ExportCookies(rawURL string) []*http.Cookie {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil
	}
	return d.http.Jar.Cookies(u)
}

func (d *Dashboard) csrf() string {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.csrfToken
}

func (d *Dashboard) setCSRF(token string) {
	d.mu.Lock()
	d.csrfToken = token
	d.mu.Unlock()
}

func (d *Dashboard) ensureCSRF() error {
	if d.csrf() != "" {
		return nil
	}
	_, err := d.AuthMe()
	return err
}

func (d *Dashboard) do(method, path string, body any) ([]byte, int, error) {
	if method != http.MethodGet {
		if err := d.ensureCSRF(); err != nil {
			return nil, 0, fmt.Errorf("csrf: %w", err)
		}
	}
	data, code, err := d.doOnce(method, path, body)
	if err != nil && code == http.StatusForbidden && isCSRFError(data) {
		if _, refreshErr := d.AuthMe(); refreshErr != nil {
			return data, code, err
		}
		return d.doOnce(method, path, body)
	}
	return data, code, err
}

func isCSRFError(data []byte) bool {
	return bytes.Contains(data, []byte("csrf"))
}

func (d *Dashboard) doOnce(method, path string, body any) ([]byte, int, error) {
	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, 0, err
		}
		reader = bytes.NewReader(b)
	}

	req, err := http.NewRequest(method, BaseURL+path, reader)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if method != http.MethodGet {
		if token := d.csrf(); token != "" {
			req.Header.Set("X-CSRF-Token", token)
		}
	}

	resp, err := d.http.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		logAPI(method, path, resp.StatusCode, body, nil, err)
		return nil, resp.StatusCode, err
	}
	if resp.StatusCode >= 400 {
		apiErr := fmt.Errorf("API %s %s: %d %s", method, path, resp.StatusCode, string(data))
		logAPI(method, path, resp.StatusCode, body, data, apiErr)
		return data, resp.StatusCode, apiErr
	}
	logAPI(method, path, resp.StatusCode, body, data, nil)
	return data, resp.StatusCode, nil
}

func (d *Dashboard) AuthMe() (*AuthMeResponse, error) {
	data, _, err := d.do(http.MethodGet, "/api/v1/auth/me", nil)
	if err != nil {
		return nil, err
	}
	var res AuthMeResponse
	if err := json.Unmarshal(data, &res); err != nil {
		return nil, err
	}
	d.setCSRF(res.CSRFToken)
	return &res, nil
}

func (d *Dashboard) Dashboard() (*DashboardResponse, error) {
	data, _, err := d.do(http.MethodGet, "/api/v1/dashboard", nil)
	if err != nil {
		return nil, err
	}
	var res DashboardResponse
	if err := json.Unmarshal(data, &res); err != nil {
		return nil, err
	}
	return &res, nil
}

func (d *Dashboard) StartTracking(lat, lon *float64, accuracy *float64) (*TrackingResponse, error) {
	body := map[string]any{
		"deviceId":    d.deviceID,
		"deviceName":  d.deviceName,
		"fingerprint": d.fingerprint,
	}
	if lat != nil && lon != nil {
		body["latitude"] = *lat
		body["longitude"] = *lon
		if accuracy != nil {
			body["accuracyMeters"] = *accuracy
		}
	}
	data, _, err := d.do(http.MethodPost, "/api/v1/tracking/start", body)
	if err != nil {
		return nil, err
	}
	var res TrackingResponse
	if err := json.Unmarshal(data, &res); err != nil {
		return nil, err
	}
	return &res, nil
}

func (d *Dashboard) StopTracking() error {
	_, _, err := d.do(http.MethodPost, "/api/v1/tracking/stop", map[string]any{})
	return err
}

func IsDeviceConflict(err error) bool {
	return err != nil && strings.Contains(err.Error(), "another device")
}

// TakeOverTracking stops any active session (e.g. ghost Chrome tab) and starts ours.
func (d *Dashboard) TakeOverTracking() error {
	slog.Info("taking over tracking session", "deviceId", d.deviceID)
	_ = d.StopTracking()
	_, err := d.StartTracking(nil, nil, nil)
	return err
}

func (d *Dashboard) Heartbeat(idle bool) (*HeartbeatResponse, error) {
	body := map[string]any{
		"deviceId":    d.deviceID,
		"deviceName":  d.deviceName,
		"fingerprint": d.fingerprint,
		"idle":        idle,
	}
	data, _, err := d.do(http.MethodPost, "/api/v1/tracking/heartbeat", body)
	if err != nil {
		return nil, err
	}
	var res HeartbeatResponse
	if err := json.Unmarshal(data, &res); err != nil {
		return nil, err
	}
	return &res, nil
}

func (d *Dashboard) FetchCaptcha() (*CaptchaResponse, error) {
	if _, err := d.AuthMe(); err != nil {
		return nil, err
	}
	data, _, err := d.do(http.MethodGet, "/api/v1/tracking/captcha", nil)
	if err != nil {
		return nil, err
	}
	var res CaptchaResponse
	if err := json.Unmarshal(data, &res); err != nil {
		return nil, err
	}
	return &res, nil
}

func (d *Dashboard) VerifyCaptcha(captchaID, solution string) error {
	_, _, err := d.do(http.MethodPost, "/api/v1/tracking/captcha/verify", map[string]string{
		"captchaId": captchaID,
		"solution":  solution,
	})
	return err
}

func (d *Dashboard) IsAuthenticated() bool {
	me, err := d.AuthMe()
	return err == nil && me.Authenticated
}

func (d *Dashboard) DeviceID() string {
	return d.deviceID
}
