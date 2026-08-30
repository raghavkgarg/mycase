# Mycase Refactor Plan

**Branch**: `feature/mycase-changes`
**Go version**: 1.27.0

This document tracks **work still to be done**. For anything already shipped:

- **System design, algorithms, data flow** → `docs/architecture.md` (incl. design decisions D1–D12)
- **Test coverage, conventions, gaps** → `docs/testing.md` (created by R15)
- **Operator/CLI usage** → `docs/runbook.md`
- **Phase status & roadmap** → `docs/roadmap.md`

A one-line ledger of completed refactor phases is kept at the bottom for git-archaeology; the durable details of each now live in the docs above.

---

## Active Work

| Phase | What | Status | Depends on |
|-------|------|--------|-----------|
| R14 | Structured logging (slog) | 🟡 R14.1+R14.2 done; R14.3–R14.7 pending | none |
| Phase 5a | Live perf attribution — NAV foundation + CLI | ⬜ Next | R14 (slog-native) |
| Phase 5b | Live perf attribution — decomposition + dashboard | ⬜ | Phase 5a |
| R15 | Test strategy & E2E testing | ⬜ Design | none (R14 recommended first) |

**R14 progress**: `pkg/logging` package (fanout handler, req_id tracing, timing/HTTP/DB helpers, rotation) + `main.go` wiring (global flags `--log-level`/`--log-dir`/`--quiet`/`--verbose`, `Before`/`After` hooks, `slog.SetDefault`) + `config/defaults.json` `logging` block are **done and verified** (92.6% coverage, clean build/vet/staticcheck, stdout stays clean). Remaining: R14.3 (Phase 5 code written slog-native — folded into Phase 5a below), R14.4–R14.7 (incremental `fmt`→slog migration of existing packages), and the `.kiro/steering/logging.md` conventions file.

---

## Phase R14 — Structured Logging (slog) — DESIGN

**Status**: ⬜ Design — not yet implemented
**Motivation**: The codebase has **no structured logging**. All diagnostic output is ad-hoc `fmt.Print*`/`fmt.Fprint*` to stdout/stderr (verified: zero imports of `log/slog`, `log`, `logrus`, or `zap` across 112 source files). Operational events — daemon lifecycle, Schwab API errors, cache warnings, pipeline stage transitions — are indistinguishable from user-facing CLI output and cannot be filtered, leveled, traced, or persisted. The API steering rules mandate "Log API errors, don't panic" but there is no logging primitive to do so consistently.

Phase 5 (Live Performance Attribution) introduces a **background NAV tracker** and **alert nudges** that need proper leveled, persistable logging. Rather than bolt logging onto that one package, R14 standardizes the pattern for the whole codebase now.

### Design principle: separate *user output* from *logs*

This is the crux. Two distinct channels that must not be conflated:

| Channel | Purpose | Mechanism | Example |
|---------|---------|-----------|---------|
| **User output** | Deliberate CLI results the investor reads | `pkg/render` (tables, KV, sections) + direct `fmt` to stdout | Holdings table, harvest candidates, pipeline diff |
| **Logs** | Diagnostic/operational trace | `slog` → stderr (text) + file (JSON) | "fetched 47 quotes in 320ms", "Schwab 401, refreshing token", "drift check fired" |

**Rule**: `pkg/render` output and command "results" stay on stdout via `fmt`/`render` — they are the product. Everything diagnostic moves to `slog`. A user piping `mycase holdings` into a script must still get clean tabular stdout; logs go to stderr/file and never pollute it.

### Reference implementation

Adapt `~/Projects/gomod/jira-task-manager/internal/logging/logging.go` (the most mature sibling pattern). It provides, and we will port:

- **Fanout handler** — JSON to a daily rotating file (machine-readable) + human-readable text to stderr, with per-channel level gating.
- **req_id tracing** — every command invocation gets a unique id (`command-HHMMSS`) attached to `context.Context`; sub-operations (API calls, DB ops, fetch batches) log with the parent req_id for end-to-end tracing across a run.
- **Timing helpers** — `defer logging.Timer(log, "pick.score")()` emits duration.
- **HTTP/DB log helpers** — `LogRequest`/`LogResponse` (status→level mapping: ≥400 warn, ≥500 error), `LogDBOp`.
- **Log rotation** — one `mycase-YYYY-MM-DD.jsonl` per day, auto-clean after N days.

Simpler siblings (`eia-api-explorer`, `patscape`) use a plain `slog.New(MultiWriter)` — insufficient here because we need the stdout/stderr split and req_id tracing.

### Package layout

```
pkg/logging/
├── logging.go      # Setup, SetupFile, Config, Logger wrapper, fanout handler
├── context.go      # WithReqID, ReqID, GenerateReqID, With(ctx) child logger
├── helpers.go      # Timer, LogRequest, LogResponse, LogDBOp
└── logging_test.go # handler level gating, req_id propagation, JSON shape, rotation cleanup
```

Placed in `pkg/` (not `internal/`) for consistency with the existing flat `pkg/` layout; all mycase code is one module so `internal/` buys nothing here.

### Configuration

Logging config lives in `config/defaults.json` (alongside broker selection) and is overridable by env + global CLI flags:

```json
{
  "logging": {
    "dir": "data/logs",
    "level": "info",
    "file": true,
    "retain_days": 14
  }
}
```

- Global flags on the root `cli.Command`: `--log-level {debug|info|warn|error}`, `--log-dir`, `--quiet` (stderr off, file only), `--verbose` (= `--log-level debug`).
- Env overrides: `MYCASE_LOG_LEVEL`, `MYCASE_LOG_DIR`.
- Precedence: flag > env > config file > default (`info`, `data/logs`, file on, 14-day retain).

### Wiring into the CLI

`main.go` currently builds the `cli.Command` and runs it. R14 adds a `Before` hook on the root command:

```go
app := &cli.Command{
    Name: "mycase",
    Flags: []cli.Flag{ /* --log-level, --log-dir, --quiet, --verbose */ },
    Before: func(ctx context.Context, c *cli.Command) (context.Context, error) {
        logger := logging.SetupFile(logging.ConfigFromFlags(c))
        reqID := logging.GenerateReqID(commandName(c))
        ctx = logging.WithReqID(ctx, reqID)
        slog.SetDefault(logger.Logger)       // package-level slog.Info(...) works everywhere
        logging.CleanOldLogs(cfg.Dir, cfg.RetainDays)
        return ctx, nil
    },
    After: func(ctx context.Context, c *cli.Command) error { /* logger.Close() */ },
    Commands: []*cli.Command{ /* ... */ },
}
```

Setting `slog.SetDefault` means existing packages can call `slog.InfoContext(ctx, ...)` without threading a `*Logger` everywhere. Hot paths (daemon, fetchers) take an explicit `*slog.Logger` for testability.

### Migration strategy — incremental, non-breaking

The migration is **classify-then-convert**, not a blind find-replace. Each `fmt.Print*` call site is one of:

1. **User-facing result** → leave as-is (or migrate to `pkg/render`). *No change.*
2. **Diagnostic/operational** ("Warning: ...", daemon status, API retry) → convert to `slog` at the appropriate level.
3. **Progress/status** (e.g., "🔐 Opening browser...") → `slog.Info` if operational, keep as stdout if genuinely interactive UX.

Rollout order (each independently shippable, tests stay green throughout):

| Step | Scope | Why first |
|------|-------|-----------|
| R14.1 | `pkg/logging` package + tests | Foundation |
| R14.2 | Wire `main.go` Before/After, global flags, `slog.SetDefault` | Enables everything downstream |
| R14.3 | **New Phase 5 code uses slog from day one** | No new debt; proves the pattern |
| R14.4 | `pkg/daemon`, `pkg/alert` — background/operational, highest logging value | Long-running, needs trace |
| R14.5 | `pkg/broker/schwab`, `pkg/datafetcher`, `pkg/yfinance` — API layer | req_id + status→level shine here |
| R14.6 | `pkg/autopilot`, `pkg/cache`, `pkg/csvloader` warnings | Operational warnings |
| R14.7 | `cmd/*` — classify each call site (result vs diagnostic) | Most nuanced, do last |

### Conventions (to be codified in steering)

- **Levels**: `Debug` (per-item fetch/score internals), `Info` (stage transitions, counts, durations), `Warn` (recoverable — skipped ticker, cache miss fallback, API retry), `Error` (operation failed but process continues — never `Fatal`; commands return `error` to `cli`).
- **Never `slog` a full API response or credentials.** Log counts, symbols, status codes, durations. Reference secrets by key name.
- **Structured attrs, not formatted strings**: `slog.Info("fetched quotes", "count", n, "ms", d.Milliseconds())` not `slog.Info(fmt.Sprintf("fetched %d quotes", n))`.
- **Always thread `ctx`** so req_id attaches: `slog.InfoContext(ctx, ...)`.
- The steering rule "Log API errors, don't panic" becomes enforceable: schwab/datafetcher error paths use `logging.LogResponse` / `slog.WarnContext`.

### Non-goals

- Not adopting a third-party logger (zap/logrus) — stdlib `slog` is sufficient and matches the "minimal deps" project constraint.
- Not adding distributed tracing / OpenTelemetry — single-binary local tool.
- Not rewriting `pkg/render` — it stays the user-output layer; logging is orthogonal.

### Deliverables

- `pkg/logging/` package + tests (target 85%+, it's pure logic).
- `main.go` global flags + Before/After wiring.
- `config/defaults.json` `logging` block + `pkg/config` struct.
- Incremental conversion R14.4–R14.7 (tracked as sub-commits).
- Steering file `.kiro/steering/logging.md` codifying the conventions above.
- New Phase 5 attribution code written slog-native from the start.

---

---

## Phase 5 — Live Performance Attribution — DESIGN

**Status**: ⬜ Phase 5a next (Phase 5b follows)
**Roadmap**: this is roadmap Phase 5 ("know if the system works"). Prerequisites (Phase 3 US factor, Phase 4 TLH, Phase 7 DuckDB) are all ✅.

**Goal**: continuously track the live portfolio's NAV against a passive benchmark and decompose returns into their sources, so "I think I'm beating the market" becomes "beating SPY by X% annualized with an information ratio of Y".

### Terrain (what already exists — reuse, don't rebuild)

Investigated via context-gatherer. Key findings:

- **`pkg/backtest` already builds a daily NAV series.** `backtest.Run(...)` produces `[]DailySnapshot{Date, PortfolioValue, BenchmarkValue}` and computes CAGR/Sharpe/Sortino/Calmar/Beta/Alpha/MaxDrawdown. The `Calc*` functions in `metrics.go` are standalone and operate on any `[]float64` NAV series — directly reusable for attribution.
- **`datafetcher.Router`** (`FetchHistoricalByDateRange`, `FetchQuotes`) routes US tickers → Schwab, else Yahoo. This is the correct seam for sourcing price series.
- **Live holdings** come from `broker.Broker.GetHoldings()` (`Quantity int`, `AveragePrice`, `LastPrice`, `PnL`); live prices from `GetQuotes`.
- **Target weights** load via `csvloader.LoadBasketCSV` → `map[ticker]weight` + ordered tickers.
- **Benchmark** is `broker.MarketConfig.Benchmark` = `^GSPC` for US (S&P 500 index, Yahoo-sourced).
- **DuckDB** uses one additive `ddl` const (`CREATE TABLE IF NOT EXISTS`), BIGINT-Unix timestamps, and the `pkg/cache/tax.go` Insert/Get/Replace pattern.
- **Server** adds a portfolio-scoped tab in 6 touchpoints (handler + route + 4 frontend edits), mirroring the Tax tab.

**Gotchas to design around** (all confirmed):
1. `indiaRiskFreeRate = 0.06` is hardcoded in `metrics.go` Sharpe/Sortino/Alpha. → add RF-parameterized variants.
2. `backtest` engine + `ValuatePortfolio` hardcode `Asia/Kolkata`. → attribution parameterizes timezone (America/New_York for US).
3. `ValuatePortfolio` and `cmd/backtest.go` bypass the Router (call `yfinance` directly). → attribution routes through the Router so US tickers hit Schwab.
4. `broker.Holding.Quantity` is `int` (fractional shares truncated) and `LastPrice` is derived (`MarketValue/qty`). → prefer fresh closes from the Router for NAV.

### Benchmark decision: `US:SPY` (the ETF), not `^GSPC`

The roadmap says "SPY". The existing backtest uses `^GSPC` (the index). Phase 5 uses **`US:SPY`** — the actual ETF:
- It is the honest "do-nothing" baseline the roadmap describes: you can buy SPY, you cannot buy `^GSPC`. It bakes in the ETF's small expense drag and dividend timing — the real alternative.
- It routes through Schwab via the Router (consistent with US data path), exercising that seam for the benchmark too.
- Configurable: the tracker takes a benchmark ticker; `US:SPY` is the default for US, overridable.

### Scope split

**Phase 5a — NAV foundation + CLI (this slice).** Delivers the measurable core: a persisted daily NAV series and alpha / information-ratio vs SPY. Self-contained, testable to `pkg/tax` standard.

**Phase 5b — decomposition + dashboard (follow-up).** Return decomposition (selection / rebalancing / tax effects) needs rebalance-event history and a counterfactual buy-and-hold baseline — more involved, and benefits from 5a's NAV store existing. Plus the dashboard tab and the negative-trailing-alpha alert nudge.

---

### Phase 5a deliverables

**1. RF-parameterized metrics (`pkg/backtest/metrics.go`)** — non-breaking additions:
```go
func CalcSharpeRF(nav []float64, riskFree float64) float64
func CalcSortinoRF(nav []float64, riskFree float64) float64
func CalcAlphaRF(portCAGR, benchCAGR, beta, riskFree float64) float64
```
Existing `CalcSharpe`/`CalcSortino`/`CalcAlpha` delegate to these with `indiaRiskFreeRate`, so backtest behavior and tests are unchanged. US attribution passes a US risk-free (~4–5%, configurable).

**2. `pkg/attribution` package** (broker-agnostic, unit-tested, slog-native):
- `Tracker` — holds a `*datafetcher.Router`, benchmark ticker, risk-free rate, timezone, and a `*slog.Logger`.
- `BuildNAVSeries(ctx, holdings []Holding, cfg) ([]NAVPoint, error)` — daily NAV for the portfolio and benchmark over `[from,to]`, sourced through the Router (US→Schwab), on the intersection of trading days (same alignment logic as the backtest engine, but timezone-parameterized). Reuses the backtest engine where practical; where the IST hardcoding blocks reuse, a thin NAV builder is added rather than mutating the engine.
- `Attribution(nav []NAVPoint) Result` — computes vs-benchmark metrics: total/annualized return, alpha, beta, **information ratio** (mean active return / tracking error, annualized), tracking error, up/down capture. Reuses `CalcCAGR`/`CalcBeta`/`CalcAlphaRF`/`CalcMaxDrawdown`.
- `NAVPoint{Date time.Time; PortfolioValue, BenchmarkValue float64}` — the persisted unit.
- All operational events logged: `slog.InfoContext(ctx, "nav.built", "days", n, "from", ..., "to", ...)`, per-ticker fetch failures as `Warn` (skip, don't abort — API discipline rule).

**3. `pkg/cache` — `nav_history` table + `nav.go`**:
- Append to `ddl` const in `db.go`: `nav_history(portfolio TEXT, ts BIGINT, nav DOUBLE, benchmark DOUBLE, PRIMARY KEY(portfolio, ts))` — append-only source-of-truth series → idempotent `ON CONFLICT (portfolio, ts) DO UPDATE`.
- `pkg/cache/nav.go`: `InsertNAVPoints(ctx, portfolio string, points []attribution.NAVPoint) error` and `GetNAVHistory(ctx, portfolio string, since time.Time) ([]attribution.NAVPoint, error)` — following the `tax.go` tx / optional-`since`-filter patterns, BIGINT-Unix timestamps.

**4. CLI — `mycase performance --vs-benchmark`**:
- Extend the existing `PerformanceCommand` with a `--vs-benchmark` flag (and `--benchmark` override, `--since`).
- When set: build the NAV series via the Tracker, print cumulative alpha, information ratio, tracking error, beta, and portfolio-vs-SPY total return, and persist the series to `nav_history`.
- Existing single-purchase valuation behavior is unchanged when the flag is absent (backward-compatible).
- slog-native: req_id-tagged `nav.built` / `nav.persisted` events; user-facing table stays on stdout via existing print path.

**5. Tests** (target ~90%, `pkg/tax` standard):
- `pkg/attribution`: NAV alignment (intersection of trading days), metric correctness on fixture series with known answers (hand-computed alpha/IR/beta), per-ticker fetch-failure skip behavior, timezone handling, empty/degenerate inputs. Use a mock `Router`-satisfying fetcher returning fixture `HistoricalData` — no network.
- `pkg/backtest`: RF-variant equivalence (`CalcSharpeRF(x, 0.06) == CalcSharpe(x)`), US-RF sanity.
- `pkg/cache`: `nav_history` round-trip, upsert idempotency, `since` filter.

### Phase 5b deliverables (deferred)

- **Return decomposition** — selection effect (did factor picks beat SPY?), rebalancing effect (vs buy-and-hold counterfactual), tax effect (TLH savings vs no-TLH baseline). Needs rebalance-event history (from `pipeline_runs`/proposals) and counterfactual NAV series.
- **Dashboard performance tab** — `pkg/server/performance_handler.go` (`GET /api/portfolio/{name}/performance`, nil-cache-guard + `available` flag, mirroring `tax_handler.go`) + `performance-tab.js` (equity curve overlaid with SPY, rolling 1Y alpha; reuse existing `equity-curve.js` echarts pattern) + the 4 frontend wiring edits (`index.html`, `api.js`, `app.js`).
- **Alert nudge** — if trailing-12-month alpha is significantly negative, dispatch a "review your strategy" alert via the existing `pkg/alert` `Alerter` interface (feeds the roadmap's "when to simplify to index funds" failure conditions).

### Non-goals (Phase 5)

- No intraday NAV — daily close granularity only (quarterly rebalance system; intraday is noise).
- No performance persistence beyond the NAV series in 5a — decomposition tables come in 5b.
- Not mutating the `backtest` engine's IST/RF hardcoding in place — attribution parameterizes its own copy of the alignment logic to avoid destabilizing backtest tests. (A future refactor could unify them; out of scope here.)

---

## Phase R15 — Test Strategy & End-to-End Testing — DESIGN

**Status**: ⬜ Design — not yet implemented
**Motivation**: Coverage is strongly bimodal (measured `go test ./... -cover`):

| Tier | Packages | Coverage |
|------|----------|----------|
| **Well-tested (pure logic)** | `costs` 95%, `tax` 93%, `render` 86%, `cache` 81%, `backtest` 70% | ✅ |
| **Partial** | `monitoring` 58%, `printer` 59%, `config` 47%, `schwab` 43%, `executor` 37% | ⚠️ |
| **Weak / zero** | `stockpicker` **9.5%**, `csvloader` 17%, `datafetcher` 21%, `yfinance` 22%, `optimizer` 27% | ❌ |
| **Untested (0%)** | `server`, `autopilot`, `selectiontracker`, `market`, `excel`, `alert`, `broker` iface, `zerodha` | ❌ |

Two structural gaps:
1. **`pkg/stockpicker` at 9.5%** — this is Layer 2 of the architecture (the scoring engine). The core selection logic is effectively untested.
2. **No end-to-end tests.** The system is a pipeline of commands (`pick → optimize → basket`, `autopilot run`, `tax import → status → harvest`) with data flowing through DuckDB and CSV. Unit tests never exercise a full command path. A regression in stage wiring (e.g., R13's DataFetcher injection) would pass all unit tests yet break the pipeline.

`docs/testing.md` does not yet exist — R15 creates it as the canonical test guide.

### Test pyramid for mycase

```
        ╱╲          E2E (few)         — full command via cli.Command.Run, mock broker,
       ╱  ╲                             temp DuckDB, fixture CSVs. Build tag: e2e.
      ╱────╲        Integration        — real network (Schwab/Yahoo), recorded or live.
     ╱      ╲       (some, tagged)       Build tag: integration (already exists).
    ╱────────╲      Unit (many)         — pure logic, table-driven, no I/O. Default `go test`.
   ╱__________╲
```

### Layer 1 — Unit tests (fill the gaps)

Priority order by risk × current gap:

| Package | Target | What to test |
|---------|--------|--------------|
| `stockpicker` | 9.5% → 70% | Scoring (US quality-momentum 6-factor, MFS 16-factor), hard filters, hysteresis, selection ranking. Pure functions given fixture `Fundamentals`/`HistoricalData`. This is the #1 priority. |
| `optimizer` | 27% → 70% | Sector-cap redistribution iteration, inverse-vol weights, rebalance bands, micro-transaction filter edge cases. |
| `datafetcher` | 21% → 60% | Router prefix logic (US→Schwab, NSE/BSE→Yahoo, fallback), with a mock provider. |
| `selectiontracker` | 0% → 60% | Cross-run diff, driver-metric extraction. |
| `autopilot` | 0% → 50% | Proposal model, scheduling math, alert formatting — with mock broker + mock fetcher. |

Convention (already in use, formalize in `docs/testing.md`): **table-driven tests**, fixtures under `pkg/<pkg>/testdata/`, no network in default `go test`.

### Layer 2 — Integration tests (build tag `integration`)

Already wired: `make test-integration` runs `go test -tags=integration`. Formalize what belongs here:

- Real Schwab market-data calls (requires valid token; skip with `t.Skip` if `config/schwab_token.json` absent).
- Real Yahoo fetches (network, may 429 — keep minimal, 1–2 tickers per API discipline rules).
- DuckDB against a real on-disk file (not just `:memory:`).
- **API discipline**: honor the steering rules — fetch once, save fixture, prefer recorded responses. Integration tests that hit live APIs are the *exception*, gated behind the tag and skippable.

### Layer 3 — End-to-end tests (new — build tag `e2e`) — the core of R15

The key insight: **the CLI is already fully programmatic**. `main.go` builds a `*cli.Command`; commands are exported (`PickCommand`, `PipelineCommand`, ...). An E2E test constructs the same command tree and calls `.Run(ctx, args)` in-process — no subprocess, no built binary needed.

```go
//go:build e2e

func TestE2E_PickOptimizeBasket(t *testing.T) {
    dir := t.TempDir()
    seedFixtures(t, dir)                 // CSVs + a temp DuckDB pre-seeded with prices/fundamentals
    t.Setenv("MYCASE_DATA_DIR", dir)     // (requires making data paths configurable — see below)

    broker := brokermock.New(...)        // deterministic holdings/quotes, IsMock()==true
    app := testApp(broker)               // same command tree as main.go, mock broker injected

    // pick → optimize → basket, asserting the artifact each stage produces
    run(t, app, "pick", "--index", "sp500", "--method", "us_quality_momentum", "--top", "5")
    assertProposalRows(t, dir, 5)
    run(t, app, "optimize", ...)
    assertWeightsSumTo1(t, dir)
    run(t, app, "basket", "--tax-optimize")
    assertOrdersGenerated(t, dir)
}
```

**Prerequisites this exposes** (worth doing regardless — improves testability):
- **Injectable data dir**: paths like `data/cache.db`, `data/candidates/` are currently hardcoded. Introduce a resolved base dir (env `MYCASE_DATA_DIR` or config) so E2E runs in `t.TempDir()`. Low-risk, high-value refactor.
- **Injectable broker**: `cmd/broker.go` factory already selects by config; extend so tests inject a `broker.MockBroker` (the type exists in `pkg/broker/mock.go`). Possibly via a package-level hook or a `newBroker` seam.
- **Deterministic fetch**: E2E must not hit the network. The `datafetcher.Router` (R13) is already an interface seam — a mock fetcher returning fixture data satisfies it.

E2E scenarios to cover (the critical command paths):

| Scenario | Command chain | Asserts |
|----------|---------------|---------|
| US factor pipeline | `pick → optimize → basket` | Proposal → weighted → orders; sector caps respected; weights sum to 1 |
| Autopilot run | `autopilot run` (non-interactive) | Proposal file written, alert formatted, no auto-execute |
| Tax workflow | `tax import → tax status → tax harvest` | Lots built from fixture transactions, FIFO correct, harvest candidates surfaced |
| Pipeline history/diff | `pipeline show / diff` | DuckDB run tracking round-trips |
| Cache lifecycle | `cache status → cache clear` | Staleness + clear semantics |

Makefile addition:
```makefile
test-e2e:
	@go test -tags=e2e -timeout 120s ./...
```

### CI / discipline

- `make test` (unit, no tags) — must stay green on every commit, <30s, no network.
- `make test-race` — before merging concurrency-touching changes (daemon, SSE broadcaster).
- `make test-e2e` — before merging pipeline/command-wiring changes.
- `make test-integration` — manual / pre-release (network, API budget).
- **Guard rail**: `go test ./...` before and after any change, zero regressions.

### Relationship to R14 (logging)

E2E tests double as logging-integration checks: assert that a full command run produces a well-formed `mycase-YYYY-MM-DD.jsonl` with the expected req_id-tagged events, and that **stdout stays clean** (user output only, no log lines) — enforcing the R14 stdout/stderr separation automatically.

### Deliverables

- `docs/testing.md` — canonical test guide: pyramid, conventions, fixture layout, how to run each tier, coverage targets per package.
- Unit test backfill, priority `stockpicker` first (9.5% → 70%).
- Testability seams: injectable data dir, injectable broker, mock fetcher wiring.
- `e2e` build tag + `make test-e2e` + the 5 scenario tests above.
- Formalize `integration` tag contents (skippable when creds/network absent).
- Coverage gate discussion: aim total ≥ 60%, pure-logic packages ≥ 85%.

### Non-goals

- No mandated 100% coverage — I/O glue (server handlers, browser-opening auth) is covered by E2E, not chased for unit %.
- No external test framework — stdlib `testing` + table-driven only (matches minimal-deps constraint).
- No mocking framework — hand-written mocks (`MockBroker` already exists); interfaces are the seams.

---

## Ongoing Guard Rails

- All `pkg/` packages should have tests. Run `go test ./...` before and after any change — zero regressions.
- `mfs.json` and `pipeline.yaml` config file formats must stay backward-compatible.
- `data/*.csv` golden copy files are user data — never touch them programmatically except through the guarded backup → overwrite flow already in place.

---

## Completed Phases (ledger)

Durable design/algorithm details live in `docs/architecture.md`; this is a chronological index for git archaeology.

| Phase | Summary | Commit(s) |
|-------|---------|-----------|
| **R1** | CLI unification — 9 `cmd/*/main.go` binaries → single `mycase` binary (urfave/cli/v3), in-process pipeline (no `os/exec`) | `aef5489`, `bd3c4c0`, `3c51409` |
| **D1** | Module rename `mycase` → `github.com/raghavkgarg/mycase` | `aef5489`, `782a663` |
| **R3 infra** | Go 1.24.4 → 1.26.3; gocsv updated | `bd3c4c0` |
| **R3 language** | Go 1.26 idioms — `math/rand/v2`, `slices.SortFunc`, `max()` builtin | `d019c9a` |
| **R2** | Code cleanup & logic extraction — mock gen, P&L, rationale, exit detection, `PipelineConfig` moved to `pkg/`; `cmd/` reduced to flag-parsing | `444e750`, `6a74e2d` |
| **R3.5** | `context.Context` threaded through all `pkg/yfinance` fetches + callers | `84ffab6` |
| **R-cache** | DuckDB price + fundamentals cache with smart expiry; `mycase cache status/clear` | `f8a5ff1`, `4c46376` |
| **R4** | Broker abstraction — `Broker` interface, `MockBroker`, zerodha impl | `20aaa5f` |
| **R5** | Drift monitoring daemon — `Alerter` interface, Telegram/Discord, `RunLoop`, state persistence, `mycase daemon` | `10f6d56` |
| **R6** | Tax & transaction costs (India) — `CostModel`, `ClassifySell` (Finance Act 2024), micro-transaction filter | `454e2a8` |
| **R7** | Backtesting engine — date-aligned simulation, sell-then-buy, 7 metrics; `mycase backtest` | — |
| **R8** | Web dashboard — stdlib HTTP, SSE, `//go:embed`, 5-tab frontend; `mycase serve` | — |
| **R10** | Autopilot pipeline — non-interactive quarterly run, proposal model, launchd scheduling; `mycase autopilot` | — |
| **R10.1** | Package cleanup — 25 → 21 packages (removed portfolio/kiteclient, merged report/performance) | — |
| **R9** | Schwab API integration — OAuth2, HTTP client, market data, US broker, transaction history; ticker routing; US cost model | — |
| **R11** | Broker factory & market abstraction — `MarketConfig`, `CostModelForBroker`, removed hardcoded India assumptions; `pkg/schwab` → `pkg/broker/schwab`; Go → 1.27.0 | — |
| **R12** | CLI rendering layer — `pkg/render` (tabwriter tables, formatters, TTY-aware color, panic-safe). See `docs/render.md` | — |
| **R13** | Stockpicker `DataFetcher` injection — interface seam, router wiring, `runPickWithOpts` collapsed. See architecture D10 | — |
| **Phase 4** | Tax-loss harvesting — `pkg/tax` (FIFO, TLH, sequencing), Schwab transactions, DuckDB tax tables, `mycase tax`, dashboard Tax tab. See architecture D11–D12 | — |
