# mycase

An automated equity factor-tilt system. It picks stocks by a transparent, quantitative
score, sizes positions under sector and concentration caps, proposes orders for the investor
to confirm, executes them through a broker, and then audits performance and watches the
portfolio for drift between rebalances. It runs as a single Go binary on a quarterly cadence,
with a behavioral-discipline layer that keeps a human in the loop before any order fires.

Everything is local and auditable — no black-box scores; every weight, filter, and cost is
explainable from the output. The engine is market-parameterized and runs across investment
universes: US equities (Schwab + SEC EDGAR) and Indian equities (Zerodha + NSE/Yahoo), with
strategies tuned per universe.

## Build & run

Requires **Go 1.27+**.

```bash
git clone https://github.com/raghavkgarg/mycase && cd mycase
make build       # builds ./dist/mycase
make install     # symlinks /usr/local/bin/mycase -> ./dist/mycase (sudo only if needed)

mycase auth --broker schwab                                       # authenticate
mycase pick --index sp500 --method us_quality_momentum --top 20   # read-only: rank stocks
mycase autopilot run                                              # full rebalance (proposes; --live to execute)
```

`pick` never trades; order execution always requires an explicit `--live` flag and a
confirmation prompt. Run `mycase <command> --help` for any command's flags.

## Documentation

The details live in the book — **[The Mycase Guide](docs/book/preface.md)** — written to be
read front-to-back or consulted by chapter. Good entry points:

- [Preface & contents](docs/book/preface.md) — what the system is and the full chapter list
- [Runbook](docs/book/1-80-runbook.md) — every command with realistic workflows and setup
- [Architecture](docs/book/2-10-architecture.md) — layers, data flow, design decisions
- [Roadmap](docs/book/9-10-roadmap.md) — status and upcoming work

---

## Quick Start

```bash
# 1. Pick top 15 small-cap stocks by multi-factor score
mycase pick --index smallcap250 --method multibagger --top 15

# 2. Optimize weights, cap at 15% per stock
mycase optimize --file data/candidates/... --method mfs --cap 0.15

# 3. Backtest the portfolio over 3 years
mycase backtest --file data/myportfolio.csv --capital 500000 \
    --from 2022-01-01 --rebalance quarterly --benchmark ^NSEI

# 4. Execute basket orders (dry-run by default)
mycase basket data/myportfolio
```

---

## Commands

| Command | Description |
|---------|-------------|
| `pipeline` | Run the full workflow (pick → optimize → report → monitor) from `pipeline.yaml` |
| `pick` | Score and rank stocks from built-in indices, CSVs, or Excel (.xlsx) files; auto-converts Excel input |
| `optimize` | Compute target weights (inverse-volatility, MFS multi-factor, or equal-weight) |
| `report` | Generate a plain-text selection rationale for each picked stock |
| `performance` | Compute P&L from a purchase date to latest close (daily or intraday) |
| `backtest` | Historical simulation: CAGR, Max Drawdown, Sharpe, Sortino, Alpha/Beta |
| `monitor` | Interactive 4-pillar portfolio health simulation |
| `basket` | Preview or execute Zerodha basket orders; applies micro-tx filter and tax warnings |
| `holdings` | Snapshot of current live or mock holdings |
| `merge combine` | Merge multiple portfolio CSVs into one |
| `merge golden` | Update a golden copy CSV from a proposals CSV |
| `daemon start` | Start the blocking drift monitoring loop (use `install` for launchd/systemd) |
| `daemon check` | One-shot drift check against live holdings |
| `daemon status` | Show last drift check result from `data/state/daemon_state.json` |
| `daemon install` | Write launchd plist (macOS) or print systemd unit (Linux) |
| `cache status` | Show DuckDB cache row counts and last fetch timestamps |
| `cache clear` | Evict one ticker or wipe the entire price cache |
| `convert` | Convert Excel (.xlsx) portfolio/ETF holdings file to clean CSV |
| `auth` | Authenticate with Zerodha Kite Connect |

---

## Configuration

### `config/pipeline.yaml` — main config

```yaml
indices: [smallcap250, nifty500]
method: multibagger
top_n: 20
capital: 500000
rebalance_tolerance: 0.10
hysteresis_buffer: 5

alerts:
  drift_threshold: 0.05
  channels: [telegram]
  telegram_bot_token: ""    # or set MYCASE_TELEGRAM_TOKEN
  telegram_chat_id: ""
  discord_webhook_url: ""   # or set MYCASE_DISCORD_WEBHOOK
```

### `config/mfs.json` — scoring weights

Per-strategy factor weights. Strategies: `balanced`, `aggressive`, `conservative`, `multibagger`. Weights must sum to 1.0 within each strategy.

### Zerodha credentials

Create `config/credentials.json`:
```json
{ "api_key": "...", "access_token": "..." }
```

Or run `mycase auth` to generate the access token from your API key and request token.

---

## Data Files

- `data/*.csv` — golden copy portfolios (never modified programmatically except via `merge golden` / rebalance confirmation)
- `data/mycase.db` — DuckDB analytical master database (prices, fundamentals, PIT runs, themes)
- `data/state/` — runtime process state (`daemon_state.json`, `scheduler_state.json`, `daemon.pid`)
- `data/cache/` — unified caches (`prices/`, `delivery/`, `constituents/`, `customer_concentration.json`)
- `data/candidates/` — strategy outputs (`proposals/`, `index_picks/`, `snapshots/`)
- `data/universe/` — canonical index constituent rosters (`NIFTY50.csv`, `microcap250.csv`, etc.)
- `data/raw/` — API wire capture response archive (`mycase raw`)
- `data/logs/` — operational diagnostics (`mycase-*.jsonl`, `scheduler.log`)
- `report/annual_reports/` — corporate PDF annual reports for qualitative scuttlebutt research

---

## Docs

- [Preface & contents](docs/book/preface.md) — what the system is and the full chapter list
- [Runbook](docs/book/1-80-runbook.md) — every command with realistic workflows and setup
- [Architecture](docs/book/2-10-architecture.md) — layers, data flow, design decisions
- [Roadmap](docs/book/9-10-roadmap.md) — status and upcoming work
