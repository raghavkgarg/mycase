# Mycase — Project Conventions

## Overview
Automated US equity factor-tilt system (Go). Single binary, quarterly rebalance, behavioral discipline engine. Picks stocks via quality+momentum scoring, generates order proposals, executes via Schwab API.

## Architecture
- **Go 1.22+** — single binary, CLI-first
- **urfave/cli/v3** — command framework
- **DuckDB** — local price/fundamentals cache
- **Schwab Trader API** — US market data + brokerage
- **Yahoo Finance** — fallback data source (India legacy, US fallback)

## Key Commands
```bash
go build ./...                                    # Build
go run . pick --index sp500 --method us_quality_momentum --top 20   # US factor pick
go run . pick --index smallcap250 --method multibagger --top 15     # India pick (legacy)
go run . auth --broker schwab                     # OAuth flow
go run . basket --broker schwab                   # Generate orders
go run . serve                                    # Web dashboard
go run . autopilot run                            # Full quarterly pipeline
```

## Project Structure
```
cmd/              CLI commands (thin wrappers calling pkg/)
pkg/
├── stockpicker/  Scoring, hard filters, hysteresis, selection
├── optimizer/    Inverse-volatility, MFS weights, sector caps
├── schwab/       Schwab API client (auth, market data, broker)
├── datafetcher/  Ticker routing (US→Schwab, India→Yahoo)
├── cache/        DuckDB price + fundamentals cache
├── yfinance/     Yahoo Finance client + shared types
├── costs/        Transaction cost model (India + US)
├── backtest/     Backtesting engine
├── monitoring/   4-pillar health scoring
├── daemon/       Drift detection + alert dispatch
├── executor/     Live order placement
├── server/       Web dashboard (HTTP, SSE, embedded)
config/           JSON/YAML configs (read-only at runtime)
data/             CSVs, proposals, backups
docs/             Architecture, roadmap, runbook
```

## Code Style
- Go standard conventions: `gofmt`, short names in tight scope
- Error wrapping: `fmt.Errorf("context: %w", err)`
- Table-driven tests preferred
- No DI frameworks — constructor injection via plain fields
- Minimal external deps — prefer stdlib

## Documentation
- `docs/roadmap.md` — phased plan, what's done, what's next
- `docs/architecture.md` — system design, algorithms, data flow
- `docs/runbook.md` — operator manual, CLI usage examples
