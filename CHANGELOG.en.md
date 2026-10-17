# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## Unreleased

### Added

- Logging: per-poll debug tracing (`api request` with `status`/`bytes`/`duration`,
  `collect endpoint` with `samples`/`error`, `poll complete` summary), an `endpoint
  recovered` event when a failed endpoint comes back, `sing-box connected` on the first
  response and a `sing-box version changed` warning when the version flips (a
  sing-box restart/upgrade shows up in the logs).
- `-log-format json` now keeps the **native JSON types** of fields: numbers, booleans
  and durations (seconds as float) are no longer flattened into strings — log shippers
  can filter and aggregate on them; the text format interface is unchanged.

## [v0.1.0] - 2026-10-17

### Breaking

- `singbox_info{version,meta,premium}` is replaced by `singbox_version_info{version}`.
  sing-box always reports `meta`/`premium` as `true`, so the labels carried no
  information. Rename the metric in existing dashboards — nothing else needs to change.

### Fixed

- `direct`, `block`/`reject`, `dns` and endpoint outbounds (WireGuard, Tailscale, ...)
  are no longer reported as dead by `singbox_proxy_down` — only real VPN server
  proxies carry delay/deadliness metrics now.
- A failed endpoint no longer drops its metric families from `/metrics`: the last
  successful values of each section are kept and failures surface in
  `singbox_endpoint_up`, `singbox_scrape_errors_total` and `singbox_up`.
- `/healthz` uses the internal availability flag instead of matching rendered text.
- Large byte counters are rendered in plain decimal notation (no exponent).
- `-interval <= 0` is now rejected during flag parsing (previously `time.NewTicker`
  panicked); `-port` is validated to `1..65535`.
- `/metrics` and `/healthz` accept `GET`/`HEAD` only (`405` otherwise); the HTTP server
  gained `ReadHeaderTimeout`/`ReadTimeout`/`WriteTimeout`/`IdleTimeout`.
- `singbox_memory_bytes` is always emitted (value `0` when `Memory == 0`), the series
  no longer disappears from the output.

### Added

- `singbox_endpoint_up` — per-endpoint reachability gauge.
- `singbox_scrape_duration_seconds`, `singbox_scrape_timestamp_seconds`,
  `singbox_last_success_timestamp_seconds` — poll timing observability.
- `singbox_connections_by_inbound`, `singbox_connections_by_rule` — active
  connection distribution by inbound and matched rule.
- `singbox_connections_traffic_upload_bytes`,
  `singbox_connections_traffic_download_bytes` — in-flight traffic per outbound.
- `singbox_proxy_delay_timestamp_seconds` and `singbox_proxy_delay_age_seconds` —
  freshness of the last URL test; new `StaleProxyHealth` alert.
- `-version` flag and build-time version injection via
  `-ldflags "-X main.version=..."`.
- `-log-level` (debug/info/warn/error) and `-log-format` (text/json) flags;
  structured JSON log lines with `ts`/`level`/`msg`/`endpoint`/`error` fields.
- Exponential backoff for repeated `collect failed` log lines (first failure
  immediately, then 1m/2m/4m/... capped at 30m per endpoint) — metrics keep
  counting every failure regardless.
- Dashboard examples reworked: `Group` variable instead of a hard-coded group
  name, `endpoint_up`, in-flight traffic per outbound, latency freshness and
  last-success panels, `increase()`-based traffic windows, regex-escaped
  instance filtering; added a declarative v2 variant
  (`examples/grafana-singbox-v2.json`, Grafana ≥ 11.4).
- Unit/integration tests, Docker `HEALTHCHECK`.
- Multi-stage `Dockerfile` — the image builds itself, no pre-built binary required.

### Changed

- Clash API endpoints are polled in parallel; the poll loop uses a ticker instead
  of a fixed sleep.
- Graceful shutdown on SIGINT/SIGTERM.
- `parseFlags` moved to `flag.NewFlagSet` with injected args — unit tests now cover
  flag validation and trailing-slash trimming.
- `store` uses `sync.RWMutex`: rendering `/metrics` takes only a read lock.
- Go toolchain upgraded 1.25 → 1.27 (`go.mod`: `go 1.27` + `toolchain go1.27.2`;
  `Dockerfile` build stage is `golang:1.27-alpine`).

### Removed

- The obsolete `scripts/check_vpn.sh` is dropped — its role is fulfilled by the
  self-healing daemon `singbox_healer.py` (in the gitignored `deploy/`, not part of
  the release).

### Alerts (`examples/alerts.yml`)

- `SingboxAllProxiesDead` and `SingboxManyProxiesDead` now group by `instance` —
  the `{{ $labels.instance }}` placeholder in notifications is populated.
- Added `StaleProxyHealth` (latency freshness).
