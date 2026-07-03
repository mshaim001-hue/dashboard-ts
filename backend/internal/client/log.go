package client

import (
	"encoding/json"
	"log/slog"
	"strings"
)

func logAPI(method, path string, status int, reqBody any, respBody []byte, err error) {
	attrs := []any{
		"method", method,
		"path", path,
		"status", status,
	}
	if reqBody != nil {
		if b, e := json.Marshal(reqBody); e == nil {
			attrs = append(attrs, "req", truncate(string(b), 300))
		}
	}
	if len(respBody) > 0 {
		attrs = append(attrs, "resp", truncate(string(respBody), 500))
		// parse tracking fields when present
		var probe map[string]any
		if json.Unmarshal(respBody, &probe) == nil {
			if tr, ok := probe["tracking"].(map[string]any); ok {
				attrs = append(attrs,
					"tracking_active", tr["active"],
					"tracking_startedAt", tr["startedAt"],
					"tracking_challenge", tr["challengePending"],
				)
			}
			if hours, ok := probe["hours"].(map[string]any); ok {
				attrs = append(attrs,
					"todaySeconds", hours["todaySeconds"],
					"weekSeconds", hours["weekSeconds"],
				)
			}
		}
	}
	if err != nil {
		attrs = append(attrs, "error", err.Error())
		slog.Error("dashboard API", attrs...)
		return
	}
	slog.Info("dashboard API", attrs...)
}

func truncate(s string, max int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) <= max {
		return s
	}
	return s[:max] + "…"
}
