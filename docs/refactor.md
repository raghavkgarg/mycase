# Mycase Refactor — Progress Tracker

**Branch**: `feature/mycase-changes`  
**Go version**: 1.26.3  
See `docs/architecture.md` for design details, CLI structure, directory layout, and decisions.

---

## Completed

| Phase | What was done | Commit(s) |
|-------|--------------|-----------|
| **R1** — CLI Unification | 9 separate `cmd/*/main.go` binaries replaced with single `mycase` binary (10 subcommands) using `urfave/cli/v3`. `pipeline.go` calls all steps as direct Go function calls — no `os/exec`. | `aef5489`, `bd3c4c0`, `3c51409` |
| **D1** — Module rename | `module mycase` → `module github.com/raghavkgarg/mycase`. All 29 source files, Makefile, and tests updated. | `aef5489`, `782a663` |
| **R3 infra** — Go + deps | `go 1.24.4` → `go 1.26.3`; `gocsv` updated from 2018 pin to latest. | `bd3c4c0` |
| **R3 language** — Go 1.26 idioms | `slices.SortFunc` replaces `sort.Interface` boilerplate (`ByPnLPct`); `max()` builtin replaces `math.Max` in printer; `math/rand/v2` replaces `math/rand` in monitor mock generator. | _current_ |
| **Makefile** | Targets: build, install, cross-compile (linux/darwin arm64/amd64), run, test, test-verbose, test-race, test-integration, test-coverage, cleanup, clean, help. LDFLAGS inject Version/GitCommit/BuildDate. | `0ad3a25`, `782a663` |

---

## Remaining

### P1 — Next up

**Tests (pre-R2 gate)** — establish coverage baseline before R2 changes logic
- `pkg/csvloader`: fuzz targets, merge/combine table tests
- `pkg/optimizer`: `capWeights` tests (currently zero coverage), property tests
- `pkg/monitoring`: mock determinism, simulator edge cases
- `pkg/yfinance`: all tests (zero today) — needs `httptest` mock server and baseURL override
- `pkg/config`: all tests (zero today)
- `cmd/`: CLI integration tests (zero today) — flag parsing, file I/O with `t.TempDir()`

**R2 — Code cleanup / logic extraction** (Medium, 2–3d)
- R2.1: Extract mock data generator from `cmd/monitor.go` → `pkg/monitoring/mock.go`
- R2.2: Extract portfolio valuation from `cmd/performance.go` → `pkg/performance/`
- R2.3: Extract heuristic text generation from `cmd/report.go` → `pkg/report/`
- R2.4: Complete rebalancing band diff move from `cmd/optimize.go` → `pkg/optimizer/rebalance.go`
- R2.5: Move `PipelineConfig` + `resolveFirst[T]()` from `cmd/pipeline.go` → `pkg/config/pipeline.go`

**R3.5 — context.Context in HTTP** (Small)
- Add `ctx context.Context` to all `pkg/yfinance` fetch functions; use `http.NewRequestWithContext`

### P1 — Planned

**R-cache — DuckDB price/fundamentals cache** (Medium, 2d)
- Add `github.com/duckdb/duckdb-go/v2`; implement `pkg/cache/` (db.go, prices.go, fundamentals.go)
- Wire cache into yfinance fetch functions (transparent cache-check-first)
- Add `mycase cache status/clear` subcommand

### P2

**R4 — Broker abstraction** (Medium, 2d)
- Define `pkg/broker/broker.go` interface; move Kite logic to `pkg/broker/zerodha/`
- Wire `cmd/basket.go` and `cmd/holdings.go` to `broker.Broker`

**R5 — Drift monitoring daemon** (Large, 4–6d)
- `pkg/alert/` — Alerter interface, Telegram, Discord webhook implementations
- `pkg/daemon/` — drift calculation engine, daemon runner with launchd/systemd integration
- `mycase daemon start/stop/status/check` subcommand

### P3+

**R6 — Tax & transaction cost awareness** (Medium, 2–3d)  
**R7 — Historical backtesting engine** (XL, 8–12d)  
**R8 — Web dashboard** (XL, 10–15d)
