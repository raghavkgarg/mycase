# Mycase Refactor Plan

**Branch**: `feature/mycase-changes`  
**Go version**: 1.26.5  
See `docs/architecture.md` for design details, CLI structure, directory layout, and decisions.  
See `docs/testing.md` for test coverage, conventions, and gaps.

---

## Completed Phases

| Phase | What was done | Commit(s) |
|-------|--------------|-----------|
| **R1** — CLI Unification | 9 separate `cmd/*/main.go` binaries replaced with single `mycase` binary (10 subcommands) using `urfave/cli/v3`. `pipeline.go` calls all steps as direct Go function calls — no `os/exec`. Old `cmd/*/main.go` subdirectories and `scripts/merge.go` deleted. | `aef5489`, `bd3c4c0`, `3c51409` |
| **D1** — Module rename | `module mycase` → `module github.com/raghavkgarg/mycase`. All 29 source files, Makefile, and tests updated. | `aef5489`, `782a663` |
| **R3 infra** — Go + deps | `go 1.24.4` → `go 1.26.3`; `gocsv` updated from 2018 pin to latest. | `bd3c4c0` |
| **R3 language** — Go 1.26 idioms | `math/rand/v2` replaces `math/rand`; `slices.SortFunc`/`slices.Sort` replace `sort.Interface` boilerplate; `max()` builtin replaces `math.Max` cast; `go mod tidy` promotes `urfave/cli/v3` to direct dep. | `d019c9a` |
| **Tests (partial)** + **R2.4 partial** | `CapWeights` extracted from `cmd/optimize.go` → `pkg/optimizer/cap_weights.go`. 28 new tests across `pkg/optimizer`, `pkg/monitoring`, `pkg/config`. | `6a74e2d` |
| **Cleanup** | `make cleanup` passes clean. Fixed: ST1005 error-string punctuation, SA1019 deprecated `strings.Title`, S1039 unnecessary `fmt.Sprintf`, SA5011 nil-pointer guard. Go toolchain bumped 1.26.3 → 1.26.5 (3 CVEs resolved). Full `gofmt`+`go fix` pass. | `ef20714` |
| **Tests** | Coverage baseline established. 35 → 96 top-level passing tests (110 incl. subtests). New: `pkg/yfinance` (RSI, SalesGrowth, DSO, VolumeBreakout, MapTickerToYahoo, CleanIntradayNoise), `pkg/optimizer` (math edge cases), `cmd` (parsePerfDate, cleanBasketArg, PipelineConfig.UnmarshalYAML). Extended: `pkg/csvloader`, `pkg/stockpicker`. Two production bugs caught. `go test -race ./...` clean. | `8dc4c18` |
| **R2** — Code Cleanup & Logic Extraction | R2.1: mock data gen → `pkg/monitoring/mock.go`. R2.2: P&L calc → `pkg/performance/valuation.go`. R2.3: selection rationale → `pkg/report/heuristics.go`. R2.4: exit detection → `pkg/optimizer/rebalance.go`. R2.5: `PipelineConfig` → `pkg/config/pipeline.go`. `cmd/` files reduced to flag-parsing + orchestration. | `444e750` |
| **R3.5** — context.Context | `ctx context.Context` added as first param to all `pkg/yfinance` fetch functions. `http.NewRequestWithContext` replaces `http.NewRequest` throughout. Context threaded through all callers. | `84ffab6` |
| **R-cache** — DuckDB Cache | `pkg/cache/`: schema `prices`, `fundamentals`, `cache_meta`. Staleness: prices fresh today IST; fundamentals 24h. `pkg/yfinance` checks cache → Yahoo → stores back on miss. `mycase cache status/clear` subcommand. | `f8a5ff1` |
| **R-cache tests** | 21 tests covering upsert/ON CONFLICT, primary key constraints, int64/float64 round-trip, staleness, range filtering, clear-ticker/clear-all, schema idempotency. Two DuckDB production bugs caught. | `4c46376` |
| **R4** — Broker Abstraction | `pkg/broker/broker.go`: `Broker` interface + `Holding`, `Order`, `OrderResult` types. `pkg/broker/mock.go`: `MockBroker`. `pkg/broker/zerodha/zerodha.go`: live broker with mock fallback. | `20aaa5f` |
| **R5** — Drift Monitoring Daemon | `pkg/alert/`: `Alerter` interface, `TelegramAlerter`, `DiscordAlerter`, `EmailAlerter` stub. `pkg/daemon/`: `CalculateDrift`, `RunCheck`, `RunLoop` (fires at 15:45 IST daily), `State` persisted to `data/daemon_state.json`. `mycase daemon start/stop/status/check/install/uninstall`. | `10f6d56` |
| **R6** — Tax & Transaction Costs | `pkg/costs/`: `CostModel`, `CostBreakdown`, `Calculate` (STT, stamp, DP, SEBI). `pkg/costs/tax.go`: `ClassifySell` with Finance Act 2024 rates (STCG 20%, LTCG 12.5% above ₹1.25L). `FilterMicroTransactions` in `pkg/optimizer/rebalance.go`. | `454e2a8` |
| **R7** — Backtesting Engine | `pkg/backtest/`: `types.go`, `engine.go` (date-aligned simulation, sell-then-buy rebalance with slippage), `metrics.go` (CAGR, MaxDrawdown, Sharpe, Sortino, Calmar, Beta, Alpha). `FetchHistoricalByDateRange` in `pkg/yfinance`. `mycase backtest` subcommand. | |
| **R8** — Web Dashboard | `pkg/server/`: stdlib `net/http`, 11 API endpoints, SSE broadcaster, `//go:embed static`. Frontend: 5-tab dashboard, 12 native Web Components, ECharts, ES2022 modules, dark theme. `mycase serve --port 8080 [--live]`. | |
| **Makefile** | Targets: build, install, cross-compile (linux/darwin arm64/amd64), run, test, test-verbose, test-race, test-integration, test-coverage, cleanup, clean, fetch-echarts, help. LDFLAGS inject Version/GitCommit/BuildDate. | `0ad3a25`, `782a663` |

---

## Ongoing Guard Rails

- All `pkg/` packages should have tests. Run `go test ./...` before and after any change — zero regressions.
- `mfs.json` and `pipeline.yaml` config file formats must stay backward-compatible.
- `data/*.csv` golden copy files are user data — never touch them programmatically except through the guarded backup → overwrite flow already in place.
