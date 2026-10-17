# singbox-exporter

![License: MIT](https://img.shields.io/badge/license-MIT-yellow.svg)
![Go](https://img.shields.io/badge/Go-1.27+-00ADD8?logo=go&logoColor=white)
![Dependencies](https://img.shields.io/badge/dependencies-0-green)
![GitHub tag](https://img.shields.io/github/v/tag/nfx88/singbox-exporter)

> **🇷🇺 Русский** · [🇬🇧 English](README.en.md)

Prometheus-экспортер для [sing-box](https://github.com/SagerNet/sing-box), работающий
через его Clash API.

> Документация: [docs/clash-api.md](docs/clash-api.md) ·
> [CHANGELOG.md](CHANGELOG.md) · [CONTRIBUTING.md](CONTRIBUTING.md) ·
> [SECURITY.md](SECURITY.md) · [examples/](examples/)

Один статический Go-бинарник с **нулём внешних зависимостей** (только stdlib). Опрашивает
Clash API sing-box по HTTP и отдаёт метрики в Prometheus-текстовом формате — без лишних
инструментов, без WebSocket-handler'ов, без сайдкарных библиотек.

Полезен, когда sing-box крутится прокси на роутере/NAS и хочется видеть:

- какой VPN-сервер сейчас выбран и насколько он быстрый,
- лежит ли весь VPN (или отдельные серверы),
- реальный объём трафика и активные соединения,
- потребление ресурсов.

## Возможности

- Статический кросс-компилируемый бинарник: работает на любом Linux/arm64-хосте
  (OpenWrt/FriendlyWrt/NanoPi и пр.).
- Используются только три read-only эндпоинта Clash API: `/version`, `/connections`, `/proxies`.
- Счётчики трафика — **кумулятивные с момента старта sing-box** (берутся из
  `uploadTotal`/`downloadTotal` payload'а `/connections`) — не нужен WebSocket-канал,
  `rate()` в PromQL работает как обычно.
- `singbox_proxy_down` — `1`, когда у сервера нет валидной latency (мёртв), иначе `0`.
  Одна метрика для алерта «упали все прокси». Non-proxy outbound'ы (`direct`, `block`, DNS,
  WireGuard/Tailscale-endpoint'ы, группы) никогда не помечаются мёртвыми.
- Секционные снэпшоты: при сбое одного эндпоинта его семейства метрик сохраняют
  **последние успешные значения** вместо исчезновения; сбой виден в
  `singbox_endpoint_up`, `singbox_scrape_errors_total` и `singbox_up`.
- Не блокирующийся HTTP-сервер: всегда отдаёт последний снэпшот, даже если API
  недоступен (`singbox_up 0`).
- Метрики собираются параллельно (`/version`, `/connections`, `/proxies`) — снэпшот
  консистентен по времени; длительность опроса экспортируется как
  `singbox_scrape_duration_seconds`.
- Graceful shutdown по SIGINT/SIGTERM (дружит с `docker stop`).
- Встроенный Dockerfile со **multi-stage сборкой** — образ собирается сам, без
  предварительно собранного бинарника на хосте.
- HTTP-сервер с таймаутами и только `GET`/`HEAD` на `/metrics` и `/healthz`.

## Метрики

| Метрика | Тип | Лейблы | Смысл |
|---|---|---|---|
| `singbox_up` | gauge | | `1`, если Clash API ответил на последнем опросе |
| `singbox_endpoint_up` | gauge | `endpoint` (`version`/`connections`/`proxies`) | доступность каждого эндпоинта на последнем опросе |
| `singbox_scrape_errors_total` | counter | | число упавших запросов к Clash API с запуска |
| `singbox_scrape_duration_seconds` | gauge | | длительность последнего опроса |
| `singbox_scrape_timestamp_seconds` | gauge | | Unix-время последнего опроса |
| `singbox_last_success_timestamp_seconds` | gauge | | Unix-время последнего опроса, когда API ответил |
| `singbox_version_info` | gauge | `version` | версия sing-box, всегда `1` |
| `singbox_traffic_upload_bytes_total` | counter | | суммарные байты вверх с момента старта sing-box |
| `singbox_traffic_download_bytes_total` | counter | | суммарные байты вниз с момента старта sing-box |
| `singbox_active_connections` | gauge | | активные соединения в текущий момент |
| `singbox_connections_by_outbound` | gauge | `outbound` | активные соединения по outbound из цепочки (реальный сервер) |
| `singbox_connections_by_network` | gauge | `network` (`tcp`/`udp`) | активные соединения по протоколу |
| `singbox_connections_by_inbound` | gauge | `inbound` | активные соединения по inbound |
| `singbox_connections_by_rule` | gauge | `rule` | активные соединения по сработавшему правилу (следите за кардинальностью) |
| `singbox_connections_traffic_upload_bytes` | gauge | `outbound` | байты вверх активных соединений, по outbound |
| `singbox_connections_traffic_download_bytes` | gauge | `outbound` | байты вниз активных соединений, по outbound |
| `singbox_memory_bytes` | gauge | | используемая память процесса sing-box (эмитится всегда, `0` если недоступно) |
| `singbox_proxy_info` | gauge | `proxy`, `type` | метаданные outbound, всегда `1` (все outbound: группы, direct, endpoint'ы) |
| `singbox_proxy_delay_ms` | gauge | `proxy` | последняя latency URL-теста в мс (`0` = неизвестно/мёртв); только реальные прокси и группы |
| `singbox_proxy_delay_timestamp_seconds` | gauge | `proxy` | Unix-время последнего замера latency |
| `singbox_proxy_delay_age_seconds` | gauge | `proxy` | возраст последнего замера latency в секундах (свежесть данных) |
| `singbox_proxy_down` | gauge | `proxy` | `1`, если валидной latency нет (мёртв); только реальные прокси |
| `singbox_proxy_selected` | gauge | `group`, `proxy` | текущий выбранный прокси группы (URLTest/Selector/Fallback) |

Именование метрик — по общему соглашению Prometheus: точки в имени → подчёркивания.

## Как это работает

```
┌─────────────┐   http://127.0.0.1:9097   ┌──────────────┐   :9098   ┌───────────────┐
│  sing-box   │ ─────────────────────────▶│   exporter   │◀────────▶│   Prometheus   │
│ Clash API   │   /version /connections   │ (same host,  │   scrape  │ / VictoriaMetr.│
│             │   /proxies  (read-only)   │ host network)│   interval│               │
└─────────────┘                          └──────────────┘           └───────────────┘
```

- При старте (и далее каждые `-interval`, по умолчанию 15 с) экспортер опрашивает три
  эндпоинта **параллельно** и сохраняет снэпшот по каждому.
- Каждый скрейп `/metrics` рендерит последний снэпшот. Если опрос упал — отдаются последние
  успешные значения, `singbox_up`/`singbox_endpoint_up` падают,
  `singbox_scrape_errors_total` растёт.
- Так как счётчики трафика берутся из кумулятивных значений sing-box, они сбрасываются при
  каждом рестарте sing-box — Prometheus `rate()`/`increase()` работают корректно.

## Требования

- sing-box с включённым Clash API (достаточно loopback — экспортер рассчитан на запуск
  на том же хосте):

```json
{
  "experimental": {
    "clash_api": {
      "external_controller": "127.0.0.1:9097"
    },
    "cache_file": {
      "enabled": true
    }
  }
}
```

  Рекомендуется биндить контроллер на `127.0.0.1`, а если привязываете на не-loopback адрес —
  задать `"secret": "..."` (тогда передавайте `-token`).

- Статический Go-toolchain (версия **≥ 1.27**, согласно `go.mod`) на build-машине.

## Быстрый старт

### 1. Сборка

```sh
# Linux/arm64 (типично для роутеров/NAS на ARM)
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath \
  -ldflags="-s -w -X main.version=v1.0.0" -o singbox-exporter .

# Linux/amd64, macOS, ... — меняйте GOOS/GOARCH,
# например, macOS (локальный запуск):
go build -o singbox-exporter .
```

### 2. Запуск

Напрямую на хосте с sing-box:

```sh
./singbox-exporter -port 9098 -api http://127.0.0.1:9097
```

Или через Docker (рекомендуется для роутеров с docker-based sing-box):

```sh
docker build -t singbox-exporter:local .   # multi-stage: собирается внутри образа
docker compose up -d   # network_mode: host -> видит 127.0.0.1:9097
```

Образ содержит `HEALTHCHECK` (wget на `/healthz`), так что `docker inspect` показывает
здоровье контейнера без дополнительных инструментов.

### 3. Скрейп

Добавьте в `scrape_configs` Prometheus / VictoriaMetrics:

```yaml
- job_name: singbox
  static_configs:
    - targets: ["192.168.18.252:9098"]
```

## Флаги командной строки

| Флаг | По умолчанию | Описание |
|---|---|---|
| `-port` | `9098` | TCP-порт для `/metrics` |
| `-api` | `http://127.0.0.1:9097` | базовый URL Clash API sing-box |
| `-token` | *(пусто)* | опциональный Bearer-токен Clash API |
| `-interval` | `15s` | как часто опрашивать Clash API |
| `-log-level` | `info` | уровень логов: `debug`, `info`, `warn` или `error` |
| `-log-format` | `text` | формат логов: `text` или `json` |
| `-version` | `false` | вывести версию сборки и выйти |

`/healthz` отдаёт `200`, пока API был доступен на последнем опросе — удобно для
healthcheck контейнеров.

## Логирование

Логи идут в **stderr** в текстовом виде по умолчанию:

```
2026/10/15 22:50:09 ERROR collect failed endpoint="proxies" error="Get \"http://127.0.0.1:9097/proxies\": dial tcp...: connection refused"
```

- `-log-level error` гасит сообщения старта/остановки и оставляет только ошибки.
- `-log-format json` выдаёт один структурированный объект на строку с полями `ts`, `level`,
  `msg` и полями события (`endpoint`, `error`):

  ```json
  {"endpoint":"proxies","error":"...","level":"error","msg":"collect failed","ts":"2026-10-15T22:50:09.123405+03:00"}
  ```

- Упавшие эндпоинты логируются с **экспоненциальным backoff**: первая ошибка выводится
  сразу, следующие через 1м/2м/4м/… (потолок 30м), пока эндпоинт не восстановится. Это
  защищает от заливания логов, пока sing-box лежит; каждая ошибка по-прежнему увеличивает
  `singbox_scrape_errors_total` и переводит `singbox_endpoint_up` в `0`.

## Примеры PromQL

```promql
# Текущий выбранный VPN-сервер
singbox_proxy_selected{group="🚀 Auto-Fastest"}

# VPN лежит (все реальные прокси мертвы одновременно), по инстансу
count by (instance) (singbox_proxy_down == 1) == count by (instance) (singbox_proxy_down) > 0

# Свежесть данных о задержке (устарело — алерт StaleProxyHealth)
max by (instance) (singbox_proxy_delay_age_seconds)

# Скорость загрузки за последние 5 минут
sum(rate(singbox_traffic_download_bytes_total[5m]))

# Per-server latency, удобно для графика
singbox_proxy_delay_ms

# Активный входящий трафик по выбраному серверу
singbox_connections_traffic_download_bytes
```

## Примеры

Готовые интеграции живут в [`examples/`](examples/):

- [`examples/alerts.yml`](examples/alerts.yml) — правила алертов для
  Prometheus/VictoriaMetrics (API down, все прокси мертвы, флапающий выбор,
  устаревшие метрики, высокая память, протухшая свежесть latency).
- [`examples/grafana-singbox.json`](examples/grafana-singbox.json) — импортируемый
  Grafana-дашборд (классический формат, работает на любом Grafana ≥ 8):
  выбранный сервер, здоровье API/эндпоинтов, пропускная способность, соединения
  и живой трафик по outbound, latency прокси + свежесть, память, диагностика опроса.
- [`examples/grafana-singbox-v2.json`](examples/grafana-singbox-v2.json) — тот же
  дашборд в декларативном **v2-формате** (Grafana ≥ 11.4, Dashboards as Code).
  При импорте любого файла выберите `Prometheus` как data source. Оба дашборда
  содержат две переменные: `Instance` и `Group` (с *All* / мультивыбором);
  переменная `Group` управляет панелью «Выбранный VPN-сервер» — название группы
  больше не захардкожено.

## Разработка

См. [CONTRIBUTING.md](CONTRIBUTING.md): структура репозитория, часто используемые
команды (`make build/test/vet/lint/release`), соглашения об именовании метрик и PR-воркфлоу.
Проект намеренно без сторонних зависимостей — пожалуйста, сохраняйте это.

## Совместимость

- Проверено с sing-box `v1.14.3`.
- Зависит только от Clash API: `/version`, `/connections`, `/proxies` — эти эндпоинты
  стабильны в последних версиях sing-box.
- **Не** нужен `/traffic` (WebSocket) и `/memory`; значение памяти берётся из поля `memory`
  payload'а `/connections`.

## Changelog

См. [CHANGELOG.md](CHANGELOG.md). Кратко для следующего (unreleased) релиза:

- **Breaking:** `singbox_info{version,meta,premium}` → `singbox_version_info{version}`
  (`meta`/`premium` sing-box всегда отдаёт `true`).
- **Fix:** `direct`/`block`/`dns`/endpoint-outbound'ы больше не помечаются мёртвыми
  в `singbox_proxy_down`; при сбое эндпоинта его семейства метрик сохраняют последние
  успешные значения.
- **New:** здоровье по эндпоинтам, тайминги опроса, живой трафик по outbound,
  соединения по inbound/rule, свежесть latency (timestamp + age), graceful shutdown,
  `-version`, валидация флагов, таймауты HTTP-сервера, тесты, Docker Healthcheck,
  multi-stage Dockerfile.

## Лицензия

[MIT](LICENSE)
