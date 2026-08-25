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
| **R10** — Autopilot Pipeline | `pkg/autopilot/`: non-interactive quarterly pipeline (proposal model, scheduling, alert formatting). `cmd/autopilot.go`: run/status/dismiss/install/uninstall. Server: 3 new API endpoints (`/api/autopilot/{proposal,confirm,dismiss}`). Frontend: `<order-preview>` renders proposal with confirm/dismiss buttons. `ScheduleConfig` in pipeline.yaml. `pkg/stockpicker/run.go` exported for programmatic use. | |
| **R10.1** — Package cleanup | Deleted `pkg/portfolio` (type alias → `broker.Holding`), `pkg/kiteclient` (dead code). Merged `pkg/report` → `pkg/stockpicker/rationale.go`, `pkg/performance` → `pkg/backtest/valuation.go`. 25 → 21 packages. | |
| **R9** — Schwab API Integration | `pkg/schwab/`: OAuth2 auth flow (auth.go, tls.go), HTTP client with auto-refresh + rate limiting (client.go), market data mapped to yfinance types (market.go, types.go), broker.Broker implementation (broker.go). `pkg/datafetcher/router.go`: ticker routing (US: → Schwab, NSE:/BSE: → Yahoo, fallback to Yahoo on error). `pkg/costs/us.go`: US cost model ($0 commission, SEC fee, TAF) + US tax classification with wash sale detection. `cmd/auth.go`: extended with `--broker schwab` flag. `pkg/config/pipeline.go`: Broker/SchwabConfig/SchwabToken fields. | |

---

---

## Phase R9 — Schwab API Integration (Broker + Market Data)

### Goal

Integrate Charles Schwab's Trader API as both a **broker** (US equity execution) and a **market data source** (price history, quotes, fundamentals) for US-listed stocks. Indian stocks continue through Yahoo Finance and Zerodha unchanged.

### Why

- Eliminates Yahoo Finance cookie/crumb fragility for US stocks (official authenticated API)
- Provides real-time quotes (not 15-min delayed)
- Official fundamentals data via instrument search endpoint
- Enables US equity execution through the existing `Broker` interface
- Price history available back to ~1985 at daily granularity, intraday down to 1-minute
- Foundation for the options overlay feature (vision doc) — Schwab provides full option chain data

### Schwab API Overview

Two product bundles (both needed):

| Product | Capabilities |
|---------|-------------|
| Accounts and Trading Production | Account info, positions/balances, place/cancel/replace orders, transaction history |
| Market Data Production | Price history (daily/intraday), current quotes, option chains, instrument fundamentals, market movers |

**Auth model**: OAuth 2.0 `authorization_code` flow. User authenticates on Schwab's Login Micro Site, gets redirected back with a code. Code is exchanged for access token (~30 min lifetime) + refresh token (~7 day lifetime). Refresh is automatic; full re-auth required roughly weekly.

**Base URLs**:
- Auth: `https://api.schwabapi.com/v1/oauth/authorize`
- Token: `https://api.schwabapi.com/v1/oauth/token`
- Trader: `https://api.schwabapi.com/trader/v1/`
- Market Data: `https://api.schwabapi.com/marketdata/v1/`

**Account hashes**: API uses hashed account IDs, not raw account numbers. Fetched via `GET /trader/v1/accounts/accountNumbers`.

### Package Layout

```
pkg/schwab/
├── auth.go         # OAuth2 authorization_code flow + token refresh
├── client.go       # HTTP client: base URL, Bearer token injection, retry on 401
├── types.go        # Schwab API request/response structs
├── market.go       # Market data: price history, quotes, fundamentals
└── broker.go       # Broker interface implementation

config/schwab.json          # App credentials (client_id, client_secret, callback_url)
config/schwab_token.json    # Auto-managed tokens (.gitignored)
```

### R9.1 — OAuth2 Auth (`pkg/schwab/auth.go`)

**Flow**:
```
mycase auth --broker schwab
  1. Read client_id + client_secret from config/schwab.json
  2. Start local HTTPS server on 127.0.0.1:8443
  3. Open browser → Schwab authorize URL with redirect_uri=https://127.0.0.1:8443/callback
  4. User logs in, selects accounts to share
  5. Schwab redirects to callback with ?code=XYZ
  6. Exchange code for access_token + refresh_token (POST /v1/oauth/token)
  7. Save tokens to config/schwab_token.json
  8. Shut down local server
```

**Token management**:
- `LoadToken(path) → *Token, error` — reads persisted token
- `RefreshToken(client_id, client_secret, refresh_token) → *Token, error` — exchanges refresh for new access token
- Auto-refresh: before any API call, check if access token expires within 60s; if so, refresh first
- If refresh token itself has expired (~7 days), return clear error directing user to re-run `mycase auth --broker schwab`

**Config file** (`config/schwab.json`):
```json
{
  "client_id": "your_app_key",
  "client_secret": "your_app_secret",
  "callback_url": "https://127.0.0.1:8443/callback"
}
```

**Token file** (`config/schwab_token.json`, .gitignored):
```json
{
  "access_token": "...",
  "refresh_token": "...",
  "token_type": "Bearer",
  "expires_at": 1721500000,
  "refresh_expires_at": 1722100000,
  "scope": "api",
  "account_hashes": [{"account_number": "123", "hash_value": "ABC123"}]
}
```

### R9.2 — HTTP Client (`pkg/schwab/client.go`)

Thin wrapper over `net/http`:
- Base URL prefix (trader vs market data)
- `Authorization: Bearer {access_token}` header on every request
- Auto-refresh on 401 response (one retry after refresh)
- Configurable timeout (default 15s)
- Rate limiting awareness (Schwab throttles at 120 req/min)
- JSON response decoding with typed error handling

```go
type Client struct {
    httpClient   *http.Client
    auth         *TokenManager
    traderBase   string  // https://api.schwabapi.com/trader/v1
    marketBase   string  // https://api.schwabapi.com/marketdata/v1
}

func New(configPath, tokenPath string) (*Client, error)
func (c *Client) Get(ctx context.Context, path string, params url.Values) (*http.Response, error)
func (c *Client) Post(ctx context.Context, path string, body any) (*http.Response, error)
```

### R9.3 — Market Data (`pkg/schwab/market.go`)

Returns existing `yfinance.HistoricalData` and `yfinance.Fundamentals` types so downstream code is unchanged.

**Price History** — `GET /marketdata/v1/pricehistory?symbol=AAPL&periodType=year&period=5&frequencyType=daily&frequency=1`

```go
func (c *Client) FetchHistoricalByDateRange(ctx context.Context, symbol string, from, to time.Time) (*yfinance.HistoricalData, error)
func (c *Client) FetchHistoricalDataWithTimestamps(ctx context.Context, symbol string, rangeStr string) (*yfinance.HistoricalData, error)
```

Maps Schwab's response (candles array with open/high/low/close/volume/datetime) to the existing `HistoricalData` struct. Results flow into DuckDB cache unchanged.

**Quotes** — `GET /marketdata/v1/quotes?symbols=AAPL,MSFT&fields=quote`

```go
func (c *Client) FetchQuotes(ctx context.Context, symbols []string) (map[string]float64, error)
```

Returns last price per symbol. Real-time (not delayed) for authenticated users.

**Fundamentals** — `GET /marketdata/v1/instruments?symbol=AAPL&projection=fundamental`

```go
func (c *Client) FetchFundamentals(ctx context.Context, symbols []string) (map[string]yfinance.Fundamentals, error)
```

Maps Schwab's fundamental fields to existing `Fundamentals` struct:
| Schwab field | → Fundamentals field |
|---|---|
| `fundamental.peRatio` / earningsGrowth | `PEGRatio` (computed) |
| `fundamental.returnOnEquity` | `ROE` |
| `fundamental.pegRatio` | `PEGRatio` |
| `fundamental.pbRatio` | `PBRatio` |
| `fundamental.operatingMarginTTM` | `OperatingMargins` |
| `fundamental.marketCap` | `MarketCap` |
| `fundamental.vol10DayAvg` / `vol3MonthAvg` | `AverageVolume` |
| `reference.description` / sector | `Sector` |

Not all 16 MFS factors will map 1:1 from Schwab fundamentals. Missing fields (e.g., insider %, detailed annual timeseries) can still fall back to Yahoo Finance's timeseries endpoint for US stocks. The priority is price history + quotes from Schwab; fundamentals are a bonus.

### R9.4 — Ticker Routing (dispatcher)

Add routing logic to select data source based on ticker prefix:

```go
// pkg/yfinance/router.go (or pkg/marketdata/router.go)

func FetchHistorical(ctx context.Context, ticker string, from, to time.Time) (*HistoricalData, error) {
    if isUSTicker(ticker) && schwabClient != nil {
        return schwabClient.FetchHistoricalByDateRange(ctx, stripPrefix(ticker), from, to)
    }
    return FetchHistoricalByDateRange(ctx, ticker, from, to) // existing Yahoo path
}

func isUSTicker(ticker string) bool {
    return strings.HasPrefix(ticker, "US:") ||
           strings.HasPrefix(ticker, "NASDAQ:") ||
           strings.HasPrefix(ticker, "NYSE:")
}
```

The DuckDB cache layer sits below this — both Schwab and Yahoo results get cached identically. Downstream code (backtest, optimizer, picker) calls the router, never the provider directly.

### R9.5 — Broker Implementation (`pkg/schwab/broker.go`)

Implements `broker.Broker` interface:

```go
type SchwabBroker struct {
    client      *Client
    accountHash string
}

func (b *SchwabBroker) IsMock() bool { return false }
```

**GetHoldings** — `GET /trader/v1/accounts/{hash}?fields=positions`

Maps Schwab positions to `[]broker.Holding`:
| Schwab field | → Holding field |
|---|---|
| `instrument.symbol` | `TradingSymbol` |
| `"NYSE"` / `"NASDAQ"` (from instrument.type) | `Exchange` |
| `longQuantity` | `Quantity` |
| `averagePrice` | `AveragePrice` |
| `marketValue / quantity` | `LastPrice` |
| `currentDayProfitLoss` | `PnL` |

T1/T2 quantities: Schwab doesn't separate settlement buckets (US is T+1 now). All quantity goes to `Quantity`; `T1Quantity` and `T2Quantity` stay 0.

**GetQuotes** — delegates to market data `FetchQuotes`.

**PlaceOrder** — `POST /trader/v1/accounts/{hash}/orders`

Maps `broker.Order` to Schwab order spec:
```json
{
  "orderType": "LIMIT",
  "session": "NORMAL",
  "duration": "DAY",
  "orderStrategyType": "SINGLE",
  "orderLegCollection": [{
    "instruction": "BUY",
    "quantity": 10,
    "instrument": {"symbol": "AAPL", "assetType": "EQUITY"}
  }],
  "price": "150.50"
}
```

**PlaceGTT** — Schwab doesn't have Zerodha-style GTT. Options:
- Map to a stop-limit order (closest equivalent)
- Return `fmt.Errorf("GTT orders not supported on Schwab; use stop-limit via PlaceOrder")`

Recommendation: return an error with guidance. GTT is India-specific (Kite's innovation). US equivalent is a GTC (Good Till Cancel) stop-limit order, which should be exposed as a regular `PlaceOrder` with appropriate order type params.

### R9.6 — Config & CLI Changes

**`config/pipeline.yaml`** — add broker selection:
```yaml
broker: zerodha          # or "schwab"
schwab_config: config/schwab.json
schwab_token: config/schwab_token.json
```

**`cmd/setup_auth`** — extend to handle `mycase auth --broker schwab`:
```go
case "schwab":
    schwab.RunAuthFlow(configPath, tokenPath)
```

**`.gitignore`** — add:
```
config/schwab_token.json
```

### R9.7 — Transaction Costs (US)

US equity costs on Schwab are effectively zero for practical purposes:
| Charge | Rate | Notes |
|--------|------|-------|
| Commission | $0 | Schwab eliminated equity commissions |
| SEC fee | ~$8.00 per $1M (sell only) | Negligible |
| TAF | $0.000166/share (sell only) | Max $0.01/share |

Implementation: add `USCostModel` to `pkg/costs/` that returns near-zero costs. The `FilterMicroTransactions` logic can be skipped for US orders (no DP charge problem).

### Implementation Order

| Step | Depends on | Deliverable |
|------|-----------|-------------|
| R9.1 | Schwab app registration | `pkg/schwab/auth.go`, `mycase auth --broker schwab` |
| R9.2 | R9.1 | `pkg/schwab/client.go` |
| R9.3 | R9.2 | `pkg/schwab/market.go` — price history + quotes working |
| R9.4 | R9.3 | Ticker routing; US tickers flow through Schwab |
| R9.5 | R9.2 | `pkg/schwab/broker.go` — holdings + order placement |
| R9.6 | R9.1 | Config + CLI changes |
| R9.7 | R9.5 | US cost model |

**Testing strategy**: Each sub-step gets unit tests with recorded HTTP responses (httptest). Integration test with live Schwab sandbox (developer portal provides a test environment). Mock fallback when tokens absent (same pattern as Zerodha).

### Prerequisites (manual, before implementation)

1. Register at https://developer.schwab.com
2. Create an app — select both "Accounts and Trading Production" and "Market Data Production"
3. Set callback URL to `https://127.0.0.1:8443/callback`
4. Note down the App Key (client_id) and Secret (client_secret)
5. Create `config/schwab.json` with credentials
6. Wait for app approval (Schwab reviews apps; may take 1–3 business days)

### Market Hours & Timezone

- US market: 9:30 AM – 4:00 PM ET (Eastern Time)
- `CleanIntradayNoise` equivalent: discard last candle if current ET time is before 16:00
- Schwab timestamps are in Unix milliseconds (not seconds like Yahoo)
- No IST conversion needed for US stocks; cache staleness uses ET

### Differences from Zerodha Integration

| Aspect | Zerodha | Schwab |
|--------|---------|--------|
| Auth | API key + daily login token | OAuth2 + 7-day refresh |
| Token lifetime | 1 day (manual re-login) | Access: 30 min (auto-refresh), Refresh: 7 days |
| Settlement | T+1 for most, T+2 buckets visible | T+1, no bucket separation |
| GTT | Native, server-side | Not available (use GTC stop-limit) |
| Commissions | ₹20/order or 0.03% | $0 |
| DP charges | ₹15.93/ISIN on sell | None |
| Market data | Separate (Yahoo Finance) | Same API (dual-purpose) |
| Exchange prefix | `NSE:`, `BSE:` | `US:`, `NASDAQ:`, `NYSE:` |
| Go SDK | `gokiteconnect/v4` (official) | None — custom HTTP client |

---

## Ongoing Guard Rails

- All `pkg/` packages should have tests. Run `go test ./...` before and after any change — zero regressions.
- `mfs.json` and `pipeline.yaml` config file formats must stay backward-compatible.
- `data/*.csv` golden copy files are user data — never touch them programmatically except through the guarded backup → overwrite flow already in place.


---

## Phase R11 — Broker Factory & Market Abstraction

### Goal

Eliminate all hardcoded `zerodha.New(...)` calls and India-specific assumptions (exchange prefixes, benchmarks, cost models, currency, scheduling) from the command layer. Replace with a broker factory driven by `config/defaults.json` so that a US-only investor can use `basket`, `holdings`, `autopilot`, `daemon`, and `retry` commands without modification.

### Why

- 6 commands hardcode `zerodha.New(live, "config/config.json")` — unusable for US/Schwab
- Exchange is hardcoded to `"NSE"` on every order in basket.go
- Benchmark defaults to `^NSEI` (Nifty 50) in optimize, backtest, and monitor
- Cost model is hardcoded to `costs.DefaultZerodha` in basket and autopilot
- Currency is hardcoded to `₹` in executor, daemon, and autopilot output
- Daemon schedules drift checks at 15:45 IST (India market close)

Schwab already implements `broker.Broker` (`pkg/schwab/broker.go`) — the interface layer is complete. The problem is purely in the command/orchestration layer.

### Design

#### 1. Broker Factory (`pkg/broker/factory.go`)

```go
package broker

import (
    "fmt"
    "github.com/raghavkgarg/mycase/pkg/config"
)

// NewFromDefaults creates the appropriate broker based on config/defaults.json.
// If live=false or credentials are missing, returns MockBroker.
func NewFromDefaults(live bool) (Broker, error) {
    defaults := config.LoadUserDefaults("config/defaults.json")
    return NewByName(defaults.Broker, live)
}

// NewByName creates a broker by explicit name.
func NewByName(name string, live bool) (Broker, error) {
    switch name {
    case "schwab":
        return newSchwabBroker(live)
    case "zerodha":
        return newZerodhaBroker(live)
    case "", "mock":
        return &MockBroker{}, nil
    default:
        return nil, fmt.Errorf("unsupported broker: %q", name)
    }
}
```

Schwab construction reads `config/schwab.json` + `config/schwab_token.json` internally. Zerodha reads `config/config.json`. The caller doesn't need to know which config file to pass.

#### 2. Market Config (`pkg/broker/market.go`)

```go
package broker

import "github.com/raghavkgarg/mycase/pkg/config"

// MarketConfig provides market-specific defaults derived from config/defaults.json.
type MarketConfig struct {
    Benchmark string // "^GSPC" or "^NSEI"
    Exchange  string // "US" or "NSE"
    Currency  string // "$" or "₹"
    CloseHour int    // 16 (US ET) or 15 (India IST)
    CloseMin  int    // 0 (US) or 30 (India)
    Timezone  string // "America/New_York" or "Asia/Kolkata"
}

func LoadMarketConfig() MarketConfig {
    defaults := config.LoadUserDefaults("config/defaults.json")
    switch defaults.Market {
    case "us":
        return MarketConfig{
            Benchmark: "^GSPC",
            Exchange:  "US",
            Currency:  "$",
            CloseHour: 16, CloseMin: 0,
            Timezone:  "America/New_York",
        }
    default: // "india" or ""
        return MarketConfig{
            Benchmark: "^NSEI",
            Exchange:  "NSE",
            Currency:  "₹",
            CloseHour: 15, CloseMin: 30,
            Timezone:  "Asia/Kolkata",
        }
    }
}
```

#### 3. Cost Model from Broker

Add to the `Broker` interface or provide via factory:

```go
// In pkg/broker/factory.go or as interface method
func CostModelForBroker(name string) costs.CostModel {
    switch name {
    case "schwab":
        return costs.DefaultUS
    default:
        return costs.DefaultZerodha
    }
}
```

### Files to Change

#### Tier 1 — Broker instantiation (replace `zerodha.New(...)` with factory)

| File | Current | After |
|------|---------|-------|
| `cmd/autopilot.go:107` | `b := zerodha.New(live, "config/config.json")` | `b, err := broker.NewFromDefaults(live)` |
| `cmd/basket.go:55` | `b := zerodha.New(liveMode, "config/config.json")` | `b, err := broker.NewFromDefaults(liveMode)` |
| `cmd/daemon.go:80` | `b := zerodha.New(c.Bool("live"), "config/config.json")` | `b, err := broker.NewFromDefaults(c.Bool("live"))` |
| `cmd/daemon.go:112` | `b := zerodha.New(c.Bool("live"), "config/config.json")` | `b, err := broker.NewFromDefaults(c.Bool("live"))` |
| `cmd/holdings.go:35` | `b := zerodha.New(liveMode, "config/config.json")` | `b, err := broker.NewFromDefaults(liveMode)` |
| `cmd/retry.go:23` | `b := zerodha.New(liveMode, "config/config.json")` | `b, err := broker.NewFromDefaults(liveMode)` |
| `cmd/serve.go:23` | `b := zerodha.New(c.Bool("live"), "config/config.json")` | `b, err := broker.NewFromDefaults(c.Bool("live"))` |

Remove `import "github.com/raghavkgarg/mycase/pkg/broker/zerodha"` from these files.

#### Tier 2 — Exchange and order construction

| File | Current | After |
|------|---------|-------|
| `cmd/basket.go` | `Exchange: "NSE"` on all orders | Derive from ticker prefix: `broker.ExchangeFromTicker(ticker)` |
| `cmd/basket.go` | `Product: "CNC"` on all orders | `broker.DeliveryProduct(marketConfig.Exchange)` — "CNC" for India, "" for US |
| `pkg/executor/executor.go` | `"NSE:" + order.TradingSymbol` for quotes | Use `order.Exchange + ":" + order.TradingSymbol` (or store full ticker key in Order) |
| `cmd/holdings.go` | `"NSE:" + h.TradingSymbol` / `"BSE:" + ...` | Use `h.Exchange + ":" + h.TradingSymbol` (Schwab already sets Exchange="US") |

#### Tier 3 — Benchmark and config paths

| File | Current | After |
|------|---------|-------|
| `cmd/backtest.go:30` | `Value: "^NSEI"` default flag | `Value: ""` → resolve via `broker.LoadMarketConfig().Benchmark` |
| `cmd/optimize.go:95` | `"^NSEI"` hardcoded benchmark | `broker.LoadMarketConfig().Benchmark` |
| `cmd/monitor.go:223` | `"^NSEI"` hardcoded benchmark | `broker.LoadMarketConfig().Benchmark` |
| `cmd/monitor.go:24` | `Value: "data/microsmall.csv"` default file | Read from `defaults.json` → `pipeline_config` → golden_copy_path |
| `cmd/daemon.go:73` | `return "data/microsmall.csv"` fallback | Read from pipeline config or defaults |
| `cmd/daemon.go:43,55` | `Value: "config/pipeline.yaml"` | `Value: ""` → resolve from `defaults.PipelineConfig` |
| `cmd/autopilot.go:42,50,63` | `Value: "config/pipeline.yaml"` | Same as above |
| `cmd/pipeline.go:27` | `Value: "config/pipeline.yaml"` | Same as above |

#### Tier 4 — Cost model and currency

| File | Current | After |
|------|---------|-------|
| `cmd/basket.go:167,174,186` | `costs.DefaultZerodha` | `broker.CostModelForBroker(defaults.Broker)` |
| `pkg/autopilot/autopilot.go:147` | `costs.DefaultZerodha` | Inject cost model via `RunConfig` |
| `pkg/executor/executor.go` | `₹` currency symbol throughout | `marketConfig.Currency` |
| `pkg/autopilot/autopilot.go` | `₹` in output | `marketConfig.Currency` |
| `pkg/daemon/daemon.go` | `₹` in alert body | `marketConfig.Currency` |

#### Tier 5 — Scheduling

| File | Current | After |
|------|---------|-------|
| `pkg/daemon/daemon.go` | `nextIST1545()` — fires at 15:45 IST | Use `marketConfig.CloseHour`, `marketConfig.CloseMin`, `marketConfig.Timezone` |

#### Tier 6 — UI text / help strings

| File | Current | After |
|------|---------|-------|
| `cmd/basket.go:28` | `"Execute or preview basket orders on Zerodha"` | `"Execute or preview basket orders"` |
| `cmd/basket.go:30` | `"Use live Zerodha API (default: dry-run)"` | `"Use live broker API (default: dry-run)"` |
| `cmd/holdings.go:20` | `"Snapshot of current Zerodha holdings"` | `"Snapshot of current holdings"` |
| `cmd/holdings.go:22` | `"Use live Zerodha API (default: dry-run)"` | `"Use live broker API (default: dry-run)"` |
| `cmd/retry.go:20` | `"Use live Zerodha broker (default: mock)"` | `"Use live broker (default: mock)"` |
| `cmd/pipeline.go:131,214,217` | Zerodha references in prompts | Use active broker name |
| `pkg/executor/executor.go` | IP whitelist references `developers.kite.trade` | Conditional on broker |

### Implementation Order

1. **`pkg/broker/factory.go`** — broker factory (`NewFromDefaults`, `NewByName`)
2. **`pkg/broker/market.go`** — market config (`LoadMarketConfig`, `MarketConfig` struct)
3. **`pkg/broker/factory.go`** — cost model helper (`CostModelForBroker`)
4. **Update cmd/ files** — replace all `zerodha.New(...)` calls (Tier 1)
5. **Update cmd/basket.go** — exchange derivation, product type (Tier 2)
6. **Update pkg/executor/executor.go** — exchange prefix, currency (Tier 2 + 4)
7. **Update benchmark defaults** — backtest, optimize, monitor (Tier 3)
8. **Update config path defaults** — pipeline config resolution (Tier 3)
9. **Update cost model injection** — basket, autopilot (Tier 4)
10. **Update daemon scheduling** — market-aware close time (Tier 5)
11. **Update help text** — remove Zerodha mentions (Tier 6)
12. **Tests** — verify all commands work with `broker=schwab` in defaults

### What stays untouched

- `pkg/broker/zerodha/zerodha.go` — still the India broker implementation
- `pkg/schwab/broker.go` — already implements `broker.Broker`
- `pkg/datafetcher/router.go` — already properly abstracted
- `config/pipeline.yaml`, `config/governance.json`, `config/themes.json` — India configs, kept for other users
- `broker.Broker` interface — `PlaceGTT()` stays (Schwab returns error, which is fine)
- `broker.Holding` — `T1Quantity`/`T2Quantity` stay (Schwab sets to 0)

### Success criteria

After this refactor:
- `mycase pick` — works (already done ✅)
- `mycase auth` — works with Schwab by default (already done ✅)
- `mycase holdings --live` — fetches Schwab positions
- `mycase basket --live` — places Schwab equity orders
- `mycase autopilot run --live` — runs full US pipeline with Schwab
- `mycase daemon check --live` — drift check against Schwab holdings
- `mycase backtest` — uses ^GSPC benchmark by default
- All commands still work with `"broker": "zerodha"` in defaults.json for India users
