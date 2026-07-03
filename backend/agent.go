package main

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"syscall"
)

func startAgentServer(addr string) {
	if addr == "" {
		return
	}

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		if isAddrInUse(err) {
			slog.Info("agent skipped: port already in use (school agent is probably running)", "addr", addr)
			return
		}
		slog.Warn("agent server failed to start", "addr", addr, "error", err)
		return
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /pair-info", func(w http.ResponseWriter, r *http.Request) {
		machineID := loadMachineID()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"machineId": machineID,
			"token":     machineID,
		})
	})

	slog.Info("agent listening", "addr", addr)
	if err := http.Serve(ln, mux); err != nil {
		slog.Warn("agent server stopped", "error", err)
	}
}

func isAddrInUse(err error) bool {
	var opErr *net.OpError
	if !errors.As(err, &opErr) {
		return false
	}
	var sysErr syscall.Errno
	if errors.As(opErr.Err, &sysErr) {
		return sysErr == syscall.EADDRINUSE
	}
	return false
}

func loadMachineID() string {
	home, _ := os.UserHomeDir()
	path := filepath.Join(home, ".ts-tracker", "device_id")
	if b, err := os.ReadFile(path); err == nil {
		return string(b)
	}
	return "ts-tracker-machine"
}
