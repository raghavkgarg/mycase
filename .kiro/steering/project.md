# Mycase — Project Conventions

## Overview
Automated US equity factor-tilt system (Go). Single binary, quarterly rebalance, behavioral discipline engine. Picks stocks via quality+momentum scoring, generates order proposals, executes via Schwab API.

## Architecture
- **Go 1.27+** — single binary, CLI-first
- **urfave/cli/v3** — command framework
- **DuckDB** — local price/fundamentals cache
- **Schwab Trader API** — US market data + brokerage
- **Yahoo Finance** — fallback data source (India legacy, US fallback)

## Build, Test & Lint (use Makefile)
```bash
make build           # Build dist/mycase binary
make test            # Run all tests (30s timeout)
make test-race       # Tests with race detector
make test-coverage   # Tests + coverage.html report
make cleanup         # gofmt + go fix + go vet + staticcheck + govulncheck
make run ARGS="..."  # Run with go run (dev mode)
make help            # Show all targets
```

**Always use `make` targets** instead of raw `go build`, `go test`, `go vet`:
- `make build` injects version/commit/date via LDFLAGS
- `make test` enforces consistent timeout
- `make cleanup` runs the full lint suite

## Key Commands
```bash
make run ARGS="pick --index sp500 --method us_quality_momentum --top 20"   # US factor pick
make run ARGS="pick --index smallcap250 --method multibagger --top 15"     # India pick (legacy)
make run ARGS="auth --broker schwab"                     # OAuth flow
make run ARGS="basket --live"                            # Generate orders
make run ARGS="serve"                                    # Web dashboard
make run ARGS="autopilot run"                            # Full quarterly pipeline
```

## Project Structure
```
cmd/              CLI commands (thin wrappers calling pkg/)
pkg/
├── broker/       Broker interface, MarketConfig, cost helpers
│   ├── schwab/   Schwab API client (auth, market data, broker)
│   └── zerodha/  Zerodha/Kite broker (India legacy)
├── brokerfactory/ Creates broker from config/defaults.json
├── stockpicker/  Scoring, hard filters, hysteresis, selection
├── optimizer/    Inverse-volatility, MFS weights, sector caps
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
