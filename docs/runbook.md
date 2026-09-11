# Mycase — Runbook

Practical usage guide: common workflows, every command with realistic examples, and tips for the non-obvious parts.

---

## Table of Contents

1. [Initial Setup](#1-initial-setup)
2. [Core Workflow: Pick → Optimize → Basket](#2-core-workflow)
3. [Backtesting](#3-backtesting)
4. [Performance Tracking](#4-performance-tracking)
5. [Portfolio Health & Drift Monitoring](#5-portfolio-health--drift-monitoring)
6. [Cache Management & Consolidated DuckDB](#6-cache-management)
7. [Pipeline (Full Automation)](#7-pipeline-full-automation)
7b. [Tax-Loss Harvesting (US)](#7b-tax-loss-harvesting-us)
8. [Unified Database Operations (`db`)](#8-unified-database-operations-db)
9. [Theme Lifecycle & Versioning (`theme`)](#9-theme-lifecycle--versioning-theme)
10. [Point-in-Time Quantitative Research (`pit`)](#10-point-in-time-quantitative-research-pit)
11. [Automated Daily Sync (`daily_sync.sh`)](#11-automated-daily-sync-dailysyncsh)
12. [Web Dashboard Server](#12-web-dashboard-server)
13. [Command Reference](#13-command-reference)

---

## 1. Initial Setup

### Install the binary

```bash
make build && make install
# or: go install github.com/raghavkgarg/mycase@latest
mycase --version
```

### Authenticate with Zerodha (live mode only)

Run the interactive authentication utility to link your Zerodha Kite Connect account:

```bash
mycase auth
```
1. Enter your Zerodha Kite API Key & API Secret (if prompted for first time).
2. The browser automatically opens the Zerodha login page.
3. Log in and authorize — local server handles the callback automatically on `http://localhost:8000`.
4. Saves API key, secret, and generated `access_token` to `config/config.json`.

> [!NOTE]
> Zerodha Kite access tokens expire daily. If any `--live` command returns `Incorrect api_key or access_token`, simply re-run `mycase auth` to refresh your token for the day.

Dry-run mode works without credentials — commands fall back to `MockBroker` silently when run without `--live`.

### Edit `config/pipeline.yaml`

```yaml
# Select constituents via built-in indices OR custom CSV / Excel (.xlsx) files
indices: [smallcap250]
# Or specify custom files:
# file: data/qtum.xlsx
# files: [data/qtum.xlsx, data/my_universe.csv]

method: multibagger
top_n: 15
capital: 500000
rebalance_tolerance: 0.10     # 0.10% weight tolerance for rebalancing bands
hysteresis_buffer: 5           # allow existing holdings to be up to rank 20 before exit

alerts:
  drift_threshold: 0.05        # alert when drift index > 5%
  channels: [telegram]
  telegram_bot_token: "..."    # or: export MYCASE_TELEGRAM_TOKEN=...
  telegram_chat_id: "..."
```

---

## 2. Core Workflow

### Step 1: Pick stocks

```bash
# Pick top 15 multibagger candidates from NSE SmallCap 250
mycase pick --index smallcap250 --method multibagger --top 15

# Pick from a custom CSV or Excel (.xlsx) file of tickers (auto-converted seamlessly)
mycase pick --file data/qtum.xlsx --method multibagger --top 10
mycase pick --file data/universe.csv --method balanced --top 20

# Convert an ETF / broker Excel file to clean CSV using the helper tool
mycase convert data/qtum.xlsx data/qtum.csv

# Pick with hysteresis (existing golden copy protects current holdings)
mycase pick --index smallcap250 --golden data/microsmall.csv \
    --method multibagger --top 15 --hysteresis-buffer 5

# Pick from NSE MidCap 150, aggressive strategy
mycase pick --index midcap150 --method aggressive --top 10

# Pick top 20 US quality-momentum stocks from S&P 500
mycase pick --index sp500 --method us_quality_momentum --top 20

# Pick US stocks with hysteresis (protect existing US holdings)
mycase pick --index sp500 --method us_quality_momentum --top 20 \
    --golden data/us_portfolio.csv --hysteresis-buffer 5
```

Output goes to `data/candidates/index_picks/{index}_{method}_{date}.csv`.

**Indices**: `nifty50`, `nifty100`, `nifty500`, `smallcap250`, `midcap150`, `microcap250`, `sp500`

**Methods**: `balanced`, `aggressive`, `conservative`, `multibagger`, `value`, `us_quality_momentum`

### Step 2: Optimize weights

```bash
# Optimize with MFS multi-factor, 15% max per stock
mycase optimize --file data/candidates/index_picks/smallcap250_multibagger_2026-07-20.csv \
    --method mfs --cap 0.15

# Inverse-volatility, no cap
mycase optimize --file data/my_picks.csv --method volatility

# Update an existing golden copy (weight-only update, preserves all tickers)
mycase optimize --file data/candidates/... --method mfs --golden data/microsmall.csv

# Remove a stock from the golden copy
mycase optimize --file data/microsmall.csv --remove "NSE:SUZLON"
```

### Step 3: Preview or execute basket orders

```bash
# Dry-run (default): shows what orders would be placed, cost summary, tax warnings
mycase basket data/microsmall

# Live execution via Zerodha
mycase basket --live data/microsmall

# Use specific CSV path
mycase basket --file data/candidates/my_new_basket.csv

# US portfolio: sequence orders to harvest losses first, flag wash sales
mycase basket --live --tax-optimize data/us_quality
```

The basket command automatically:
- Filters micro-transactions where `cost/value > 0.5%` (DP charge dominates small sell orders)
- Prints STCG/LTCG warnings for sell orders based on Finance Act 2024 rates (India) or federal ST/LT rates (US)
- Shows total transaction cost summary

With `--tax-optimize` (US only), it reorders the batch so loss-harvesting sells execute before gain sells and buys, and warns if a buy would repurchase a security sold at a loss (wash sale). This uses the FIFO lots from `mycase tax import` (see §7b), so US sell warnings show real short/long-term status and cost basis instead of "Unknown".

---

## 3. Backtesting

```bash
# 3-year quarterly backtest vs Nifty 50
mycase backtest --file data/microsmall.csv --capital 500000 \
    --from 2022-01-01 --rebalance quarterly --benchmark ^NSEI

# Monthly rebalance, 0.1% slippage per trade
mycase backtest --file data/microsmall.csv --capital 1000000 \
    --from 2023-01-01 --to 2026-01-01 \
    --rebalance monthly --slippage 0.1 --benchmark ^CNXSC

# Drift-triggered rebalance (fires when any stock drifts > 5% from target)
mycase backtest --file data/microsmall.csv --capital 500000 \
    --from 2023-06-01 --rebalance drift-triggered --drift-threshold 5.0

# Use portfolio name shorthand
mycase backtest microsmall --capital 500000 --from 2022-01-01
```

**Output includes**: Total Return, CAGR, Max Drawdown, Sharpe, Sortino, Calmar, Alpha, Beta, year-by-year breakdown.

**Benchmarks**: `^NSEI` (Nifty 50), `^CNXSC` (Nifty SmallCap 250), `^CNXMID` (Nifty MidCap 150)

**Note on slippage**: Default is 0 (no slippage). For realistic simulation, use 0.05–0.1%. Slippage applies on every buy and sell during rebalancing.

---

## 4. Performance Tracking

Track P&L from a specific purchase date to now:

```bash
# From a specific date (daily close mode)
mycase performance --file data/microsmall.csv --capital 500000 --date 2025-01-15

# From a specific time today (intraday mode, within 7 days)
mycase performance --file data/microsmall.csv --capital 500000 \
    --date 2026-07-18 --time 09:30

# Date formats: YYYY-MM-DD or YYYYMMDD
mycase performance --file data/microsmall.csv --capital 500000 --date 20250115
```

For purchase dates > 7 days ago, the command automatically switches to daily close mode and selects the appropriate Yahoo Finance range (1mo/3mo/6mo/1y/2y/5y).

### Holdings Snapshot

View current Zerodha portfolio holdings grouped by theme (`config/themes.json`) with P&L and allocation breakdown:

```bash
# Dry-run / mock holdings snapshot
mycase holdings

# Live holdings snapshot directly from Zerodha account
mycase holdings --live
```

Snapshots are automatically formatted and saved to `holding/holding_YYYYMMDD.txt`.

---

## 5. Portfolio Health & Drift Monitoring

### Interactive 4-Pillar Health Simulation

Run an interactive portfolio health simulation scoring holdings across 4 pillars (Capital Stall, Fundamental Drift, Valuation Stretch, Risk/Volatility):

```bash
# Basic interactive health simulation
mycase monitor --file data/microsmall.csv --interactive

# Specify strategy preset and simulation start date
mycase monitor --file data/microsmall.csv --interactive --strategy multibagger --date 2026-01-01

# Run with style preset (hyper-aggressive, moderate, passive) and custom capital
mycase monitor --file data/microsmall.csv --style hyper-aggressive --capital 500000
```

### Drift Monitoring Daemon

The daemon checks drift at 15:45 IST daily (post-market close) and sends alerts when `DriftIndex > threshold`.

### One-shot check (no loop)

```bash
# Check drift against mock holdings
mycase daemon check --file data/microsmall.csv

# Check drift against live Zerodha holdings
mycase daemon check --live --file data/microsmall.csv
```

### Install as system service

```bash
# macOS (launchd): writes ~/Library/LaunchAgents/com.mycase.daemon.plist
mycase daemon install
launchctl load ~/Library/LaunchAgents/com.mycase.daemon.plist

# Linux: prints systemd unit — pipe to systemd or copy manually
mycase daemon install > /etc/systemd/system/mycase.service
systemctl enable --now mycase
```

### Manual start/stop

```bash
mycase daemon start          # blocking loop, use tmux or background it
mycase daemon status         # show last check from data/daemon_state.json
mycase daemon stop           # sends SIGTERM to running daemon
mycase daemon uninstall      # removes launchd plist
```

### Drift threshold

Set in `config/pipeline.yaml`:
```yaml
alerts:
  drift_threshold: 0.05   # 5% total variation distance triggers alert
```

`DriftIndex = 0.05` means 5% of the portfolio value has drifted to the wrong stock. At `0.10` it's a significant rebalancing signal.

---

## 6. Cache Management


```bash

# Show cache state: row counts and last fetch timestamps per ticker
mycase cache status

# Clear a specific ticker (forces re-fetch next time)
mycase cache clear --ticker NSE:TCS

# Wipe the entire price cache
mycase cache clear --all
```

The unified DuckDB database (`data/mycase.db`) stores:
- **Prices**: permanent for historical dates; same-day freshness for today's prices
- **Fundamentals**: 24h TTL (re-fetched if older than 24 hours)
- **Date-range queries** (used by backtest): historical ranges never expire
- **Metadata**: tracked in `cache_meta` with validated date bounds to guarantee cache integrity

---

## 7. Pipeline (Full Automation)

```bash
# Go to Project
cd /Users/raghavgarg/Projects/mygo/mycase

# Run all steps configured in config/pipeline.yaml (default config path)
mycase pipeline

# Run with a custom configuration YAML path
mycase pipeline --config config/pipeline.yaml

# Run directly via go run (without installing the binary)
go run main.go pipeline --config config/pipeline.yaml

# Run via Makefile
make run ARGS="pipeline"

# Override specific params from pipeline.yaml via CLI flags
mycase pipeline --index nifty500 --method aggressive --top 10 --capital 1000000

# Start directly from execution steps (auth + basket execution)
mycase pipeline --exec-only
```

`config/pipeline.yaml` supports selecting constituents by `indices`, `file` (single custom CSV/XLSX), `files` (multiple custom files), or combining `indices` and `files` together.

### 11-Step Automated Pipeline Architecture

When running multi-index pipelines (e.g. `microcap250` + `small250` into `microsmall.csv`), the automated runner executes an 11-step end-to-end selection, pruning, report generation, and trade execution workflow:

1. **Step 1/11 — Initial Candidate Pick (Primary Source)**:
   - Downloads/loads universe constituents (e.g., `microcap250`).
   - Fetches 1Y price history and Yahoo Finance fundamentals (ROCE, CROIC, DSO, Debt/Equity, Promoter Pledging).
   - Applies Hard Safety Filters (`mfs.json`) to eliminate non-compliant stocks.
   - Calculates 100-point Relative Scoring Matrix with Sector Caps and Hysteresis Buffer.
   - Saves index pick results to `data/candidates/index_picks/<source>_<strategy>.csv` and selection reasons report to `report/<universe>/executions/<date>_01_selection_reasons.txt`.

2. **Step 2/11 — Candidate Pick (Secondary Source)**:
   - Evaluates second universe (e.g., `small250`) through identical safety filters and scoring models.
   - Saves index pick results to `data/candidates/index_picks/<source>_<strategy>.csv`.

3. **Step 3/11 — Multi-Source Candidate Combination**:
   - Merges candidate sets from Steps 1 & 2 into temporary combined file `data/candidates/temp/combine_<goldenBase>.csv`.

4. **Step 4/11 — Combined Proposal Selection (Top N + 5 Draft)**:
   - Evaluates combined pool against golden copy holdings with hysteresis buffer.
   - Selects draft Top $N+5$ proposal (e.g., Top 25) to allow manual review room.
   - Saves draft proposal to `data/candidates/proposals/<date>_<goldenBase>_<strategy>.csv`.
   - Prompts user if they want to manually remove any unwanted shares before weight optimization.

5. **Step 5/11 — Proposal Pruning to Top N & Single Portfolio Comparison**:
   - Prunes draft proposal down to exact Top $N$ target (e.g., Top 20).
   - Calculates normalized target portfolio weights and saves optimized file `data/candidates/proposals/<date>_<goldenBase>_<strategy>_optim.csv`.
   - Generates automated Scuttlebutt research check (`..._scuttlebutt.txt`).
   - **Prints the single consolidated `PORTFOLIO COMPARISON REPORT` table to stdout** and saves it to `report/<goldenBase>_<strategy>/executions/<date>_02_comparison.txt`.

6. **Step 6/11 — Golden Copy Update & Automated Backup**:
   - Prompts user to open and review the saved comparison report.
   - Upon confirmation (`y`), creates a timestamped safety backup in `data/backups/<goldenBase>/bk_<timestamp>.csv`.
   - Merges newly selected candidates into golden copy (`data/<goldenBase>.csv`).
   - Retains exited tickers at `0.0000` weight to trigger liquidation orders.

7. **Step 7/11 — Explanation & Portfolio Report Generation**:
   - Generates comprehensive portfolio breakdown and saves report to `report/<goldenBase>_<strategy>/executions/<date>_03_portfolio_report.txt`.

8. **Step 8/11 — Capital & Purchase Performance Simulation**:
   - Prompts for initial capital and purchase date.
   - Simulates holding performance against market benchmarks (`^NSEI`).

9. **Step 9/11 — 4-Pillar Health & Drift Monitoring Simulation**:
   - Evaluates portfolio against historical backtest or custom date timeframe.
   - Scores Capital Stall, Fundamental Drift, Valuation Stretch, and Volatility pillars.
   - Saves simulation report to `report/<goldenBase>_<strategy>/simulations/<timestamp>_monitoring.txt`.

10. **Step 10/11 — Zerodha Authentication Setup**:
    - Launches browser OAuth flow via `mycase auth` if token expired (skipped automatically for US market portfolios).

11. **Step 11/11 — Live / Dry-Run Basket Order Execution**:
    - Previews or executes Zerodha basket orders via Kite API.
    - Applies micro-transaction cost filter (`cost/value > 0.5%`) and prints STCG/LTCG tax alerts.

The pipeline runs shared in-memory data fetching and one DuckDB connection across all 11 steps for maximum efficiency.

---

## 7b. Autopilot (Scheduled Non-Interactive Pipeline)

The autopilot runs the same pick → optimize → merge → compute-orders steps as the interactive pipeline, but without any user prompts. It generates a proposal, sends an alert, and waits for confirmation.

### Run manually (one-shot)

```bash
# Dry-run with mock broker (for testing)
mycase autopilot run --skip-trading-day-check

# Live run (uses real holdings/quotes for order computation)
mycase autopilot run --live
```

### Check status

```bash
mycase autopilot status
```

Shows: pending proposal details, expiry countdown, and next scheduled run date.

### Confirm or dismiss proposals

Proposals are confirmed via the web dashboard (`http://localhost:8080/#/rebalance`) or dismissed via CLI:

```bash
mycase autopilot dismiss
```

### Install as scheduled service

```bash
# macOS (launchd): fires quarterly on Jan 2, Apr 2, Jul 2, Oct 2 at 10:00 IST
mycase autopilot install
launchctl load ~/Library/LaunchAgents/com.mycase.autopilot.plist

# Linux: prints systemd unit + timer
mycase autopilot install
```

### Uninstall

```bash
mycase autopilot uninstall
```

### Configuration

Add to `config/pipeline.yaml`:

```yaml
schedule:
  frequency: quarterly          # quarterly, monthly, or drift-triggered
  day: first_trading_day        # first_trading_day, last_trading_day, or day number
  notify: [telegram]            # alert channels
  auto_execute: false           # true = skip confirmation (dangerous)
  drift_trigger_pct: 15         # mid-quarter drift % for early rebalance (0 = disabled)
  proposal_ttl_days: 7          # days before proposal expires
```

### Workflow

1. Autopilot fires (scheduled or manual)
2. If not a trading day → skips, retries next day
3. Runs stock selection → weight optimization → golden copy merge
4. Computes rebalance orders against live holdings
5. Saves proposal to `data/autopilot/pending_proposal.json`
6. Sends Telegram/Discord alert with summary + dashboard link
7. Investor reviews at `http://localhost:8080/#/rebalance`
8. Clicks "Confirm & Execute" → orders placed via broker
9. Confirmation alert sent

---

## 7b. Tax-Loss Harvesting (US)

FIFO lot tracking and tax-loss harvesting for US portfolios. Requires Schwab auth (`mycase auth --broker schwab`).

```bash
# 1. Import transaction history and build FIFO lots (run after auth, then quarterly)
mycase tax import --broker schwab --years 3

# 2. Review open lots + realized gains (YTD and all-time, short/long-term split)
mycase tax status

# 3. Find harvestable losses with estimated tax saving + substitute suggestions
mycase tax harvest --min-loss 50
```

Then, when rebalancing, add `--tax-optimize` to sequence loss sells first and flag wash sales:

```bash
mycase basket --live --tax-optimize data/us_quality
```

Notes:
- **Lots are derived state.** `tax import` recomputes lots and realized gains from the full stored transaction history every run, so re-importing is safe and idempotent (keyed on Schwab `activityId`).
- **Wash-sale rule.** A loss is disallowed if you buy the same (or substantially identical) security within 30 days before or after the loss sale. `tax harvest` flags at-risk positions; `basket --tax-optimize` warns if a buy in the batch would trigger one. The tool advises — it does not block.
- **Substitutes.** Harvest suggestions propose a same-sector name from the universe that you don't already hold, to keep factor exposure without repurchasing the sold security.
- **Coverage limits.** Schwab positions expose only a blended average price; accurate lots depend on the `/transactions` history. Positions older than the account's transaction window can't be reconstructed. Sells with no matching buy history are reported as warnings, not fabricated lots.

The dashboard **Tax** tab (`#/tax`) shows the same data: realized gains (YTD + all-time), harvest candidates, a wash-sale calendar, and open lots with unrealized P&L.

---

---

## 8. Unified Database Operations (`db`)

All market data, quantitative research snapshots, pipeline staging, and theme history reside in a single ACID-compliant DuckDB database: **`data/mycase.db`**.

```bash
# View table row counts, domain layers, and online status across all 14 tables/views
mycase db stats

# Run unified EOD update across market cache, PIT screening, and theme sync
mycase db update --all --index niftytotalmarket --method earlymb --top 10

# Shorthand via top-level flags:
mycase --database --update
# or:
mycase -db -u

# Preview update sequence without writing to DuckDB:
mycase db update --dry-run
```

The database partitions into 4 architectural domains:
1. **Market Data Cache**: `prices`, `fundamentals`, `cache_meta`
2. **PIT Quant Research**: `pit_runs`, `pit_candidate_scores`, `v_pit_candidate_scores`, `v_pit_runs`, `index_constituents`
3. **Pipeline Staging**: `pipeline_runs`, `index_picks`, `proposals`, `selections`
4. **Theme Lifecycle**: `theme_rebalances`, `theme_history`

---

## 9. Theme Lifecycle & Versioning (`theme`)

Manage active investment themes (configured in `config/themes.json`) with version-controlled constituent turnover and rebalance history.

```bash
# Display active constituent roster and weight breakdown for a theme
mycase theme show microsmall

# Display chronological rebalance history, dates, sources, and turnover %
mycase theme history microsmall

# Synchronize or backfill theme rebalances from proposal CSVs into mycase.db
mycase theme sync

# Filter by exit state (e.g. view dropped constituents)
mycase theme show microsmall --exited
```

---

## 10. Point-in-Time Quantitative Research (`pit`)

Institutional-grade quantitative research framework that evaluates stocks using 2-stage gating and invariant 4-pillar scoring.

```bash
# Run daily PIT screening and persist candidate scores to data/mycase.db
mycase pit update --index niftytotalmarket --method earlymb --top 10

# Query rolling empirical score quantiles (P40, P50, P75, P90) across Stage-1 survivors
# (Uses dynamic SQL views to query sub-indices on-the-fly with zero row duplication!)
mycase pit stats --index microcap250 --method earlymb
mycase pit stats --index smallcap250 --method earlymb
mycase pit stats --index NIFTY50 --method earlymb

# Inspect chronological score, VCP ATR tightness, and RVOL trajectory for a specific stock
mycase pit stats --ticker INOXINDIA

# Targeted recovery: re-fetch and re-score failed candidates without re-evaluating 750 stocks
mycase pit retry --index niftytotalmarket --method earlymb --date 2026-09-10

# Run deep 8-section quantitative deduction analysis (funnel, sentry, quantiles, incubator, velocity)
mycase --index niftytotalmarket --method earlymb --analysis
```

---

## 11. Automated Daily Sync (`daily_sync.sh`)

To eliminate manual daily terminal commands, post-market updates are automated via `scripts/daily_sync.sh` scheduled via macOS LaunchAgent (`~/Library/LaunchAgents/com.mycase.daily_sync.plist`) at **21:00 IST (9:00 PM)** Monday through Friday. Running at 9:00 PM IST guarantees that NSE Bhavcopy, Security-wise Deliverable Positions (MTO), and Yahoo Finance settled daily candles are 100% available without race conditions.

### Execution Flow:
1. **Weekend & Holiday Guard**: Skips execution on Saturdays, Sundays, and NSE market holidays.
2. **Broker Authentication**: Runs `mycase auth` to refresh or verify the Zerodha Kite session.
3. **Trade Ingestion**: Navigates to `myportfolio` and runs `./dist/myportfolio fetch-trades` to pull today's broker fills into `portfolio.db`.
4. **Unified Database Update**: Runs `./mycase db update --all --index niftytotalmarket --method earlymb --top 10` to warm caches, screen Nifty Total Market, and sync theme rebalances into `data/mycase.db`.

Logs stream with timestamps to `logs/pit_update.log`.

---

## 12. Web Dashboard Server

Launch the web UI dashboard in your browser to visualize portfolio allocations, factor scores, backtest forms, drift timelines, and order previews:

```bash
# Start dashboard on default port (http://localhost:8080)
mycase serve

# Run on a custom port
mycase serve --port 3000

# Run with live Zerodha API connection
mycase serve --port 8080 --live
```

Once started, open `http://localhost:8080` in your web browser.

Features included in the web dashboard:
- Interactive backtest controls and performance factor visualization
- Dynamic weight distribution donuts & comparison charts (powered by ECharts)
- Real-time stock health & monitoring table
- Basket order preview and tax impact breakdown
- Tax tab (US): realized gains (YTD + all-time), harvest candidates, wash-sale calendar, open lots

---

## 13. Command Reference

### `auth`

| Command | Flags | Description |
|---------|-------|-------------|
| `mycase auth` | — | Interactively authenticate with Zerodha Kite Connect and save daily access token |

### `pick`

| Flag | Default | Description |
|------|---------|-------------|
| `--index`, `-i` | `smallcap250` | Index to fetch constituents from |
| `--file`, `-f` | — | Custom CSV with `ticker` column (overrides `--index`) |
| `--method`, `-m` | `balanced` | Scoring strategy: `balanced`, `aggressive`, `conservative`, `multibagger`, `value`, `us_quality_momentum` |
| `--top` | `20` | Number of top-ranked stocks to select |
| `--range` | `3mo` | Price history range for factor calculations |
| `--golden` | — | Golden copy CSV; enables hysteresis and rebalancing bands |
| `--rebalance-tolerance` | `0.10` | Weight band %; stocks within band are not re-ranked |
| `--hysteresis-buffer` | `5` | Extra ranks before an existing holding is evicted |

### `optimize`

| Flag | Default | Description |
|------|---------|-------------|
| `--file`, `-f` | required | Input portfolio CSV |
| `--method`, `-m` | `volatility` | `volatility`, `mfs`, `equal` |
| `--cap` | `1.0` | Maximum weight per stock (0.0–1.0) |
| `--golden` | — | Write result into this golden copy |
| `--remove` | — | Comma-separated tickers to zero-weight and remove |

### `backtest`

| Flag | Default | Description |
|------|---------|-------------|
| `--file`, `-f` | required | Portfolio CSV |
| `--capital` | `100000` | Initial capital in INR |
| `--from` | required | Start date (`YYYY-MM-DD`) |
| `--to` | today | End date (`YYYY-MM-DD`) |
| `--rebalance` | `quarterly` | `monthly`, `quarterly`, `drift-triggered` |
| `--slippage` | `0.0` | Slippage per trade in % (e.g. `0.1` = 0.1%) |
| `--benchmark` | `^NSEI` | Benchmark ticker for Alpha/Beta |
| `--drift-threshold` | `5.0` | Drift % to trigger rebalance (drift-triggered mode) |

### `basket`

| Flag | Default | Description |
|------|---------|-------------|
| `--live` | false | Use live Zerodha API (dry-run without this flag) |
| `--file` | `data/basket.csv` | Portfolio CSV path |
| `--tax-optimize` | false | US: sequence loss sells first, flag wash sales (needs `tax import`) |
| positional arg | — | Portfolio name (shorthand for `data/{name}.csv`) |

### `tax`

| Command | Flags | Description |
|---------|-------|-------------|
| `mycase tax import` | `--broker` (`schwab`), `--years` (`3`) | Import broker transaction history and rebuild FIFO lots |
| `mycase tax status` | — | Show open lots and YTD/all-time realized gains/losses |
| `mycase tax harvest` | `--min-loss` (`50`), `--live` | Identify tax-loss harvesting candidates |

### `performance`

| Flag | Default | Description |
|------|---------|-------------|
| `--file`, `-f` | required | Portfolio CSV |
| `--capital` | `100000` | Total invested capital |
| `--date` | today | Purchase date (`YYYY-MM-DD` or `YYYYMMDD`) |
| `--time` | `09:30` | Purchase time in IST (`HH:MM`), used only within 7-day intraday window |

### `monitor`

| Flag | Default | Description |
|------|---------|-------------|
| `--file`, `-f` | `data/microsmall.csv` | Portfolio CSV path |
| `--interactive` | false | Run in interactive terminal mode |
| `--strategy` | `balanced` | Weighting strategy preset (`balanced`, `aggressive`, `conservative`, `multibagger`) |
| `--date` | — | Start date for simulation (`YYYY-MM-DD`) |
| `--style` | `moderate` | Monitoring style preset (`hyper-aggressive`, `moderate`, `passive`) |
| `--capital` | `100000.0` | Initial capital invested |

### `daemon`

| Subcommand | Flags | Description |
|-----------|-------|-------------|
| `start` | `--live`, `--config`, `--file` | Start blocking drift loop |
| `stop` | — | SIGTERM the running daemon |
| `status` | — | Print last state from `data/daemon_state.json` |
| `check` | `--live`, `--config`, `--file` | One-shot check |
| `install` | — | Write launchd plist (macOS) or print systemd unit |
| `uninstall` | — | Remove launchd plist |

### `cache`

| Subcommand | Flags | Description |
|-----------|-------|-------------|
| `status` | — | Row counts and last-fetch timestamps |
| `clear` | `--ticker`, `--all` | Evict ticker or wipe all |

### `convert`

| Flag | Default | Description |
|------|---------|-------------|
| `--file`, `-f` | positional arg | Input Excel (`.xlsx`) portfolio or ETF holdings file |
| `--output`, `-o` | `<input>.csv` | Path to output clean CSV file |

### `holdings`

| Flag | Default | Description |
|------|---------|-------------|
| `--live` | `false` | Fetch live Zerodha holdings (default: mock holdings) |

### `merge`

| Subcommand | Arguments | Description |
|-----------|-----------|-------------|
| `combine` | `<out_csv> <in1_csv> <in2_csv>...` | Combine multiple candidate CSV files into one |
| `golden` | `<source_csv> <dest_csv>` | Merge source CSV into golden copy (preserves exited tickers at 0 weight) |

### `pipeline`

| Flag | Default | Description |
|------|---------|-------------|
| `--config` | `config/pipeline.yaml` | Path to pipeline YAML configuration file |
| `--exec-only` | `false` | Start directly from execution steps (auth + basket execution) |
| `--index`, `-i` | — | Index to pick stocks from (e.g. `nifty50`, `smallcap250`) |
| `--file`, `-f` | — | Path to custom CSV/XLSX file |
| `--strategy`, `-m` | — | Scoring strategy (`balanced`, `aggressive`, `conservative`, `multibagger`, `value`) |
| `--top`, `-n` | — | Number of top stocks to pick |
| `--golden` | — | Path to golden copy CSV for hysteresis and rebalancing band |
| `--capital` | — | Initial capital for performance simulation |
| `--purchase-date`, `--date` | — | Purchase date for performance simulation (`YYYY-MM-DD`) |
| `--rebalance-tolerance` | — | Rebalancing weight tolerance % (e.g. `0.10` for 0.10%) |
| `--hysteresis-buffer` | — | Extra ranks to allow existing holdings to drift |

### `report`

| Flag | Default | Description |
|------|---------|-------------|
| `--file`, `-f` | required | Path to stockpicker output CSV file |
| `--method`, `-m` | `balanced` | Weighting strategy (`balanced`, `aggressive`, `conservative`, `multibagger`) |

### `serve`

| Flag | Default | Description |
|------|---------|-------------|
| `--port` | `8080` | HTTP server port for local dashboard |
| `--live` | `false` | Connect with live Zerodha API (default: mock broker) |

### `autopilot`

| Subcommand | Flags | Description |
|-----------|-------|-------------|
| `run` | `--live`, `--config`, `--skip-trading-day-check` | Run non-interactive pipeline, generate proposal, send alerts |
| `status` | `--config` | Show pending proposal and next scheduled run |
| `dismiss` | — | Dismiss pending proposal without executing |
| `install` | `--config` | Install launchd plist (macOS) or print systemd unit (Linux) |
| `uninstall` | — | Remove installed service |

### `db`

| Subcommand | Flags | Description |
|-----------|-------|-------------|
| `stats` | — | Display row counts, domain layers, and online status across all 14 tables/views in `data/mycase.db` |
| `update` | `--all`, `--index`, `--method`, `--top`, `--dry-run` | Run unified EOD sequence (market cache warming, PIT quant screening, and theme lifecycle sync) |

### `theme`

| Subcommand | Arguments / Flags | Description |
|-----------|-------------------|-------------|
| `show` | `<theme_name> [--exited]` | Display active (or exited) constituent roster and weights for a theme |
| `history` | `<theme_name>` | Display chronological rebalance versions, dates, sources, and constituent turnover |
| `sync` | — | Synchronize/backfill theme rebalances from proposal CSVs into `data/mycase.db` |

### `pit`

| Subcommand | Flags | Description |
|-----------|-------|-------------|
| `update` | `--index`, `--method`, `--top` | Execute daily 4-pillar PIT screening and persist candidate scores to `data/mycase.db` |
| `stats` | `--index`, `--method`, `--ticker` | Query rolling empirical quantiles ($P_{40}, P_{50}, P_{75}, P_{90}$) or individual stock trajectories |
| `retry` | `--index`, `--method`, `--date` | Targeted recovery: re-fetch and re-score failed candidates for a historical date |

---

## Common Mistakes

**"no common trading days found"** during backtest: The `--from` date predates when one of your tickers started trading. Try a later `--from`, or remove the newer ticker from the CSV.

**Tax warnings show "check manually"**: The broker API doesn't expose purchase dates. `STCG/LTCG` classification requires you to verify against your broker's P&L statement.

**Micro-transaction filtered**: A sell order was dropped because the DP charge (₹15.93 flat) exceeded 0.5% of the trade value. This is correct behavior for small positions — executing a ₹500 sell incurs 3%+ in charges.

**Drift index is always 0.5 with MockBroker**: MockBroker returns pre-seeded sample holdings that don't match your portfolio CSV. Run with `--live` for real drift measurements.
