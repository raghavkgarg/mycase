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

**Decision**: Rename `module mycase` → `module github.com/[username]/mycase`.

**Rationale**: One-time find-replace across all import paths. Makes the module correctly identifiable, importable if ever open-sourced, and aligns with Go module conventions. `go mod edit -module github.com/[username]/mycase` followed by a sed pass on all `"mycase/pkg/..."` imports.

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
