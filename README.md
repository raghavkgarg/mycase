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
