# Mycase Refactor Plan: Architecture, Modernization & Product Roadmap

**Branch**: `feature/mycase-changes`  
**Date**: 2026-07-19  
**Go version target**: 1.26.x

---

## Executive Summary

The codebase is functionally complete for Phase 1 (Core Engine & CLI) and has solid modular `pkg/` architecture. The primary structural weaknesses are at the **command layer**: 9 independent `cmd/` binaries with inconsistent argument parsing, no shared CLI framework, and a pipeline that orchestrates them via `os/exec` shell-outs instead of direct Go function calls. This creates fragility, duplicated flag-parsing code, and a poor UX where commands feel disconnected.

The refactor targets three outcomes:
1. **Unify** all commands under a single binary with a hierarchical `urfave/cli` command tree.
2. **Modernize** the codebase for Go 1.24 idioms and eliminate technical debt.
3. **Architect** the foundation so Phases 2–6 of the product vision can be built without structural rewrites.

---

## 1. Current State Assessment

### 1.1 What Works Well (Keep & Preserve)

| Area | Status | Notes |
|------|--------|-------|
| `pkg/optimizer` | Solid | Modular files, well-separated math/types/helpers |
| `pkg/stockpicker` | Solid | Loader/filters/scoring/io separation is clean |
| `pkg/yfinance` | Good | Concurrent worker pools, crumb/cookie handling |
| `pkg/monitoring` | Good | Types/simulator separation; deterministic mock fixed |
| Multibagger strategy | Excellent | 11 safety filters + 4 technical filters + 100-pt scoring |
| Test coverage | Good | `pkg/stockpicker`, `pkg/monitoring`, `pkg/optimizer`, `pkg/csvloader` have tests |
| Config-driven params | Good | `mfs.json`, `pipeline.yaml` keep thresholds out of code |

### 1.2 Structural Problems (Must Fix)

#### A. Fragmented CLI — No Unified Entry Point
**Impact**: High. Every `cmd/*/main.go` is an independent binary. Users must know 9 different commands. Pipeline runs them via `os/exec` shell-out — no type safety, no error propagation, no shared context, shell quoting issues.

**Evidence**:
- `cmd/pipeline/main.go` (495 lines): Calls `./bin/stockpicker`, `./bin/monitoring`, `./bin/performance` etc. via `exec.Command`. Requires `go build` step 0 to populate `bin/`.
- `scripts/merge.go` is an orphan — not a proper `cmd/`, callable only via `go run scripts/merge.go`.

#### B. Inconsistent Flag Parsing
**Impact**: Medium. Three different patterns used across the codebase:

| Style | Commands | Problem |
|-------|----------|---------|
| `flag` stdlib | `stockpicker`, `monitoring`, `performance`, `report`, `optimize_weights` | Fine, but no `--help` grouping, no subcommand routing |
| Manual `os.Args` loop | `basket`, `holdings` | Fragile. `--live` and `-- name` are positional by convention with no validation |
| Config YAML + flags | `pipeline` | Two-layer config with complex `resolveFirst[T]()` generics to handle both sources |

#### C. Business Logic in `cmd/` Mains
**Impact**: Medium. Several `cmd/` mains contain logic that belongs in `pkg/`:
- `cmd/monitoring/main.go` (594 lines): Mock data generation, report file writing, interactive menu, AND simulator orchestration all mixed together.
- `cmd/performance/main.go` (350 lines): Date parsing, CSV loading, portfolio valuation math, and table printing all in `main()`.
- `cmd/report/main.go` (365 lines): Heuristic text generation logic not separated from I/O.
- `cmd/optimize_weights/main.go` (388 lines): Rebalancing band logic, golden copy diffing, and CSV writing inline.

#### D. Go Module Naming
**Impact**: Low but clean-up worthy. Module name is `mycase` (not a full domain path like `github.com/user/mycase`). Acceptable for a private tool but worth noting.

#### E. Missing Broker Abstraction (Product Vision Blocker)
**Impact**: High for roadmap. `cmd/basket/main.go` and `cmd/holdings/main.go` are tightly coupled to `pkg/kiteclient` (Zerodha). Phase 5 (Multi-Broker) requires a `Broker` interface in `pkg/broker/` before any second broker can be added.

#### F. No Drift Monitoring Daemon (Phase 2 Gap)
**Impact**: High for roadmap. The `cmd/monitoring` tool is an **interactive backtest simulator**, not a live drift alerting daemon. Phase 2 needs a background service with notification integrations.

---

## 2. Target Architecture

### 2.1 Single Binary with Hierarchical Commands

Replace 9 binaries + `scripts/merge.go` with **one binary** (`mycase`) using `urfave/cli/v2`.

```
mycase [global flags]
├── pipeline                     # Orchestrate full workflow (replaces cmd/pipeline)
│   └── --config, --exec-only
│
├── pick                         # Stock selection (replaces cmd/stockpicker)
│   ├── --index, --file, --method, --top, --range
│   └── --golden, --rebalance-tolerance, --hysteresis-buffer
│
├── optimize                     # Weight optimizer (replaces cmd/optimize_weights)
│   ├── --file, --method, --cap
│   └── --golden, --remove
│
├── report                       # Portfolio explanation report (replaces cmd/report)
│   └── --file, --method
│
├── performance                  # Historical backtest (replaces cmd/performance)
│   └── --file, --capital, --date, --time
│
├── monitor                      # Drift simulator (replaces cmd/monitoring)
│   ├── --file, --style, --strategy, --date
│   └── --interactive
│
├── basket                       # Order execution (replaces cmd/basket)
│   ├── --live
│   └── [portfolio-name]
│
├── holdings                     # Holdings snapshot (replaces cmd/holdings)
│   └── --live
│
├── merge                        # CSV merge utility (replaces scripts/merge.go)
│   └── combine | update-golden
│
└── auth                         # Zerodha auth setup (replaces cmd/setup_auth)
```

**Key synergies unlocked by a unified CLI:**
- `pipeline` can call `pick`, `optimize`, `report`, `performance`, `monitor` as **Go function calls** — no `os/exec`, no `bin/` build step required.
- Shared `--config` global flag wires `pipeline.yaml` values as defaults into every subcommand.
- `--dry-run` global flag can be added once and honored across all execution commands.
- Shell completion (`mycase --generate-completion bash/zsh`) comes free from `urfave/cli`.

### 2.2 Proposed Directory Structure

```
mycase/
├── main.go                      # Single entry point — wires urfave/cli app
├── cmd/                         # One file per subcommand (thin orchestrators only)
│   ├── pipeline.go
│   ├── pick.go
│   ├── optimize.go
│   ├── report.go
│   ├── performance.go
│   ├── monitor.go
│   ├── basket.go
│   ├── holdings.go
│   ├── merge.go
│   └── auth.go
├── pkg/                         # All business logic (unchanged structure, additions below)
│   ├── broker/                  # NEW: Phase 5 broker abstraction
│   │   ├── broker.go            # Broker interface
│   │   └── zerodha/             # Existing kite logic moved here
│   ├── alert/                   # NEW: Phase 2 notification integrations
│   │   ├── alert.go             # Alerter interface
│   │   ├── telegram.go
│   │   └── discord.go
│   ├── daemon/                  # NEW: Phase 2 background drift monitor
│   │   └── daemon.go
│   ├── config/                  # Existing — add pipeline.yaml loader here
│   ├── csvloader/               # Existing
│   ├── executor/                # Existing — wire to broker interface
│   ├── kiteclient/              # Existing — implement broker interface
│   ├── market/                  # Existing
│   ├── monitoring/              # Existing
│   ├── optimizer/               # Existing
│   ├── portfolio/               # Existing
│   ├── printer/                 # Existing
│   ├── selectiontracker/        # Existing
│   ├── stockpicker/             # Existing
│   └── yfinance/                # Existing
├── config/
├── data/
├── report/
└── docs/
```

---

## 3. Refactor Phases

---

### Phase R1 — CLI Unification (Foundation)

**Goal**: Replace the 9-binary pattern with a single `mycase` binary using `urfave/cli/v2`. No business logic changes — pure structural reorganization.

**Effort**: Large (3–5 days)  
**Risk**: Low (no logic changes; existing `pkg/` is untouched)  
**Dependency**: All subsequent phases depend on this.

#### Tasks

**R1.1 — Add `urfave/cli/v2` dependency**
```bash
go get github.com/urfave/cli/v2
```
*Reference*: [urfave/cli v2 docs](https://cli.urfave.org/v2/getting-started/) — supports subcommands, flags, shell completion, `--help` auto-generation, `Before`/`After` hooks for shared setup.

**R1.2 — Create `main.go` at project root**
Wire the `cli.App` with all subcommands. Each subcommand's `Action` calls a function in `cmd/[name].go`.

**R1.3 — Migrate each `cmd/*/main.go` to `cmd/[name].go`**
For each command:
1. Extract the flag definitions into a `cli.Command.Flags` slice.
2. Move the `main()` body into a function like `runPick(c *cli.Context) error`.
3. Delete the old `cmd/*/main.go`.

Migration map:

| Old binary | New subcommand | New file |
|------------|----------------|----------|
| `cmd/stockpicker/main.go` | `mycase pick` | `cmd/pick.go` |
| `cmd/optimize_weights/main.go` | `mycase optimize` | `cmd/optimize.go` |
| `cmd/report/main.go` | `mycase report` | `cmd/report.go` |
| `cmd/performance/main.go` | `mycase performance` | `cmd/performance.go` |
| `cmd/monitoring/main.go` | `mycase monitor` | `cmd/monitor.go` |
| `cmd/basket/main.go` | `mycase basket` | `cmd/basket.go` |
| `cmd/holdings/main.go` | `mycase holdings` | `cmd/holdings.go` |
| `cmd/setup_auth/main.go` | `mycase auth` | `cmd/auth.go` |
| `cmd/pipeline/main.go` | `mycase pipeline` | `cmd/pipeline.go` |
| `scripts/merge.go` | `mycase merge` | `cmd/merge.go` |

**R1.4 — Refactor `pipeline.go` to use direct Go calls**
Replace all `exec.Command("./bin/stockpicker", ...)` calls with direct calls to the same functions that `cmd/pick.go` calls. This eliminates the build-first requirement and gives the pipeline proper error propagation.

**R1.5 — Standardize `basket` and `holdings` flag parsing**
Convert the manual `os.Args` loop in both to `urfave/cli` flags:
- `basket`: `--live bool`, positional arg for portfolio name → `cli.StringArg`
- `holdings`: `--live bool`

**R1.6 — Add Makefile / build target**
```makefile
build:
    go build -o bin/mycase .

install:
    go install .
```

---

### Phase R2 — Code Cleanup & Logic Extraction

**Goal**: Move business logic out of `cmd/` mains and into `pkg/`. Reduce cmd files to thin flag-parsing + orchestration.

**Effort**: Medium (2–3 days)  
**Risk**: Low (covered by existing tests)

#### Tasks

**R2.1 — Extract mock data generator from `cmd/monitoring/main.go`**
The `generateMockData` function (plus the synchronized `rand.New(rand.NewSource(42))` logic) belongs in `pkg/monitoring/mock.go`. The cmd should just call `monitoring.GenerateMockPortfolio(...)`.

**R2.2 — Extract portfolio valuation from `cmd/performance/main.go`**
The date-matching, price lookup, and P&L calculation logic belongs in `pkg/performance/` (new package). The cmd becomes: parse flags → call `performance.RunBacktest(...)` → print result.

**R2.3 — Extract heuristic text from `cmd/report/main.go`**
The `generateHeuristics()` / narrative text generation belongs in `pkg/report/` (new package). The cmd becomes: parse flags → call `report.Generate(...)` → write file.

**R2.4 — Extract rebalancing band diff logic from `cmd/optimize_weights/main.go`**
The golden copy diffing and exit-weight injection logic belongs in `pkg/optimizer/rebalance.go`. Already partially there — complete the move.

**R2.5 — Consolidate `pkg/config/config.go`**
Currently `config.go` handles only broker credentials. Move `pipeline.yaml` loading (currently inline in `cmd/pipeline/main.go` as `PipelineConfig` + `rawPipelineConfig` + the `resolveFirst[T]()` generics helper) into `pkg/config/pipeline.go`. The `resolveFirst` generics hack is a workaround for YAML ambiguity — replace with a proper strict YAML struct and explicit defaults.

---

### Phase R3 — Go 1.26 Modernization

**Goal**: Upgrade to Go 1.26.x and apply all modern idioms from 1.21 → 1.26. See Section 7.2 for the complete feature table.

**Effort**: Small (1 day)  
**Risk**: Very Low

#### Tasks

**R3.1 — Replace `sort.Interface` boilerplate with `slices.SortFunc`**
`cmd/holdings/main.go` (and others) implement `ByPnLPct` with 3-method sort interface. Go 1.21+ provides `slices.SortFunc` — remove the boilerplate types.

```go
// Before
sort.Sort(ByPnLPct(holdings))

// After (Go 1.21+)
slices.SortFunc(holdings, func(a, b Holding) int {
    return cmp.Compare(a.PnLPct, b.PnLPct)
})
```

**R3.2 — Use `maps` stdlib package where applicable**
Go 1.21 added `maps.Keys()`, `maps.Values()` — replace manual key-extraction loops in optimizer and yfinance packages.

**R3.3 — Use `min` / `max` builtins**
Go 1.21 added `min()` and `max()` builtins. Replace `math.Min(float64(a), float64(b))` patterns used for integer comparisons.

**R3.4 — Structured logging with `log/slog`**
Go 1.21 added `log/slog`. Replace scattered `fmt.Fprintf(os.Stderr, ...)` and `log.Printf` calls with structured `slog.Info`, `slog.Warn`, `slog.Error`. Use `--verbose` flag (added in R1) to toggle `slog.SetLogLoggerLevel`.

**R3.5 — `context.Context` propagation**
HTTP calls in `pkg/yfinance/prices.go` and `pkg/yfinance/metrics.go` don't accept `context.Context`. Add `ctx context.Context` as the first parameter so callers can set deadlines (e.g., pipeline timeout). Use `http.NewRequestWithContext`.

**R3.6 — Replace `math/rand` with `math/rand/v2`**
Go 1.22 introduced `math/rand/v2` with a cleaner API and no global state. Replace `rand.New(rand.NewSource(42))` in `cmd/monitoring/main.go`'s mock generator with `rand/v2`'s `rand.New(rand.NewPCG(42, 0))` — more statistically sound and idiomatic.

**R3.7 — Apply `range N` where appropriate**
Go 1.22 allows `for i := range 15 { }` — replace `for i := 0; i < numWorkers; i++` worker-spawn loops in `pkg/yfinance/prices.go` and `pkg/stockpicker/loader.go`.

**R3.8 — Update `gocarina/gocsv` dependency**
Current version is pinned to `2018-08-09`. Update: `go get github.com/gocarina/gocsv@latest`. See Section 7.3.

**R3.9 — Move all dependencies from `indirect` to `direct`**
`go.mod` marks all 4 dependencies as `// indirect`. After R1 adds `urfave/cli/v3` and R-cache adds `modernc.org/sqlite`, run `go mod tidy` to properly classify direct vs. indirect deps.

---

### Phase R-cache — Persistent Price & Fundamentals Cache

**Goal**: Add SQLite-backed persistent cache layer under `pkg/cache/` so Yahoo Finance API is only called when data is genuinely stale. Decisions documented in D5.

**Effort**: Medium (2 days)  
**Risk**: Low (additive layer; yfinance fetch functions remain unchanged as the cache-miss path)

#### Tasks

**R-cache.1 — Add `github.com/duckdb/duckdb-go/v2`**
```bash
go get github.com/duckdb/duckdb-go/v2
```
Official DuckDB Go driver — same package already in production in sanvasify, eia-api-explorer, patscape. Platform bindings ship as go module deps (darwin-arm64, linux-amd64, etc.) — no manual setup.

**R-cache.2 — Implement `pkg/cache/`**
Mirrors sanvasify's `pkg/db/` structure — `database/sql` interface, `InitSchema(ctx)`, transactions for bulk inserts, `log/slog` for logging. See D5 for full schema and query patterns.
- `db.go`: `New(path string) (*Cache, error)` — `sql.Open("duckdb", path)`, `InitSchema`, `Close`
- `prices.go`: `GetPrices(ticker, rangeKey string) (*yfinance.HistoricalData, bool)` / `StorePrices(...)` with `ON CONFLICT DO UPDATE`
- `fundamentals.go`: `GetFundamentals(ticker string) (*yfinance.Fundamentals, bool)` / `StoreFundamentals(...)`

**R-cache.3 — Wire cache into yfinance fetch functions**
Wrap `FetchHistoricalDataWithTimestamps` and `FetchFundamentals` with a cache-check-first pattern. The caller API is unchanged — cache is transparent.

**R-cache.4 — `mycase cache` subcommand**
```
mycase cache status          # Show row counts, last fetch timestamps per ticker
mycase cache clear --ticker  # Evict a specific ticker (force re-fetch)
mycase cache clear --all     # Wipe entire cache
```

---

### Phase R4 — Broker Abstraction Layer (Phase 5 Foundation)

**Goal**: Decouple order execution from Zerodha-specific implementation. Enables Phase 5 (multi-broker support) without touching `cmd/basket` or `cmd/holdings`.

**Effort**: Medium (2 days)  
**Risk**: Low (additive — doesn't change Zerodha behavior)

#### Tasks

**R4.1 — Define `pkg/broker/broker.go` interface**

```go
package broker

type Holding struct {
    TradingSymbol string
    Exchange      string
    Quantity      int
    T1Quantity    int
    AveragePrice  float64
    LastPrice     float64
    PnL           float64
}

type Order struct {
    TradingSymbol string
    Exchange      string
    TransactionType string // "BUY" or "SELL"
    Quantity      int
    OrderType     string // "LIMIT", "GTT"
    Price         float64
    TriggerPrice  float64
}

type Broker interface {
    GetHoldings() ([]Holding, error)
    PlaceOrders(orders []Order) ([]string, error)
    GetPositions() ([]Holding, error)
    IsAuthenticated() bool
}
```

**R4.2 — Implement `pkg/broker/zerodha/`**
Move Kite-specific logic from `pkg/kiteclient/client.go`, `pkg/executor/executor.go`, and `pkg/portfolio/portfolio.go` into `pkg/broker/zerodha/zerodha.go` which implements the `Broker` interface.

**R4.3 — Wire `cmd/basket.go` to `broker.Broker`**
The basket command receives a `Broker` via the app's `Metadata` map or a package-level factory. This keeps the command broker-agnostic.

**R4.4 — Research: Fyers & AngelOne APIs**
- Fyers: [fyers.in/api](https://myapi.fyers.in/docs/) — REST API, Go community wrapper available (`gofyers`)
- AngelOne SmartAPI: [smartapi.angelbroking.com](https://smartapi.angelbroking.com/docs) — REST + WebSocket, no official Go SDK, requires custom HTTP client
- Upstox: [upstox.com/developer/api-documentation](https://upstox.com/developer/api-documentation/) — REST API, Go SDK available (`upstox-go`)

*Effort for each broker implementation*: Small–Medium (1–2 days each) once the interface is defined.

---

### Phase R5 — Drift Monitoring Daemon (Phase 2 of Product Vision)

**Goal**: Build a real background drift alerting service, distinct from the interactive backtest simulator. Sends notifications when portfolio drift exceeds threshold.

**Effort**: Large (4–6 days)  
**Risk**: Medium (new infrastructure — notification delivery, daemon process management)

#### Tasks

**R5.1 — Define `pkg/alert/alert.go` Alerter interface**

```go
type Alert struct {
    Title   string
    Body    string
    Level   string // "info", "warn", "critical"
}

type Alerter interface {
    Send(a Alert) error
}
```

**R5.2 — Implement Telegram bot alerter (`pkg/alert/telegram.go`)**
- Requires `TELEGRAM_BOT_TOKEN` and `TELEGRAM_CHAT_ID` in config.
- Uses `https://api.telegram.org/bot{token}/sendMessage` — no SDK needed, plain HTTP POST.
- *Research*: [Telegram Bot API](https://core.telegram.org/bots/api#sendmessage)

**R5.3 — Implement Discord webhook alerter (`pkg/alert/discord.go`)**
- Uses Discord Incoming Webhook URL — single HTTP POST with JSON body.
- *Research*: [Discord Webhook docs](https://discord.com/developers/docs/resources/webhook#execute-webhook)

**R5.4 — Drift calculation engine (`pkg/daemon/drift.go`)**
Implements the drift formula from Phase 2 of the product vision:
$$\text{Drift} = \frac{1}{2} \sum_{i} |w_{\text{actual}, i} - w_{\text{target}, i}|$$
Fetches live quotes for portfolio, computes actual vs. target weights, returns drift index.

**R5.5 — Daemon runner (`pkg/daemon/daemon.go`)**
- Runs drift check at configurable interval (default: daily at 15:45 IST, post-market).
- Persists last-check state to `data/daemon_state.json` (survives restarts).
- On drift > threshold: calls all configured `Alerter`s.

**R5.6 — Add `mycase daemon` subcommand**
```
mycase daemon start   # Start background daemon (writes PID file)
mycase daemon stop    # Stop running daemon
mycase daemon status  # Show last drift check results
mycase daemon check   # One-shot drift check (no loop)
```

**R5.7 — Config additions to `config/pipeline.yaml`**
```yaml
alerts:
  drift_threshold: 0.05        # 5% drift triggers alert
  channels:
    - telegram
    - discord
  telegram_bot_token: ""       # Override via env MYCASE_TELEGRAM_TOKEN
  telegram_chat_id:  ""
  discord_webhook_url: ""
```

---

### Phase R6 — Tax & Transaction Awareness (Phase 3 of Product Vision)

**Goal**: Make the optimizer and basket engine aware of real-world friction costs before suggesting orders.

**Effort**: Medium (2–3 days)  
**Risk**: Low (additive filter layer)

#### Tasks

**R6.1 — Transaction cost model (`pkg/costs/costs.go`)**
Model all Indian market charges:
- STT (Securities Transaction Tax): 0.1% on buy, 0.1% on sell (equity delivery)
- Stamp duty: 0.015% on buy (equity delivery)
- DP charge: flat ₹15.93 per ISIN per day of sell (NSDL/CDSL)
- Brokerage: configurable (Zerodha = ₹0 for equity delivery)
- SEBI charges: 0.0001%

**R6.2 — Micro-transaction filter**
Before generating an order, check: if `transaction_cost / trade_value > configurable_threshold` (e.g., 0.5%), skip the trade. Add to `pkg/optimizer/rebalance.go`.

**R6.3 — STCG/LTCG warning layer**
Cross-reference proposed sell orders against purchase date (from `data/backups/` or holdings average price). Flag orders where `holding_period < 365 days` as STCG (15% tax) vs. LTCG (10% above ₹1L). Show warning banner in basket output.

*Research needed*: STCG/LTCG rates as of FY2026 under Finance Act 2024 amendments — verify current rates and grandfathering rules.

---

### Phase R7 — Historical Backtesting Engine (Phase 4 of Product Vision)

**Goal**: Build a full portfolio simulator with configurable rebalancing frequency and comprehensive performance analytics.

**Effort**: Extra-Large (8–12 days)  
**Risk**: Medium (complex time-series logic, external data dependency)

#### Tasks

**R7.1 — Historical price data store**
Extend `pkg/yfinance/prices.go` to fetch and cache daily adjusted close prices for multi-year windows. Cache to `data/price_cache/[ticker]_[range].json` to avoid re-fetching on repeated runs.

**R7.2 — Simulation engine (`pkg/backtest/`)**
```
pkg/backtest/
├── types.go       # SimConfig, SimResult, DailySnapshot
├── engine.go      # Core simulation loop
└── metrics.go     # CAGR, Max Drawdown, Sharpe, Sortino, Calmar
```

Simulation parameters:
- Initial capital, start date, end date
- Rebalancing frequency: `monthly`, `quarterly`, `drift-triggered` (drift > X%)
- Slippage: configurable % per trade

**R7.3 — Benchmark comparison**
Download Nifty 50 (`^NSEI`) and Nifty Smallcap 250 (`^CNXSC`) as benchmarks. Calculate portfolio Alpha and Beta over the simulation period.

**R7.4 — Add `mycase backtest` subcommand**
```
mycase backtest \
  --file data/microsmall.csv \
  --capital 100000 \
  --from 2023-01-01 \
  --to 2026-07-01 \
  --rebalance quarterly \
  --slippage 0.1
```

*Research reference*: [Portfolio performance metrics — CFA Institute](https://www.cfainstitute.org/) for Sharpe/Sortino/Calmar formulas; existing `pkg/monitoring/simulator.go` has partial implementations to leverage.

---

### Phase R8 — Web Dashboard (Phase 6 of Product Vision)

**Goal**: Local web UI to visualize portfolio, adjust weights interactively, and trigger rebalance.

**Effort**: Extra-Large (10–15 days)  
**Risk**: High (frontend stack decision, new tech surface)

#### Tasks

**R8.1 — Go HTTP server (`pkg/server/`)**
Lightweight REST API using `net/http` stdlib (no Gin needed for this scope):
```
GET  /api/portfolio/:name     → Current holdings + target weights
GET  /api/quotes/:name        → Live prices for portfolio
POST /api/rebalance/:name     → Trigger basket order (requires auth)
GET  /api/performance/:name   → Backtest results
```

**R8.2 — Frontend: Plain HTML/CSS/JS + Web Components + Apache ECharts**
Stack decided in D3. No framework, no build pipeline. See D3 for directory layout and component breakdown.

Key ECharts usage:
- Portfolio weight donut/pie chart (actual vs. target weights)
- Drift timeline line chart (daily drift index history)
- Backtest equity curve with benchmark overlay (area line chart)
- Holdings table with sparklines for intraday price movement

Web Components:
- `<portfolio-summary>` — weight table + donut
- `<weight-slider>` — interactive weight adjustment with live recalculation
- `<holdings-table>` — sortable table with P&L columns
- `<drift-alert>` — banner shown when drift > threshold

Live quote updates via SSE (`text/event-stream`): the Go server pushes quote refresh events every 60 seconds during market hours; the JS client updates ECharts series data in place.

**R8.3 — Embed static assets**
Use `//go:embed static/*` directive to embed the entire `static/` tree into the binary. `mycase serve --port 8080` starts the dashboard. No separate static file server needed. ECharts vendored in `static/vendor/echarts.min.js` — no CDN dependency.

---

## 4. Effort & Priority Summary

| Phase | Goal | Effort | Priority | Dependency |
|-------|------|--------|----------|------------|
| **R1** | CLI Unification (urfave/cli v3) | Large (3–5d) | P0 — do first | None |
| **R2** | Code cleanup / logic extraction | Medium (2–3d) | P1 | R1 |
| **R3** | Go 1.26 modernization + dep updates | Small (1d) | P1 | R1 |
| **R-cache** | SQLite price/fundamentals cache | Medium (2d) | P1 | R2, R3 |
| **R4** | Broker abstraction layer | Medium (2d) | P2 | R2 |
| **R5** | Drift monitoring daemon + launchd | Large (4–6d) | P2 | R2, R4 |
| **R6** | Tax & transaction cost awareness | Medium (2–3d) | P3 | R2 |
| **R7** | Historical backtesting engine | XL (8–12d) | P3 | R2, R3, R-cache |
| **R8** | Web dashboard | XL (10–15d) | P4 | R4, R7 |

**Total estimated effort**: ~37–54 working days for full roadmap.  
**Minimum viable refactor (R1–R3 + module rename)**: ~6–9 days — delivers clean unified CLI without any feature loss.

---

## 5. Implementation Guidelines

### 5.1 Do Not Break
- All existing `pkg/` packages have tests. Run `go test ./...` before and after each phase — zero regressions.
- `mfs.json` and `pipeline.yaml` config file formats must stay backward-compatible through R1–R3.
- Existing `data/*.csv` golden copy files are user data — never touch them programmatically except through the guarded backup → overwrite flow already in place.

### 5.2 urfave/cli v2 Pattern

Each subcommand in `cmd/[name].go` follows this pattern:

```go
package cmd

import "github.com/urfave/cli/v2"

var PickCommand = &cli.Command{
    Name:  "pick",
    Usage: "Select top stocks from an index or file",
    Flags: []cli.Flag{
        &cli.StringFlag{Name: "index", Aliases: []string{"i"}, Usage: "Index name (microcap250, small250...)"},
        &cli.StringFlag{Name: "file",  Aliases: []string{"f"}, Usage: "Custom CSV file path"},
        &cli.StringFlag{Name: "method", Value: "balanced", Usage: "Scoring strategy"},
        &cli.IntFlag{   Name: "top",   Value: 20,           Usage: "Number of stocks to select"},
    },
    Action: runPick,
}

func runPick(c *cli.Context) error {
    // thin orchestration — delegate to pkg/stockpicker
}
```

`main.go` simply assembles them:

```go
app := &cli.App{
    Name:     "mycase",
    Usage:    "Portfolio basket & rebalancing engine",
    Commands: []*cli.Command{
        cmd.PipelineCommand,
        cmd.PickCommand,
        cmd.OptimizeCommand,
        // ...
    },
}
```

### 5.3 Phase R1 Migration Safety Net

Before deleting any `cmd/*/main.go`, verify the subcommand produces identical output:
1. Run the old binary: `go run cmd/stockpicker/main.go [flags] > old_output.txt`
2. Run the new subcommand: `mycase pick [same flags] > new_output.txt`
3. `diff old_output.txt new_output.txt` — must be empty.

### 5.4 Naming Conventions (Existing — Preserve)
- Dates: `YYYYMMDD`
- Timestamps: `YYYYMMDD_HHMMSS`
- Report files: lowercase snake_case
- Step prefixes in execution reports: `01_`, `02_`, `03_`

---

## 6. Decisions Log

All open questions from initial draft are now resolved.

---

### D1 — Module Path: Rename to full domain path

**Decision**: Rename `module mycase` → `module github.com/raghavkgarg/mycase`.

**Rationale**: One-time find-replace across all import paths. Makes the module correctly identifiable, importable if ever open-sourced, and aligns with Go module conventions. `go mod edit -module github.com/raghavkgarg/mycase` followed by a sed pass on all `"mycase/pkg/..."` imports.

**Status**: Done. Module is `github.com/raghavkgarg/mycase`. Repository at `https://github.com/raghavkgarg/mycase`.

**Task**: Do this as the very first commit of R1 — before any structural changes — so the diff is clean and isolated.

---

### D2 — Daemon Process Model: launchd + systemd documentation

**Decision**: Integrate with macOS **launchd** as the primary deployment target. Document **systemd** unit file for Linux deployment.

**launchd plist** (`~/Library/LaunchAgents/com.mycase.daemon.plist`):
```xml
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>com.mycase.daemon</string>
    <key>ProgramArguments</key>
    <array>
        <string>/usr/local/bin/mycase</string>
        <string>daemon</string>
        <string>run</string>
        <string>--config</string>
        <string>/Users/[username]/.mycase/config/pipeline.yaml</string>
    </array>
    <key>StartCalendarInterval</key>
    <dict>
        <key>Hour</key><integer>15</integer>
        <key>Minute</key><integer>45</integer>
    </dict>
    <key>StandardOutPath</key>
    <string>/tmp/mycase-daemon.log</string>
    <key>StandardErrorPath</key>
    <string>/tmp/mycase-daemon-error.log</string>
    <key>RunAtLoad</key>
    <false/>
</dict>
</plist>
```

**systemd unit** (`/etc/systemd/system/mycase-daemon.service`) — documented in `docs/deploy-linux.md`:
```ini
[Unit]
Description=Mycase Portfolio Drift Monitor
After=network.target

[Service]
Type=simple
ExecStart=/usr/local/bin/mycase daemon run --config /etc/mycase/pipeline.yaml
Restart=on-failure
RestartSec=30
User=mycase

[Install]
WantedBy=multi-user.target
```

`mycase daemon install` / `mycase daemon uninstall` subcommands will write and load the plist on macOS, or print the systemd instructions on Linux.

---

### D3 — Web Dashboard Frontend: Plain HTML/CSS/JS + Web Components + Apache ECharts

**Decision**: No framework. Plain HTML5, modern CSS, vanilla JS (ES2022+), Web Components for reusable UI elements, Apache ECharts for all charts.

**Why not HTMX**: HTMX's strength is server-rendered HTML fragments — the right fit for traditional multi-page apps. This dashboard has live quote streaming, interactive sliders, and complex financial charts (ECharts needs a DOM node to mount into). Vanilla JS with `fetch()` and Web Components handles all of this cleanly without adding a library dependency.

**Why not React**: No build pipeline, no `node_modules`, no hydration complexity. The dashboard is a local tool — component reuse via Web Components is sufficient and keeps the Go binary self-contained via `//go:embed`.

**Stack**:
- `//go:embed static/*` — all HTML/CSS/JS embedded in the Go binary
- ECharts for: portfolio weight donut, drift timeline line chart, backtest equity curve, candlestick if needed
- Web Components for: `<portfolio-summary>`, `<weight-slider>`, `<holdings-table>`, `<alert-banner>`
- Server-Sent Events (SSE) or WebSocket for live quote streaming to avoid polling

**Directory layout for R8**:
```
static/
├── index.html
├── css/
│   └── app.css
├── js/
│   ├── app.js
│   └── components/
│       ├── portfolio-summary.js
│       ├── weight-slider.js
│       └── holdings-table.js
└── vendor/
    └── echarts.min.js     # vendored, no CDN dependency
```

---

### D4 — Alert Channels: Defer email/SMTP

**Decision**: Telegram and Discord webhook implementations proceed in R5. Email (SMTP) is deferred — implement the `Alerter` interface with a placeholder `EmailAlerter` struct but leave `Send()` returning `errors.New("email alerter not yet implemented")`. Wire it in when other fundamentals are stable.

---

### D5 — Price Cache: DuckDB via `github.com/duckdb/duckdb-go/v2`

**Decision**: Use **DuckDB** via `github.com/duckdb/duckdb-go/v2` as the persistent price and fundamentals cache.

**Correction from initial draft**: CGO is not a build constraint — DuckDB is already a live production dependency across multiple services (sanvasify on AWS, voice-provisioning-tool, eia-api-explorer, patscape on OCI). The `go-duckdb` binary bindings ship pre-built per-platform via go module deps — no manual gcc step required. The "pure Go" justification for SQLite doesn't apply in this environment.

**Why DuckDB over SQLite**: For the backtesting workloads in R7 (rolling windows, multi-ticker correlations, period returns across 250+ tickers, percentile rank scoring), DuckDB's columnar execution is the right tool. The same query pattern already in use in sanvasify's `GetSchemeReturns` — CTEs with `INTERVAL` arithmetic and multi-period lookbacks — maps directly to what mycase's backtesting engine needs. DuckDB also natively reads Parquet, supports `TRY_CAST`, and has `INSERT OR REPLACE` / `ON CONFLICT DO UPDATE` upsert syntax already in use across the codebase.

**Driver**: `github.com/duckdb/duckdb-go/v2` (official DuckDB Go driver — same as sanvasify). Uses standard `database/sql` interface: `sql.Open("duckdb", path)`.

**Schema** (`data/cache.db`) — following sanvasify's `InitSchema` pattern:
```go
func (c *Cache) InitSchema(ctx context.Context) error {
    _, err := c.db.ExecContext(ctx, `
        CREATE TABLE IF NOT EXISTS prices (
            ticker  VARCHAR NOT NULL,
            date    DATE    NOT NULL,
            close   DOUBLE  NOT NULL,
            open    DOUBLE,
            volume  DOUBLE,
            PRIMARY KEY (ticker, date)
        );

        CREATE TABLE IF NOT EXISTS fundamentals (
            ticker       VARCHAR PRIMARY KEY,
            fetched_at   TIMESTAMP NOT NULL,
            sector       VARCHAR,
            market_cap   DOUBLE,
            roe          DOUBLE,
            forward_pe   DOUBLE,
            peg_ratio    DOUBLE,
            pb_ratio     DOUBLE,
            operating_margins DOUBLE,
            net_debt_ebitda   DOUBLE,
            insiders_pct      DOUBLE,
            ttm_revenue  DOUBLE,
            avg_volume   DOUBLE,
            debt_to_equity    DOUBLE,
            -- annual time-series stored as JSON arrays (DuckDB JSON support)
            annual_revenue     JSON,
            annual_capex       JSON,
            annual_net_ppe     JSON,
            annual_ar          JSON,
            annual_op_income   JSON,
            raw_json           JSON   -- full Fundamentals struct as escape hatch
        );

        CREATE TABLE IF NOT EXISTS cache_meta (
            ticker     VARCHAR NOT NULL,
            range_key  VARCHAR NOT NULL,  -- e.g. "1y", "3mo"
            fetched_at TIMESTAMP NOT NULL,
            PRIMARY KEY (ticker, range_key)
        );
    `)
    return err
}
```

**Upsert pattern** (consistent with sanvasify's `ON CONFLICT DO UPDATE`):
```go
_, err = tx.ExecContext(ctx, `
    INSERT INTO prices (ticker, date, close, open, volume)
    VALUES (?, strptime(?, '%Y-%m-%d')::DATE, ?, ?, ?)
    ON CONFLICT (ticker, date) DO UPDATE SET
        close  = EXCLUDED.close,
        open   = EXCLUDED.open,
        volume = EXCLUDED.volume
`, ticker, dateStr, close, open, volume)
```

**Staleness policy**:
- Price data for past trading days: permanent (close price never changes)
- Price data for current date: stale before 15:30 IST (consistent with `CleanIntradayNoise()`)
- Fundamentals: stale after 24 hours

**Backtesting window queries (R7)** — DuckDB columnar window functions:
```sql
-- Rolling 200-day SMA for all portfolio tickers — single query, one pass
SELECT ticker, date, close,
    AVG(close) OVER (
        PARTITION BY ticker
        ORDER BY date
        ROWS BETWEEN 199 PRECEDING AND CURRENT ROW
    ) AS sma200,
    AVG(volume) OVER (
        PARTITION BY ticker
        ORDER BY date
        ROWS BETWEEN 19 PRECEDING AND CURRENT ROW
    ) AS vol_ma20
FROM prices
WHERE ticker IN (SELECT UNNEST(?::VARCHAR[]))
  AND date BETWEEN ? AND ?
ORDER BY ticker, date;

-- Period return calculation (mirrors sanvasify GetSchemeReturns pattern)
WITH latest AS (
    SELECT ticker, MAX(date) AS max_date FROM prices GROUP BY ticker
),
cur AS (SELECT p.ticker, p.close, p.date FROM prices p JOIN latest l ON p.ticker=l.ticker AND p.date=l.max_date),
p3m AS (SELECT p.ticker, p.close FROM prices p JOIN latest l ON p.ticker=l.ticker
        WHERE p.date=(SELECT MAX(date) FROM prices WHERE ticker=p.ticker AND date <= l.max_date - INTERVAL '3 months'))
SELECT c.ticker,
    (c.close - m.close) / m.close * 100 AS ret_3m
FROM cur c LEFT JOIN p3m m ON c.ticker = m.ticker;
```

**Package**: `pkg/cache/` (new) — following sanvasify's `pkg/db/` pattern:
```
pkg/cache/
├── db.go           # New(path), InitSchema, Close — mirrors sanvasify pkg/db/db.go
├── prices.go       # GetPrices, StorePrices, IsPriceStale
└── fundamentals.go # GetFundamentals, StoreFundamentals
```

`pkg/yfinance/prices.go` calls `cache.GetPrices(ticker, rangeKey)` first; on miss or stale, fetches from Yahoo and calls `cache.StorePrices(...)`.

---

## 7. Go 1.26.x Modernization — Full Stack Update

Go releases every 6 months with no LTS designation — always run the current stable. Upgrading from 1.24.4 → 1.26.x and updating all dependencies.

### 7.1 `go.mod` Updates

```
go 1.26.3   // confirmed current stable (sanvasify is already on 1.26.3)

require (
    github.com/urfave/cli/v3                       v3.x.x   // NEW: unified CLI framework
    github.com/duckdb/duckdb-go/v2                 v2.x.x   // NEW: price/fundamentals cache (same as sanvasify)
    github.com/zerodha/gokiteconnect/v4            v4.x.x   // update to latest
    github.com/gocarina/gocsv                      v0.x.x   // update from 2018 pin
    gopkg.in/yaml.v3                               v3.x.x   // update
)
```

Note: `github.com/google/go-querystring` is a transitive dep of gokiteconnect — moves to `// indirect` after `go mod tidy`. DuckDB platform bindings (`duckdb-go-bindings/darwin-arm64`, `linux-amd64`, etc.) appear as `// indirect` automatically.

**urfave/cli v3 vs v2**: v3 (stable as of 2025) has cleaner flag definitions, proper `context.Context` threading through `Action` funcs, and better shell completion. Use v3 since we're starting fresh.

### 7.2 Go Language Features to Apply (1.22 → 1.26)

| Feature | Go version | Where to apply |
|---------|-----------|----------------|
| `range N` (range over integer) | 1.22 | Replace `for i := 0; i < n; i++` worker pool loops in yfinance |
| `math/rand/v2` | 1.22 | Replace `rand.New(rand.NewSource(42))` in mock generator with `rand/v2` |
| `slices.SortFunc`, `slices.Contains` | 1.21 | Replace all `sort.Interface` boilerplate types |
| `maps.Keys`, `maps.Values` | 1.21 | Replace manual key-extraction loops |
| `min()` / `max()` builtins | 1.21 | Replace `math.Min(float64(a), float64(b))` for int comparisons |
| `log/slog` | 1.21 | Replace `fmt.Fprintf(os.Stderr, ...)` and `log.Printf` calls |
| `iter` package + range over func | 1.23 | Iterate over portfolio holdings, candidate lists |
| `context.Context` in HTTP calls | best practice | Add to all `pkg/yfinance` fetch functions |
| Generic type aliases | 1.24 | Simplify `resolveFirst[T]()` YAML helper if retained |

### 7.3 `gocarina/gocsv` — Replace or Update

The current pin (`v0.0.0-20180809181117`) is from 2018 and is marked `// indirect`. Latest version has significantly improved error handling and struct tag support. Run:
```bash
go get github.com/gocarina/gocsv@latest
go mod tidy
```
Verify `pkg/csvloader/loader_test.go` still passes — it's the regression test for this package.

---

## 8. Comprehensive Testing Plan

### 8.1 Strategy & Principles

Testing a financial CLI tool requires confidence at three distinct levels:

1. **Pure logic correctness** — math functions (RSI, Sharpe, Sortino, normalizeValue, capWeights) must be exact. Table-driven unit tests with ±ε tolerances are the primary tool.
2. **Behavioral correctness** — command flag parsing, file I/O flows, error paths. Integration tests drive the real `cmd.Run(ctx, args)` without network.
3. **Invariant correctness** — financial constraints that must hold for any input (weights sum to 1.0, no weight exceeds cap, RSI ∈ [0,100]). Property-based and fuzz tests catch edge cases that hand-crafted examples miss.

**Core rules:**
- No test may make real HTTP calls to Yahoo Finance or Zerodha. All network-dependent tests are behind `//go:build integration` and run separately (`make test-integration`).
- Tests in the `cmd` package must not write to the repo's `data/`, `report/`, or `config/` directories. All file I/O in cmd tests uses `t.TempDir()`.
- `go test -race ./...` must pass clean. Every goroutine that shares state gets a race test.
- Each test file lives next to its source in the same package (white-box), except `cmd/` integration tests which live in `cmd_test` (black-box, package suffix `_test`).

**Test dependency matrix (current state vs. target):**

| Package | Unit | Table | Property | Fuzz | Integration | Golden | Benchmark |
|---------|------|-------|----------|------|-------------|--------|-----------|
| `pkg/csvloader` | ✅ partial | ✅ | ❌ | ❌ | ❌ | ❌ | ❌ |
| `pkg/stockpicker` | ✅ partial | ✅ | ❌ | ❌ | ❌ | ❌ | ❌ |
| `pkg/optimizer` | ✅ partial | ✅ | ❌ | ❌ | ❌ | ❌ | ❌ |
| `pkg/monitoring` | ✅ partial | ✅ | ❌ | ❌ | ❌ | ❌ | ❌ |
| `pkg/yfinance` | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| `pkg/config` | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| `cmd` | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |

Target: all cells filled where applicable.

---

### 8.2 Test Tooling

**Stdlib only (no new deps for unit/property/fuzz):**
- `testing` — all test types
- `testing/quick` — property-based tests (basic; no shrinking)
- `go test -fuzz` (Go 1.18+) — native fuzzing

**One new test dependency — `pgregory.net/rapid` v1:**
```bash
go get -t pgregory.net/rapid
```
Use `rapid` instead of `testing/quick` for financial invariant tests. Its distinguishing feature is **automatic shrinking**: when a failing input is found, it reduces it to the minimal counter-example. For a function like `capWeights`, a failing input with 50 tickers shrinks to the 2-ticker case that reveals the bug. `testing/quick` cannot do this.

**`net/http/httptest`** (stdlib) — mock HTTP server for yfinance tests. No separate mock library needed.

---

### 8.3 `pkg/csvloader` — Tests to Add

**Existing**: `TestGetUniverseName` (9 table cases).

**Add `loader_test.go`:**

```go
// TestLoadBasketCSV_Valid — header variants, NSE: prefix normalization
// TestLoadBasketCSV_MissingHeader — returns error, not panic
// TestLoadBasketCSV_EmptyFile — returns empty map, empty keys, no error  
// TestLoadBasketCSV_DuplicateTicker — last row wins (or error — define expected behavior)
// TestLoadBasketCSV_WeightParsing — scientific notation, commas, negative weights

// TestCombineMultipleCSVs_Basic — 3 files, tickers deduplicated, weights summed/replaced
// TestCombineMultipleCSVs_SingleFile — passthrough case
// TestCombineMultipleCSVs_EmptyFile — skips silently
// TestCombineMultipleCSVs_MissingFile — returns error

// TestMergeGoldenCopy_NewTickers — tickers not in golden get added
// TestMergeGoldenCopy_ExitedTickers — tickers in golden but not in src get weight 0.0000
// TestMergeGoldenCopy_WeightUpdate — existing tickers get updated weight
// TestMergeGoldenCopy_NoChangeNeeded — idempotent merge returns no error
```

**Fuzz target — `FuzzLoadBasketCSV`:**
```go
func FuzzLoadBasketCSV(f *testing.F) {
    f.Add("ticker,weight\nNSE:TCS,0.5\n")
    f.Add("")
    f.Add("ticker\nNSE:TCS\n") // missing weight column
    f.Fuzz(func(t *testing.T, input string) {
        r := strings.NewReader(input)
        // must not panic; error is acceptable
        _, _, _ = loadBasketCSVFromReader(r) // requires extracting reader-based variant
    })
}
```
*Prerequisite*: extract `loadBasketCSVFromReader(r io.Reader)` from `LoadBasketCSV(path string)` — standard io.Reader refactor for testability. The path-based function becomes a one-liner wrapper.

**Fuzz target — `FuzzGetUniverseName`:**
```go
func FuzzGetUniverseName(f *testing.F) {
    f.Add("data/microsmall.csv")
    f.Add("")
    f.Add("/")
    f.Add("....csv")
    f.Fuzz(func(t *testing.T, path string) {
        result := GetUniverseName(path)
        if result == "" {
            t.Errorf("GetUniverseName must never return empty string, got %q for input %q", result, path)
        }
    })
}
```

---

### 8.4 `pkg/stockpicker` — Tests to Add

**Existing**: `TestIsAbove200DaySMA`, `TestNormalizeValue` (7 table cases), `TestLoadLocalCSVConstituents`, `TestIsEligible`.

**Add to `stockpicker_test.go`:**

```go
// TestNormalizeValue_Boundary — val == minVal, val == maxVal, val outside range
// TestNormalizeValue_ZeroRange — minVal == maxVal (already covered — verify no div/0)
// TestNormalizeValue_NaN — val is NaN → returns 0 or maxPoints (define behavior)

// TestCalculateSharpe — known returns series, expected Sharpe ≈ expected within ±0.001
// TestCalculateSortino — downside-only series, all-positive series, mixed
// TestCalculateBeta — perfectly correlated with benchmark → beta = 1.0
// TestCalculateAlpha — same as benchmark → alpha ≈ 0.0
// TestCalculateUlcer — constant prices → ulcer = 0.0

// TestScoreStock_Balanced — known inputs → deterministic score
// TestScoreStock_Multibagger — validates each of the 11 safety filter outcomes
// TestApplyRebalanceTolerance — within-tolerance stocks are retained in same rank order
// TestApplyHysteresisBuffer — top-21 request with buffer=5 returns at most top-25 candidates
```

**Property test — `TestNormalizeValue_Invariants`:**
```go
func TestNormalizeValue_Invariants(t *testing.T) {
    f := func(val, lo, hi, max float64) bool {
        if lo >= hi || max <= 0 || math.IsNaN(val) || math.IsInf(val, 0) {
            return true // skip degenerate inputs
        }
        result := normalizeValue(val, lo, hi, max, true)
        return result >= 0 && result <= max+1e-9
    }
    if err := quick.Check(f, nil); err != nil {
        t.Error(err)
    }
}
```

**Property test with `rapid` — `TestScoreOrdering_StableSort`:**
Generates random portfolios of 5–30 stocks with random but valid Fundamentals. After scoring, verifies that re-running `ScoreStocks` with the identical input returns the same ranking (determinism invariant). Any nondeterminism here would cause pipeline to produce different golden copies on repeated runs.

---

### 8.5 `pkg/optimizer` — Tests to Add

**Existing**: `TestOptimizeFreshBuy`, `TestVolatility`, `TestOptimizeInverseVolatility`.

**Gaps**: `capWeights` has no tests despite being a financial-critical function. `OptimizeMultiFactor` has no tests.

**Add `optimizer_test.go`:**

```go
// TestCapWeights_Basic — single stock over cap → weight clamped, sum remains 1.0
// TestCapWeights_AllUnderCap — no capping → output equals input
// TestCapWeights_CapTooTight — cap < 1/N → equal weight fallback triggered
// TestCapWeights_SingleStock — N=1, any cap → weight = 1.0
// TestCapWeights_ZeroWeights — some stocks have weight 0.0 → excluded from redistribution
// TestCapWeights_NegativeWeight — should either error or treat as 0 (define behavior)

// TestOptimizeFreshBuy_ExactBudget — budget exactly covers N shares of each
// TestOptimizeFreshBuy_InsufficientBudget — returns all zeros, no partial allocations
// TestOptimizeFreshBuy_SingleStock — 100% weight, full budget
// TestOptimizeFreshBuy_ZeroPrice — stock with price=0.0 → skipped, no div/0 panic

// TestCalculateDailyReturns_Empty — empty slice → empty returns (not panic)
// TestCalculateDailyReturns_SinglePrice — one price → empty returns
// TestCalculateVolatility_ConstantReturns — all same → 0.0
// TestCalculateVolatility_Empty — empty → 0.0 (no NaN/Inf)
```

**Property tests with `rapid` — `TestCapWeights_Invariants`:**
```go
func TestCapWeights_Invariants(t *testing.T) {
    rapid.Check(t, func(t *rapid.T) {
        n := rapid.IntRange(1, 30).Draw(t, "n")
        cap := rapid.Float64Range(0.01, 1.0).Draw(t, "cap")
        // Generate n weights that sum to 1.0
        weights := drawNormalizedWeights(t, n)
        result := capWeights(weights, cap)
        var total float64
        for _, w := range result {
            if w > cap+1e-9 {
                t.Fatalf("weight %f exceeds cap %f", w, cap)
            }
            total += w
        }
        if math.Abs(total-1.0) > 1e-9 {
            t.Fatalf("weights sum to %f, expected 1.0", total)
        }
    })
}
```

**Benchmark — `BenchmarkOptimizeMultiFactor`:**
```go
func BenchmarkOptimizeMultiFactor(b *testing.B) {
    // 25-ticker portfolio, 3mo price history (63 days each)
    // Measures time to run one full MFS optimization pass
    // Target: < 50ms per run (it runs synchronously in pipeline)
}
```

---

### 8.6 `pkg/monitoring` — Tests to Add

**Existing**: `TestGetCapStallSeverity` (4 table cases), `TestRunSimulation` (minimal).

**Gaps**: Mock data generation determinism is untested. The simulator's rebalancing logic has no edge case coverage.

**Add `simulator_test.go`:**

```go
// TestRunSimulation_SingleStock — portfolio with one stock at weight=1.0
// TestRunSimulation_ZeroCapital — capital=0 → error or zero returns (define behavior)
// TestRunSimulation_FlatPrices — all prices constant → return=0, drawdown=0
// TestRunSimulation_AllDecline — all stocks fall to 0 → drawdown ≈ 100%, no panic
// TestRunSimulation_ShortHistory — fewer days than SMADays → handles gracefully
// TestRunSimulation_Determinism — same inputs, same seed → identical SimulationResult (bit-for-bit)

// TestGetCapStallSeverity_Boundary — ttmGrowth == cagr3y exactly (boundary between None/Mild)
// TestGetCapStallSeverity_NegativeGrowth — both negative → should still classify correctly
```

**Mock determinism test:**
```go
func TestMockDataDeterminism(t *testing.T) {
    portfolio := []StockInfo{{Ticker: "NSE:TEST", Weight: 1.0}}
    data1 := GenerateMockPortfolioData(portfolio, 250)
    data2 := GenerateMockPortfolioData(portfolio, 250)
    // Seeded with 42 — must produce identical slices every run
    if !reflect.DeepEqual(data1, data2) {
        t.Error("mock data generator is not deterministic with fixed seed")
    }
}
```
This test is a canary for the seed-42 determinism guarantee used across the codebase. If R3.6 (`math/rand/v2` migration) accidentally breaks the seed, this test catches it.

---

### 8.7 `pkg/yfinance` — Tests to Add (No Existing Tests)

This is the largest gap. Every command ultimately calls into yfinance, yet it has zero tests. The primary obstacle is HTTP dependency — solved with `httptest.NewServer`.

**Refactor prerequisite**: Extract a `baseURL string` field (or constructor option) from the package-level `const`. Functions currently hardcode `https://query1.finance.yahoo.com`. Add:
```go
var yfinanceBaseURL = "https://query1.finance.yahoo.com" // overrideable in tests
```
Then tests set `yfinance.SetBaseURLForTesting(ts.URL)` (unexported via `export_test.go`).

**Pure math tests — no HTTP needed:**
```go
// TestCalculateRSI_AllUp — 15 sessions all up 1% → RSI near 100
// TestCalculateRSI_AllDown — 15 sessions all down 1% → RSI near 0
// TestCalculateRSI_Alternating — up/down alternating → RSI near 50
// TestCalculateRSI_Insufficient — fewer than 14 prices → returns 50.0 (neutral)
// TestCalculateRSI_Bounds — for any 14+ price series, result ∈ [0, 100]

// TestCalculateSalesGrowth_Accelerating — TTM > CAGR → passed=true
// TestCalculateSalesGrowth_Decelerating — TTM < CAGR → passed=false
// TestCalculateSalesGrowth_InsufficientHistory — < 3 annual revenue points → passed=false
// TestCalculateSalesGrowth_NegativeRevenue — revenue turns negative mid-series → no panic

// TestCalculateDSO_Normal — 2 years A/R and revenue data → correct days calculation
// TestCalculateDSO_MissingAR — empty AnnualAR → passed=false, no panic
// TestCalculateDSO_ZeroRevenue — division-by-zero guard

// TestCalculateAssetTurnoverCapEx_Improving — asset turnover up YoY, capex flat → passed=true
// TestCalculateAssetTurnoverCapEx_CapExSpike — capex > 1.15× last year → passed=false
// TestCalculateAssetTurnoverCapEx_MissingData — empty slices → passed=false, no panic

// TestCheckVolumeBreakout_Clear — strong green day on 3× average volume → true
// TestCheckVolumeBreakout_Insufficient — lookback > available data → false (no panic)
// TestCheckVolumeBreakout_NoBreakout — volume always below multiplier × avg → false

// TestMapTickerToYahoo — NSE:TCS → TCS.NS, BSE:500112 → 500112.BO, ^NSEI → ^NSEI
// TestCleanIntradayNoise — today's after-15:30 IST data is stripped
```

**HTTP mock tests — use `httptest.NewServer`:**
```go
// TestFetchHistoricalPrices_Success — fixture JSON with known price series
// TestFetchHistoricalPrices_RateLimit — 429 response → returns wrapped error, not panic
// TestFetchHistoricalPrices_EmptyResult — valid JSON, zero events → returns empty slice
// TestFetchHistoricalPrices_MalformedJSON — garbage body → returns error
// TestFetchFundamentals_Success — fixture JSON matching Fundamentals struct fields
// TestFetchFundamentals_PartialData — missing fields → zero values, no panic
// TestFetchQuotes_MultiTicker — batch quote response for 5 tickers
// TestFetchQuotes_UnknownTicker — ticker not in response → value=0.0, no key error
```

**Fixture JSON files** live in `pkg/yfinance/testdata/`:
- `testdata/historical_1y_TCS.json` — 252 trading days of TCS.NS
- `testdata/fundamentals_TCS.json` — full fundamentals response
- `testdata/quotes_batch.json` — 5-ticker quote response
- `testdata/historical_empty.json` — valid envelope, zero results array

**Fuzz target — `FuzzParseFundamentalsJSON`:**
```go
func FuzzParseFundamentalsJSON(f *testing.F) {
    b, _ := os.ReadFile("testdata/fundamentals_TCS.json")
    f.Add(b)
    f.Add([]byte("{}"))
    f.Add([]byte("null"))
    f.Fuzz(func(t *testing.T, data []byte) {
        // parseFundamentalsResponse is the internal JSON unmarshal helper
        // must not panic; zero-value Fundamentals is acceptable on bad input
        _, _ = parseFundamentalsResponse(data)
    })
}
```

**Property test — `TestRSI_AlwaysInBounds`:**
```go
func TestRSI_AlwaysInBounds(t *testing.T) {
    rapid.Check(t, func(t *rapid.T) {
        n := rapid.IntRange(5, 500).Draw(t, "n")
        prices := make([]float64, n)
        for i := range prices {
            prices[i] = rapid.Float64Range(0.01, 10000.0).Draw(t, "price")
        }
        rsi := CalculateRSI(prices)
        if rsi < 0 || rsi > 100 {
            t.Fatalf("RSI(%v...) = %f: must be in [0, 100]", prices[:min(3, n)], rsi)
        }
    })
}
```

---

### 8.8 `pkg/config` — Tests to Add (No Existing Tests)

```go
// TestLoadMFSConfig_Balanced — loads config/mfs.json "balanced" section, spot-checks weights
// TestLoadMFSConfig_UnknownMethod — falls back to defaults, no error
// TestLoadMFSConfig_MissingFile — returns defaults, no error (graceful degradation)
// TestLoadMFSConfig_MalformedJSON — returns error
// TestLoadMFSConfig_NegativeWeights — loaded weights are non-negative

// TestPipelineConfig_UnmarshalYAML_Defaults — minimal YAML, all defaults populated
// TestPipelineConfig_UnmarshalYAML_ListValues — strategy: [balanced, multibagger] → takes first
// TestPipelineConfig_UnmarshalYAML_NegativeTolerance — negative rebalance_tolerance → clamped to 0.10
// TestPipelineConfig_UnmarshalYAML_ZeroTopN — top_n=0 → clamped to default (20)
```

**Fuzz target — `FuzzPipelineConfigYAML`:**
```go
func FuzzPipelineConfigYAML(f *testing.F) {
    f.Add([]byte("indices: [nifty50]\nstrategy: balanced\ntop_n: 20\n"))
    f.Add([]byte("{}"))
    f.Add([]byte("strategy: [a, b, c]\ntop_n: [[1,2],[3,4]]\n"))
    f.Fuzz(func(t *testing.T, data []byte) {
        var cfg PipelineConfig
        _ = yaml.Unmarshal(data, &cfg)
        // must not panic; invalid YAML returns error
    })
}
```

---

### 8.9 `cmd` Package — CLI Integration Tests

The `cmd` package has zero tests today. Each subcommand needs two tiers:

**Tier 1 — Flag parsing (no I/O, no network):** Verify that required flags are enforced, unknown flags return errors, help text renders without panic.

**Tier 2 — File I/O integration (local files, no network):** Run the command with a fixture CSV in `t.TempDir()`. Verify the output file exists and has valid content.

**Test file**: `cmd/cmd_integration_test.go` (package `cmd_test`)

```go
package cmd_test

import (
    "context"
    "os"
    "path/filepath"
    "testing"

    mycmd "github.com/raghavkgarg/mycase/cmd"
    "github.com/urfave/cli/v3"
)

func newApp() *cli.Command {
    return &cli.Command{
        Name: "mycase",
        Commands: []*cli.Command{
            mycmd.PickCommand, mycmd.OptimizeCommand,
            mycmd.ReportCommand, mycmd.PerformanceCommand,
            mycmd.MergeCommand,
        },
    }
}

// TestPickCommand_Help — mycase pick --help exits 0, output contains "pick"
// TestPickCommand_MissingArgs — mycase pick (no --index, no --file) → error
// TestPickCommand_UnknownFlag — mycase pick --notaflag → error
// TestPickCommand_InvalidMethod — mycase pick --file f.csv --method invalid → error or fallback

// TestOptimizeCommand_InvalidRange — --range 5yr → error
// TestOptimizeCommand_CapBelowZero — --cap -0.1 → error or clamp to 0
// TestOptimizeCommand_WithFixture — --file testdata/basket.csv → output CSV in tempdir

// TestReportCommand_MissingFile — mycase report → error (--file required)
// TestReportCommand_NonExistentFile — --file /nonexistent.csv → error with path in message
// TestReportCommand_WithFixture — --file testdata/golden.csv → report file written to report/

// TestPerformanceCommand_InvalidDate — --date notadate → error
// TestPerformanceCommand_InvalidTime — --time 25:99 → error
// TestPerformanceCommand_InvalidCapital — --capital -1000 → error or clamp

// TestMergeCommand_Combine_TwoFiles — merge combine a.csv b.csv → output contains all tickers
// TestMergeCommand_Golden_MissingArgs — merge golden (no args) → error

// TestParsePerfDate_Formats — "2026-01-15", "20260115", "" (today), "baddate" → verify
// TestCleanBasketArg_LeadingDashes — "--MICROSMALL" → "MICROSMALL"
// TestResolveFirst_Types — int, string, float64, nil, list → expected type extraction
```

**Fixture files**: `cmd/testdata/`
- `basket.csv` — 5 tickers with weights
- `golden.csv` — 10 tickers matching real golden copy structure
- `pipeline.yaml` — minimal valid config

**Important**: Commands that open `data/` or `report/` relative to CWD need to be run with `os.Chdir(t.TempDir())` or accept a working directory parameter. If the latter refactor is not done, use `t.Chdir()` (Go 1.24+, sets CWD for the test and restores it after).

---

### 8.10 Fuzz Testing Summary

All fuzz targets, their corpus seeds, and expected invariants:

| Target | File | Invariant |
|--------|------|-----------|
| `FuzzLoadBasketCSV` | `pkg/csvloader/csvloader_fuzz_test.go` | No panic; error OK |
| `FuzzGetUniverseName` | `pkg/csvloader/csvloader_fuzz_test.go` | Returns non-empty string |
| `FuzzParseFundamentalsJSON` | `pkg/yfinance/yfinance_fuzz_test.go` | No panic |
| `FuzzPipelineConfigYAML` | `pkg/config/config_fuzz_test.go` | No panic |
| `FuzzParsePerfDate` | `cmd/cmd_fuzz_test.go` | No panic; valid time or error |
| `FuzzCleanBasketArg` | `cmd/cmd_fuzz_test.go` | No panic; result has no leading dashes |
| `FuzzResolveFirst_String` | `cmd/cmd_fuzz_test.go` | Never panics for any interface{} |

Run all fuzz targets for 60 seconds each in CI:
```bash
go test -fuzz=FuzzLoadBasketCSV -fuzztime=60s ./pkg/csvloader/
```

Fuzz corpus entries are checked into `testdata/fuzz/<FuzzFuncName>/` alongside each package.

---

### 8.11 Property-Based Testing with `rapid`

Install once:
```bash
go get -t pgregory.net/rapid@latest
```

Key invariants to verify across random inputs:

| Invariant | Package | Test name |
|-----------|---------|-----------|
| `∀k: capWeights(w,c)[k] ≤ c+ε` and `Σw ≈ 1.0` | `optimizer` | `TestCapWeights_Invariants` |
| `OptimizeInverseVolatility`: `Σw ≈ 1.0`, all `w > 0` | `optimizer` | `TestInverseVol_SumsToOne` |
| `CalculateRSI(prices) ∈ [0, 100]` for any valid prices | `yfinance` | `TestRSI_AlwaysInBounds` |
| `normalizeValue(v, lo, hi, max, _) ∈ [0, max]` | `stockpicker` | `TestNormalize_Bounded` |
| `MergeGoldenCopy`: exited tickers have `weight == 0.0000` | `csvloader` | `TestMergeGolden_ExitedWeight` |
| `CombineMultipleCSVs`: no ticker appears twice in output | `csvloader` | `TestCombine_NoDuplicates` |
| `OptimizeFreshBuy`: total spend ≤ budget | `optimizer` | `TestFreshBuy_BudgetConstraint` |
| `ScoreStocks` is deterministic: same input → same order | `stockpicker` | `TestScore_Deterministic` |

**Stateful property test** — `TestPortfolioCycleStability`:
Simulate a sequence of operations: generate random basket → optimize → merge golden → optimize again. After 3 cycles, verify that weights are stable (the pipeline converges rather than oscillating). This catches feedback-loop bugs in the rebalancing logic.

---

### 8.12 Golden File Tests

Golden file tests capture command output (report text, CSV structure) and detect unintended regressions in output format. Useful for `report`, `monitor`, and `performance` where the output is human-readable text.

**Pattern:**
```go
func TestReportOutput_Balanced_Golden(t *testing.T) {
    // Run report with fixture golden.csv + mocked yfinance HTTP
    // Compare output file against testdata/golden/report_balanced.txt
    // On first run (no golden file): write and pass
    // On subsequent runs: diff; fail if changed
    // To update: DELETE the golden file and re-run
}
```

**Golden files** stored in `testdata/golden/`:
- `report_balanced.txt` — full report for 5-ticker fixture portfolio
- `report_multibagger.txt` — multibagger strategy report
- `monitor_output.txt` — monitor command output for fixture + mock prices
- `performance_output.txt` — performance simulation tabular output

Update golden files with:
```bash
UPDATE_GOLDEN=1 go test ./cmd/...
```

The test checks `os.Getenv("UPDATE_GOLDEN") != ""` before writing vs. comparing.

---

### 8.13 Benchmark Tests

Focus on functions called in the hot path of `pipeline` (sequential), `pick` (concurrent yfinance fetches), and `optimize` (math-heavy).

| Benchmark | File | Measure |
|-----------|------|---------|
| `BenchmarkOptimizeInverseVolatility_25` | `pkg/optimizer/` | 25-stock portfolio, 3mo prices |
| `BenchmarkOptimizeMultiFactor_25` | `pkg/optimizer/` | 25-stock portfolio with fundamentals |
| `BenchmarkCapWeights_25` | `pkg/optimizer/` | 25-stock portfolio, cap=0.10 |
| `BenchmarkCalculateRSI` | `pkg/yfinance/` | 252-day price series |
| `BenchmarkScoreStocks_25` | `pkg/stockpicker/` | 25-stock scoring pass |
| `BenchmarkCombineMultipleCSVs_3x25` | `pkg/csvloader/` | 3 files × 25 tickers each |

Run with:
```bash
go test -bench=. -benchmem -count=5 ./pkg/optimizer/ ./pkg/yfinance/ ./pkg/stockpicker/
```

Performance budgets (approximate targets, not hard CI gates initially):
- `capWeights(25 stocks)` < 1 µs
- `OptimizeInverseVolatility(25 stocks, 63 days)` < 100 µs
- `ScoreStocks(25 stocks)` < 500 µs

---

### 8.14 Error Path Coverage Checklist

For each command, verify these error conditions return a wrapped, descriptive error (not panic, not silent swallow):

**`pick`**
- [ ] Index name not in `pkg/datafetcher` lookup table → `"unknown index: X"`
- [ ] `--file` path does not exist → OS error with path in message
- [ ] `--file` CSV has no `ticker` column → format error
- [ ] `--range` is unsupported → `"unsupported range 'X'"`
- [ ] `--top` is 0 or negative → error or default

**`optimize`**
- [ ] `--file` does not exist → error
- [ ] `--method` is unknown → warning + fallback to `volatility`
- [ ] `--cap` > 1.0 → clamp to 1.0 with warning
- [ ] `--remove` contains a ticker not in basket → warning only (not error)
- [ ] All tickers removed via `--remove` → `"no active tickers remaining"`

**`report`**
- [ ] `--file` required but missing → urfave/cli required-flag error
- [ ] CSV exists but has < 2 rows → `"CSV file contains no data rows"`
- [ ] `report/` directory not writable → OS error with path

**`performance`**
- [ ] `--date` in wrong format → `"invalid date format: X"`
- [ ] `--time` in wrong format → `"invalid time format"`
- [ ] `--capital` = 0 → all allocations = 0, returns = 0 (not div/0)
- [ ] CSV has no data rows → error

**`basket`**
- [ ] No portfolio name arg and `--file` not specified → error
- [ ] `--file` does not exist → error
- [ ] Zerodha auth token missing/expired → descriptive auth error (not raw JSON)

**`pipeline`**
- [ ] `--config` file not found → error with path
- [ ] YAML config has no `indices` → `"no indices configured"`
- [ ] Individual step failure propagates with step number: `"step 2 (pick nifty50): ..."`

**`merge combine`**
- [ ] Fewer than 2 file args → usage error
- [ ] One of the source files does not exist → error with filename

---

### 8.15 Race Condition Tests

The following concurrent patterns in the codebase need `go test -race` verification:

| Pattern | Location | Risk |
|---------|----------|------|
| Concurrent `FetchHistoricalPrices` goroutines writing to `priceHistory map` | `cmd/optimize.go`, `cmd/report.go` | data race on map write |
| Concurrent `FetchFundamentals` goroutines | `cmd/report.go` | same map race |
| `RunSimulation` internal goroutines writing `holdings` state | `pkg/monitoring/simulator.go` | TBD on inspection |
| `yfinance.saveToCache` / `loadFromCache` file I/O concurrent calls | `pkg/yfinance/prices.go` | concurrent file writes |

Add `go test -race ./...` to `make test-race` (already in Makefile). Promote to required CI gate. The existing `monitoring` and `optimizer` tests pass under `-race` — verify `csvloader` and `stockpicker` too after adding concurrent tests.

---

### 8.16 Integration Tests (Network, `//go:build integration`)

These tests make real HTTP calls to Yahoo Finance and are excluded from the default `make test` run. They verify the full live data pipeline works end-to-end.

**Build tag**: `//go:build integration`  
**Run**: `make test-integration` → `go test -tags=integration -timeout=120s ./...`

```go
//go:build integration

// TestFetchHistoricalPrices_Live_TCS — fetches TCS.NS 3mo data, verifies ≥ 60 prices
// TestFetchFundamentals_Live_Reliance — fetches RELIANCE.NS fundamentals, spot-checks ForwardPE > 0
// TestPickCommand_Live_Nifty50 — runs pick --index nifty50 --top 5, verifies 5 tickers returned
// TestMonitorCommand_Live_Fixture — runs monitor --file testdata/golden.csv with live yfinance data
```

Integration tests must:
- Not write to real `data/` or `report/` directories
- Use `t.TempDir()` for all output
- Respect `MYCASE_SKIP_INTEGRATION=1` env var for offline environments
- Have a 30-second per-test timeout

---

### 8.17 Phase-by-Phase Testing Gates

Each refactor phase ships with test coverage as a gate to merge:

| Phase | Gate |
|-------|------|
| **R1 (done)** | `go test ./...` passes; `go test -race ./...` passes |
| **R2** | All new `pkg/` packages have ≥ 80% statement coverage; `cmd/` integration tests pass |
| **R3** | All fuzz targets added; `make cleanup` (staticcheck + govulncheck) passes |
| **R-cache** | `pkg/cache/` has schema tests, CRUD round-trip tests, staleness policy tests |
| **R4** | `pkg/broker/` has interface compliance tests; Zerodha implementation passes mock test |
| **R5** | `pkg/alert/` has mock Alerter test; daemon state persistence round-trip test |
| **R7** | `pkg/backtest/` has simulation tests with known CAGR, Sharpe from fixture data |
| **R8** | `pkg/server/` has HTTP handler tests using `httptest.NewRecorder` |

**Coverage target**: `go tool cover -func=coverage.out` must show ≥ 80% for each package in R1–R3. Not enforced as a hard gate for later phases (R7, R8 are exploratory) but tracked.

```makefile
test-coverage:
	@go test -coverprofile=coverage.out ./...
	@go tool cover -func=coverage.out | grep -E "^(total|github)" | tail -1
	@go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report: coverage.html"
```
