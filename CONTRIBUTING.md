# Contributing

Thanks for taking the time to contribute! This file explains how to work on
`singbox-exporter` efficiently. It is a small, focused project, so we favour
few simple files over a large structure.

## Repository layout

| Path | Purpose |
|---|---|
| `main.go` | entrypoint, flags, HTTP server, graceful shutdown |
| `client.go` | sing-box Clash API HTTP client and authentication |
| `types.go` | Clash API JSON payload types |
| `store.go` | Prometheus text-format store and rendering |
| `collect.go` | metric collection, filtering and aggregation |
| `logging.go` | leveled logger (`-log-level`, `-log-format`) and failure backoff |
| `*_test.go` | unit and end-to-end tests (faked Clash API via `net/http/httptest`) |

## Environment

- Go ≥ 1.22 (the module targets `go 1.25`; any newer toolchain works too).
- No third-party Go dependencies — the project intentionally stays **stdlib only**.
  Please keep it that way unless there is a very strong reason not to.
- `make` is optional but handy for the common tasks below.

## Common commands

```sh
go build ./...          # compile
go vet ./...            # static checks
go test ./...           # run tests
go test -race ./...     # run tests with the race detector
gofmt -l .              # list unformatted files
golangci-lint run       # optional, see .golangci.yml
make                    # vet + test + build in one go
make release            # cross-compile static binaries into dist/
```

## Adding a metric

1. Read the relevant sing-box Clash API handler in the
   [sing-box source](https://github.com/SagerNet/sing-box/tree/testing/experimental/clashapi)
   so the field names and semantics are taken from the implementation, not from memory.
2. Add the payload field to `types.go` if it is not parsed yet.
3. Emit the sample in the matching collector in `collect.go` (connections, proxies
   or version), or a new collector if it needs a new endpoint.
4. Follow the naming guidelines:
   - dots in the logical name become underscores (`traffic_download_bytes_total`);
   - suffixes `_total` (counter), `_bytes`, `_seconds`, `_info`, `_up` are used
     consistently;
   - kind is stored literally in the `sample` — keep `typ` in sync.
5. Update `collect_test.go` with a realistic payload assertion and document the
   metric in `README.md` and the examples in `examples/`.
6. Add a line to `CHANGELOG.md` under *Unreleased*.

## Pull requests

- Keep changes small and focused; split unrelated work into separate PRs.
- Make sure `gofmt`, `go vet ./...`, `go test ./...` and (if available)
  `golangci-lint run` pass locally — CI enforces the first three.
- Reference the issue your PR fixes, e.g. `Fixes #12`.
- Write clear commit messages: a short imperative summary, optionally followed
  by a blank line and a longer explanation of *why* the change is made.

## Commit message style

```
Fix singbox_proxy_down false positives for direct outbound

/version /proxies returns every tagged outbound including direct, block
and DNS-only entries, which have no URL-test history. Only real proxy
types are now considered for delay/down metrics.
```

## Issue triage

- **Bugs**: include the exporter flags, sing-box version and, if possible,
  the raw response of one Clash API endpoint (`curl -s <api>/proxies`).
- **Feature requests**: describe the metric you want, the query you would run
  against it and your sing-box configuration shape.

## Code of conduct

All contributors are expected to follow the
[Code of Conduct](CODE_OF_CONDUCT.md).