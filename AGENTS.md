# AGENTS.md

## Project overview

`singbox-exporter` — Prometheus-экспортер для [sing-box](https://github.com/SagerNet/sing-box),
работающий через его Clash API. Единственный **строгий принцип**: Go-код использует только
**stdlib** — никаких сторонних зависимостей (`go.mod` без `require`).

Экспортер поллит три read-only эндпоинта Clash API (`/version`, `/connections`, `/proxies`),
хранит секционные снэпшоты и отдаёт их в Prometheus-формате на `/metrics`.

Дополнительно в `deploy/` живёт Python-демон самолечения `singbox_healer.py` — это
gitignored scratch-зона деплоя, **не** часть сборки Go-проекта (см. раздел про deploy).

## Repository layout

| Путь | Назначение |
|---|---|
| `main.go` | entrypoint, флаги, HTTP-сервер, graceful shutdown |
| `client.go` | HTTP-клиент Clash API + Bearer-авторизация |
| `types.go` | JSON-структуры payload'ов Clash API |
| `store.go` | секционные снэпшоты + рендер Prometheus-текста |
| `collect.go` | сбор, фильтрация и агрегация метрик |
| `logging.go` | leveled logger (`-log-level`, `-log-format`) + error-backoff |
| `*_test.go` | unit/integration-тесты (fake Clash API через `net/http/httptest`) |
| `deploy/` | gitignored: `singbox_healer.py` + конфиги мониторинга (не коммитить) |
| `docs/clash-api.md` | reference используемой части Clash API |
| `examples/` | алерты Prometheus/VictoriaMetrics + Grafana-дашборды |

## Критические ограничения

- **Никаких внешних Go-зависимостей.** Не добавлять их без явного запроса пользователя.
- Не менять семантику уже существующих метрик без обсуждения (имя/тип/лейблы) — это
  ломает дашборды и алерты.
- Не коммитить секреты: `.env`, токены, реальные URL подписок, `deploy/`.
- Metric naming: точки в имени → подчёркивания (`traffic_download_bytes_total`); суффиксы
  `_total` (counter), `_bytes`, `_seconds`, `_info`, `_up` — консистентно.

## Build / проверки

```sh
go build ./...          # компиляция
go vet ./...            # статический анализ
go test ./...           # тесты
go test -race ./...     # тесты с race-детектором
gofmt -l .              # список неотформатированных файлов
golangci-lint run       # опционально (см. .golangci.yml)
make                    # vet + test + build
make release            # cross-compile static в dist/ (linux/{amd64,arm64,386}, darwin)
```

Версия вшивается через: `go build -ldflags "-X main.version=vX.Y.Z"`.

**Всегда запускайте перед завершением задачи:** `gofmt -l .`, `go vet ./...`,
`go test -race ./...` — весь набор должен быть зелёным.

## Тестирование

- Добавляйте тесты на новое поведение (даже если не просили).
- `collect_test.go` использует `httptest.Server` + `fakeAPI` с реалистичными JSON
  payload'ами Clash API (GLOBAL, группы, direct/endpoint, history, connections).
- Юнит-части: `store_test.go` (рендер/escape/sections), `logging_test.go` (уровни,
  форматы, backoff).
- При изменении коллектора — обновите соответствующие ассерты в `collect_test.go`.

## Добавление метрики

1. Сверьте поля с исходниками sing-box (`experimental/clashapi`, ветка `testing`) —
   брать семантику из кода, а не из памяти.
2. Добавьте поле в `types.go`, если его ещё нет.
3. Эмитируйте sample в нужном коллекторе `collect.go` (connections/proxies/version).
4. Уважайте naming conventions (см. выше).
5. Обновите таблицу метрик в `README.md`, `examples/` и `CHANGELOG.md`.

## Clash API: важные факты (кратко)

- `/proxies` отдаёт **все** tagged outbound'ы, включая `direct`/`block`/`dns`/endpoint'ы.
  `singbox_proxy_down`/`delay` эмитятся **только** для allowlist реальных прокси-типов
  (`proxyTypeSet` в `collect.go`) и групп — direct/endpoint'ы не должны помечаться «dead».
- `chains[0]` — финальный (реальный) outbound, группировка `by_outbound` идёт по нему.
- `uploadTotal`/`downloadTotal` в `/connections` — кумулятивные с момента старта sing-box.
- Полная reference: `docs/clash-api.md`.

## deploy/ (Python-хилер) — не часть Go-сборки

- Gitignored (`/deploy/` в `.gitignore`) — артефакты локального деплоя, не коммитить.
- `singbox_healer.py` — демон самолечения: поллит VictoriaMetrics (PromQL), при сбое VPN
  обновляет подписку, latency-тестирует кандидатов и рестартит sing-box.
- Python 3, stdlib only. Проверки: `python3 -m py_compile deploy/singbox_healer.py`,
  `python3 deploy/singbox_healer_test.py` (23 unit-теста, без docker/VM).
- Подкоманды: `run`, `update`, `check`; флаги `--dry-run`, `--env`, `--verbose`.
- Конфигурация — через env / `.env` (`SUBSCRIPTION_URL`, `VM_URL`, `INSTANCE`, ...).
- Подробности: `deploy/README.md`.

## Безопасность

- Никогда не логировать и не коммитить секреты (токены, uuid подписок, ключи).
- Рекомендация: Clash API на loopback + `secret`; экспортер с `-token` при
  не-loopback адресе; `/metrics` не выставлять в недоверенные сети.
- label values экранируются в рендере — но это не повод открывать `/metrics` наружу.

## Коммиты и PR

- Короткий императивный заголовок; при необходимости абзац «почему».
- Ссылайтесь на issue: `Fixes #12`.
- Для user-visible изменений обновляйте `README.md`, `CHANGELOG.md` (Keep a Changelog,
  раздел Unreleased) и `examples/` при необходимости.
- PR checklist — в `.github/PULL_REQUEST_TEMPLATE.md`; подробности — в `CONTRIBUTING.md`.
- CI (`.github/workflows/ci.yml`): `go vet`, `go test`, сборка static под linux.

## Docs

- `README.md` — для людей; `CHANGELOG.md` — Keep a Changelog; `CONTRIBUTING.md` — вклад;
  `docs/clash-api.md` — детали Clash API; `SECURITY.md` — как сообщать об уязвимостях.
