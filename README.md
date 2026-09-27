# mycase

An automated US equity factor-tilt system. It picks stocks by a transparent quality +
momentum score, sizes positions under sector and concentration caps, proposes orders for
the investor to confirm, executes them through the Schwab Trader API, and then audits
performance and watches the portfolio for drift between rebalances. It runs as a single Go
binary on a quarterly cadence, with a behavioral-discipline layer that keeps a human in the
loop before any order fires.

Everything is local and auditable — no black-box scores. Every weight, filter, and cost is
explainable from the output. US market data and brokerage come from Schwab; authoritative
fundamentals come from SEC EDGAR; Yahoo Finance is the fallback. The same
market-parameterized engine also drives a legacy **India-Path** (Zerodha + NSE/Yahoo) — see
[Market paths](#market-paths).

---

## How it works

A rebalance flows through a fixed pipeline, each stage feeding the next:

1. **Pick** — score every constituent of the target index on the active strategy (US:
   quality + momentum) and rank them. Hard filters eliminate unsuitable names; hysteresis
   avoids churning holdings near the selection boundary.
2. **Optimize** — turn the ranked survivors into target weights (inverse-volatility or
   multi-factor), applying per-stock and per-sector caps and a micro-transaction filter.
3. **Propose** — generate an order basket as a proposal. Nothing executes without explicit
   investor confirmation (`--live` + a prompt).
4. **Execute** — place orders through the broker with rate-limiting and automatic retry of
   failures.
5. **Audit & monitor** — track live performance versus the benchmark, run FIFO tax-lot
   accounting and tax-loss harvesting (US), and run a background drift daemon that alerts
   when holdings wander from targets.

Prices and fundamentals are cached in a local DuckDB database (`data/mycase.db`), so warm
re-runs are offline and API budgets are respected. Every external API response is archived
to `data/raw/` for offline debugging.

---

## Install

Requires **Go 1.27+**.

```bash
git clone https://github.com/raghavkgarg/mycase
cd mycase
make build       # builds ./dist/mycase (version-stamped)
make install     # symlinks /usr/local/bin/mycase -> ./dist/mycase (sudo only if needed)
mycase --version
```

`make install` deliberately **symlinks** the binary rather than copying it: `mycase` finds
its `config/` and `data/` directories by following the symlink back to this project tree, so
the installed command works from any directory. For a sudo-free user install use
`make install PREFIX=~/.local`; to point a standalone `go install` binary at the tree, set
`MYCASE_HOME`. See the [Runbook](docs/book/1-80-runbook.md) for the full resolution rules.

---

## Quick start (US path)

```bash
# One-time: authenticate with the Schwab Trader API
mycase auth --broker schwab

# 1. Pick the top 20 S&P 500 names by quality + momentum
mycase pick --index sp500 --method us_quality_momentum --top 20

# 2. Optimize weights, capping any single position at 15%
mycase optimize --file data/candidates/... --method mfs --cap 0.15

# 3. Preview the order basket (execution requires --live + confirmation)
mycase basket --live

# 4. Run the whole rebalance non-interactively
mycase autopilot run
```

`mycase pick` is read-only — it never places a trade. Order execution always requires the
explicit `--live` flag and a confirmation prompt.

---

## Commands

Run `mycase <command> --help` for full flags. The main commands:

| Command | What it does |
|---------|--------------|
| `pick` | Score and rank stocks from an index, CSV, or Excel file |
| `optimize` | Compute target weights (inverse-volatility, multi-factor, or equal-weight) with sector/position caps |
| `basket` | Preview or (with `--live`) execute broker order baskets |
| `pipeline` | Run the full pick → report → execute workflow from a pipeline YAML |
| `autopilot` | Non-interactive scheduled rebalance (quarterly/monthly), investor-in-the-loop preserved |
| `scheduler` | Autonomous OS-timer orchestrator for the EOD / drift / rebalance cadences |
| `report` | Generate a plain-text selection rationale per picked stock |
| `backtest` | Historical simulation: CAGR, max drawdown, Sharpe, Sortino, alpha/beta |
| `performance` | P&L from a purchase date to the latest close |
| `monitor` | 4-pillar portfolio health scoring |
| `daemon` | Background drift-monitoring loop with alerting (`start`/`check`/`status`/`install`) |
| `holdings` | Snapshot of current broker (or mock) holdings |
| `tax` | FIFO lot tracking and tax-loss harvesting (US) |
| `returns` | Audited returns (HPR, XIRR/MWR, TWR), dividends, and rebalancing gains |
| `db` | Manage the consolidated DuckDB database and daily EOD update runs |
| `cache` | Inspect/manage the DuckDB price & fundamentals cache |
| `raw` | Inspect the archived raw API responses (`data/raw/`) |
| `pit` | Point-in-time research database + empirical calibration analytics |
| `calibrate` | Rolling Spearman rank-IC and parameter calibration for strategy pillars |
| `serve` | Start the web dashboard server |
| `theme` | Theme lifecycle & exact-return engine (India-Path) |
| `merge` | Combine candidate CSVs or update a golden-copy portfolio |
| `convert` | Convert an Excel (.xlsx) holdings file to clean CSV |
| `auth` | Authenticate with a broker (Schwab or Zerodha) |

---

## Configuration

Config lives in `config/` and is read-only at runtime. Key files:

- **`config/defaults.json`** — the active market path and its defaults (broker, market
  clock, EOD index/method, pipeline YAML). Switch paths with `make use-us` / `make
  use-india`, which copy `defaults.us.json` / `defaults.india.json` over it.
- **`config/pipeline_us.yaml`** (US) / **`config/pipeline.yaml`** (India) — the pipeline
  stages, indices, top-N, capital, tolerances, and alert channels.
- **`config/mfs.json`** — per-strategy factor weights (must sum to 1.0 per strategy).
- **`config/schwab.json`** — Schwab app credentials; **`config/schwab_token.json`** — OAuth
  tokens. Both are git-ignored and never committed. Run `mycase auth --broker schwab` to
  generate the token.

---

## Data

- `data/mycase.db` — the consolidated DuckDB database: price & fundamentals cache, pipeline
  run/proposal/selection state, tax lots, attribution, themes, and the holiday calendar.
- `data/candidates/` — pick output and order proposals.
- `data/raw/` — archived raw API responses for offline debugging (`mycase raw`).
- `data/*.csv` — golden-copy portfolios, mutated only via `mycase merge golden`.

---

## Market paths

The system is market-parameterized: one engine, two paths, selected by
`config/defaults.json`.

- **US-Path** (active) — Schwab + SEC EDGAR, NYSE calendar, `sp500` / `us_quality_momentum`.
- **India-Path** (legacy) — Zerodha + NSE/Yahoo, NSE calendar, `niftytotalmarket` /
  `multibagger` / `earlymb`. The strategy and subsystem chapters for this path are grouped
  under Module F in the docs.

---

## Docs

The full guide lives in [`docs/`](docs/README.md), written as a book grouped into modules.
Start here:

- [Guide index](docs/README.md) — all chapters, grouped into modules
- [Architecture](docs/book/2-10-architecture.md) — layers, data flow, design decisions
- [Runbook](docs/book/1-80-runbook.md) — every command with realistic workflows
- [Roadmap](docs/book/9-10-roadmap.md) — status and upcoming work

Contributing to the docs? Read [Chapter 0 — The Style Guide](docs/book/0-10-style-guide.md) first.

---

## Testing

```bash
make test            # unit tests (fast, offline)
make test-race       # with the race detector
make test-coverage   # + coverage.html
make cleanup         # gofmt + go fix + go vet + staticcheck + govulncheck + check-deps
```
