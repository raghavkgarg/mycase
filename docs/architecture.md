# Mycase — Architecture & Design Reference

**Module**: `github.com/raghavkgarg/mycase`  
**Go version**: 1.26.5  
**Binary**: `mycase` (single binary, 10 subcommands)

---

## 1. What It Does

`mycase` is a portfolio basket and rebalancing engine for Indian equity markets (NSE/BSE). It covers the full workflow: stock selection → weight optimization → order generation → performance monitoring. Data is sourced from Yahoo Finance (no vendor lock-in for market data) and orders are placed via Zerodha Kite Connect.

---

## 2. CLI Command Hierarchy

```
mycase [global flags]
├── pipeline       Orchestrate full workflow (pick → optimize → report → performance → monitor)
├── pick           Stock selection from an index or CSV file
├── optimize       Weight optimizer (volatility, MFS multi-factor, equal-weight)
├── report         Generate portfolio explanation report
├── performance    Historical backtest simulation
├── monitor        Interactive drift simulator
├── basket         Order execution via Zerodha Kite
├── holdings       Live/mock holdings snapshot
├── merge
│   ├── combine    Merge multiple CSVs into one
│   └── golden     Update golden copy from a proposals CSV
└── auth           Zerodha session setup
```

Key flags per command:

| Command | Key flags |
|---------|-----------|
| `pick` | `--index`, `--file`, `--method`, `--top`, `--range`, `--golden`, `--rebalance-tolerance`, `--hysteresis-buffer` |
| `optimize` | `--file`, `--method`, `--cap`, `--golden`, `--remove` |
| `report` | `--file`, `--method` |
| `performance` | `--file`, `--capital`, `--date`, `--time` |
| `monitor` | `--file`, `--style`, `--strategy`, `--date`, `--interactive` |
| `basket` | `--live`, `[portfolio-name]` |
| `holdings` | `--live` |

---

## 3. Directory Structure

```
mycase/
├── main.go                     # Wires urfave/cli app; injects Version/GitCommit/BuildDate via LDFLAGS
├── cmd/                        # One file per subcommand — thin flag parsing + orchestration only
│   ├── pipeline.go             # Calls other cmds as direct Go function calls
│   ├── pick.go
│   ├── optimize.go
│   ├── report.go
│   ├── performance.go
│   ├── monitor.go
│   ├── basket.go
│   ├── holdings.go
│   ├── merge.go
│   └── auth.go
├── pkg/                        # All business logic
│   ├── cache/                  # DuckDB persistent cache: prices, fundamentals, cache_meta tables
│   ├── config/                 # Broker credentials (config.go); themes; PipelineConfig (pipeline.go)
│   ├── csvloader/              # basket CSV I/O, golden copy merge, pipeline CSV helpers
│   ├── datafetcher/            # Live/mock market data fetch (FetchMarketData via Kite or mock quotes)
│   ├── executor/               # Order execution logic
│   ├── kiteclient/             # Zerodha Kite Connect client wrapper
│   ├── market/                 # Market hours, holiday calendar helpers
│   ├── monitoring/             # Portfolio simulator (types, simulator, mock data); drift scoring
│   ├── optimizer/              # Weight optimization: volatility, MFS multi-factor, fresh-buy, exit detection
│   ├── performance/            # Portfolio valuation: ValuatePortfolio (daily-close + intraday modes)
│   ├── portfolio/              # Holdings type, Zerodha holdings fetch
│   ├── printer/                # Terminal output rendering (tables, holdings snapshot)
│   ├── report/                 # Selection rationale text: BuildRationale (multibagger + standard paths)
│   ├── selectiontracker/       # Records why each stock was kept/removed during pick
│   ├── stockpicker/            # Stock selection: loaders, filters, scoring, I/O
│   └── yfinance/               # Yahoo Finance client: prices, fundamentals, quotes, RSI (all ctx-aware)
├── config/
│   ├── mfs.json                # Scoring weights per strategy (balanced, aggressive, multibagger…)
│   ├── pipeline.yaml           # Pipeline run config (indices, strategy, top-N, tolerances)
│   ├── themes.json             # Portfolio theme → CSV path mapping
│   ├── csvlinks.json           # Index name → URL mapping for datafetcher
│   └── governance.json         # Per-sector governance overrides
└── data/
    ├── *.csv                   # Golden copy files (user data — never touch programmatically)
    ├── backups/                # Auto-backups before golden copy overwrites
    ├── candidates/             # Pick output CSVs
    └── .cache/                 # Yahoo Finance JSON cache (date-stamped, auto-created)
```

**Planned additions** (not yet implemented):
```
pkg/
├── broker/       # R4: Broker interface + pkg/broker/zerodha/
├── alert/        # R5: Alerter interface, Telegram, Discord
├── daemon/       # R5: Background drift monitor
└── backtest/     # R7: Historical backtesting engine
```

---

## 4. cmd/ Pattern (urfave/cli v3)

Each subcommand follows a two-function pattern:

```go
// Public command exported for main.go
var PickCommand = &cli.Command{
    Name:  "pick",
    Flags: []cli.Flag{ /* ... */ },
    Action: runPick,
}

// Action: parses flags → delegates to inner function
func runPick(ctx context.Context, c *cli.Command) error {
    return runPickWithOpts(ctx, pickOptsFromCmd(c))
}

// Inner function: called directly by pipeline.go — no exec.Command
func runPickWithOpts(ctx context.Context, opts *stockpicker.Options) error {
    // thin orchestration — delegates to pkg/
}
```

`pipeline.go` calls the inner functions directly — no subprocess spawning.

### urfave/cli v3 API notes (v3 differs from v2)
- Action signature: `func(ctx context.Context, cmd *cli.Command) error`
- Float flags: `&cli.FloatFlag{}` + `c.Float("name")` (not `Float64Flag`)
- Explicit flag detection: `c.IsSet("name")`
- Positional args: `c.Args().Slice()`, `c.Args().Get(n)`

---

## 5. Data Flow

```
pipeline.yaml
     │
     ▼
pipeline ──► pick ──► optimize ──► report
                           │
                           ▼
                       performance
                           │
                           ▼
                        monitor
                           │
                           ▼
                      basket (--live)
```

All steps are direct Go function calls within the same process. `pipeline.go` reads `pipeline.yaml`, resolves config precedence (CLI flag > YAML > default), and calls each step sequentially.

---

## 6. Config Files

| File | Purpose |
|------|---------|
| `config/pipeline.yaml` | Run parameters: indices, strategy, top-N, ranges, tolerances |
| `config/mfs.json` | Scoring strategy weights (balanced, aggressive, conservative, multibagger) |
| `config/themes.json` | Portfolio themes for holdings grouping |
| `config/csvlinks.json` | Index name → NSE/BSE constituent CSV URL |
| `config/governance.json` | Sector-level governance score overrides |

Config format is backward-compatible through R3 — never break existing `pipeline.yaml` or `mfs.json` files.

---

## 7. Naming Conventions

- Dates: `YYYYMMDD`
- Timestamps: `YYYYMMDD_HHMMSS`
- Report/output files: `lowercase_snake_case`
- Step prefixes in pipeline execution output: `01_`, `02_`, `03_`
- Yahoo Finance ticker mapping: `NSE:TCS` → `TCS.NS`, `BSE:500112` → `500112.BO`, `^NSEI` → `^NSEI`

---

## 8. Testing Strategy

### Principles
- No test may make real HTTP calls to Yahoo Finance or Zerodha. All network-dependent tests use `//go:build integration`.
- cmd tests use `t.TempDir()` for all file I/O — never write to repo's `data/`, `report/`, or `config/`.
- `go test -race ./...` must pass clean.
- White-box unit tests live next to source in the same package; cmd integration tests use `cmd_test` package suffix.

### Test types per package

| Package | Target coverage | Key gaps today |
|---------|----------------|---------------|
| `pkg/csvloader` | 80%+ | fuzz targets, merge/combine tests |
| `pkg/stockpicker` | 80%+ | Sharpe/Sortino/Beta math, scoring invariants |
| `pkg/optimizer` | 80%+ | `capWeights` has no tests |
| `pkg/monitoring` | 80%+ | mock determinism, simulator edge cases |
| `pkg/yfinance` | 80%+ | zero tests today — needs httptest mock server |
| `pkg/config` | 80%+ | zero tests today |
| `cmd/` | integration | zero tests today |

### Test tooling
- `testing/quick` — basic property tests (no shrinking)
- `pgregory.net/rapid` — property tests with automatic shrinking (to add)
- `net/http/httptest` — mock HTTP server for yfinance tests (stdlib)
- `go test -fuzz` — native fuzzing (Go 1.18+)

### Key financial invariants to verify
- `capWeights(w, c)[k] ≤ c+ε` and `Σw ≈ 1.0` for any input
- `CalculateRSI(prices) ∈ [0, 100]` for any valid price series
- `normalizeValue(v, lo, hi, max, _) ∈ [0, max]`
- `ScoreStocks` is deterministic: same input → same ranking
- `OptimizeFreshBuy`: total spend ≤ budget

### yfinance test prerequisite
Add a `baseURL` override for tests: `var yfinanceBaseURL = "https://query1.finance.yahoo.com"` with `SetBaseURLForTesting(url)` exported via `export_test.go`. Functions currently hardcode the URL.

---

## 9. Go 1.26 Modernization Targets

| Feature | Go version | Status | Location |
|---------|-----------|--------|---------|
| `math/rand/v2` | 1.22 | ✅ Done (R3) | `cmd/monitor.go` |
| `slices.SortFunc`, `slices.Sort` | 1.21 | ✅ Done (R3) | `pkg/portfolio/`, `pkg/printer/` |
| `max()` builtin | 1.21 | ✅ Done (R3) | `pkg/printer/printer.go` |
| `range N` (integer range) | 1.22 | n/a | No pure-counter loops found in codebase |
| `maps.Keys`, `maps.Values` | 1.21 | n/a | No manual key-extraction loops found |
| `log/slog` | 1.21 | n/a | No `fmt.Fprintf(stderr)` or `log.Printf` calls |
| `context.Context` in HTTP | best practice | ✅ Done (R3.5) | all `pkg/yfinance` fetch functions + callers |
| Generic type aliases | 1.24 | ✅ Done (R2.5) | `resolveFirst[T]()` in `pkg/config/pipeline.go` |

---

## 10. Design Decisions

### D2 — Daemon Process Model
**Decision**: macOS launchd as primary deployment target; systemd unit file documented for Linux.

`mycase daemon install` / `mycase daemon uninstall` will write/load the launchd plist at `~/Library/LaunchAgents/com.mycase.daemon.plist`. Scheduled at 15:45 IST (post-market close) daily. On Linux, prints systemd unit instructions.

### D3 — Web Dashboard Frontend
**Decision**: Plain HTML5 + modern CSS + vanilla JS (ES2022+) + Web Components + Apache ECharts. No framework, no build pipeline.

Stack rationale: dashboard is a local tool. `//go:embed static/*` keeps it self-contained in the Go binary. ECharts for portfolio donut, drift timeline, backtest equity curve. Web Components for `<portfolio-summary>`, `<weight-slider>`, `<holdings-table>`, `<drift-alert>`. SSE for live quote streaming (no polling).

### D4 — Alert Channels
**Decision**: Telegram bot and Discord webhook in R5. Email (SMTP) deferred — stub `EmailAlerter` with `errors.New("not yet implemented")`.

### D5 — Price Cache Backend
**Decision**: DuckDB via `github.com/duckdb/duckdb-go/v2` at `data/cache.db`.

DuckDB is already in production across this org (sanvasify, eia-api-explorer, patscape). For R7 backtesting workloads (rolling windows, multi-ticker correlations across 250+ tickers), DuckDB's columnar execution is the right fit. Same `database/sql` interface; `ON CONFLICT DO UPDATE` upsert syntax already in use.

Schema: `prices(ticker, date, close, open, volume)`, `fundamentals(ticker, fetched_at, ...)`, `cache_meta(ticker, range_key, fetched_at)`.

Staleness policy: past trading day prices are permanent; current-day prices stale before 15:30 IST; fundamentals stale after 24h.

### D6 — Broker Abstraction (R4)
**Decision**: Define `pkg/broker/broker.go` interface (`GetHoldings`, `PlaceOrders`, `GetPositions`, `IsAuthenticated`). Move Kite logic to `pkg/broker/zerodha/`. Candidate second brokers: Fyers, AngelOne SmartAPI, Upstox.
