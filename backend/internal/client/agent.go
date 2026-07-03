package client

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"time"
)

const agentInfoURL = "http://127.0.0.1:47836/pair-info"

type pairInfo struct {
	MachineID string `json:"machineId"`
	Token     string `json:"token"`
}

func (d *Dashboard) PairAgent() error {
	slog.Info("agent pair: fetching pair-info", "url", agentInfoURL)
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(agentInfoURL)
	if err != nil {
		slog.Error("agent pair: pair-info unreachable", "error", err)
		return fmt.Errorf("агент на :47836 недоступен: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("pair-info: HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	var info pairInfo
	if err := json.Unmarshal(body, &info); err != nil {
		return err
	}
	if info.MachineID == "" {
		return fmt.Errorf("pair-info: пустой machineId")
	}
	token := info.Token
	if token == "" {
		token = info.MachineID
	}
	_, _, err = d.do(http.MethodPost, "/api/v1/agent/pair", map[string]string{
		"machineId": info.MachineID,
		"token":     token,
	})
	if err != nil {
		slog.Error("agent pair failed", "machineId", info.MachineID, "error", err)
		return err
	}
	slog.Info("agent pair ok", "machineId", info.MachineID)
	return nil
}

// Fingerprint returns a stable browser-like device fingerprint.
func Fingerprint(dataDir, login string) string {
	path := filepath.Join(dataDir, "fingerprint-v2")
	if b, err := os.ReadFile(path); err == nil && len(b) == 64 {
		return string(b)
	}

	hostname, _ := os.Hostname()
	ua := "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"
	canvas := "tomorrow-school-canvas-v1"
	parts := []string{
		ua,
		"ru-RU,ru,en-US,en",
		runtime.GOOS,
		"8",
		"8",
		"0",
		"1920x1080x24",
		"1920x1080",
		"2",
		"Asia/Almaty",
		canvas,
		hostname,
		login,
	}
	raw := ""
	for i, p := range parts {
		if i > 0 {
			raw += "||"
		}
		raw += p
	}
	sum := sha256.Sum256([]byte(raw))
	fp := hex.EncodeToString(sum[:])
	_ = os.MkdirAll(dataDir, 0o700)
	_ = os.WriteFile(path, []byte(fp), 0o600)
	return fp
}
