# Mycase — Architecture Reference

**Module**: `github.com/raghavkgarg/mycase` | **Go**: 1.26.5 | **Binary**: `mycase`

---

## 1. System Overview

`mycase` is an Indian equity portfolio engine covering the full lifecycle:

```
Index/CSV → pick (rank + filter) → optimize (weights) → basket (execute orders)
                                         ↓
                              backtest (historical P&L)
                                         ↓
                               daemon (drift alerts)
```

All steps are direct Go function calls within one process. `pipeline` orchestrates them sequentially from `config/pipeline.yaml`. Zerodha Kite Connect is used for live execution; `MockBroker` handles everything else.

---

## 2. Key Algorithms

### 2.1 Inverse-Volatility Weighting (`pkg/optimizer/volatility.go`)

```
w_i = (1/σ_i) / Σ(1/σ_j)
```

`σ_i` is the **population standard deviation of daily log returns** over the price window (typically 3 months). A 5% floor on `σ_i` prevents any single low-volatility stock from dominating when it has a short history or flat price.

Volatility is computed from price closes only — no bid/ask spread or intraday data. This means two stocks with the same daily close movement get identical weight regardless of intraday swings.

### 2.2 MFS Multi-Factor Scoring (`pkg/optimizer/mfs.go`, `pkg/stockpicker/scoring.go`)

The MFS pipeline:

1. **Fetch** 3-month daily prices + fundamentals for each candidate
2. **Score** each stock across 16 factors (Sharpe, Sortino, Beta, Alpha, Ulcer Index, PEG, ROE, FCF Yield, Operating Margin, P/B, Debt/EBITDA, Revenue CAGR, Insider %, Institutional %, Forward P/E, Market Cap tier)
3. **Normalize** each factor to [0, maxPoints] via `normalizeValue(v, lo, hi, maxPoints)` — values outside [lo, hi] are clamped (no extrapolation beyond 0 or maxPoints)
4. **Weight** normalized scores by strategy (balanced / aggressive / conservative / multibagger) from `config/mfs.json`
5. **Cap weights** iteratively: if any stock exceeds `--cap`, redistribute the excess proportionally across all uncapped stocks until convergence

The cap redistribution is iterative (not a single pass) because redistributing excess to other stocks may push them over the cap too.

### 2.3 Backtest Engine (`pkg/backtest/engine.go`)

**Date alignment**: builds a common trading calendar by intersecting the date sets of all tickers AND the benchmark. Days where any ticker is missing data are excluded. This prevents survivorship-bias skew from stocks with incomplete history.

**Initial buy**: at the first common day's close price, with slippage applied: `effectivePrice = close * (1 + slippage)`. The TotalReturn is measured from `InitialCapital`, not from the first NAV snapshot, so slippage cost is reflected in the return figure.

**Rebalance execution**: sells happen first (freeing cash), then buys. Targets are computed from the pre-rebalance portfolio value. Slippage applies on both sides: sells receive `close * (1 - slippage)`, buys pay `close * (1 + slippage)`. Cash from sells is bounded to what's available before buying.

**Drift-triggered rebalance**: fires when any stock's actual weight deviates more than `DriftThreshold` from its target. Checks daily.

**Metrics** (all in `pkg/backtest/metrics.go`):
- CAGR: `(final/initial)^(365/calendarDays) - 1`
- Max Drawdown: running peak-to-trough; `peak` updates continuously
- Sharpe/Sortino: annualized (`× √252`), excess over 6% India risk-free rate
- Sortino downside deviation uses the full `n` in denominator (not just down-day count)
- Beta: `cov(port, bench) / var(bench)` from daily returns — the `(n-1)` cancels
- Jensen's Alpha: `portCAGR - (0.06 + β × (benchCAGR - 0.06))`

### 2.4 Drift Monitoring (`pkg/daemon/drift.go`)

```
DriftIndex = ½ × Σ|w_actual_i - w_target_i|
```

This is the **total variation distance** between actual and target weight vectors, bounded [0, 1]. A drift of 0.1 means 10% of the portfolio is "in the wrong place." Alerts fire when `DriftIndex > threshold` (default 5%).

Actual weights are computed from live quotes × held quantities from the broker. T1/T2 unsettled quantities are included in the position count.

### 2.5 Hard Filters in Stock Picker (`pkg/stockpicker/scoring.go`)

The multibagger strategy applies hard filters before scoring:
- Min market cap threshold (configurable)
- EBITDA positive (profit constraint)
- Debt/EBITDA ≤ 2× (leverage constraint)
- Insider holding ≥ minimum % (alignment)
- Pledged shares ≤ maximum %
- Forward P/E within range
- Revenue 3Y CAGR ≥ minimum

Stocks that fail any hard filter are **removed before scoring** (they don't get a low score — they're excluded entirely). This prevents them from entering the portfolio at any weight via low contribution.

---

## 3. Non-Obvious Implementation Details

### 3.1 DuckDB BIGINT vs TIMESTAMP

DuckDB v1.5.3 does not reliably scan `TIMESTAMP` columns into `time.Time` via `database/sql` — values come back as zero. All time columns (`fetched_at`, `ts`) are stored as `BIGINT` (Unix epoch seconds). Convert with `time.Unix(n, 0)` on read. This is a hard bug in the driver, not something that can be detected at schema design time.

The symptom: staleness checks always fail (everything appears fresh or stale) because `time.Unix(0, 0)` compares wrong against `time.Now()`.

### 3.2 IST Timezone Throughout

All date operations (price staleness, CleanIntradayNoise, backtest calendar) use IST (`Asia/Kolkata`, UTC+5:30). Yahoo Finance timestamps for Indian stocks are midnight IST which appears as 18:30 UTC the previous day. Converting with `time.Unix(ts, 0).In(istLoc)` gives the correct trading date.

Using `time.UTC` instead causes price dates to shift by one day for Indian stocks, corrupting the backtest calendar alignment.

### 3.3 CleanIntradayNoise

`HistoricalData.CleanIntradayNoise()` discards the last data point if it represents today's date AND the current IST time is before 15:30 (market close). This prevents a partial trading day from being treated as a full day's close, which would understate volatility and overstate returns for strategies that check today's price.

### 3.4 Purchase Date Unavailability

Neither Zerodha Kite API nor `MockBroker` exposes the purchase date in `broker.Holding`. `pkg/costs/tax.go:ClassifySell` accepts `time.Time{}` (zero value) as "unknown" and returns `TaxUnknown` with a "check manually" note rather than crashing or defaulting to a wrong classification.

### 3.5 DP Charge Dominates Micro-Transactions

The CDSL DP charge (₹15.93 flat per ISIN per sell day) makes small sell orders uneconomical at any price. Example: 1 share × ₹50 sell → cost ratio ≈ 32%, far above the 0.5% micro-transaction threshold. The DP charge is flat, not percentage-based, so it disproportionately impacts small positions. This is the primary reason `FilterMicroTransactions` exists.

### 3.6 Yahoo Finance Cookie/Crumb Auth

Yahoo Finance requires a valid `crumb` parameter for some API endpoints. `pkg/yfinance/yfinance.go:FetchCookieAndCrumb` fetches a session cookie and then extracts the crumb from the response. This is session-based and must be re-fetched if the session expires. The chart API endpoints used for price data (`/v8/finance/chart/`) do not require a crumb — only the quoteSummary endpoint does.

### 3.7 Cache Key for Date-Range Queries

`FetchHistoricalByDateRange` uses Yahoo's `period1`/`period2` Unix timestamp params instead of `range`. The DuckDB cache key is `"dr_YYYYMMDD_YYYYMMDD"`. Historical ranges (where `to < today`) never expire — once fetched, past prices are immutable. Only ranges ending on today use `isFreshToday` staleness.

### 3.8 Broker Abstraction Fallback

`pkg/broker/zerodha.New()` falls back to `MockBroker` when Kite API credentials are absent or invalid. This means `mycase basket` and `mycase holdings` work without credentials (dry-run mode) without any special flag — the fallback is transparent from the user's perspective.

---

## 4. Data Flow Details

### Price Data Path

```
FetchHistoricalByDateRange(ticker, from, to)
  → DuckDB cache: GetPricesByDateRange     (historical ranges: permanent)
  → File cache: ~/.../data/.cache/         (same-day, no DuckDB required)
  → Yahoo Finance chart API: period1/period2
  → Store to DuckDB + file cache
```

### Fundamental Data Path

```
FetchFundamentals(ticker)
  → DuckDB cache: GetFundamentalsJSON      (24h TTL)
  → Yahoo Finance quoteSummary + timeseries APIs
  → Unmarshal into Fundamentals struct
  → Store to DuckDB
```

### Order Execution Path

```
LoadBasketCSV → FetchMarketData → OptimizeFreshBuy
  → FilterMicroTransactions (costs check)
  → printCostSummary + printTaxWarnings
  → executor.ExecuteBasketOrders (mock or Zerodha)
```

---

## 5. Config Files

| File | Purpose | Hot-reload? |
|------|---------|-------------|
| `config/pipeline.yaml` | Run parameters: indices, strategy, top-N, tolerances, alert credentials | No — read at startup |
| `config/mfs.json` | Scoring strategy weights per factor (balanced, aggressive, conservative, multibagger) | No |
| `config/themes.json` | Portfolio name → CSV path mapping for `holdings` | No |
| `config/csvlinks.json` | Index name → NSE/BSE constituent CSV URL for `pick --index` | No |
| `config/governance.json` | Per-sector governance score overrides | No |

---

## 6. CLI Invocation Signatures (urfave/cli v3)

Every action has signature `func(ctx context.Context, cmd *cli.Command) error`. Key v3 differences from v2:
- `Float64Flag` → `FloatFlag`; `c.Float64("name")` → `c.Float("name")`
- Explicit flag detection: `c.IsSet("name")` (not checking zero-value)
- Positional args: `c.Args().Slice()`, `c.Args().Get(n)`

`pipeline.go` calls the inner `runXxxWithOpts` functions directly — no `exec.Command` subprocess. This is what allows the pipeline to share a single process's DuckDB connection and avoid re-auth overhead.

---

## 7. Design Decisions

### D2 — Daemon Process Model
macOS launchd is the primary deployment target. `mycase daemon install` writes `~/Library/LaunchAgents/com.mycase.daemon.plist` (KeepAlive=true, RunAtLoad=true, 15:45 IST daily). No self-daemonization in Go — lifecycle is fully managed by the OS service layer. State persists to `data/daemon_state.json` across restarts.

### D3 — Web Dashboard Frontend (R8, not yet implemented)
Plain HTML5 + vanilla JS ES2022 + Web Components + Apache ECharts (vendored). No framework, no build pipeline. `//go:embed static/*` keeps everything in the binary. SSE for live quote streaming.

### D4 — Alert Channels
Telegram (HTTP POST to bot API, no SDK) and Discord webhook. Email (`EmailAlerter`) is a stub returning `errors.New("not yet implemented")`. Credentials in `config/pipeline.yaml` under `alerts:`, overridable via `MYCASE_TELEGRAM_TOKEN` and `MYCASE_DISCORD_WEBHOOK`.

### D5 — DuckDB Cache Backend
DuckDB via `duckdb-go/v2` at `data/cache.db`. Chosen for R7 backtesting workloads: rolling windows, multi-ticker correlations, range queries across 250+ tickers in columnar storage. Uses `database/sql` interface with `ON CONFLICT DO UPDATE` upsert. Schema uses `BIGINT` timestamps (see §3.1).

### D6 — Broker Abstraction
`pkg/broker/broker.go` interface: `GetHoldings`, `PlaceOrders`, `GetPositions`, `IsAuthenticated`, `GetQuotes`. ZerodhaBroker lives in `pkg/broker/zerodha/`. Candidate second brokers: Fyers, AngelOne SmartAPI, Upstox — all need custom HTTP clients (no official Go SDKs for Fyers/Angel).

### D7 — Finance Act 2024 Rates
The refactor.md spec listed pre-Budget 2024 rates (STCG 15%, LTCG 10% above ₹1L). Actual current rates: STCG 20%, LTCG 12.5% above ₹1.25L. The code uses current rates with a comment noting the discrepancy. NSE exchange transaction charges (0.00297%) are excluded from the cost model — out of scope per spec.
