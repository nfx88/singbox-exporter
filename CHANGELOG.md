# Changelog

Все заметные изменения проекта документируются в этом файле.

Формат — [Keep a Changelog](https://keepachangelog.com/ru/1.1.0/),
проект придерживается [Semantic Versioning](https://semver.org/lang/ru/).

## Unreleased

## [v0.1.0] - 2026-10-17

### Breaking

- `singbox_info{version,meta,premium}` заменена на `singbox_version_info{version}`.
  sing-box всегда отдаёт `meta`/`premium` как `true`, так что эти лейблы не несли
  информации. Переименуйте метрику в существующих дашбордах — больше ничего менять
  не нужно.

### Fixed

- `direct`, `block`/`reject`, `dns` и endpoint-outbound'ы (WireGuard, Tailscale, ...)
  больше не помечаются мёртвыми через `singbox_proxy_down` — метрики delay/down теперь
  есть только у реальных прокси.
- При сбое эндпоинта его семейства метрик не пропадают из `/metrics`: для каждой
  секции сохраняются последние успешные значения, а сбой виден в
  `singbox_endpoint_up`, `singbox_scrape_errors_total` и `singbox_up`.
- `/healthz` использует внутренний флаг доступности вместо сравнения отрендеренного текста.
- Большие байтовые счётчики выводятся в обычной десятичной записи (без экспоненты).
- `-interval <= 0` теперь отклоняется при разборе флагов (ранее `time.NewTicker`
  паниковал); `-port` валидируется в диапазоне `1..65535`.
- `/metrics` и `/healthz` принимают только `GET`/`HEAD` (иначе `405`); HTTP-сервер
  получил таймауты `ReadHeaderTimeout`/`ReadTimeout`/`WriteTimeout`/`IdleTimeout`.
- `singbox_memory_bytes` эмитится всегда (при `Memory == 0` — значение `0`), серия
  больше не исчезает из выдачи.

### Added

- `singbox_endpoint_up` — готовность каждого эндпоинта.
- `singbox_scrape_duration_seconds`, `singbox_scrape_timestamp_seconds`,
  `singbox_last_success_timestamp_seconds` — наблюдаемость опроса.
- `singbox_connections_by_inbound`, `singbox_connections_by_rule` — распределение
  активных соединений по inbound и сработавшему правилу.
- `singbox_connections_traffic_upload_bytes`,
  `singbox_connections_traffic_download_bytes` — живой трафик по outbound.
- `singbox_proxy_delay_timestamp_seconds` и `singbox_proxy_delay_age_seconds` —
  свежесть последнего URL-теста; добавлен алерт `StaleProxyHealth`.
- Флаг `-version` и инъекция версии на этапе сборки через
  `-ldflags "-X main.version=..."`.
- Флаги `-log-level` (debug/info/warn/error) и `-log-format` (text/json);
  структурированные JSON-логи с полями `ts`/`level`/`msg`/`endpoint`/`error`.
- Экспоненциальный backoff для повторяющихся `collect failed` (первая ошибка сразу,
  затем 1м/2м/4м/... с потолком 30м на эндпоинт) — метрики считают каждую ошибку.
- Примеры дашбордов переработаны: переменная `Group` вместо захардкоженной группы,
  `endpoint_up`, живой трафик по outbound, панели свежести latency и последнего
  успеха, `increase()`-окна трафика, regex-эскейпинг фильтра по инстансу; добавлен
  декларативный v2-вариант (`examples/grafana-singbox-v2.json`, Grafana ≥ 11.4).
- Юнит/интеграционные тесты, Docker `HEALTHCHECK`.
- Multi-stage `Dockerfile` — образ собирается сам без предварительно собранного бинарника.

### Changed

- Эндпоинты Clash API опрашиваются параллельно; цикл опроса использует ticker вместо
  фиксированного sleep.
- Graceful shutdown по SIGINT/SIGTERM.
- `parseFlags` переведён на `flag.NewFlagSet` с инъекцией аргументов — появились
  юнит-тесты на валидацию флагов и обрезку trailing slash.
- `store` использует `sync.RWMutex`: рендер `/metrics` берёт читающую блокировку.
- Go toolchain обновлён 1.25 → 1.27 (`go.mod`: `go 1.27` + `toolchain go1.27.2`;
  build-stage в `Dockerfile` — `golang:1.27-alpine`).

### Removed

- Устаревший `scripts/check_vpn.sh` удалён — его функции берёт на себя демон
  самолечения `singbox_healer.py` (в gitignored `deploy/`, не является частью релиза).

### Alerts (примеры `examples/alerts.yml`)

- `SingboxAllProxiesDead` и `SingboxManyProxiesDead` теперь группируют по
  `instance` — плейсхолдер `{{ $labels.instance }}` в уведомлениях заполняется.
- Добавлен `StaleProxyHealth` (свежесть latency).
