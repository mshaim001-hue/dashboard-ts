import { useCallback, useEffect, useState } from "react";
import {
  fetchLogs,
  fetchStatus,
  formatDuration,
  formatHours,
  getLoginURL,
  logout,
  pollBrowserLogin,
  removeAccount,
  saveSession,
  selectAccount,
  startBrowserLogin,
  startTracking,
  stopTracking,
  type Account,
  type Status,
} from "./api";

export default function App() {
  const [status, setStatus] = useState<Status | null>(null);
  const [logs, setLogs] = useState<string[]>([]);
  const [logFile, setLogFile] = useState("");
  const [showLogs, setShowLogs] = useState(false);
  const [cookie, setCookie] = useState("");
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(false);
  const [now, setNow] = useState(Date.now());

  const refresh = useCallback(async () => {
    try {
      const s = await fetchStatus();
      setStatus(s);
    } catch {
      setError("Не удалось связаться с backend (запущен ли go run .?)");
    }
  }, []);

  useEffect(() => {
    refresh();
    const id = setInterval(refresh, 5000);
    return () => clearInterval(id);
  }, [refresh]);

  useEffect(() => {
    if (!showLogs) return;
    const load = async () => {
      try {
        const data = await fetchLogs();
        setLogs(data.lines);
        setLogFile(data.logFile);
      } catch {
        /* backend down */
      }
    };
    load();
    const id = setInterval(load, 3000);
    return () => clearInterval(id);
  }, [showLogs]);

  useEffect(() => {
    const id = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(id);
  }, []);

  const sessionMs =
    status?.tracking?.active && status.tracking.startedAt
      ? Math.max(0, now - new Date(status.tracking.startedAt).getTime())
      : 0;

  async function handleLogin() {
    setError("");
    const { url } = await getLoginURL();
    window.open(url, "_blank");
  }

  async function handleBrowserLogin() {
    setLoading(true);
    setError("");
    try {
      await startBrowserLogin();
      const deadline = Date.now() + 5 * 60 * 1000;
      while (Date.now() < deadline) {
        await new Promise((r) => setTimeout(r, 1500));
        const st = await pollBrowserLogin();
        if (st.authenticated || st.account) {
          await refresh();
          return;
        }
        if (st.error && !st.running) throw new Error(st.error);
      }
      throw new Error("Время ожидания входа истекло");
    } catch (e) {
      setError(e instanceof Error ? e.message : "Ошибка входа");
    } finally {
      setLoading(false);
    }
  }

  async function handleSelectAccount(login: string) {
    await selectAccount(login);
    await refresh();
  }

  async function handleAddFriend() {
    await handleBrowserLogin();
  }

  async function handleSaveCookie() {
    setLoading(true);
    setError("");
    try {
      await saveSession(cookie.trim());
      setCookie("");
      await refresh();
    } catch (e) {
      setError(e instanceof Error ? e.message : "Ошибка");
    } finally {
      setLoading(false);
    }
  }

  async function handleStart() {
    setLoading(true);
    setError("");
    try {
      await startTracking();
      await refresh();
    } catch (e) {
      setError(e instanceof Error ? e.message : "Ошибка");
    } finally {
      setLoading(false);
    }
  }

  async function handleStop() {
    setLoading(true);
    await stopTracking();
    await refresh();
    setLoading(false);
  }

  async function handleLogout() {
    await logout();
    await refresh();
  }

  if (!status) {
    return (
      <div className="page">
        <p>Загрузка…</p>
      </div>
    );
  }

  return (
    <div className="page">
      <header className="header">
        <div>
          <h1>TS Tracker</h1>
          <p className="muted">Локальный помощник для учёта часов Tomorrow School</p>
        </div>
        {status.authenticated && status.user && (
          <div className="user">
            <strong>{status.user.displayName}</strong>
            <span className="muted">@{status.user.login}</span>
          </div>
        )}
      </header>

      {error && <div className="alert alert--error">{error}</div>}

      {!status.authenticated ? (
        <section className="card">
          <h2>Вход</h2>
          <p className="muted" style={{ marginBottom: 16 }}>
            Войди через Gitea — потом можно добавить друзей кнопкой «+ Добавить
            аккаунт». Chrome нужен только на минуту для входа, не для учёта.
          </p>
          <button
            className="btn btn--primary"
            disabled={loading}
            onClick={handleBrowserLogin}
          >
            {loading ? "Ожидаю вход…" : "Войти через браузер"}
          </button>

          <details style={{ marginTop: 20 }}>
            <summary className="muted" style={{ cursor: "pointer" }}>
              Ручной вход (если браузер не открылся)
            </summary>
            <ol className="steps">
              <li>
                Нажми «Открыть Gitea» — после входа откроется{" "}
                <strong>dashboard.tomorrow-school.ai</strong> (не localhost).
              </li>
              <li>
                DevTools → Network → любой <code>/api/v1/</code> запрос → скопируй
                заголовок <code>Cookie</code>.
              </li>
              <li>Вставь ниже и нажми «Сохранить сессию».</li>
            </ol>
            <button className="btn btn--ghost" onClick={handleLogin}>
              Открыть Gitea
            </button>
            <textarea
              className="input"
              rows={3}
              placeholder="session=abc123; ..."
              value={cookie}
              onChange={(e) => setCookie(e.target.value)}
            />
            <button
              className="btn btn--primary"
              disabled={!cookie.trim() || loading}
              onClick={handleSaveCookie}
            >
              Сохранить сессию
            </button>
          </details>
        </section>
      ) : (
        <>
          <section className="grid">
            <div className="card stat">
              <span className="label">Сегодня (засчитано)</span>
              <span className="value">
                {formatHours(status.hours?.todaySeconds ?? 0)}
              </span>
              <span className="hint">это реальное время от сервера</span>
            </div>
            <div className="card stat">
              <span className="label">За неделю</span>
              <span className="value">
                {formatHours(status.hours?.weekSeconds ?? 0)}
              </span>
            </div>
            <div className="card stat">
              <span className="label">Сессия</span>
              <span
                className={`value ${status.tracking?.active ? "value--live" : ""}`}
              >
                {status.tracking?.active
                  ? formatDuration(sessionMs)
                  : "не активна"}
              </span>
              <span className="hint">
                {status.trackerRunning
                  ? "API heartbeat каждые 30 сек — Chrome не нужен"
                  : "нажми «Запустить учёт»"}
              </span>
            </div>
          </section>

          {status.stalled && (
            <div className="alert alert--error">
              ⚠️ Время не растёт 4+ мин — проверь Wi-Fi, captcha, idle (экран
              заблокирован &gt; 60 сек не считается).
            </div>
          )}

          {status.tracking?.challengePending && (
            <div className="alert alert--warning">
              Ожидаю captcha — решаю автоматически.
            </div>
          )}

          {status.accounts && status.accounts.length > 0 && (
            <section className="card">
              <h3>Аккаунты ({status.accounts.length})</h3>
              <p className="muted" style={{ marginBottom: 12 }}>
                Старт/Стоп — учёт для каждого отдельно, работают параллельно.
                «Смотреть» — переключить карточки сверху на этого человека.
              </p>
              {status.accounts.map((a: Account) => {
                const isActive = status.user?.login === a.login;
                return (
                <div key={a.login} className={`account-row${isActive ? " account-row--active" : ""}`}>
                  <div>
                    <strong>{a.displayName}</strong>
                    <span className="muted"> @{a.login}</span>
                    <div className="muted" style={{ fontSize: "0.8rem" }}>
                      сегодня {formatHours(a.todaySeconds)}
                      {a.tracking ? " · учёт идёт" : ""}
                      {a.stalled ? " · ⚠️ не растёт" : ""}
                      {isActive ? " · смотришь сейчас" : ""}
                    </div>
                    {a.lastError && (
                      <div style={{ fontSize: "0.75rem", color: "#fecaca", marginTop: 4 }}>
                        {a.lastError}
                      </div>
                    )}
                  </div>
                  <div>
                    {!isActive && (
                      <button
                        className="btn btn--ghost"
                        style={{ padding: "6px 12px", fontSize: "0.85rem" }}
                        onClick={() => handleSelectAccount(a.login)}
                      >
                        Смотреть
                      </button>
                    )}
                    {!a.tracking ? (
                      <button
                        className="btn btn--success"
                        style={{ padding: "6px 12px", fontSize: "0.85rem" }}
                        disabled={loading}
                        onClick={async () => {
                          await startTracking(a.login);
                          await refresh();
                        }}
                      >
                        Старт
                      </button>
                    ) : (
                      <button
                        className="btn btn--danger"
                        style={{ padding: "6px 12px", fontSize: "0.85rem" }}
                        onClick={async () => {
                          await stopTracking(a.login);
                          await refresh();
                        }}
                      >
                        Стоп
                      </button>
                    )}
                    <button
                      className="btn btn--ghost"
                      style={{ padding: "6px 10px", fontSize: "0.85rem", opacity: 0.7 }}
                      title="Удалить аккаунт"
                      onClick={async () => {
                        if (!confirm(`Удалить @${a.login}?`)) return;
                        await removeAccount(a.login);
                        await refresh();
                      }}
                    >
                      ✕
                    </button>
                  </div>
                </div>
              );
              })}
              <button
                className="btn btn--ghost"
                disabled={loading}
                onClick={handleAddFriend}
              >
                + Добавить аккаунт (друга)
              </button>
            </section>
          )}

          {status.lastError && (
            <div className="alert alert--error">{status.lastError}</div>
          )}

          <section className="card actions">
            {!status.tracking?.active ? (
              <button
                className="btn btn--success"
                disabled={loading}
                onClick={handleStart}
              >
                Запустить учёт
              </button>
            ) : (
              <button
                className="btn btn--danger"
                disabled={loading}
                onClick={handleStop}
              >
                Остановить учёт
              </button>
            )}
            <button className="btn btn--ghost" onClick={() => setShowLogs((v) => !v)}>
              {showLogs ? "Скрыть логи" : "Логи"}
            </button>
            <button className="btn btn--ghost" onClick={handleLogout}>
              Выйти
            </button>
          </section>

          {showLogs && (
            <section className="card">
              <h3>Логи диагностики</h3>
              {logFile && (
                <p className="muted" style={{ marginBottom: 8, fontSize: "0.8rem" }}>
                  Файл: <code>{logFile}</code>
                </p>
              )}
              <pre className="log-box">
                {logs.length === 0
                  ? "Нет логов — backend запущен? (go run . в backend/)"
                  : logs.join("\n")}
              </pre>
            </section>
          )}

          <section className="card info">
            <h3>Режим API — без Chrome</h3>
            <ul>
              <li>
                Heartbeat каждые <strong>30 сек</strong> напрямую на сервер
                школы.
              </li>
              <li>
                <strong>Idle</strong>: если экран заблокирован &gt; 60 сек — время
                не капает (как в dashboard).
              </li>
              <li>
                <strong>Captcha</strong> — решается автоматически (macOS Vision).
              </li>
              <li>
                <strong>Agent pair</strong> каждые 10 мин (:47836).
              </li>
              <li>
                Друг сидел всю ночь, а 2ч — скорее idle + captcha + сессия
                упала. Смотри логи.
              </li>
            </ul>
          </section>
        </>
      )}

      <style>{`
        .page { max-width: 720px; margin: 0 auto; padding: 32px 20px 64px; }
        .header { display: flex; justify-content: space-between; align-items: flex-start; gap: 16px; margin-bottom: 24px; }
        h1 { margin: 0 0 4px; font-size: 1.75rem; }
        h2, h3 { margin: 0 0 12px; }
        .muted { color: var(--text-2); font-size: 0.9rem; }
        .user { text-align: right; display: flex; flex-direction: column; gap: 2px; }
        .card { background: var(--surface); border: 1px solid var(--border); border-radius: 16px; padding: 20px 22px; margin-bottom: 16px; }
        .grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(180px, 1fr)); gap: 12px; margin-bottom: 16px; }
        .stat { display: flex; flex-direction: column; gap: 4px; margin-bottom: 0; }
        .label { font-size: 0.8rem; color: var(--text-2); text-transform: uppercase; letter-spacing: 0.04em; }
        .value { font-size: 1.75rem; font-weight: 600; font-variant-numeric: tabular-nums; }
        .value--live { color: var(--success); }
        .hint { font-size: 0.75rem; color: var(--text-2); }
        .btn { border: none; border-radius: 10px; padding: 10px 18px; font-weight: 500; margin-right: 8px; margin-top: 8px; }
        .btn--primary { background: var(--accent); color: #0a0b0f; }
        .btn--success { background: var(--success); color: #0a0b0f; }
        .btn--danger { background: var(--danger); color: #fff; }
        .btn--ghost { background: transparent; border: 1px solid var(--border); color: var(--text); }
        .btn:disabled { opacity: 0.5; cursor: not-allowed; }
        .input { width: 100%; margin-top: 12px; padding: 12px; border-radius: 10px; border: 1px solid var(--border); background: var(--bg); color: var(--text); resize: vertical; }
        .steps { margin: 0 0 16px; padding-left: 20px; color: var(--text-2); line-height: 1.6; }
        .steps li { margin-bottom: 8px; }
        code { background: var(--bg); padding: 2px 6px; border-radius: 4px; font-size: 0.85em; }
        .alert { padding: 12px 16px; border-radius: 10px; margin-bottom: 16px; font-size: 0.9rem; }
        .alert--error { background: rgba(248,113,113,0.12); border: 1px solid rgba(248,113,113,0.3); color: #fecaca; }
        .alert--warning { background: rgba(251,191,36,0.12); border: 1px solid rgba(251,191,36,0.3); color: #fde68a; }
        .info ul { margin: 0; padding-left: 20px; color: var(--text-2); line-height: 1.7; }
        .info li { margin-bottom: 6px; }
        .account-row {
          display: flex; justify-content: space-between; align-items: center;
          padding: 10px 0; border-bottom: 1px solid var(--border); gap: 12px;
        }
        .account-row--active {
          background: rgba(110,231,183,0.06);
          margin: 0 -12px; padding: 10px 12px;
          border-radius: 10px;
        }
        .log-box {
          margin: 0; max-height: 320px; overflow: auto;
          background: var(--bg); border: 1px solid var(--border);
          border-radius: 8px; padding: 12px;
          font-family: ui-monospace, monospace; font-size: 0.72rem;
          line-height: 1.45; color: var(--text-2); white-space: pre-wrap;
        }
        a { color: var(--accent); }
      `}</style>
    </div>
  );
}
