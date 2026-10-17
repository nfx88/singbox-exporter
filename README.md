# singbox-exporter

![License: MIT](https://img.shields.io/badge/license-MIT-yellow.svg)
![Go](https://img.shields.io/badge/Go-1.25+-00ADD8?logo=go&logoColor=white)
![Dependencies](https://img.shields.io/badge/dependencies-0-green)

Prometheus exporter for [sing-box](https://github.com/SagerNet/sing-box) backed by its Clash API.

> Documentation & community: [docs/clash-api.md](docs/clash-api.md) ·
> [CHANGELOG.md](CHANGELOG.md) · [CONTRIBUTING.md](CONTRIBUTING.md) ·
> [SECURITY.md](SECURITY.md) · [examples/](examples/)

A single static Go binary with **zero external dependencies** (stdlib only). It polls
the sing-box Clash API over plain HTTP and serves metrics in the Prometheus text format —
no extra tooling, no WebSocket handling, no sidecar libraries.

Useful when you run sing-box as a proxy on a router/NAS and want to watch:

- which VPN server is currently selected and how fast it is,
- whether the whole VPN (or individual servers) is down,
- real traffic volume and active connections,
- resource usage.

## Features

- Static cross-compiled binary, works on any Linux/arm64 host including routers (OpenWrt/FriendlyWrt/NanoPi etc.).
- Only three read-only Clash API endpoints are used: `/version`, `/connections`, `/proxies`.
- Traffic counters are **cumulative since sing-box start** (taken from `uploadTotal`/`downloadTotal`
  of the `/connections` payload) — no WebSocket traffic drain needed, `rate()` in PromQL just works.
- `singbox_proxy_down` — 1 when a server has no valid latency (dead), 0 otherwise. A single
  metric to alert on when every proxy is down. Non-proxy outbounds (`direct`, `block`, DNS,
  WireGuard/Tailscale endpoints, groups) are never marked dead.
- Per-section snapshots: if one endpoint fails, its metric families keep the **last successful
  values** instead of vanishing; the failure is visible in `singbox_endpoint_up`,
  `singbox_scrape_errors_total` and `singbox_up`.
- Non-blocking: the HTTP server always serves the last collected snapshot, even if the API
  is unreachable (`singbox_up 0`).
- Metrics are collected in parallel (`/version`, `/connections`, `/proxies`) so the snapshot
  is consistent in time; the poll duration is exported as `singbox_scrape_duration_seconds`.
- Graceful shutdown on SIGINT/SIGTERM (docker stop friendly).

## Metrics

| Metric | Type | Labels | Meaning |
|---|---|---|---|
| `singbox_up` | gauge | | 1 when the Clash API answered at the last poll |
| `singbox_endpoint_up` | gauge | `endpoint` (`version`/`connections`/`proxies`) | per-endpoint reachability at the last poll |
| `singbox_scrape_errors_total` | counter | | failed Clash API requests since start |
| `singbox_scrape_duration_seconds` | gauge | | duration of the last poll |
| `singbox_scrape_timestamp_seconds` | gauge | | Unix time of the last poll |
| `singbox_last_success_timestamp_seconds` | gauge | | Unix time of the last poll where the API answered |
| `singbox_version_info` | gauge | `version` | sing-box version, always 1 |
| `singbox_traffic_upload_bytes_total` | counter | | cumulative bytes up since sing-box start |
| `singbox_traffic_download_bytes_total` | counter | | cumulative bytes down since sing-box start |
| `singbox_active_connections` | gauge | | currently active connections |
| `singbox_connections_by_outbound` | gauge | `outbound` | active connections per reverse-chain outbound (the actual server) |
| `singbox_connections_by_network` | gauge | `network` (`tcp`/`udp`) | active connections per protocol |
| `singbox_connections_by_inbound` | gauge | `inbound` | active connections per inbound |
| `singbox_connections_by_rule` | gauge | `rule` | active connections per matched routing rule (watch cardinality) |
| `singbox_connections_traffic_upload_bytes` | gauge | `outbound` | bytes uploaded by currently active connections, per outbound |
| `singbox_connections_traffic_download_bytes` | gauge | `outbound` | bytes downloaded by currently active connections, per outbound |
| `singbox_memory_bytes` | gauge | | sing-box process inuse memory |
| `singbox_proxy_info` | gauge | `proxy`, `type` | outbound metadata, always 1 (all outbounds, incl. groups/direct/endpoints) |
| `singbox_proxy_delay_ms` | gauge | `proxy` | last URL-test latency in ms (`0` = unknown/dead); real proxies and groups |
| `singbox_proxy_delay_timestamp_seconds` | gauge | `proxy` | Unix time of the last latency measurement |
| `singbox_proxy_down` | gauge | `proxy` | `1` when no valid latency (dead); real proxies only |
| `singbox_proxy_selected` | gauge | `group`, `proxy` | currently selected proxy per group (URLTest/Selector/Fallback) |

Group prometheus uses a common convention: metric name dots → underscores, so label/instance
names keep lowercase.

## How it works

```
┌─────────────┐   http://127.0.0.1:9097   ┌──────────────┐   :9098   ┌───────────────┐
│  sing-box   │ ─────────────────────────▶│   exporter   │◀────────▶│   Prometheus   │
│ Clash API   │   /version /connections   │ (same host,  │   scrape  │ / VictoriaMetr.│
│             │   /proxies  (read-only)   │ host network)│   interval│               │
└─────────────┘                          └──────────────┘           └───────────────┘
```

- On start (and then every `-interval`, default 15 s) the exporter queries the three API
  endpoints **in parallel** and stores a snapshot per endpoint.
- Each scrape of `/metrics` renders the latest snapshot. If a poll fails, the last successful
  values are still served, `singbox_up`/`singbox_endpoint_up` drop and
  `singbox_scrape_errors_total` increases.
- Because traffic counters come from sing-box's own cumulative totals, they reset every time
  sing-box restarts — Prometheus `rate()`/`increase()` handle counter resets transparently.

## Requirements

- sing-box with the Clash API enabled (loopback is enough; the exporter is meant to run on
  the same host):

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

  Recommended: bind the controller to `127.0.0.1` since the exporter reaches it over loopback,
  and add `"secret": "..."` if you ever bind it to a non-loopback address (then pass `-token`).

- A static Go toolchain (any version ≥ 1.22) on the build machine.

## Quick start

### 1. Build

```sh
# Linux/arm64 (typical for ARM routers/NAS)
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath \
  -ldflags="-s -w -X main.version=v1.0.0" -o singbox-exporter .

# Linux/amd64, macOS, windows... just change GOOS/GOARCH,
# e.g. macOS (local run):
go build -o singbox-exporter .
```

### 2. Run

Directly on the sing-box host:

```sh
./singbox-exporter -port 9098 -api http://127.0.0.1:9097
```

Or with Docker (recommended for routers with an existing docker-based sing-box):

```sh
docker build -t singbox-exporter:local .
docker compose up -d   # uses network_mode: host so it sees 127.0.0.1:9097
```

The image ships `HEALTHCHECK` (via `wget` on `/healthz`), so `docker inspect` reports
container health without extra tooling.

### 3. Scrape it

Add to Prometheus / VictoriaMetrics `scrape_configs`:

```yaml
- job_name: singbox
  static_configs:
    - targets: ["192.168.18.252:9098"]
```

## Command-line flags

| Flag | Default | Description |
|---|---|---|
| `-port` | `9098` | TCP port for `/metrics` |
| `-api` | `http://127.0.0.1:9097` | sing-box Clash API base URL |
| `-token` | *(empty)* | optional Bearer token for the Clash API |
| `-interval` | `15s` | how often to poll the Clash API |
| `-log-level` | `info` | log verbosity: `debug`, `info`, `warn` or `error` |
| `-log-format` | `text` | log output format: `text` or `json` |
| `-version` | `false` | print build version and exit |

`/healthz` returns `200` while the API was reachable at the last poll — convenient for
container healthchecks.

## Logging

Logs go to **stderr** in plain text by default:

```
2026/10/15 22:50:09 ERROR collect failed endpoint="proxies" error="Get \"http://127.0.0.1:9097/proxies\": dial tcp...: connection refused"
```

- `-log-level error` silences the startup/shutdown messages and keeps only errors.
- `-log-format json` emits one structured object per line with `ts`, `level`, `msg`
  and per-event fields (`endpoint`, `error`):

  ```json
  {"endpoint":"proxies","error":"...","level":"error","msg":"collect failed","ts":"2026-10-15T22:50:09.123405+03:00"}
  ```

- Failed endpoint polls are logged with **exponential backoff**: the first failure
  of an endpoint appears immediately, subsequent ones at 1m/2m/4m/… (capped at 30m)
  until the endpoint recovers. This prevents log flooding while sing-box is down;
  every failure still increments `singbox_scrape_errors_total` and flips
  `singbox_endpoint_up` to `0`.

## Example PromQL queries

```promql
# Current selected VPN server
singbox_proxy_selected{group="🚀 Auto-Fastest"}

# VPN is down (every real proxy dead at once)
sum(singbox_proxy_down == 1) == count(singbox_proxy_down) > 0

# Download speed over the last 5 minutes
sum(rate(singbox_traffic_download_bytes_total[5m]))

# Per-server latency, sensible for a graph
singbox_proxy_delay_ms{type!="URLTest", type!="Selector", type!="Fallback"}

# Active download traffic per selected server
singbox_connections_traffic_download_bytes
```

## Examples

Ready-made integrations live in [`examples/`](examples/):

- [`examples/alerts.yml`](examples/alerts.yml) — alerting rules for
  Prometheus/VictoriaMetrics (API down, all proxies dead, flapping selection,
  stale metrics, high memory).
- [`examples/grafana-singbox.json`](examples/grafana-singbox.json) — importable
  Grafana dashboard (classic format, works on any Grafana ≥ 8):
  selected server, API/endpoint health, throughput, per-outbound connections
  and live traffic, proxy latency + freshness, memory, poll diagnostics.
- [`examples/grafana-singbox-v2.json`](examples/grafana-singbox-v2.json) — the
  same dashboard in the declarative **v2 format** (Grafana ≥ 11.4, Dashboards
  as Code). Pick `Prometheus` as the data source when importing either file.
  Both dashboards ship two variables: `Instance` and `Group` (with *All* /
  multi-select); the `Group` variable drives the "Selected VPN server" panel,
  so it no longer hard-codes a group name.

## Development

See [CONTRIBUTING.md](CONTRIBUTING.md) for the layout, common commands
(`make build/test/vet/lint/release`), metric naming conventions and the PR
workflow. The project has no third-party dependencies on purpose — please keep
it that way.

## Companion script

`scripts/check_vpn.sh` is an optional self-healing helper for OpenWrt/FriendlyWrt hosts that
run sing-box as a SOCKS/HTTP proxy. On a schedule (cron) it:

1. probes two endpoints through the proxy (anti-false-positive),
2. only reacts after **3 consecutive failures** (anti-flap),
3. escalates: refresh subscription (`update_subscription.py`) → hard container restart,
4. backs off with 5/15/30/60 min pauses, persisting state in a temp file.

It is deployment-specific and is included as a reference rather than a generic tool.

## Compatibility

- Tested with sing-box `v1.14.3`.
- Relies only on the Clash API: `/version`, `/connections`, `/proxies` — these endpoints are
  stable across recent sing-box releases.
- Does **not** need `/traffic` (WebSocket) or `/memory`; the memory value is read from the
  `memory` field of the `/connections` payload.

## Changelog

See [CHANGELOG.md](CHANGELOG.md). The short version for the next (unreleased)
release:

- **Breaking:** `singbox_info{version,meta,premium}` → `singbox_version_info{version}`
  (`meta`/`premium` are hard-coded to `true` by sing-box).
- **Fix:** `direct`/`block`/`dns`/endpoint outbounds are no longer flagged dead
  by `singbox_proxy_down`; a failed endpoint keeps its last successful values
  instead of dropping the series from `/metrics`.
- **New:** per-endpoint health, poll timing, in-flight traffic per outbound,
  connections by inbound/rule, latency freshness timestamp, graceful shutdown,
  `-version`, tests, CI, Docker healthcheck.

## License

[MIT](LICENSE)