# sing-box Clash API reference (as consumed by this exporter)

This page documents the parts of the sing-box
[Clash API](https://sing-box.sagernet.org/configuration/experimental/clash-api/)
that `singbox-exporter` relies on. Field names and semantics below were taken
from the sing-box source
(`experimental/clashapi`, branch `testing`) so the exporter stays faithful to
the implementation rather than to marketing descriptions.

Enabling the API in sing-box config:

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

- `external_controller` empty → the API is disabled entirely.
- A non-empty `secret` requires every (non-WebSocket) request to carry
  `Authorization: Bearer <secret>`, which this exporter sends when
  `-token` is set.

## Endpoints used

The exporter only issues `GET` requests; it never mutates sing-box state.

### `GET /version`

Response:

```json
{
  "version": "sing-box 1.14.3",
  "premium": true,
  "meta": true
}
```

Note: sing-box hard-codes `premium: true` and `meta: true`; the values carry no
information, so the exporter exposes only `version` (in `singbox_version_info`).

### `GET /connections`

Response (non-WebSocket clients get a single snapshot):

```json
{
  "downloadTotal": 1048576,
  "uploadTotal": 524288,
  "connections": [ /* connectionObject */ ],
  "memory": 47185920
}
```

- `downloadTotal` / `uploadTotal` — cumulative bytes since the sing-box
  **process started** (not since the last read). Prometheus `rate()`/`increase()`
  handle resets when sing-box restarts.
- `connections` — active connections, with DNS-only connections already
  filtered out by sing-box.
- `memory` — inuse memory of the sing-box process (Go runtime
  `StackInuse + HeapInuse + HeapIdle - HeapReleased`), not RSS.

Each `connectionObject` marshals to:

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

Fields used by the exporter:

| Field | Meaning | Exported as |
|---|---|---|
| `chains` | outbound chain, **reversed**: `[0]` is the final (deepest) outbound, the last element is the entry group | `singbox_connections_by_outbound{outbound=<chains[0]>}`, traffic gauges per outbound |
| `metadata.network` | `tcp` or `udp` | `singbox_connections_by_network` |
| `metadata.type` | inbound string, e.g. `mixed/mixed-in`, `tun` | `singbox_connections_by_inbound` |
| `rule` | matched rule description, e.g. `geosite:netflix => route`; `final` when the default route matched | `singbox_connections_by_rule` |
| `upload` / `download` | bytes of the **live** connection since it opened (not cumulative across connections) | `singbox_connections_traffic_upload_bytes{outbound=...}` etc. as gauges |

> `chains[0]` is the actual VPN server (or `direct`), not the group, because
> sing-box reverses the chain before marshaling. See `common/trafficcontrol/tracker.go`.

### `GET /proxies`

Response shape:

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

Important facts, straight from `experimental/clashapi/proxies.go`:

- The map contains a synthetic **`GLOBAL`** entry (type `Fallback`, `now` = the
  default outbound) — harmless for queries, kept for dashboards that expect it.
- Not only proxies are listed. **Every tagged outbound and endpoint** is present:
  `direct`, `block`/`reject`, `dns`, endpoints (WireGuard, Tailscale, ...).
  They have no URL-test semantics, so this exporter never flags them dead
  (`singbox_proxy_down` is only emitted for allowlisted proxy types).
- Groups (`URLTest`, `Selector`, `Fallback`) carry `now` (current selection) and
  `all` (member tags). Non-group entries have neither.
- `history` is a **single-element array** with the last successful URL test
  `{time, delay}`; `delay` is in milliseconds. When reachable, it can hold zero
  elements — the exporter treats that as "dead".
- A failed URL test (`/proxies/{name}/delay` returning 503 or 0) deletes the
  history entry, so absence of history ⇔ dead.
- `history[0].time` is the time of the last test; the exporter derives **freshness**
  from it (`singbox_proxy_delay_timestamp_seconds`, `singbox_proxy_delay_age_seconds`).

Fields used by the exporter:

| Field | Meaning | Exported as |
|---|---|---|
| `type` | outbound display type (`VMess`, `Trojan`, `Shadowsocks`, `Selector`, ...) | `singbox_proxy_info{type=...}` |
| `history[-1].delay` | last latency in ms (0/absent = dead) | `singbox_proxy_delay_ms`, `singbox_proxy_down` |
| `history[-1].time` | RFC3339 time of the last test | `singbox_proxy_delay_timestamp_seconds`, `singbox_proxy_delay_age_seconds` |
| `now` | current selection of a group | `singbox_proxy_selected{group,proxy=now}` |

## Endpoints that are *not* used

- `GET /traffic` (WebSocket/SSE) — real-time per-second deltas. Not needed:
  cumulative totals come from `/connections`.
- `GET /memory` (WebSocket/SSE) — superseded by the `memory` field of
  `/connections`.
- `GET /logs` — streamed logs, out of scope for a metrics exporter.
- `POST`/`PUT`/`PATCH`/`DELETE` variants — the exporter is strictly read-only.

## Compatibility notes

- Endpoints `/version`, `/connections`, `/proxies` have been stable across
  recent sing-box releases (verified against `v1.14.x`).
- Since sing-box 1.8.0 the `store_mode`/`store_selected`/`store_fakeip` fields
  moved to `experimental.cache_file`; the JSON contract of the three endpoints
  above is unaffected.
- Since sing-box 1.14.0 a separate **gRPC** "sing-box API"
  (`service` type `api`) exists. This exporter intentionally keeps using the
  Clash API to stay a dependency-free, HTTP-only binary.
