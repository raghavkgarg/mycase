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

**Next session theme: fix before feature.** Close the "Known Loose Ends" (L1–L8 below) and complete R16 before starting any new feature. The recurring problem is *build-it-then-never-wire-it*; the remedy for each item is **either wire it or delete it**.

| Phase | What | Status | Depends on |
|-------|------|--------|-----------|
| Loose ends | Wire-or-delete L1–L8 (~~render~~✅, ~~selections~~✅, ~~config_json~~✅, ~~final stage~~→P9, server paths, tax effect, ~~EmailAlerter~~✅, ~~skip-masking test~~✅) | 🟡 L1–L3+L7+L8 done, L4 deferred→roadmap P9; L6 next (L5 deferred to R15) | none |
| R16 | Dependency untangling (break cycle-magnet packages) | ⬜ Next | none |
| R14 | Structured logging (slog) — R14.4–R14.7 migration + steering | 🟡 R14.1+R14.2+R14.3 done | none |
| R15 | Test strategy & E2E testing | ⬜ Design | R16 (seams land there) |

**R14 progress**: `pkg/logging` package (fanout handler, req_id tracing, timing/HTTP/DB helpers, rotation) + `main.go` wiring (global flags `--log-level`/`--log-dir`/`--quiet`/`--verbose`, `Before`/`After` hooks, `slog.SetDefault`) + `config/defaults.json` `logging` block are **done and verified**. R14.3 (new Phase 5 code written slog-native) shipped with Phase 5a/5b. Remaining: R14.4–R14.7 (incremental `fmt`→slog migration of existing packages), and the `.kiro/steering/logging.md` conventions file.

**Phase 5 shipped** (5a commit `ed49161`; 5b this branch): full live performance attribution. `pkg/attribution` provides NAV series, vs-benchmark metrics (alpha/IR/beta/tracking-error vs `US:SPY`), return decomposition (selection/rebalancing/tax), and a trailing-alpha strategy-review nudge. Surfaced via `mycase performance --vs-benchmark [--decompose]`, the dashboard Performance tab, and an autopilot alert. See "Completed Phases" ledger.

---

## Phase R14 — Structured Logging (slog) — DESIGN

**Status**: 🟡 R14.1–R14.4 shipped (`pkg/logging` + `main.go` wiring + slog-native Phase 5 + `pkg/daemon` migration + `.kiro/steering/logging.md`); R14.5–R14.7 (migrate remaining packages) pending. `pkg/alert` needed no migration — its error paths already return errors cleanly rather than printing.
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
| R14.4 ✅ | `pkg/daemon` (migrated), `pkg/alert` (no diagnostics — clean error returns) — background/operational, highest logging value | Long-running, needs trace |
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

**Status**: ✅ Shipped (5a commit `ed49161`; 5b this branch)
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

**Status: ✅ shipped (commit `ed49161`).** All five delivered — RF-parameterized metrics, `pkg/attribution` package, attribution-owned `nav_history` table, `mycase performance --vs-benchmark`, and tests.

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

**3. `pkg/cache` — `nav_history` table, owned by `pkg/attribution`**:
- To avoid deepening the `cache → domain` coupling (R16 problem P4), **`attribution` owns its persistence** rather than `cache` importing `attribution`. `attribution` takes a `*cache.Cache` (or `*sql.DB`) handle and defines its own table + access methods:
  - Table `nav_history(portfolio TEXT, ts BIGINT, nav DOUBLE, benchmark DOUBLE, PRIMARY KEY(portfolio, ts))` — append-only source-of-truth series → idempotent `ON CONFLICT (portfolio, ts) DO UPDATE`. Created lazily via `CREATE TABLE IF NOT EXISTS` on first write (or appended to the cache `ddl` const — but the Insert/Get methods live in `pkg/attribution`, not `pkg/cache`).
  - `pkg/attribution/store.go`: `InsertNAVPoints(ctx, db, portfolio, points)` and `GetNAVHistory(ctx, db, portfolio, since)` — following the `tax.go` tx / optional-`since`-filter patterns, BIGINT-Unix timestamps.
- This means **no `cache → attribution` edge** — `attribution` imports `cache` (one-way, correct direction), consistent with the R16 target shape. (The existing `cache → tax` edge stays until R16 fix D migrates it.)

**4. CLI — `mycase performance --vs-benchmark`**:
- Extend the existing `PerformanceCommand` with a `--vs-benchmark` flag (and `--benchmark` override, `--since`).
- When set: build the NAV series via the Tracker, print cumulative alpha, information ratio, tracking error, beta, and portfolio-vs-SPY total return, and persist the series to `nav_history`.
- Existing single-purchase valuation behavior is unchanged when the flag is absent (backward-compatible).
- slog-native: req_id-tagged `nav.built` / `nav.persisted` events; user-facing table stays on stdout via existing print path.

**5. Tests** (target ~90%, `pkg/tax` standard):
- `pkg/attribution`: NAV alignment (intersection of trading days), metric correctness on fixture series with known answers (hand-computed alpha/IR/beta), per-ticker fetch-failure skip behavior, timezone handling, empty/degenerate inputs. Use a mock `Router`-satisfying fetcher returning fixture `HistoricalData` — no network.
- `pkg/backtest`: RF-variant equivalence (`CalcSharpeRF(x, 0.06) == CalcSharpe(x)`), US-RF sanity.
- `pkg/cache`: `nav_history` round-trip, upsert idempotency, `since` filter.

### Phase 5b deliverables (shipped)

**Status: ✅ shipped (this branch).**

- **Return decomposition** — `pkg/attribution/decompose.go`: `Tracker.Decompose` splits active return into **selection** (buy-and-hold-first-basket vs benchmark), **rebalancing** (actual vs buy-and-hold-first-basket), and **tax** (realized TLH saving / initial capital, reported alongside). Identity: `ActiveReturn = Selection + Rebalancing`. Rebalance history is reconstructed from `pipeline_runs` + `proposals(stage="optimized")` via `LoadRebalanceHistory` (`store.go`), oldest-first, one completed run = one rebalance. Buy-and-hold counterfactual reuses `BuildNAVSeries` with the earliest in-window basket held untouched. Exposed on the CLI via `mycase performance --vs-benchmark --decompose`.
- **Dashboard performance tab** — `pkg/server/performance_handler.go` (`GET /api/portfolio/{name}/performance`, nil-fetcher guard + `available` flag, mirrors `tax_handler.go`) + `performance-tab.js` (echarts equity curve portfolio vs SPY, metrics table, decomposition table) + the 4 frontend wiring edits (`api.js`, `app.js` import/VIEWS/routes/componentTags, `index.html` tab+view). `Server` gained a `WithFetcher(attribution.PriceFetcher)` option (variadic `New`, backward-compatible); `cmd/serve.go` wires the router. First tests for `pkg/server`.
- **Alert nudge** — `pkg/attribution/nudge.go`: pure `AssessNudge(Result, threshold)` (default threshold −2% annualized alpha, ≥60 trading days required). `pkg/autopilot/alert.go`: `FormatAlphaNudgeAlert` + `SendAlphaNudgeAlerts` (no-op unless nudging) + `AssessPortfolioAlpha` (builds 1y NAV series, best-effort). Wired into `cmd/autopilot.go` after proposal alerts — logged and non-fatal. First tests for `pkg/autopilot`.

**Note on output layer**: Phase 5b originally shipped with `fmt.Printf` output because `pkg/render` (R12) was imported by zero packages and never adopted. This was closed by **L1 (R12.5)** — `render` is now the single reporting layer adopted across all of `cmd/*` and `pkg/{printer,executor}`. Performance output now renders via `render.Section`/`KV`/`Table`.


### Non-goals (Phase 5)

- No intraday NAV — daily close granularity only (quarterly rebalance system; intraday is noise).
- No performance persistence beyond the NAV series in 5a — decomposition tables come in 5b.
- Not mutating the `backtest` engine's IST/RF hardcoding in place — attribution parameterizes its own copy of the alignment logic to avoid destabilizing backtest tests. (A future refactor could unify them; out of scope here.)

---

---

## Phase R16 — Dependency Untangling — DESIGN

**Status**: ⬜ Design — do after Phase 5 (Phase 5a/5b apply its principles locally already).
**Motivation**: The internal package graph is **currently acyclic** (it compiles), but a handful of low-level packages have become *hubs* that mix type definitions with behavior and configuration. Every new feature package risks closing a loop against one of them — this is why Phase 5a had to invent a local `PriceFetcher` interface and have `attribution` own its cache table. The pain is not existing cycles; it's that the shape *invites* them, making the system progressively harder to understand, test, and extend.

### The dependency graph (measured `go list`, Aug 2026)

Layers, arrows point to dependencies (downward):

```
L4  cmd (composition root — imports ~20 pkgs)   server (imports 13)
        │                                            │
L3  autopilot   daemon   executor   printer          │
        │           │        │         │             │
L2  stockpicker ─► optimizer, csvloader, excel, selectiontracker
    datafetcher ─► broker/schwab, stockpicker (!back-edge), broker, yfinance
    backtest    monitoring
        │
L1  broker ─► config, costs        optimizer ─► broker, costs, market
    broker/schwab ─► broker, tax, yfinance
        │
L0  yfinance ─► cache ─► tax ─► broker ─► config, costs   (!leaf-that-isn't)
    market   alert   config   costs   excel   selectiontracker
```

Fan-in (most depended-upon): `broker` 11, `yfinance` 10, `config` 8, `optimizer` 5, `csvloader` 5, `costs` 5, `tax` 4, `cache` 4.

### The four cycle-magnet problems

**P1 — `yfinance → cache` inverts the natural direction (root wart).**
`yfinance` looks like a low-level data package, but it imports `cache` (for its price/fundamentals store), which imports `tax`, which imports `broker`, which imports `config`+`costs`. So importing `yfinance` — which nearly every package does — transitively drags in `broker`/`tax`/`cache`/`config`/`costs`. A data-fetch package caching *itself* is the inversion. This is the single biggest source of latent cycles.

**P2 — `datafetcher → stockpicker` is a back-edge.**
`datafetcher` (L2 data routing) imports `stockpicker` (L2 strategy) solely to satisfy `stockpicker.DataFetcher` (compile-time assert `var _ stockpicker.DataFetcher = (*Router)(nil)`). The low-level router depends on the high-level strategy purely for an interface the strategy defined. This is the edge that forced `attribution` to define its own `PriceFetcher` rather than import `datafetcher`.

**P3 — `broker` is an 11-way hub mixing types + behavior + config.**
One package holds the `Broker` interface, the pure DTOs (`Holding`, `Order`, `OrderResult`), *and* `MarketConfig`, *and* imports `config`+`costs`. Anything wanting just a `Holding` struct (e.g. `tax → broker`, `attribution`, `printer`, `optimizer`) pulls in `config` and `costs` too. `tax → broker` exists only for order-sequencing *types*.

**P4 — `cache → tax` couples the generic store to one domain.**
The DuckDB cache imports `tax` for lot/gain types. Adding `nav_history` naïvely would make `cache → attribution` too — and since `yfinance → cache` and everything imports `yfinance`, that closes a cycle the moment any price-fetching package also reads NAV. (Phase 5a sidesteps this: `attribution` owns its table via a DB handle.)

**Unifying diagnosis**: a package should either **define** types or **consume** them — not both, when it sits low in the stack. `yfinance`, `broker`, and `stockpicker` all do both, which is precisely why they act as cycle magnets.

### Fixes

The remedy is the standard Go pattern: **extract shared types into leaf packages with zero internal imports, and invert interface ownership to the consumer side.** No package collapsing — that would lose good separation. Four independent fixes:

| # | Problem | Fix | Effort |
|---|---------|-----|--------|
| **A** | P1 `yfinance→cache` | Invert the edge. Introduce `pkg/marketdata` (leaf: `HistoricalData`, `Fundamentals`, `Quote` types — zero imports). `cache` imports `marketdata`; `yfinance` imports `marketdata` + `cache`, or better: `yfinance` returns `marketdata` types and a thin caching wrapper lives above. Goal: `yfinance` (or a `marketdata/fetch` pkg) becomes a true leaf that pulls in nothing heavy. | Medium |
| **B** | P2 `datafetcher→stockpicker` | Move the `DataFetcher` interface to the consumer or a leaf. Either define it in `datafetcher` (and have `stockpicker` consume `datafetcher`'s interface), or put it in `pkg/marketdata`. Drop the back-edge + the compile-time assert. | Small |
| **C** | P3 `broker` hub | Split `pkg/broker/types` (pure `Holding`/`Order`/`OrderResult`/`MarketConfig` — zero imports) from `pkg/broker` (the `Broker` interface + `config`/`costs` wiring). `tax`, `attribution`, `printer`, `optimizer` import only `broker/types`. | Medium |
| **D** | P4 `cache→tax` | Cache should not import domain packages. Preferred: each domain package owns its persistence — it takes a `*cache.Cache`/`*sql.DB` handle and defines its own tables + Insert/Get (this is exactly what Phase 5a's `attribution` does). Migrate `tax`'s tables the same way, removing `cache → tax`. Alternative: a shared `pkg/store` types package. | Medium |

### Target shape after R16

```
L0 leaves (zero internal imports):  marketdata (types+iface)   broker/types   config   costs   market   alert
L1 stores/impls:                    cache(→marketdata)   yfinance(→marketdata,cache)   broker(→broker/types,config,costs)
L2 domains own their persistence:   tax(→cache,broker/types)   attribution(→cache,marketdata)
...                                 datafetcher(→marketdata,broker/schwab)  [no →stockpicker]
```

No package both defines widely-shared types and imports heavy dependencies. New feature packages depend on leaf type packages, never on hubs.

### Verification & guard rail

- After each fix (A–D independently shippable), `go build ./...` + `go test ./...` must stay green — these are pure move-and-reimport refactors, no behavior change.
- Add a CI/`make` check that fails on new cycles or on a package importing above its layer. Candidate: a small `go list`-based script (the one used to produce the graph above) asserting the layer ordering, or adopt an existing import-linter. Codify the layer rules in `.kiro/steering/`.
- **Relationship to R15**: the testability seams R15 wants (injectable data dir, injectable broker, mock fetcher) are cleaner once B and C land — mocks target leaf interfaces, not hubs. Hence R15 depends on R16.

### Non-goals

- Not collapsing packages — the separation is good; the problem is *type placement*, not too many packages.
- Not rewriting logic — R16 is move-and-reimport only. Any behavior change is out of scope and would be a separate phase.
- Not introducing a DI framework — plain constructor injection + leaf interfaces, consistent with the existing "no DI frameworks" constraint.

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

## Known Loose Ends (tracked debt)

Half-finished or orphaned pieces discovered during Phase 5b. None are urgent, but they should be closed rather than left to accumulate. Listed so they are visible and can be scheduled.

| # | Loose end | Evidence | Suggested fix / phase |
|---|-----------|----------|----------------------|
| L1 | ✅ **CLOSED (R12.5).** `pkg/render` was built in R12 but adopted by zero packages. Resolved by **adopting it everywhere** and making it interface-first: `render.Renderer` (Section/Banner/KV/Table/Writer) with a default tabwriter+ANSI impl behind it, so a specialized rendering lib can later be swapped in without touching call sites. `TableOpts` gained `Footer` + `Border` (pipe style); added `PnL`/`PnLPct` formatters. All of `cmd/*` and `pkg/{printer,executor}` now render through it. `pkg/printer` was rebuilt as a thin domain-report layer composing `render` (all hand-rolled padding/table code deleted) — establishing **one primitives layer** as the standard: `cmd`/`executor` → `printer` (domain reports) → `render` (generic primitives). Note: file-saved holdings/basket snapshots now use the `render` style. | was: `grep -r pkg/render` → no importers | Done. |
| L2 | ✅ **CLOSED (delete).** The `selections` DuckDB table + `Selection` struct + `Insert/Get/GetPreviousSelections` were built (Phase 7) but the write path was never wired — nothing called `InsertSelections`, so `pipeline show`'s "Final Selections" section always rendered empty. Wiring it is real feature work (build `Selection` records from the tracker + fundamentals), so it was **deleted** rather than left dead; the intended richer feature is captured as **Phase 8** in `docs/roadmap.md` (recoverable from git). Removed: table DDL (`db.go`), struct + 3 methods + `DeleteRunData` clause (`pipeline.go`), the empty "Final Selections" section (`pipeline_show.go`), and 4 round-trip tests. Zero runtime behavior change. | was: `cmd/pipeline.go` writes no selections; only `*_test.go` calls `InsertSelections` | Done. |
| L3 | ✅ **CLOSED (wire).** `pipeline_runs.config_json` now records a JSON snapshot of the resolved `PipelineConfig` per run (reproducibility / decomposition provenance). Added `PipelineConfig.Snapshot()` (`pkg/config/pipeline.go`, best-effort — returns "" on marshal failure) and wired it into both run creators (`cmd/pipeline.go`, `pkg/autopilot/autopilot.go`). | was: `InsertRun` passes `run.ConfigJSON` but callers leave it "" | Done. |
| L4 | ⬜ **DEFERRED → roadmap Phase 9.** The proposal `"final"` stage is *defined but unimplemented* — `pipeline show`/`diff` reference it on the read side, but nothing writes it (`GetProposals(runID, "final")` is always empty). Rather than invent a post-confirmation lifecycle speculatively, the intent (write executed-basket data back as `final` to close the proposed-vs-executed loop, feeding Phase 5 attribution) is captured as **roadmap Phase 9** to build with hindsight — or drop the read references and document `optimized` as terminal. No code change this pass. | `GetProposals(runID, "final")` returns nothing in practice | Deferred to Phase 9. |
| L5 | **Server data paths are hardcoded relative (`data/<name>.csv`, `data/cache.db`).** Blocks in-process E2E tests and made the Phase 5b performance handler untestable end-to-end. | `pkg/server/handlers.go`, `performance_handler.go` | Injectable data dir (`MYCASE_DATA_DIR`) — already called out as an R15 prerequisite. Do it with R15. |
| L6 | **`pkg/attribution` tax effect is always 0 on the live paths.** `Decompose` accepts a `TaxSaving` but both the CLI and dashboard pass 0 (tax is surfaced separately by `mycase tax`). The decomposition's tax line only appears in tests. | `cmd/performance.go`, `performance_handler.go` pass `TaxSaving: 0` | Wire realized TLH saving from `pkg/tax` into the decomposition input, or drop the tax line from the live decomposition and keep it purely in the Tax tab. |
| L7 | ✅ **CLOSED (delete).** `alert.EmailAlerter` was an unimplemented stub — `Send` always returned "not yet implemented", nothing constructed it, and `sendToChannels` only wired telegram/discord. Deleted `pkg/alert/email.go`; no email channel exists to reference it. Zero behavior change. | was: `pkg/alert/email.go`; dead orphan | Done. |
| L8 | ✅ **CLOSED (fix).** `monitoring/simulator_test.go` `TestRunSimulation_NoNaNInResult` now `t.Fatalf`s on a simulation error instead of `t.Skip` — a real error can no longer pass silently while masking the NaN check. | was: `pkg/monitoring/simulator_test.go:136` `t.Skip` | Done. |

**Meta-observation**: the recurring shape is *build-it-then-never-wire-it* — `render`, `selections`, `config_json`, the `final` stage, `EmailAlerter` were all constructed with intent and left unconnected. The next session's theme (fix before feature) should bias toward **either wire it or delete it** for each, so the codebase stops carrying aspirational surface area.


---



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
| **R12.5** | Render adoption (closes L1) — made `pkg/render` interface-first (`Renderer`: Section/Banner/KV/Table/Writer + swappable default impl), added `Footer`/`Border` table opts + `PnL`/`PnLPct`; adopted across all `cmd/*` and `pkg/{printer,executor}`; rebuilt `pkg/printer` as a thin domain-report layer over `render` (deleted all hand-rolled padding/table code). One primitives layer standard: cmd/executor → printer → render | — |
| **R13** | Stockpicker `DataFetcher` injection — interface seam, router wiring, `runPickWithOpts` collapsed. See architecture D10 | — |
| **Phase 4** | Tax-loss harvesting — `pkg/tax` (FIFO, TLH, sequencing), Schwab transactions, DuckDB tax tables, `mycase tax`, dashboard Tax tab. See architecture D11–D12 | — |
| **R14.1–R14.2** | Structured logging foundation — `pkg/logging` (fanout handler, req_id tracing, timing/HTTP/DB helpers, rotation), `main.go` Before/After wiring + global flags, `config/defaults.json` logging block | `db6e046` |
| **Phase 5a** | Live perf attribution (NAV foundation) — `pkg/attribution` (NAV series, alpha/IR/beta/tracking-error vs `US:SPY`), attribution-owned `nav_history` DuckDB table, RF-parameterized backtest metrics, `mycase performance --vs-benchmark`; R14.3 slog-native | `ed49161` |
| **Phase 5b** | Live perf attribution (decomposition + dashboard + nudge) — `attribution.Decompose` (selection/rebalancing/tax) + `LoadRebalanceHistory`, dashboard Performance tab (`performance_handler.go` + `performance-tab.js` + `WithFetcher` option), trailing-alpha strategy-review nudge (`AssessNudge` + autopilot dispatch); first tests for `pkg/server` + `pkg/autopilot`; removed dead code (`isNumeric`, `newTestClient`) | — |
