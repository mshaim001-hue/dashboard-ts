# ts-tracker

Локальное приложение для учёта часов на [dashboard.tomorrow-school.ai](https://dashboard.tomorrow-school.ai) без открытого Chrome.

Go backend шлёт heartbeat напрямую в API школы, решает captcha (macOS Vision), делает agent pair. React UI — вход, несколько аккаунтов, старт/стоп, логи.

## Возможности

- **Heartbeat каждые 30 сек** — `idle: false` по умолчанию
- **Несколько аккаунтов** — у каждого свой `deviceId`, виртуальный **hostname** (`E3-XX`) и fingerprint в heartbeat
- **Captcha** — автоматически на macOS (Vision OCR)
- **Agent pair** — локальный сервер на `:47836` (или использует школьный agent, если порт занят)
- **Вход через Chrome** — только на минуту для Gitea OAuth, для учёта браузер не нужен

## Требования

- **Go** 1.22+
- **Node.js** 18+ (только для dev UI)
- **macOS** — captcha OCR и опциональный `-detect-idle`
- Chrome — один раз при входе (кнопка «Войти через Chrome»)

## Быстрый старт

```bash
# 1. Backend
cd backend
go run .

# 2. Frontend (другой терминал)
cd frontend
npm install
npm run dev
```

Открой http://localhost:5173

### Production (один бинарник + статика)

```bash
cd frontend && npm run build
cd ../backend && go run . -frontend ../frontend/dist
```

UI будет на http://localhost:8080

## Использование

1. **Войти** — «Войти через Chrome» или вставить cookie `tmr_session` вручную
2. **Добавить аккаунт** — «+ Добавить аккаунт», повторить вход для друга
3. **Старт** — нажать «Старт» у нужного аккаунта (можно несколько параллельно)
4. **Смотреть** — переключить активный аккаунт в UI (статистика и логи)

Данные хранятся в `~/.ts-tracker/`:

```
~/.ts-tracker/
├── ts-tracker.log          # логи
├── device_id               # legacy (локальный agent :47836)
└── accounts/
    └── {login}/
        ├── account.json    # cookies, deviceId, hostname
        ├── device_id       # UUID для heartbeat
        └── fingerprint-v3  # fingerprint под виртуальный hostname
```

**Agent pair** — всегда реальный Mac через школьный agent на `:47836` (serial из инвентаря + токен).

**Heartbeat** — у каждого аккаунта свой виртуальный hostname `E3-01`…`E3-99` (deviceName + fingerprint).

При добавлении аккаунта hostname генерируется автоматически. Старые аккаунты получают его при первом запуске после обновления.

## Флаги backend

```bash
go run . [флаги]
```

| Флаг | По умолчанию | Описание |
|------|--------------|----------|
| `-addr` | `:8080` | HTTP API и UI (если `-frontend`) |
| `-agent-addr` | `:47836` | Локальный agent pair (`""` — выключить) |
| `-data` | `~/.ts-tracker` | Папка с сессиями |
| `-frontend` | — | Путь к собранному frontend (`dist`) |
| `-detect-idle` | `false` | `idle=true` если Mac без ввода >60 сек |

По умолчанию **всегда active** — время капает даже если свернул окно и не трогаешь мышь. Школьное поведение idle — только с `-detect-idle`.

## Архитектура

```
backend/
├── main.go
├── agent.go                    # локальный :47836/pair-info
└── internal/
    ├── api/                    # REST + UI
    ├── accounts/               # multi-account
    ├── client/                 # HTTP к dashboard API
    ├── tracking/               # heartbeat 30s, captcha, agent pair
    ├── captcha/                # OCR (macOS Vision)
    ├── idle/                   # HID idle (опционально)
    └── login/                  # вход через Rod/Chrome

frontend/
└── src/App.tsx                 # UI
```

## API (локальный)

| Метод | Путь | Описание |
|-------|------|----------|
| GET | `/api/status` | Статус, аккаунты, tracking |
| GET | `/api/logs` | Хвост лог-файла |
| POST | `/api/tracking/start` | Старт учёта |
| POST | `/api/tracking/stop` | Стоп |
| POST | `/api/login/browser` | Вход через Chrome |
| GET | `/api/login-url` | URL OAuth |

## Диагностика

Смотри логи в UI («Показать логи») или:

```bash
tail -f ~/.ts-tracker/ts-tracker.log
```

**Нормально:**
- `heartbeat OK` + `idle=false` + `deltaSeconds: 30`
- `time credited delta=30`
- `agent pair ok`

**Проблемы:**

| Симптом | Что делать |
|---------|------------|
| Время не растёт 4+ мин | Wi-Fi, captcha, перезапусти tracking |
| `403` CSRF | Перелогинься или перезапусти backend |
| `409` another device | Нажми Старт снова (takeover) или закрой Chrome с dashboard |
| Captcha не решается | Только macOS; проверь логи `captcha` |

## Заметки

- `todaySeconds` в API — **за весь день**, не длина текущей сессии
- Один активный tracking на deviceId у школы — при конфликте backend делает stop → start
- Heartbeat идёт в фоне (`context.Background`) — не умирает после закрытия HTTP-запроса
