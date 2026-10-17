# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## Unreleased

> No tagged releases yet. This section tracks changes on the default branch.

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

### Added

- `singbox_endpoint_up` — per-endpoint reachability gauge.
- `singbox_scrape_duration_seconds`, `singbox_scrape_timestamp_seconds`,
  `singbox_last_success_timestamp_seconds` — poll timing observability.
- `singbox_connections_by_inbound`, `singbox_connections_by_rule` — active
  connection distribution by inbound and matched rule.
- `singbox_connections_traffic_upload_bytes`,
  `singbox_connections_traffic_download_bytes` — in-flight traffic per outbound.
- `singbox_proxy_delay_timestamp_seconds` — freshness of the last URL test.
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
- Unit/integration tests, GitHub Actions CI, Docker `HEALTHCHECK`.

### Changed

- Clash API endpoints are polled in parallel; the poll loop uses a ticker instead
  of a fixed sleep.
- Graceful shutdown on SIGINT/SIGTERM.