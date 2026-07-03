package main

import (
	"flag"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"

	"github.com/zutemiss/dashboard-tracker/internal/api"
	"github.com/zutemiss/dashboard-tracker/internal/idle"
	"github.com/zutemiss/dashboard-tracker/internal/logx"
)

func main() {
	addr := flag.String("addr", ":8080", "HTTP listen address")
	agentAddr := flag.String("agent-addr", ":47836", "local agent listen address (empty to disable)")
	dataDir := flag.String("data", defaultDataDir(), "directory for session storage")
	frontend := flag.String("frontend", "", "path to built frontend (optional)")
	detectIdle := flag.Bool("detect-idle", false, "idle=true когда Mac без ввода >60с (по умолчанию всегда active)")
	flag.Parse()

	idle.SetDetectIdle(*detectIdle)

	_ = os.MkdirAll(*dataDir, 0o700)
	logPath := logx.Init(*dataDir)

	srv := api.New(*dataDir, *frontend)
	srv.LoadSavedSession()

	go startAgentServer(*agentAddr)

	slog.Info("starting", "addr", *addr, "data", *dataDir, "log", logPath, "detectIdle", *detectIdle)
	if err := http.ListenAndServe(*addr, srv.Handler()); err != nil {
		slog.Error("server failed", "error", err)
		os.Exit(1)
	}
}

func defaultDataDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".ts-tracker")
}
