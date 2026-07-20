# mycase

Portfolio basket and rebalancing engine for Indian equity markets (NSE/BSE).

Covers the full workflow: stock selection → weight optimization → order generation → backtesting → drift monitoring. Data from Yahoo Finance; execution via Zerodha Kite Connect. Runs without credentials in dry-run mode using `MockBroker`.

---

## Install

```bash
go install github.com/raghavkgarg/mycase@latest
```

Or build from source:

```bash
git clone https://github.com/raghavkgarg/mycase
cd mycase
make build       # outputs ./bin/mycase
make install     # installs to $GOPATH/bin
```

Requires Go 1.26+.

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
| `pick` | Score and rank stocks from an index or CSV; apply hysteresis if a golden copy exists |
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
| `daemon status` | Show last drift check result from `data/daemon_state.json` |
| `daemon install` | Write launchd plist (macOS) or print systemd unit (Linux) |
| `cache status` | Show DuckDB cache row counts and last fetch timestamps |
| `cache clear` | Evict one ticker or wipe the entire price cache |
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

- `data/*.csv` — golden copy portfolios (never modified programmatically except via `merge golden`)
- `data/candidates/` — pick output CSVs
- `data/.cache/` — Yahoo Finance JSON cache (auto-created, date-stamped)
- `data/cache.db` — DuckDB persistent price and fundamentals cache
- `data/daemon_state.json` — drift daemon last-check state

---

## Docs

- [`docs/architecture.md`](docs/architecture.md) — algorithms, subtle implementation details, design decisions
- [`docs/runbook.md`](docs/runbook.md) — usage guide, common workflows, all flags
- [`docs/refactor.md`](docs/refactor.md) — implementation history and phase notes
