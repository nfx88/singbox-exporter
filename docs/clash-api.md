# Reference Clash API sing-box (в части, которую использует экспортер)

Эта страница документирует части Clash API sing-box
([документация](https://sing-box.sagernet.org/configuration/experimental/clash-api/)),
на которые опирается `singbox-exporter`. Имена полей и семантика взяты из исходников
sing-box (`experimental/clashapi`, ветка `testing`) — экспортер следует реализации,
а не маркетинговым описаниям.

Включение API в конфиге sing-box:

```json
{
  "experimental": {
    "clash_api": {
      "external_controller": "127.0.0.1:9097",
      "secret": ""
    }
  }
}
```

- Пустой `external_controller` → API полностью выключен.
- Непустой `secret` требует, чтобы каждый (не-WebSocket) запрос нёс
  `Authorization: Bearer <secret>`; экспортер посылает его, когда задан `-token`.

## Используемые эндпоинты

Экспортер выполняет только `GET`-запросы; состояние sing-box он никогда не меняет.

### `GET /version`

Ответ:

```json
{
  "version": "sing-box 1.14.3",
  "premium": true,
  "meta": true
}
```

Примечание: sing-box жёстко зашивает `premium: true` и `meta: true`; эти значения не
несут информации, поэтому экспортер отдаёт только `version` (в `singbox_version_info`).

### `GET /connections`

Ответ (не-WebSocket клиенты получают один снэпшот):

```json
{
  "downloadTotal": 1048576,
  "uploadTotal": 524288,
  "connections": [ /* connectionObject */ ],
  "memory": 47185920
}
```

- `downloadTotal` / `uploadTotal` — кумулятивные байты **с момента старта процесса**
  sing-box (не с последнего чтения). Prometheus `rate()`/`increase()` корректно
  обрабатывают сброс при рестарте.
- `connections` — активные соединения; DNS-only соединения sing-box уже отфильтровал.
- `memory` — используемая память процесса sing-box (Go runtime
  `StackInuse + HeapInuse + HeapIdle - HeapReleased`), не RSS.

Каждый `connectionObject` маршалится так:

```json
{
  "id": "uuid",
  "metadata": {
    "network": "tcp",
    "type": "mixed/mixed-in",
    "sourceIP": "192.168.1.2",
    "sourcePort": "54321",
    "destinationIP": "1.2.3.4",
    "destinationPort": "443",
    "host": "example.com",
    "dnsMode": "normal",
    "processPath": "/usr/bin/curl"
  },
  "upload": 1024,
  "download": 4096,
  "start": "2026-10-15T10:00:00.123Z",
  "chains": ["node1", "🚀 Auto-Fastest"],
  "rule": "geosite:netflix => route",
  "rulePayload": ""
}
```

Поля, используемые экспортером:

| Поле | Смысл | Экспортируется как |
|---|---|---|
| `chains` | цепочка outbound'ов, **в обратном порядке**: `[0]` — финальный (самый глубокий) outbound, последний элемент — входная группа | `singbox_connections_by_outbound{outbound=<chains[0]>}`, gauge'и трафика по outbound |
| `metadata.network` | `tcp` или `udp` | `singbox_connections_by_network` |
| `metadata.type` | строка inbound, напр. `mixed/mixed-in`, `tun` | `singbox_connections_by_inbound` |
| `rule` | описание сработавшего правила, напр. `geosite:netflix => route`; `final`, если сработал маршрут по умолчанию | `singbox_connections_by_rule` |
| `upload` / `download` | байты **живого** соединения с момента его открытия (не кумулятивные между соединениями) | `singbox_connections_traffic_upload_bytes{outbound=...}` и т.п. как gauge |

> `chains[0]` — это реальный VPN-сервер (или `direct`), а не группа, потому что
> sing-box разворачивает цепочку перед маршаллом. См. `common/trafficcontrol/tracker.go`.

### `GET /proxies`

Форма ответа:

```json
{
  "proxies": {
    "GLOBAL": { "type": "Fallback", "name": "GLOBAL", "udp": true, "history": [], "all": ["node1", "node2"], "now": "node1" },
    "🚀 Auto-Fastest": { "type": "URLTest", "name": "🚀 Auto-Fastest", "udp": true, "history": [{ "time": "2026-10-15T09:59:00Z", "delay": 123 }], "all": ["node1", "node2"], "now": "node1" },
    "node1": { "type": "VMess", "name": "node1", "udp": true, "history": [{ "time": "2026-10-15T10:00:00Z", "delay": 80 }] },
    "node2": { "type": "Trojan", "name": "node2", "udp": true, "history": [] },
    "direct": { "type": "Direct", "name": "direct", "udp": true, "history": [] },
    "wg0": { "type": "WireGuard", "name": "wg0", "udp": true, "history": [] }
  }
}
```

Важные факты прямо из `experimental/clashapi/proxies.go`:

- В карте есть синтетическая запись **`GLOBAL`** (тип `Fallback`, `now` = outbound
  по умолчанию) — для запросов безвредна, хранится для дашбордов, которые её ждут.
- Перечислены **не только прокси**. Присутствует **каждый tagged outbound и endpoint**:
  `direct`, `block`/`reject`, `dns`, endpoint'ы (WireGuard, Tailscale, ...). У них нет
  семантики URL-теста, поэтому экспортер никогда не помечает их мёртвыми
  (`singbox_proxy_down` эмитится только для allowlist-прокси-типов).
- Группы (`URLTest`, `Selector`, `Fallback`) несут `now` (текущий выбор) и `all`
  (теги участников). У остальных записей их нет.
- `history` — **массив из одного элемента** с последним успешным URL-тестом
  `{time, delay}`; `delay` в миллисекундах. Когда соединение достижимо, он может быть
  пустым — экспортер трактует это как «мёртв».
- Неудачный URL-тест (`/proxies/{name}/delay`, возвращающий 503 или 0) удаляет запись
  из `history`, поэтому отсутствие history ⇔ мёртв.
- `history[0].time` — время последнего теста; по нему экспортер считает **свежесть**
  (`singbox_proxy_delay_timestamp_seconds`, `singbox_proxy_delay_age_seconds`).

Поля, используемые экспортером:

| Поле | Смысл | Экспортируется как |
|---|---|---|
| `type` | отображаемый тип outbound (`VMess`, `Trojan`, `Shadowsocks`, `Selector`, ...) | `singbox_proxy_info{type=...}` |
| `history[-1].delay` | последняя latency в мс (0/отсутствует = мёртв) | `singbox_proxy_delay_ms`, `singbox_proxy_down` |
| `history[-1].time` | RFC3339-время последнего теста | `singbox_proxy_delay_timestamp_seconds`, `singbox_proxy_delay_age_seconds` |
| `now` | текущий выбор группы | `singbox_proxy_selected{group,proxy=now}` |

## Эндпоинты, которые *не* используются

- `GET /traffic` (WebSocket/SSE) — реальновременные дельты в секунду. Не нужен:
  кумулятивные итоги берутся из `/connections`.
- `GET /memory` (WebSocket/SSE) — заменён полем `memory` из `/connections`.
- `GET /logs` — стрим логов, вне рамок метрического экспортера.
- `POST`/`PUT`/`PATCH`/`DELETE` варианты — экспортер строго read-only.

## Замечания о совместимости

- Эндпоинты `/version`, `/connections`, `/proxies` стабильны в последних версиях
  sing-box (проверено на `v1.14.x`).
- Начиная с sing-box 1.8.0 поля `store_mode`/`store_selected`/`store_fakeip` переехали
  в `experimental.cache_file`; JSON-контракт трёх эндпоинтов выше не изменился.
- Начиная с sing-box 1.14.0 существует отдельный **gRPC** "sing-box API"
  (`service` типа `api`). Экспортер намеренно остаётся на Clash API, чтобы оставаться
  свободным от зависимостей HTTP-only бинарником.
