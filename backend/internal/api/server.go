package api

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"

	"github.com/zutemiss/dashboard-tracker/internal/accounts"
	"github.com/zutemiss/dashboard-tracker/internal/client"
	"github.com/zutemiss/dashboard-tracker/internal/login"
	"github.com/zutemiss/dashboard-tracker/internal/logx"
)

type Server struct {
	accounts *accounts.Manager
	dataDir  string
	frontend string
}

func New(dataDir, frontend string) *Server {
	return &Server{
		accounts: accounts.NewManager(dataDir),
		dataDir:  dataDir,
		frontend: frontend,
	}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /api/logs", s.handleLogs)
	mux.HandleFunc("GET /api/status", s.handleStatus)
	mux.HandleFunc("GET /api/accounts", s.handleAccounts)
	mux.HandleFunc("POST /api/accounts/select", s.handleSelectAccount)
	mux.HandleFunc("DELETE /api/accounts/{login}", s.handleRemoveAccount)
	mux.HandleFunc("GET /api/login-url", s.handleLoginURL)
	mux.HandleFunc("POST /api/login/browser", s.handleBrowserLogin)
	mux.HandleFunc("GET /api/login/browser/status", s.handleBrowserLoginStatus)
	mux.HandleFunc("POST /api/session", s.handleSaveSession)
	mux.HandleFunc("POST /api/tracking/start", s.handleTrackingStart)
	mux.HandleFunc("POST /api/tracking/stop", s.handleTrackingStop)
	mux.HandleFunc("GET /api/dashboard", s.handleDashboard)
	mux.HandleFunc("POST /api/logout", s.handleLogout)
	mux.HandleFunc("GET /auth/done", s.handleAuthDone)

	if s.frontend != "" {
		mux.Handle("/", spaHandler(s.frontend))
	} else {
		mux.HandleFunc("GET /{$}", s.handleRoot)
	}

	return corsMiddleware(recoverMiddleware(requestLogMiddleware(mux)))
}

func recoverMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				slog.Error("panic recovered", "path", r.URL.Path, "panic", rec)
				jsonErr(w, http.StatusInternalServerError, fmt.Sprintf("internal error: %v", rec))
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func requestLogMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		slog.Debug("http", "method", r.Method, "path", r.URL.Path)
		next.ServeHTTP(w, r)
	})
}

func (s *Server) handleLogs(w http.ResponseWriter, r *http.Request) {
	jsonOK(w, map[string]any{
		"logFile": logx.LogPath(s.dataDir),
		"lines":   logx.Recent(200),
	})
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	active := s.accounts.Active()
	if active == nil {
		jsonOK(w, map[string]any{
			"authenticated": false,
			"accounts":      s.accounts.List(),
		})
		return
	}
	jsonOK(w, map[string]any{
		"authenticated":  true,
		"mode":           "api",
		"user":           map[string]string{"login": active.Login, "displayName": active.DisplayName},
		"trackerRunning": active.Tracking,
		"stalled":        active.Stalled,
		"lastError":      active.LastError,
		"hours": map[string]int{
			"todaySeconds": active.TodaySeconds,
			"weekSeconds":  active.WeekSeconds,
		},
		"tracking": map[string]any{
			"active":           active.SessionActive || active.Tracking,
			"startedAt":        active.StartedAt,
			"challengePending": active.ChallengePending,
		},
		"accounts": s.accounts.List(),
		"logFile":  logx.LogPath(s.dataDir),
	})
}

func (s *Server) handleAccounts(w http.ResponseWriter, r *http.Request) {
	jsonOK(w, map[string]any{"accounts": s.accounts.List()})
}

func (s *Server) handleSelectAccount(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Login string `json:"login"`
	}
	if json.NewDecoder(r.Body).Decode(&body) != nil || body.Login == "" {
		jsonErr(w, http.StatusBadRequest, "нужен login")
		return
	}
	if err := s.accounts.Select(body.Login); err != nil {
		jsonErr(w, http.StatusNotFound, err.Error())
		return
	}
	jsonOK(w, map[string]any{"ok": true})
}

func (s *Server) handleRemoveAccount(w http.ResponseWriter, r *http.Request) {
	login := r.PathValue("login")
	if login == "" {
		jsonErr(w, http.StatusBadRequest, "login required")
		return
	}
	if err := s.accounts.Remove(login); err != nil {
		jsonErr(w, http.StatusNotFound, err.Error())
		return
	}
	jsonOK(w, map[string]any{"ok": true})
}

func (s *Server) handleLoginURL(w http.ResponseWriter, r *http.Request) {
	returnTo := client.BaseURL
	jsonOK(w, map[string]string{"url": client.BaseURL + "/api/v1/auth/gitea/start?returnTo=" + returnTo})
}

func (s *Server) handleBrowserLogin(w http.ResponseWriter, r *http.Request) {
	loginURL := client.BaseURL + "/api/v1/auth/gitea/start?returnTo=" + client.BaseURL
	if err := login.DefaultBrowser.Start(loginURL); err != nil {
		jsonErr(w, http.StatusConflict, err.Error())
		return
	}
	jsonOK(w, map[string]any{"started": true})
}

func (s *Server) handleBrowserLoginStatus(w http.ResponseWriter, r *http.Request) {
	running, done, errMsg, cookies := login.DefaultBrowser.Status()
	if done && len(cookies) > 0 {
		acct, err := s.accounts.AddFromCookies(cookies)
		login.DefaultBrowser.Reset()
		if err != nil {
			jsonErr(w, http.StatusUnauthorized, err.Error())
			return
		}
		jsonOK(w, map[string]any{"authenticated": true, "account": acct})
		return
	}
	resp := map[string]any{"running": running, "done": done}
	if errMsg != "" {
		resp["error"] = errMsg
	}
	jsonOK(w, resp)
}

func (s *Server) handleRoot(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(w, `<!DOCTYPE html><html><body><h1>TS Tracker</h1><p><a href="http://localhost:5173">frontend</a></p></body></html>`)
}

type sessionPayload struct {
	Cookie  string         `json:"cookie"`
	Cookies []cookieRecord `json:"cookies"`
}

func (s *Server) handleSaveSession(w http.ResponseWriter, r *http.Request) {
	var p sessionPayload
	if json.NewDecoder(r.Body).Decode(&p) != nil {
		jsonErr(w, http.StatusBadRequest, "неверный JSON")
		return
	}
	cookies, err := parseSessionInput(p.Cookie, p.Cookies)
	if err != nil || len(cookies) == 0 {
		jsonErr(w, http.StatusBadRequest, "нужен cookie")
		return
	}
	acct, err := s.accounts.AddFromCookies(cookies)
	if err != nil {
		jsonErr(w, http.StatusUnauthorized, err.Error())
		return
	}
	jsonOK(w, map[string]any{"authenticated": true, "account": acct})
}

func (s *Server) handleTrackingStart(w http.ResponseWriter, r *http.Request) {
	active := s.accounts.Active()
	if active == nil {
		jsonErr(w, http.StatusUnauthorized, "сначала добавь аккаунт")
		return
	}
	loginName := active.Login
	var body struct {
		Login string `json:"login"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	if body.Login != "" {
		loginName = body.Login
	}
	if err := s.accounts.StartTracking(loginName); err != nil {
		jsonErr(w, http.StatusBadRequest, err.Error())
		return
	}
	slog.Info("tracking started via API", "login", loginName)
	jsonOK(w, map[string]any{"ok": true, "mode": "api", "login": loginName})
}

func (s *Server) handleTrackingStop(w http.ResponseWriter, r *http.Request) {
	loginName := ""
	var body struct {
		Login string `json:"login"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	if body.Login != "" {
		loginName = body.Login
	} else if active := s.accounts.Active(); active != nil {
		loginName = active.Login
	}
	if loginName != "" {
		s.accounts.StopTracking(loginName)
	}
	jsonOK(w, map[string]any{"ok": true})
}

func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	active := s.accounts.Active()
	if active == nil {
		jsonErr(w, http.StatusUnauthorized, "не авторизован")
		return
	}
	jsonOK(w, active)
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	active := s.accounts.Active()
	if active != nil {
		s.accounts.StopTracking(active.Login)
		s.accounts.Remove(active.Login)
	}
	jsonOK(w, map[string]any{"ok": true})
}

func (s *Server) LoadSavedSession() {
	// accounts loaded in NewManager
	list := s.accounts.List()
	if len(list) > 0 {
		slog.Info("accounts restored", "count", len(list))
	}
}

type cookieRecord struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

func parseCookieHeader(raw string) []*http.Cookie {
	header := http.Header{}
	header.Add("Cookie", raw)
	return (&http.Request{Header: header}).Cookies()
}

func parseSessionInput(raw string, records []cookieRecord) ([]*http.Cookie, error) {
	if raw != "" {
		var jsonCookies []cookieRecord
		if json.Unmarshal([]byte(raw), &jsonCookies) == nil && len(jsonCookies) > 0 {
			return recordsToCookies(jsonCookies), nil
		}
		return parseCookieHeader(raw), nil
	}
	if len(records) > 0 {
		return recordsToCookies(records), nil
	}
	return nil, fmt.Errorf("empty")
}

func recordsToCookies(records []cookieRecord) []*http.Cookie {
	var out []*http.Cookie
	for _, c := range records {
		if c.Name != "" {
			out = append(out, &http.Cookie{Name: c.Name, Value: c.Value})
		}
	}
	return out
}

func jsonOK(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func jsonErr(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

func spaHandler(dir string) http.Handler {
	fs := http.FileServer(http.Dir(dir))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := filepath.Join(dir, r.URL.Path)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			http.ServeFile(w, r, filepath.Join(dir, "index.html"))
			return
		}
		fs.ServeHTTP(w, r)
	})
}
