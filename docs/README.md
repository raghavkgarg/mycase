# The Mycase Guide

This is the book-form table of contents for `docs/`. Each document is a **chapter**;
chapters are grouped into **parts** and ordered so the guide reads front-to-back — from
*why the system exists* through *how it's built*, *what it does*, and *how to run it*,
ending with reference and legacy material.

**Conventions**
- **`roadmap.md` (Ch. 3) is the single home for status and plans** — what's done, in
  progress, and next. Don't spawn satellite plan or per-feature process docs (impl
  plans, bug trackers, one-off audits): record plans/progress in the roadmap, and
  document a *feature or subsystem* in its own chapter. Point-in-time process artifacts
  are not kept — git history preserves them.
- **One chapter per feature/subsystem, not per implementation step.** New durable docs
  join the guide as a numbered chapter in the right part and get listed here.

---

## Part I — Foundations

*Why the system exists and the rules it's built on.*

| Ch. | Chapter | What it covers |
|----:|---------|----------------|
| 1 | [Vision](vision.md) | Who it's for, the problem, what it sets out to build |
| 2 | [Principles](principles.md) | Durable architectural principles — the review rubric for changes and merges |
| 3 | [Roadmap](roadmap.md) | **Canonical status + plan.** Phases, current state, known gaps, technical debt |

## Part II — Architecture

*How the system is structured.*

| Ch. | Chapter | What it covers |
|----:|---------|----------------|
| 4 | [Architecture Reference](architecture.md) | Layers, `cmd/pkg` breakdown, data flow, design decisions (D-decisions) |
| 5 | [Refactor History](refactor.md) | R-numbered phase ledger of structural changes. Referenced by steering + code |
| 6 | [Rendering Layer](render.md) | CLI output (`pkg/render`) — tables, formatters, TTY-aware color |

> Diagrams: `architecture-overview.d2` (source) / `architecture-overview.svg` (rendered)
> — layer/package overview accompanying Ch. 4.

## Part III — Data & Persistence

*Where the numbers come from and how they're stored.*

| Ch. | Chapter | What it covers |
|----:|---------|----------------|
| 7 | [Data Sources](datasources.md) | Schwab / EDGAR / Yahoo, provenance, gap analysis |
| 8 | [EDGAR Design](edgar-design.md) | SEC EDGAR client + XBRL concept mapper (Phase 10c, as built) |
| 9 | [EDGAR Facts Reference](edgar-facts-reference.md) | The EDGAR XBRL fact universe — what we extract + future-factor candidates |
| 10 | [DuckDB Migration](duckdb-migration.md) | Intermediate pipeline data → DuckDB (Phase 7); shipped vs deferred |
| 11 | [Data Directory Inventory](DataFile.md) | Every file/dir under `data/`, provenance, keep/delete status |

## Part IV — Strategies

*How stocks are scored and selected.*

| Ch. | Chapter | Strategy |
|----:|---------|----------|
| 12 | [Multibagger](multibagger.md) | India micro/small/mid-cap — 11 hard filters + 100-pt scoring |
| 13 | [Early Multibagger](earlyMB.md) | `earlymb` regime-gated pre-breakout engine (VCP/RVOL/pocket-pivot/delivery); incl. the data-integrity audit + bug-resolution ledger |
| 14 | [Value](value.md) | Large-cap Value — EPV-based, dual-path BFSI/industrial filters |
| 15 | [Exit & Addition Rationale](detailedExit.md) | Portfolio exit vetting & addition-driver tracking |
| 16 | [Scuttlebutt Research](scuttlebutt.md) | Qualitative research reporting pipeline |
| 17 | [Screener / nselib Integration](screener.md) | NSE `nselib` + Screener.in enrichment (India) |

> The active strategy, **US Quality-Momentum**, is specced inline in Ch. 4
> (Architecture) + Ch. 3 (Roadmap) rather than a standalone chapter. The India
> strategies above are legacy from the earlier multi-market design, but their code
> still runs.

## Part V — Execution & Operations

*Turning selections into orders, and running the system.*

| Ch. | Chapter | What it covers |
|----:|---------|----------------|
| 18 | [Runbook](runbook.md) | Operator manual — every command with realistic examples and workflows |
| 19 | [Feature Specs](feature.md) | Tax-optimized rebalancing (FIFO), options overlay |
| 20 | [Order Execution & Retry](executor-retry.md) | Rate limiting & failure recovery (`pkg/executor`, `cmd/retry`) |
| 21 | [Testing](testing.md) | Running tests, coverage, race detector |

## Part VI — India-Legacy Subsystems

*Documented because the code still runs, though not part of the active US strategy.*

| Ch. | Chapter | Subsystem |
|----:|---------|-----------|
| 22 | [Themes](themes.md) | Theme lifecycle DB (`pkg/themedb`) + exact-return engine (`pkg/themereturn`, `mycase returns`) |
| 23 | [Static IP Setup](staticip.md) | Zerodha Kite static-IP (staticip.in) — the proxy `pkg/yfinance` bypasses for Yahoo |
