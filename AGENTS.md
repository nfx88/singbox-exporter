# AGENTS.md

## Project overview

`singbox-exporter` is a Prometheus exporter for [sing-box](https://github.com/SagerNet/sing-box)
that reads its Clash API and serves metrics in the Prometheus text format. It is a single
static Go binary with **zero external dependencies** — the module `go.mod` has no `require`
lines. This is the one hard rule of the project; do not add third-party dependencies without
an explicit user request.

Reliability is designed in, not bolted on:

- Flags are validated at startup (`-interval > 0`, `-port` in `1..65535`).
- HTTP server has read/write/idle timeouts and serves only `GET`/`HEAD`.
- `store` uses `sync.RWMutex`; the scrape snapshot is sectional — a failed endpoint keeps
  its last successful values instead of dropping the series.
- Metrics are collected in parallel; traffic counters come from sing-box's own cumulative
  totals.

Besides the Go exporter, the gitignored `deploy/` scratch zone contains a Python
self-healing daemon (`singbox_healer.py`) — see the `deploy/` section below. It is **not**
part of the Go build.

## Setup commands

Go ≥ 1.27 is required. The module is pinned by `go 1.27` + `toolchain go1.27.2` in `go.mod`;
newer toolchains also work. There are no dependencies to install (`go env GOMODCACHE` may be
empty for this module).

```sh
go build ./...          # compile
go vet ./...            # static analysis
go test ./...           # run tests
go test -race ./...     # tests with the race detector
gofmt -l .              # list unformatted files
make                    # vet + test + build in one go
make release            # cross-compile static binaries into dist/
```

Optional strict linter (needs golangci-lint **v2**, see `.golangci.yml`):

```sh
go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest
golangci-lint run
```

Version is injected at build time:

```sh
go build -ldflags "-X main.version=vX.Y.Z"
```

**Always run before finishing a task — the whole set must be green:**
`gofmt -l .`, `go vet ./...`, `go test -race ./...`.

## Repository layout

| Path | Purpose |
|---|---|
| `main.go` | entrypoint, flag parsing, HTTP server, graceful shutdown |
| `client.go` | Clash API HTTP client + Bearer auth |
| `types.go` | Clash API JSON payload structs |
| `store.go` | sectional snapshots + Prometheus text rendering |
| `collect.go` | collection, filtering, aggregation of metrics |
| `logging.go` | leveled logger (`-log-level`, `-log-format`) + error backoff |
| `*_test.go` | unit/integration tests (faked Clash API via `net/http/httptest`) |
| `docs/clash-api.md`, `docs/clash-api.en.md` | Clash API reference used by the exporter (RU/EN) |
| `examples/` | Prometheus/VictoriaMetrics alerts + Grafana dashboards |
| `deploy/` | gitignored: `singbox_healer.py` + monitoring configs (do not commit) |
| `README.md`/`README.en.md`, `CONTRIBUTING.md`/`.en.md`, `SECURITY.md`/`.en.md` | docs (RU primary + EN copies) |

## Code style

- **stdlib only.** Never add a `require` to `go.mod`; if a third-party package seems
  unavoidable, stop and ask the user first.
- Run `gofmt -w`; keep files formatted. Do not reorder struct fields or break the
  sectional-store design without a reason.
- Metric naming conventions:
  - dots in logical names become underscores (`traffic_download_bytes_total`);
  - suffixes are used consistently: `_total` (counter), `_bytes`, `_seconds`, `_info`, `_up`;
  - the `typ` field of a sample must stay in sync with the literal kind.
- Concurrency: the `store` snapshot is protected by `RWMutex` — reads take the read lock,
  writes the write lock. Collection runs concurrently per endpoint and must not mutate shared
  state without a lock.

## Testing instructions

- Add tests for new behavior even if nobody asked.
- `collect_test.go` drives `httptest.Server` + a `fakeAPI` with realistic Clash API payloads
  (GLOBAL, groups, direct/endpoint entries, history, connections). When you change the
  collector, update the corresponding assertions there.
- Unit parts: `store_test.go` (rendering/escaping/sections), `logging_test.go` (levels,
  formats, backoff), `main_test.go` (flag validation: `-interval > 0`, `-port` bounds,
  trailing-slash trimming).
- Python healer tests (see `deploy/`): `python3 -m py_compile deploy/singbox_healer.py` and
  `python3 deploy/singbox_healer_test.py` (39 unit tests, no docker/VM needed).

## Adding a metric

1. Verify the field against the sing-box source (`experimental/clashapi`, `testing` branch) —
   take the semantics from the code, not from memory.
2. Add the payload field to `types.go` if it is not parsed yet.
3. Emit the sample in the matching collector in `collect.go` (connections/proxies/version).
4. Follow the metric naming conventions above.
5. Update the metric table in `README.md`/`README.en.md`, `examples/`, and
   `CHANGELOG.md`/`CHANGELOG.en.md` (Unreleased section).

Do not change the semantics (name/type/labels) of existing metrics without discussion —
that breaks dashboards and alerts.

## Clash API facts (short)

- `/proxies` returns **every** tagged outbound including `direct`/`block`/`dns`/endpoints.
  `singbox_proxy_down`/`delay` are emitted **only** for the real-proxy allowlist
  (`proxyTypeSet` in `collect.go`) and groups — direct/endpoints must never be flagged dead.
- `chains[0]` is the final (real) outbound; `by_outbound` grouping keys on it.
- `/connections` `uploadTotal`/`downloadTotal` are cumulative since sing-box start.
- `history[0].time` → `singbox_proxy_delay_timestamp_seconds` +
  `singbox_proxy_delay_age_seconds` (freshness; `StaleProxyHealth` alert). Stale latency
  by itself still yields `singbox_proxy_down 0` — by design, freshness is tracked separately.
- Full reference: `docs/clash-api.md`.

## deploy/ — Python healer (not part of the Go build)

- Gitignored (`/deploy/` in `.gitignore`) — local deployment artifacts, never commit them.
- `singbox_healer.py` — self-healing daemon: polls VictoriaMetrics (PromQL), on VPN failure
  refreshes the subscription, latency-tests candidates, restarts sing-box.
- Python 3, stdlib only. Checks: `python3 -m py_compile deploy/singbox_healer.py`,
  `python3 deploy/singbox_healer_test.py` (39 unit tests, no docker/VM).
- Subscription parsing: `vless://`, `vmess://` (base64 JSON + legacy), `trojan://`,
  `ss://` (sip002 + legacy); `parse_link` dispatcher, tag dedup, `IGNORE_KEYWORDS`.
- Tri-state "unknown": if the VM is down AND the curl probe is unavailable, the cycle is
  skipped without touching strikes (no fail-open, no blind healing).
- Secrets: the subscription URL in errors/logs must go through `redact_url`/`sanitize_error`
  (query/token stripped). Do not break this when editing.
- `PortPool` must not grow beyond `TEST_PORT_SPAN` when exhausted (unbounded growth is a
  regression).
- Subcommands: `run`, `update`, `check`; flags `--dry-run`, `--env`, `--verbose`.
- Config via env / `.env` (`SUBSCRIPTION_URL`, `VM_URL`, `INSTANCE`, ...).
- Details: `deploy/README.md`.

## Security considerations

- Never log or commit secrets: tokens, subscription UUIDs, keys, `.env`.
- Threat model: exporter runs on the same host as sing-box, loopback only. Recommended:
  Clash API bound to `127.0.0.1` with a `secret`; exporter passes `-token`; `/metrics` not
  exposed to untrusted networks.
- Label values are escaped in the renderer — that is not a reason to expose `/metrics`
  publicly.
- The exporter is strictly read-only against the Clash API; it never mutates sing-box state.

## Commits and PRs

- Short imperative subject; add a "why" paragraph when it helps.
- Reference issues: `Fixes #12`.
- For user-visible changes update `README.md`/`README.en.md`,
  `CHANGELOG.md`/`CHANGELOG.en.md` (Keep a Changelog, Unreleased section) and `examples/`
  where relevant. Documentation is bilingual: Russian primary (`*.md`), English copies
  (`*.en.md`) — keep both languages in sync.
- PR checklist lives in `.github/PULL_REQUEST_TEMPLATE.md`; details in `CONTRIBUTING.md`.
- Commit only when the user asks; never push without an explicit request.

## Docs

- Russian primary: `README.md`, `CHANGELOG.md`, `CONTRIBUTING.md`, `SECURITY.md`,
  `CODE_OF_CONDUCT.md`, `docs/clash-api.md`.
- English copies in `*.en.md`: `README.en.md`, `CHANGELOG.en.md`, `CONTRIBUTING.en.md`,
  `SECURITY.en.md`, `CODE_OF_CONDUCT.en.md`, `docs/clash-api.en.md`. When editing a
  document, update both versions.

## Authorship / AI-generated code notice

**English:**

- The architecture of this project was designed by a human.
- The source code and tests were generated by an AI agent and reviewed by a human.
- The software is provided "as is" (AS IS) under the [MIT license](LICENSE), without
  warranty of any kind.
- Review the code independently before using it in production (see `SECURITY.md`).

**Русский:**

- Архитектура проекта спроектирована человеком.
- Исходный код и тесты сгенерированы ИИ-агентом и вычитаны человеком.
- Программное обеспечение предоставляется «как есть» (AS IS) по
  [MIT-лицензии](LICENSE), без каких-либо гарантий.
- Перед использованием в продакшене выполните независимую проверку кода (см.
  `SECURITY.md`).
