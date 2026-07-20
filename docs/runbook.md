# Mycase — Runbook

Practical usage guide: common workflows, every command with realistic examples, and tips for the non-obvious parts.

---

## Table of Contents

1. [Initial Setup](#1-initial-setup)
2. [Core Workflow: Pick → Optimize → Basket](#2-core-workflow)
3. [Backtesting](#3-backtesting)
4. [Performance Tracking](#4-performance-tracking)
5. [Drift Monitoring Daemon](#5-drift-monitoring-daemon)
6. [Cache Management](#6-cache-management)
7. [Pipeline (Full Automation)](#7-pipeline-full-automation)
8. [Command Reference](#8-command-reference)

---

## 1. Initial Setup

### Install the binary

```bash
make build && make install
# or: go install github.com/raghavkgarg/mycase@latest
mycase --version
```

### Authenticate with Zerodha (live mode only)

```bash
mycase auth
# Follow the prompts: paste your API key, open the login URL, paste the request token
# Saves access token to config/credentials.json
```

Dry-run mode works without credentials — commands fall back to `MockBroker` silently.

### Edit `config/pipeline.yaml`

```yaml
indices: [smallcap250]
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

# Pick from a custom CSV of tickers
mycase pick --file data/universe.csv --method balanced --top 20

# Pick with hysteresis (existing golden copy protects current holdings)
mycase pick --index smallcap250 --golden data/microsmall.csv \
    --method multibagger --top 15 --hysteresis-buffer 5

# Pick from NSE MidCap 150, aggressive strategy
mycase pick --index midcap150 --method aggressive --top 10
```

Output goes to `data/candidates/index_picks/{index}_{method}_{date}.csv`.

**Indices**: `nifty50`, `nifty100`, `nifty500`, `smallcap250`, `midcap150`, `microcap250`

**Methods**: `balanced`, `aggressive`, `conservative`, `multibagger`

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
```

The basket command automatically:
- Filters micro-transactions where `cost/value > 0.5%` (DP charge dominates small sell orders)
- Prints STCG/LTCG warnings for sell orders based on Finance Act 2024 rates
- Shows total transaction cost summary

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

---

## 5. Drift Monitoring Daemon

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
# Run all steps from config/pipeline.yaml
mycase pipeline

# Override specific params
mycase pipeline --index nifty500 --method aggressive --top 10 --capital 1000000
```

The pipeline runs: pick → optimize → report → performance → monitor, in sequence. Each step gets the output of the previous one. All steps share one process and one DuckDB connection.

---

## 8. Command Reference

### `pick`

| Flag | Default | Description |
|------|---------|-------------|
| `--index`, `-i` | `smallcap250` | Index to fetch constituents from |
| `--file`, `-f` | — | Custom CSV with `ticker` column (overrides `--index`) |
| `--method`, `-m` | `balanced` | Scoring strategy: `balanced`, `aggressive`, `conservative`, `multibagger` |
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
| positional arg | — | Portfolio name (shorthand for `data/{name}.csv`) |

### `performance`

| Flag | Default | Description |
|------|---------|-------------|
| `--file`, `-f` | required | Portfolio CSV |
| `--capital` | `100000` | Total invested capital |
| `--date` | today | Purchase date (`YYYY-MM-DD` or `YYYYMMDD`) |
| `--time` | `09:30` | Purchase time in IST (`HH:MM`), used only within 7-day intraday window |

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

---

## Common Mistakes

**"no common trading days found"** during backtest: The `--from` date predates when one of your tickers started trading. Try a later `--from`, or remove the newer ticker from the CSV.

**Tax warnings show "check manually"**: The broker API doesn't expose purchase dates. `STCG/LTCG` classification requires you to verify against your broker's P&L statement.

**Micro-transaction filtered**: A sell order was dropped because the DP charge (₹15.93 flat) exceeded 0.5% of the trade value. This is correct behavior for small positions — executing a ₹500 sell incurs 3%+ in charges.

**Drift index is always 0.5 with MockBroker**: MockBroker returns pre-seeded sample holdings that don't match your portfolio CSV. Run with `--live` for real drift measurements.
