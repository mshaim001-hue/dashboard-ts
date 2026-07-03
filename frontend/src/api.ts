export type Account = {
  login: string;
  displayName: string;
  tracking: boolean;
  sessionActive?: boolean;
  todaySeconds: number;
  weekSeconds: number;
  stalled?: boolean;
  lastError?: string;
  virtualHostname?: string;
};

export type Status = {
  authenticated: boolean;
  user?: { login: string; displayName: string };
  mode?: string;
  trackerRunning?: boolean;
  stalled?: boolean;
  lastError?: string;
  tracking?: {
    active: boolean;
    startedAt?: string;
    challengePending?: boolean;
  };
  hours?: {
    todaySeconds: number;
    weekSeconds: number;
    totalSeconds?: number;
  };
  accounts?: Account[];
  logFile?: string;
};

const API = "/api";

export async function fetchStatus(): Promise<Status> {
  const res = await fetch(`${API}/status`);
  if (!res.ok) throw new Error(`backend ${res.status}`);
  return res.json();
}

export async function fetchAccounts(): Promise<Account[]> {
  const res = await fetch(`${API}/accounts`);
  const data = await res.json();
  return data.accounts ?? [];
}

export async function selectAccount(login: string): Promise<void> {
  await fetch(`${API}/accounts/select`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ login }),
  });
}

export async function removeAccount(login: string): Promise<void> {
  await fetch(`${API}/accounts/${encodeURIComponent(login)}`, { method: "DELETE" });
}

export async function getLoginURL(): Promise<{ url: string }> {
  const res = await fetch(`${API}/login-url`);
  return res.json();
}

export async function startBrowserLogin(): Promise<void> {
  const res = await fetch(`${API}/login/browser`, { method: "POST" });
  if (!res.ok) {
    const data = await res.json().catch(() => ({}));
    throw new Error(data.error ?? "Не удалось открыть браузер");
  }
}

export async function pollBrowserLogin(): Promise<{
  running: boolean;
  authenticated?: boolean;
  error?: string;
  account?: Account;
}> {
  const res = await fetch(`${API}/login/browser/status`);
  return res.json();
}

export async function saveSession(cookie: string): Promise<void> {
  const res = await fetch(`${API}/session`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ cookie }),
  });
  if (!res.ok) {
    const data = await res.json().catch(() => ({}));
    throw new Error(data.error ?? "Ошибка сохранения сессии");
  }
}

export async function startTracking(login?: string): Promise<void> {
  const res = await fetch(`${API}/tracking/start`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(login ? { login } : {}),
  });
  if (!res.ok) {
    const data = await res.json().catch(() => ({}));
    throw new Error(data.error ?? "Не удалось запустить трекинг");
  }
}

export async function stopTracking(login?: string): Promise<void> {
  await fetch(`${API}/tracking/stop`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(login ? { login } : {}),
  });
}

export async function fetchLogs(): Promise<{ logFile: string; lines: string[] }> {
  const res = await fetch(`${API}/logs`);
  return res.json();
}

export async function logout(): Promise<void> {
  await fetch(`${API}/logout`, { method: "POST" });
}

export function formatDuration(ms: number): string {
  const total = Math.floor(ms / 1000);
  const h = Math.floor(total / 3600);
  const m = Math.floor((total % 3600) / 60);
  const s = total % 60;
  return [h, m, s].map((n) => String(n).padStart(2, "0")).join(":");
}

export function formatHours(seconds: number): string {
  const h = Math.floor(seconds / 3600);
  const m = Math.floor((seconds % 3600) / 60);
  if (h === 0) return `${m}м`;
  return `${h}ч ${m}м`;
}
