# Mycase — Architecture Reference

**Module**: `github.com/raghavkgarg/mycase` | **Go**: 1.26.5 | **Binary**: `mycase`

---

## Table of Contents

1. [What Mycase Does](#1-what-mycase-does)
2. [Inputs: Where Data Comes From](#2-inputs-where-data-comes-from)
3. [Outputs: Reports and What They Answer](#3-outputs-reports-and-what-they-answer)
4. [System Design](#4-system-design)
5. [Stock Selection Algorithm](#5-stock-selection-algorithm)
6. [Weight Optimization](#6-weight-optimization)
7. [Portfolio Monitoring (4-Pillar)](#7-portfolio-monitoring-4-pillar)
8. [Backtesting Engine](#8-backtesting-engine)
9. [Transaction Cost Model](#9-transaction-cost-model)
10. [Data Infrastructure](#10-data-infrastructure)
11. [Design Decisions](#11-design-decisions)

---

## 1. What Mycase Does

An Indian retail investor managing a self-directed equity portfolio faces a recurring problem: which stocks to hold, in what proportions, and when to rebalance. Standard tools either charge for curation (advisory services) or require building everything from scratch (raw broker APIs). Mycase automates the full cycle from stock selection through to live order execution.

The workflow is:

```
Universe (NSE index or CSV)
  → pick      score and filter → candidate list
  → optimize  assign weights  → target portfolio
  → basket    generate orders → execute at broker
  → backtest  test on history → performance audit
  → daemon    watch live      → drift alerts
```

Each step is a `mycase` subcommand that can run independently or as part of `mycase pipeline` (all steps from config). The engine handles two Indian equity strategies: **Multibagger** (high-quality compounders, small/mid-cap focused) and **MFS-style multi-factor** (quality + value + momentum composite).

---

## 2. Inputs: Where Data Comes From

### Yahoo Finance (primary data source)

All price and fundamental data is fetched from Yahoo Finance's unofficial APIs. There are two endpoints:

**Chart API** (`/v8/finance/chart/{symbol}`): used for daily OHLCV price history.
- Range mode: `range=1mo|3mo|6mo|1y|2y|5y` for recent history
- Date-range mode: `period1={unix}&period2={unix}` for backtesting arbitrary windows

**Quote Summary + Timeseries APIs**: used for fundamentals (P/E, P/B, market cap, debt/EBITDA, insider %, revenue growth, ROCE, interest coverage). Some endpoints require a cookie + crumb; the chart API does not.

Yahoo Finance uses IST midnight = 18:30 UTC previous day for Indian stock timestamps. Converting with `time.Unix(ts, 0).In(istLoc)` gives the correct trading date; using UTC shifts dates by one day.

### NSE/BSE Index Constituent CSVs

`config/csvlinks.json` maps index names to constituent download URLs (NSE bhav copy format). When `mycase pick --index smallcap250`, the picker fetches the constituent list from NSE, extracts tickers, and appends `.NS` suffix for Yahoo Finance compatibility.

Supported indices: `nifty50`, `nifty100`, `nifty500`, `smallcap250`, `midcap150`, `microcap250`, `niftynext50`.

### Golden Copy CSVs (`data/*.csv`)

The golden copy is the source of truth for what the investor currently wants to hold. It is a human-readable CSV with columns: `ticker`, `weight` (0–1), `sector`, optionally `qty` and `avg_cost`.

Golden copies are **never overwritten programmatically** except via `mycase merge golden`. All other commands read from them. This prevents automation from silently destroying a hand-curated portfolio.

### Zerodha Kite Connect API

Live holdings (`GetHoldings`), positions (`GetPositions`), quotes (`GetQuotes`), and order placement (`PlaceOrder`) via `pkg/broker/zerodha/`. Requires `config/credentials.json` or token from `mycase auth`. Falls back to `MockBroker` silently when credentials are absent.

### Config Files

| File | What it controls |
|------|-----------------|
| `config/pipeline.yaml` | Run parameters: index, strategy, top-N, capital, tolerances, alert credentials |
| `config/mfs.json` | Factor weights per strategy (balanced / aggressive / conservative / multibagger) |
| `config/themes.json` | Portfolio name → CSV path mapping |
| `config/csvlinks.json` | Index name → NSE constituent download URL |
| `config/governance.json` | Per-sector governance score overrides |

---

## 3. Outputs: Reports and What They Answer

### `pick` — Which stocks to hold and why

Outputs a scored CSV to `data/candidates/index_picks/{index}_{method}_{date}.csv` with columns: ticker, score, rank, sector, and per-factor scores.

Answers: "Of the 250 stocks in this index, which 15 are currently best positioned by quality, valuation, and momentum?" The score breakdown shows which factors drove each selection, making the ranking auditable.

Hysteresis protection: if a golden copy is provided, existing holdings are protected up to `--hysteresis-buffer` extra ranks before being evicted. This prevents unnecessary churn from small ranking fluctuations.

### `optimize` — How much of each stock to hold

Outputs an updated golden copy CSV with `weight` column filled. Methods:
- `volatility`: inverse-volatility weights (lower-volatility stocks get higher weight)
- `mfs`: MFS-score-proportional weights with sector cap and per-stock cap
- `equal`: uniform 1/N weights

Answers: "Given these 15 stocks, what allocation minimizes concentration risk while reflecting quality differences?" The weight column is what `basket` uses to generate order sizes.

### `basket` — What orders to place, and what they cost

Prints an order table (ticker, action, qty, estimated price, value), a cost summary (STT, DP charge, SEBI fee, stamp duty), and STCG/LTCG warnings for sell orders.

Answers: "If I rebalance today, exactly what orders would I place, what would it cost in charges, and what are the tax consequences?" The micro-transaction filter silently drops orders where transaction costs exceed 0.5% of trade value — this is the primary guard against paying more in CDSL DP charges than the trade is worth.

### `backtest` — How would this portfolio have performed historically

Outputs to terminal: Total Return, CAGR, Max Drawdown, Sharpe, Sortino, Calmar, Alpha, Beta, and a year-by-year breakdown table.

Answers: "Would this strategy have beaten the index? What was the worst drawdown I would have endured? Was the extra return worth the extra volatility (Sharpe)?" Alpha and Beta decompose returns into market exposure vs. skill.

### `performance` — How is the current portfolio doing since purchase

Outputs a per-ticker table showing unrealized P&L (₹ and %), and a portfolio total. Supports daily-close mode (purchase > 7 days ago) and intraday mode (within 7 days).

Answers: "Since I bought this portfolio, how am I doing right now?" Useful for tracking the gap between expected (backtest) and actual (live) returns.

### `monitor` — Which holdings need action

Outputs a table with 4 verdict columns per stock and a final verdict badge: KEEP HOLD, HIGH ALERT, or AUTO EXIT.

Answers: "Do I still believe in these companies? Have any deteriorated in revenue growth, cash generation, or liquidity?" Provides an evidence-based exit decision rather than emotional reactions to price movements.

### `daemon` — Am I still on-target without watching every day

The daemon runs at 15:45 IST daily, computes the drift index, and sends a Telegram/Discord alert when any stock drifts beyond the threshold.

Answers: "Has the portfolio drifted far enough from target weights that I need to rebalance?" Eliminates the need to manually check positions daily. Alert contains: drift index, top-drifting tickers, and a suggested rebalance action.

### `report` — Plain-text rationale per stock

Outputs a paragraph per selected stock explaining why it passed filters and what factors drove its score. Used for the investor's own records and second-guessing. (Implementation lives in `pkg/stockpicker/rationale.go`.)

### `autopilot` — Hands-off quarterly rebalancing

Runs the full pick → optimize → merge → compute-orders pipeline non-interactively, generates a proposal file, and sends a Telegram/Discord alert. The investor reviews and confirms via the web dashboard. No orders are placed without explicit confirmation.

Answers: "Run the quarterly rebalance without me babysitting 15 terminal prompts, and tell me when it's ready for my approval."

---

## 4. System Design

### Layer Architecture

```
cmd/           — thin CLI wrappers; parse flags, call pkg functions
pkg/           — domain logic; no CLI imports
  stockpicker/ — scoring, hard filters, hysteresis, selection rationale
  optimizer/   — inverse-volatility, MFS weights, sector caps
  backtest/    — engine, metrics, portfolio valuation
  autopilot/   — non-interactive pipeline, proposal model, scheduling, alerts
  yfinance/    — price and fundamental data fetching
  cache/       — DuckDB read/write for prices and fundamentals
  broker/      — Broker interface; zerodha/, schwab/, and mock/ implementations
  schwab/      — Schwab Trader API: OAuth2 auth, HTTP client, market data, US broker
  daemon/      — drift computation, alert dispatch
  costs/       — transaction cost model (India + US), tax classification
  monitoring/  — 4-pillar health scoring
  alert/       — Alerter interface; Telegram, Discord implementations
  executor/    — live order placement with retry logic
  printer/     — terminal output formatting
  csvloader/   — CSV/golden copy operations, comparison reports
  excel/       — native Excel (.xlsx) parsing & smart ticker extraction
  config/      — configuration loading/parsing (pipeline, mfs, alerts)
  datafetcher/ — market data retrieval with broker fallback + ticker routing (US→Schwab, India→Yahoo)
  market/      — market hours detection, GTT price calculations
  selectiontracker/ — audit trail for stock selection decisions
  server/      — web dashboard (HTTP, SSE, embedded static)
config/        — YAML/JSON configs (read-only at runtime)
data/          — golden copies, candidate output, DuckDB cache
```

No package imports cmd/ — data flows down from cmd → pkg → cache. `pipeline.go` calls inner `runXxxWithOpts()` functions directly; there is no exec.Command subprocess chaining.

### Broker Abstraction

`pkg/broker/broker.go` defines the `Broker` interface:
```go
GetHoldings(ctx) ([]Holding, error)
GetPositions(ctx) ([]Position, error)
GetQuotes(ctx, tickers []string) (map[string]float64, error)
PlaceOrder(ctx, order Order) error
IsAuthenticated() bool
```

`zerodha.New()` falls back to `MockBroker` when credentials are absent. This makes every command work in dry-run mode without special flags.

### In-Process Pipeline

All pipeline steps share one Go process: one DuckDB connection, one Yahoo Finance session, one broker client. `mycase pipeline` incurs zero inter-process overhead. The only coordination is sequential function calls — no channels, no goroutines across steps.

Concurrency happens within individual commands: `backtest` fetches all tickers concurrently via goroutines + buffered channel; `pick` fetches fundamentals for 250 stocks in parallel with a semaphore.

---

## 5. Stock Selection Algorithm

### Multibagger Hard Filters

Applied before scoring. A stock failing any filter is excluded entirely — it does not receive a low score. The filters and their exact thresholds:

| Filter | Threshold | Rationale |
|--------|-----------|-----------|
| A — Market Cap | ₹500 Cr – ₹50,000 Cr | Small/mid-cap sweet spot; below ₹500 Cr = illiquid micro-cap; above ₹50,000 Cr = large-cap, slower grower |
| B — ADV | ≥ ₹1 Cr/day | Average daily volume; ensures exit without significant market impact |
| C — CFO/PAT | ≥ 25% | Cash flow quality; profits must convert to cash. Bypassed if CFO = FCF = 0 (pre-revenue or asset-light platform) |
| D — Earnings trend | (disabled) | Originally required 3 consecutive quarters of positive PAT; disabled to allow turnaround stories |
| E — Promoter ownership | ≥ 25% | Founder/promoter skin in the game; below 25% suggests low alignment or exit risk |
| F — Price vs 200-SMA | Price ≥ 200-day SMA | Structural uptrend filter; stocks below 200-SMA are in medium-term downtrend regardless of fundamentals |
| G — Pledging | < 5% | Pledged promoter shares create forced-selling risk in downturns |
| H — ROCE | ≥ 12% (latest OR 3-year average) | Return on Capital Employed = EBIT / (Total Assets − Current Liabilities); 12% is above India 10Y bond yield; shows capital allocation quality |
| I — D/E | < 1.5 | Debt-to-equity; ≥ 1.5 indicates leverage that can impair equity in a downturn |
| J — Interest Coverage | > 3× | EBIT / Interest Expense; below 3× suggests earnings cannot safely service debt |
| K — CROIC | ≥ 6% | Cash Return on Invested Capital = FCF / (Equity + Debt); 6% floor ensures real cash generation. Bypassed if FCF = CFO = 0 |

Filters C and K have explicit bypass conditions for businesses that are legitimately FCF = 0 (e.g., capital-intensive growth phase with zero debt service).

### Multibagger 100-Point Scoring

Stocks that pass all 11 filters are scored across 6 factors (total 100 points):

| Factor | Points | What it measures |
|--------|--------|-----------------|
| Revenue Accrual Quality | 20 | CFO / Revenue; measures how much revenue converts to cash (not just accounting profit) |
| Asset Turnover | 20 | Revenue / Total Assets; capital efficiency — how hard assets are working |
| PEG Ratio | 15 | P/E / Earnings Growth Rate; price paid per unit of growth; lower is better |
| ROCE | 15 | Return on Capital Employed; punishes businesses that need lots of capital to grow |
| Volume Breakout | 15 | 20-day avg volume vs 200-day avg volume; rising institutional interest signal |
| Relative Strength | 15 | 6-month return vs index; recent price momentum indicates market recognition |

Lower-is-better factors (PEG) are inverted before normalization. All factors are normalized to their [min, max] range within the candidate set, then multiplied by point allocation.

### MFS Multi-Factor Scoring (16 Factors)

Used by `optimize --method mfs`. Each stock is scored across 16 factors:

**Market/Return factors — Higher is better:**

| Factor | Weight direction | What it measures |
|--------|-----------------|-----------------|
| Sharpe Ratio | Higher → higher score | Risk-adjusted return (annualized, 6% RF, √252) |
| Sortino Ratio | Higher → higher score | Downside-risk-adjusted return (downside deviation) |
| Alpha | Higher → higher score | Excess return over benchmark after Beta adjustment |
| Treynor Ratio | Higher → higher score | Return per unit of systematic (market) risk |

**Market/Risk factors — Lower is better (inverted):**

| Factor | Weight direction | What it measures |
|--------|-----------------|-----------------|
| Beta | Lower → higher score | Systematic market sensitivity; lower Beta = less correlated with index crashes |
| Ulcer Index | Lower → higher score | Severity and duration of drawdowns; penalizes strategies with deep, slow recoveries |

**Fundamental/Valuation — Lower is better (inverted):**

| Factor | Weight direction | What it measures |
|--------|-----------------|-----------------|
| PEG Ratio | Lower → higher score | Price-to-earnings / growth rate; lower = more growth per rupee of price |
| Forward P/E | Lower → higher score | Near-term earnings multiple; cheaper is better at same quality |
| P/B Ratio | Lower → higher score | Price to book value; flags overvaluation |
| Net Debt / EBITDA | Lower → higher score | Leverage normalized by earnings power; penalizes overleveraged businesses |
| Market Cap | Lower → higher score | Smaller market cap = more room to grow; avoids fully-valued large caps |

**Fundamental/Quality — Higher is better:**

| Factor | Weight direction | What it measures |
|--------|-----------------|-----------------|
| ROE | Higher → higher score | Return on equity; quality of management's use of shareholder capital |
| Operating Margin | Higher → higher score | Business efficiency; wide margins indicate pricing power or cost advantages |
| Insider % | Higher → higher score | Insider/promoter ownership; alignment of management with shareholders |
| Volume Breakout | Higher → higher score | Unusual volume vs 200-day average; institutional accumulation signal |
| Sales Growth | Higher → higher score | Top-line momentum; revenue growth precedes earnings growth |

### MFS Normalization and Sector Caps

**Normalization**: each factor is min-max normalized across the candidate set to [0, maxPoints]. Values outside the [min, max] range are clamped (no extrapolation). Factors marked "lower is better" are inverted as `maxPoints - normalizedValue`.

**Sector cap redistribution** (iterative, up to 10 rounds):
1. Compute raw MFS-proportional weights
2. If any sector's total weight exceeds 25%, redistribute the excess proportionally across other uncapped stocks
3. Apply per-stock cap (e.g. `--cap 0.15`): if any stock exceeds the cap, redistribute excess
4. Enforce max 3 stocks per sector: if a sector has > 3 stocks, zero-weight the lowest-scored ones
5. Repeat until no violations remain (convergence typically in 2–3 rounds; 10-round limit prevents infinite loop)

The iterative approach is necessary because redistributing excess from one capped entity may push another over its limit.

---

## 6. Weight Optimization

### Inverse-Volatility

```
w_i = (1/σ_i) / Σ(1/σ_j)
```

`σ_i` = population standard deviation of daily log returns over the price window (typically 3 months). A 5% floor on `σ_i` prevents a stock with short history or flat prices from receiving an arbitrarily large weight.

Log returns are used (not arithmetic returns) because they are additive over time and more normally distributed for long windows.

### Equal Weight

`w_i = 1/N`. Used as a baseline for backtesting — if inverse-volatility or MFS fails to outperform equal weight over a 3-year backtest, the extra complexity is not justified.

---

## 7. Portfolio Monitoring (4-Pillar)

### The Four Pillars

| Pillar | Signal | Data Source |
|--------|--------|------------|
| Revenue Momentum | Consecutive quarters of revenue slowdown | Yahoo Finance quarterly revenue timeseries |
| Cash Flow Quality | DSO (Days Sales Outstanding) deterioration | Yahoo Finance quarterly balance sheet + revenue |
| Technical Health | Consecutive days below 200-day SMA | Yahoo Finance daily prices |
| Capital Allocation | (future) ROCE trend, capex intensity | Fundamentals |

A stock fails a pillar when the relevant metric crosses its threshold for the configured consecutive period. Failure of any pillar triggers an alert; failure of multiple pillars triggers AUTO EXIT.

### Verdict Logic

```
0 pillars failed  → KEEP HOLD  (green)
1 pillar failed   → HIGH ALERT (amber) — watch for 1 quarter
2+ pillars failed → AUTO EXIT  (red) — exit on next rebalance
```

### Monitoring Style Presets

The thresholds that define "failure" vary by investor style:

| Threshold | Hyper-Aggressive | Moderate | Passive |
|-----------|-----------------|----------|---------|
| Consecutive revenue slowdowns | 1 quarter | 2 quarters | 3 quarters |
| DSO deterioration | 10% | 15% | 25% |
| Days below SMA200 | 5 days | 10 days | 20 days |
| Rebalance period | 3 months | 6 months | 12 months |
| Max weight drift before rebalance | 12% | 15% | 20% |

Hyper-Aggressive exits quickly on early signals; Passive allows more time to confirm deterioration before acting. These map to the 4 pillar `MonitorConfig` fields and are set in `config/pipeline.yaml`.

---

## 8. Backtesting Engine

### Date Alignment

The backtest engine builds a **common trading calendar** by intersecting the date sets of all portfolio tickers plus the benchmark. Days where any ticker (including the benchmark) has no price data are excluded from the simulation.

This prevents two failure modes:
1. **Missing-data survivorship bias**: a ticker that started trading in 2023 would, if included naively in a 2022 backtest, appear to have zero value on 2022 days. Intersection avoids fabricating returns.
2. **Holiday misalignment**: Indian stocks don't trade on all NYSE holidays and vice versa. Intersection ensures the portfolio is only valued on days when all positions can be marked to market.

### Rebalance Execution

On each rebalance day, the engine:
1. Computes target weights from `holdings[]`
2. Values current portfolio at today's close
3. **Sells first**: for each stock that is over-weight, sell the excess shares at `close × (1 − slippage)`. Cash increases.
4. **Buys second**: for each stock that is under-weight, buy additional shares at `close × (1 + slippage)`. Cash decreases.
5. Residual cash stays in the portfolio (earning 0% — no bond allocation in the model).

The sell-then-buy order is critical: buying before selling could require more cash than is available from the initial capital alone.

### Drift-Triggered Rebalance

Fires when any single stock's actual weight deviates more than `DriftThreshold` from its target weight, checked daily. `DriftThreshold` is different from the daemon's `DriftIndex`: here it tracks per-stock deviation, not the total variation distance.

### Return Measurement

`TotalReturn = (finalValue − InitialCapital) / InitialCapital`

The first NAV snapshot already reflects slippage (buying at `close × (1 + slippage)` means fewer shares, hence lower portfolio value than `InitialCapital`). Using `InitialCapital` as the denominator correctly shows the investor the true P&L from day zero.

### Metrics Formulas

| Metric | Formula | Notes |
|--------|---------|-------|
| CAGR | `(final/initial)^(365/calDays) − 1` | calDays = calendar days from first to last snapshot |
| Max Drawdown | Running `peak − value) / peak`, minimum over all days | Negative fraction; e.g. −0.25 = 25% drawdown |
| Sharpe | `mean(excess_daily) / stddev(excess_daily) × √252` | Excess = daily return − daily risk-free (6% / 252) |
| Sortino | `mean(excess_daily) / downside_dev × √252` | Downside deviation uses full N in denominator, not just down-day count |
| Calmar | `CAGR / abs(MaxDrawdown)` | Higher = better recovery per unit of max pain |
| Beta | `cov(port_returns, bench_returns) / var(bench_returns)` | (n−1) cancels from both; pure ratio of variances |
| Alpha | `portCAGR − (0.06 + β × (benchCAGR − 0.06))` | Jensen's Alpha; 6% India risk-free rate |

---

## 9. Transaction Cost Model

### Charges Applied Per Trade

| Charge | Rate | Direction | Notes |
|--------|------|-----------|-------|
| STT | 0.1% of trade value | Both buy and sell | Securities Transaction Tax; mandatory |
| Stamp Duty | 0.015% of buy value | Buy only | Maharashtra stamp; not on sell |
| CDSL DP charge | ₹15.93 flat per ISIN | Sell only | Charged per ISIN per settlement day, regardless of qty |
| SEBI fee | 0.0001% of trade value | Both | Negligible but included |
| Exchange transaction charges | Not modeled | — | Out of scope per spec |

The DP charge is flat, not percentage-based. It makes small sell trades disproportionately expensive:

| Sell value | DP charge as % |
|-----------|----------------|
| ₹15,930 | 0.1% (acceptable) |
| ₹1,593 | 1.0% (marginal) |
| ₹500 | 3.2% (uneconomical) |

`FilterMicroTransactions` drops any order where `totalCosts / tradeValue > 0.005` (0.5% threshold). The DP charge is the primary trigger for this filter on small sell orders.

### Tax Classification (Finance Act 2024)

| Holding Period | Rate | Exemption |
|---------------|------|-----------|
| STCG (< 12 months) | 20% | None |
| LTCG (≥ 12 months) | 12.5% | First ₹1,25,000 gain per year |

`ClassifySell` returns `TaxUnknown` when the purchase date is not available from the broker (Zerodha API does not expose purchase dates in holdings). The basket command prints "check manually" warnings for these — it does not guess or default to a wrong classification.

---

## 10. Data Infrastructure

### DuckDB Cache (`data/cache.db`)

DuckDB via `duckdb-go/v2` serves as the persistent price and fundamentals store. Schema:

```sql
CREATE TABLE prices (
  ticker TEXT, range_key TEXT, fetched_at BIGINT,  -- BIGINT, not TIMESTAMP
  data JSON,
  PRIMARY KEY (ticker, range_key)
);
CREATE TABLE cache_meta (
  ticker TEXT, key TEXT, fetched_at BIGINT,
  PRIMARY KEY (ticker, key)
);
```

**BIGINT timestamps, not TIMESTAMP**: DuckDB v1.5.3 driver does not reliably scan `TIMESTAMP` columns into `time.Time` — they come back as zero value. All time columns are stored as Unix epoch seconds (BIGINT). Read with `time.Unix(n, 0)`. This is a driver bug; detecting it requires catching silent zero-value returns, which is why the schema enforces BIGINT.

### Cache Expiry Policy

| Data type | Cache key | Expiry |
|-----------|-----------|--------|
| Price range (historical) | `dr_YYYYMMDD_YYYYMMDD` | Never — past prices are immutable |
| Price range (ends today) | `dr_YYYYMMDD_{today}` | Fresh until 15:30 IST (market close) |
| Price range (keyword) | `1mo`, `3mo`, etc. | Fresh today if fetched after 15:30 IST |
| Fundamentals | `fundamentals` | 24-hour TTL |

Historical date-range keys never expire because stock prices for past dates do not change. This is the key optimization for backtesting: a 5-year backtest across 15 tickers fetches each ticker once and never re-fetches unless the cache is manually cleared.

### File Cache (`data/.cache/`)

Date-stamped JSON files for same-day price data. Used as a fallback when DuckDB is unavailable and as a warm cache for intraday performance tracking. Not used by the backtest engine (DuckDB only for date-range queries).

### IST Timezone Throughout

All date operations use `Asia/Kolkata` (UTC+5:30):

- **Cache staleness**: "today" is today in IST, not UTC. A price fetched at 11:30 UTC (17:00 IST) is fresh.
- **CleanIntradayNoise**: discards the last price point if the current IST time is before 15:30 (market has not closed yet).
- **Backtest calendar**: Yahoo Finance returns midnight IST = 18:30 UTC previous day for Indian stocks. `time.Unix(ts, 0).In(istLoc)` gives the correct trading date.
- **Daemon schedule**: fires at 15:45 IST (post-market close) using the IST timezone in the launchd plist interval calculation.

---

## 11. Design Decisions

### D1 — No Exec Subprocess in Pipeline

`mycase pipeline` calls inner `runXxxWithOpts()` functions directly — not `exec.Command`. This means one DuckDB connection, one Yahoo Finance session, one authenticated broker client for the entire pipeline run. A subprocess model would require re-reading config, re-opening cache, and re-authenticating for every step.

### D2 — MockBroker as Default, Not Error

`zerodha.New()` returns `MockBroker` (not an error) when credentials are absent. This means every command works in dry-run mode without documentation or special flags. The cost is that MockBroker returns pre-seeded sample data — drift index will read 0.5 against any real portfolio. Live mode requires `--live`.

### D3 — Daemon Uses OS Service Layer

No self-daemonization in Go. `mycase daemon install` writes a launchd plist (macOS) or prints a systemd unit (Linux). Process lifecycle (restart on crash, run at login, log rotation) is handled by the OS. State persists to `data/daemon_state.json` across restarts so `mycase daemon status` can report the last check even after a reboot.

### D4 — Web Dashboard Without a Framework (R8, implemented in pkg/server)

Plain HTML5 + ES2022 + native Web Components + Apache ECharts (vendored). No React, no HTMX, no build pipeline. `//go:embed static/*` bundles all assets into the binary. SSE for live quote streaming. HTMX was rejected because it cannot drive live chart updates (ECharts requires imperative JS calls); React adds a build step and framework dep to a tool meant to have zero install friction.

### D5 — Finance Act 2024 Rates

The system uses STCG 20% and LTCG 12.5% (₹1.25L exemption) — the rates enacted in Budget 2024, not the pre-2024 rates (15%/10%). NSE exchange transaction charges (0.00297% each side) are excluded from the cost model as out of scope.

### D6 — Broker Interface for Future Expansion

The `Broker` interface in `pkg/broker/broker.go` allows adding AngelOne SmartAPI, Fyers, or Upstox without changes to any command. Each broker would live in its own `pkg/broker/{name}/` directory. There are no official Go SDKs for Fyers/Angel — all would require custom HTTP clients.

### D7 — Autopilot as Separate Subcommand, Not a Pipeline Flag

The interactive pipeline has ~15 user prompts, file editing pauses, and report-opening commands. Rather than adding `--non-interactive` (which would require every new prompt to check a skip condition), `mycase autopilot run` is a clean, purpose-built non-interactive pipeline. It shares the internal `runPickWithOpts`, `runReportWithParams`, etc. but never calls `reader.ReadString`. The existing `mycase pipeline` stays as-is for manual inspection workflows.

### D8 — Autopilot Scheduling via launchd, Not In-Process Loop

The drift daemon uses an in-process sleep-until loop for daily checks — acceptable for 24h intervals. Quarterly autopilot runs span months, making a long-lived process impractical (memory leaks, OS updates, reboots). Instead, `mycase autopilot install` writes a launchd `StartCalendarInterval` plist that fires on the 2nd of Jan/Apr/Jul/Oct at 10:00 IST. The process runs once, does its work, exits. If the scheduled day is not a trading day (checked by attempting a quote fetch), the drift daemon (running daily at 15:45) picks up a retry marker and re-invokes next trading day.

### D9 — Proposal State Decouples Pipeline from Confirmation

After autopilot runs pick → optimize → report, it persists a `Proposal` JSON (`data/autopilot/pending_proposal.json`) containing proposed orders, cost breakdown, tax warnings, golden copy diff, and a 7-day expiry. The web dashboard reads this file to render the `/rebalance` confirmation page; the Telegram alert summarizes and links to it. This decouples the pipeline run from the confirmation step — they don't need to happen in the same process, and the investor can confirm hours or days later. Three server endpoints manage the lifecycle: `GET /api/autopilot/proposal`, `POST /api/autopilot/confirm`, `POST /api/autopilot/dismiss`.
