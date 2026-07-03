package logx

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
)

var (
	mu      sync.RWMutex
	buffer  = newRing(1000)
	logFile *os.File
)

type ring struct {
	lines []string
	max   int
}

func newRing(max int) *ring {
	return &ring{max: max}
}

func (r *ring) add(line string) {
	if len(r.lines) >= r.max {
		r.lines = r.lines[1:]
	}
	r.lines = append(r.lines, line)
}

func (r *ring) snapshot() []string {
	out := make([]string, len(r.lines))
	copy(out, r.lines)
	return out
}

type teeHandler struct {
	stdout slog.Handler
}

func (h *teeHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.stdout.Enabled(ctx, level)
}

func (h *teeHandler) Handle(ctx context.Context, r slog.Record) error {
	buf := make([]byte, 0, 512)
	r.Attrs(func(a slog.Attr) bool {
		buf = append(buf, ' ')
		buf = append(buf, a.Key...)
		buf = append(buf, '=')
		buf = append(buf, a.Value.String()...)
		return true
	})
	line := r.Time.Format("2006-01-02 15:04:05") + " [" + r.Level.String() + "] " + r.Message + string(buf)
	mu.Lock()
	buffer.add(line)
	if logFile != nil {
		_, _ = logFile.WriteString(line + "\n")
	}
	mu.Unlock()
	return h.stdout.Handle(ctx, r)
}

func (h *teeHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &teeHandler{stdout: h.stdout.WithAttrs(attrs)}
}

func (h *teeHandler) WithGroup(name string) slog.Handler {
	return &teeHandler{stdout: h.stdout.WithGroup(name)}
}

func Init(dataDir string) string {
	_ = os.MkdirAll(dataDir, 0o700)
	path := filepath.Join(dataDir, "ts-tracker.log")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err == nil {
		logFile = f
	}

	stdout := slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug})
	slog.SetDefault(slog.New(&teeHandler{stdout: stdout}))
	slog.Info("logging initialized", "file", path)
	return path
}

func Recent(n int) []string {
	mu.RLock()
	defer mu.RUnlock()
	lines := buffer.snapshot()
	if n <= 0 || n >= len(lines) {
		return lines
	}
	return lines[len(lines)-n:]
}

func LogPath(dataDir string) string {
	return filepath.Join(dataDir, "ts-tracker.log")
}
