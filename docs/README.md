# Mycase Docs Index

Map of the `docs/` tree. **`roadmap.md` is the single home for status and plans** —
what's done, in progress, and next. Don't spawn satellite plan docs; record plans and
progress there.

Docs are grouped below by role. Point-in-time notes (resolved bug trackers, one-off
audits, completed implementation plans, setup how-tos) live under [`archive/`](#archive)
and are kept for provenance, not maintained.

## Start here

| Doc | What it is |
|-----|-----------|
| [`vision.md`](vision.md) | Product vision — who it's for and what it sets out to build |
| [`roadmap.md`](roadmap.md) | **Canonical status + plan.** Phases, current state, known gaps, technical debt |
| [`runbook.md`](runbook.md) | Operator manual — every command with realistic examples and workflows |
| [`architecture.md`](architecture.md) | System design — layers, `cmd/pkg` breakdown, data flow, design decisions (D-decisions) |
| [`principles.md`](principles.md) | Durable architectural principles — the review rubric for changes and merges |

## Design & subsystems

Durable design references for specific subsystems (kept current as the code evolves).

| Doc | Subsystem |
|-----|-----------|
| [`datasources.md`](datasources.md) | Data-source design — Schwab / EDGAR / Yahoo, provenance, gap analysis |
| [`edgar-design.md`](edgar-design.md) | SEC EDGAR fundamentals client + XBRL concept mapper (Phase 10c, as built) |
| [`edgar-facts-reference.md`](edgar-facts-reference.md) | Catalogue of the EDGAR XBRL fact universe — what we extract + candidates for future factors |
| [`duckdb-migration.md`](duckdb-migration.md) | Intermediate pipeline data → DuckDB (Phase 7); what shipped vs deferred |
| [`DataFile.md`](DataFile.md) | `data/` directory inventory — every file/dir, provenance, keep/delete status |
| [`render.md`](render.md) | CLI rendering layer (`pkg/render`) — tables, formatters, TTY-aware color |
| [`feature.md`](feature.md) | Feature specs — tax-optimized rebalancing (FIFO), options overlay |
| [`refactor.md`](refactor.md) | Refactor history + phase ledger (R-numbered). Referenced by steering + code |

## Strategy specs

Per-method scoring/selection specifications.

| Doc | Strategy |
|-----|----------|
| [`multibagger.md`](multibagger.md) | Multibagger — India micro/small/mid-cap, 11 hard filters + 100-pt scoring |
| [`earlyMB.md`](earlyMB.md) | Early Multibagger (`earlymb`) — regime-gated pre-breakout engine (VCP/RVOL/pocket-pivot/delivery) |
| [`value.md`](value.md) | Large-cap Value — EPV-based, dual-path BFSI/industrial filters |
| [`screener.md`](screener.md) | NSE `nselib` + Screener.in integration (India enrichment) |
| [`scuttlebutt.md`](scuttlebutt.md) | Scuttlebutt qualitative research reporting pipeline |
| [`detailedExit.md`](detailedExit.md) | Portfolio exit vetting & addition-driver rationale tracking |

> **Note**: US Quality-Momentum (the active strategy) is specced inline in
> `architecture.md` + `roadmap.md` rather than a standalone doc; the India strategies
> above are legacy from the earlier multi-market design.

## Testing

| Doc | What it is |
|-----|-----------|
| [`testing.md`](testing.md) | How to run tests, coverage, race detector |

## Diagrams

- `architecture-overview.d2` / `architecture-overview.svg` — layer/package overview
  (text-to-diagram source + rendered SVG).

## Archive

Point-in-time notes — resolved bug trackers, one-off audits, completed implementation
plans, and setup how-tos. Kept for provenance; **not maintained**.

| Doc | What it was |
|-----|-------------|
| [`archive/Bugs_EMB.md`](archive/Bugs_EMB.md) | EarlyMB bug tracker (Bugs 001–007, all resolved) |
| [`archive/DataAudit.md`](archive/DataAudit.md) | One-time system-wide data-integrity audit |
| [`archive/value_impl.md`](archive/value_impl.md) | Completed implementation plan for the Value strategy |
| [`archive/executorError.md`](archive/executorError.md) | Order-execution rate-limit / retry RCA note |
| [`archive/embPrompt.md`](archive/embPrompt.md) | One-off prompt template for logging an earlymb run |
| [`archive/ThemeBasedReturn.md`](archive/ThemeBasedReturn.md) | India-legacy theme-return engine design/plan |
| [`archive/ThemeDatabase.md`](archive/ThemeDatabase.md) | India-legacy theme lifecycle DB + daily-sync design |
| [`archive/git-sync-guide.md`](archive/git-sync-guide.md) | Generic git sync / conflict-resolution how-to |
| [`archive/StaticIP.md`](archive/StaticIP.md) | Zerodha Kite static-IP (staticip.in) setup how-to (India legacy) |
