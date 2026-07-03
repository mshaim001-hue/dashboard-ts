package api

import (
	"fmt"
	"net/http"
)

func (s *Server) handleAuthDone(w http.ResponseWriter, r *http.Request) {
	appURL := r.URL.Query().Get("app")
	if appURL == "" {
		appURL = "http://localhost:5173"
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(w, authDoneHTML, appURL, appURL)
}

const authDoneHTML = `<!DOCTYPE html>
<html lang="ru">
<head>
  <meta charset="UTF-8" />
  <meta name="viewport" content="width=device-width, initial-scale=1.0" />
  <title>TS Tracker — вход выполнен</title>
  <style>
    body { font-family: system-ui, sans-serif; max-width: 560px; margin: 48px auto; padding: 0 20px; background: #0a0b0f; color: #e8eaf0; line-height: 1.6; }
    h1 { font-size: 1.4rem; }
    .ok { color: #34d399; }
    ol { padding-left: 20px; }
    li { margin-bottom: 10px; }
    code { background: #12141c; padding: 2px 6px; border-radius: 4px; font-size: 0.9em; }
    a.btn { display: inline-block; margin-top: 16px; padding: 10px 18px; background: #6ee7b7; color: #0a0b0f; text-decoration: none; border-radius: 10px; font-weight: 600; }
    .muted { color: #9aa0b4; font-size: 0.9rem; }
  </style>
</head>
<body>
  <h1><span class="ok">✓</span> Вход в dashboard выполнен</h1>
  <p>Остался один шаг — передать сессию в локальное приложение TS Tracker.</p>
  <ol>
    <li>Открой <a href="https://dashboard.tomorrow-school.ai" target="_blank">dashboard.tomorrow-school.ai</a> (должен быть уже залогинен)</li>
    <li>Нажми <code>F12</code> → вкладка <strong>Network</strong></li>
    <li>Обнови страницу (<code>Cmd+R</code>)</li>
    <li>Кликни любой запрос к <code>/api/v1/</code></li>
    <li>В <strong>Request Headers</strong> найди <code>Cookie:</code> и скопируй всё значение</li>
    <li>Вставь в поле на странице TS Tracker</li>
  </ol>
  <p class="muted">Или нажми «Войти через браузер» в приложении — cookies подтянутся автоматически.</p>
  <a class="btn" href="%s">Вернуться в TS Tracker</a>
</body>
</html>`
