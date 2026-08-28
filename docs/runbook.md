# Mycase — Runbook

Practical usage guide: common workflows, every command with realistic examples, and tips for the non-obvious parts.

---

## Table of Contents

1. [Initial Setup](#1-initial-setup)
2. [Core Workflow: Pick → Optimize → Basket](#2-core-workflow)
3. [Backtesting](#3-backtesting)
4. [Performance Tracking](#4-performance-tracking)
5. [Portfolio Health & Drift Monitoring](#5-portfolio-health--drift-monitoring)
6. [Cache Management](#6-cache-management)
7. [Pipeline (Full Automation)](#7-pipeline-full-automation)
7b. [Tax-Loss Harvesting (US)](#7b-tax-loss-harvesting-us)
8. [Web Dashboard Server](#8-web-dashboard-server)
9. [Command Reference](#9-command-reference)

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

The DuckDB cache (`data/cache.db`) stores:
- **Prices**: permanent for historical dates; same-day freshness for today's prices
- **Fundamentals**: 24h TTL (re-fetched if older than 24 hours)
- **Date-range queries** (used by backtest): historical ranges never expire

---

## 7. Pipeline (Full Automation)

```bash
# Run all steps configured in config/pipeline.yaml (supports indices, file, or files)
mycase pipeline

# Override specific params
mycase pipeline --index nifty500 --method aggressive --top 10 --capital 1000000
```

`config/pipeline.yaml` supports selecting constituents by `indices`, `file` (single custom CSV/XLSX), `files` (multiple custom files), or combining `indices` and `files` together.

The pipeline runs: pick → optimize → report → performance → monitor, in sequence. Each step gets the output of the previous one. All steps share one process and one DuckDB connection.

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

## 8. Web Dashboard Server

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

## 9. Command Reference

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

---

## Common Mistakes

**"no common trading days found"** during backtest: The `--from` date predates when one of your tickers started trading. Try a later `--from`, or remove the newer ticker from the CSV.

**Tax warnings show "check manually"**: The broker API doesn't expose purchase dates. `STCG/LTCG` classification requires you to verify against your broker's P&L statement.

**Micro-transaction filtered**: A sell order was dropped because the DP charge (₹15.93 flat) exceeded 0.5% of the trade value. This is correct behavior for small positions — executing a ₹500 sell incurs 3%+ in charges.

**Drift index is always 0.5 with MockBroker**: MockBroker returns pre-seeded sample holdings that don't match your portfolio CSV. Run with `--live` for real drift measurements.
