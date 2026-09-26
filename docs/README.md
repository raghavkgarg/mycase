# The Mycase Guide

This is the book-form table of contents for `docs/`. Each document is a **chapter**;
chapters are grouped into **modules** and ordered so the guide reads front-to-back —
from *why the system exists* through *how it's built*, *what it does*, and *how to run
it*, ending with the India-Path material. **Status and plans are not part of the reading
spine** — they live in the Roadmap (Ch. 3), which is the one appendix-like chapter you
consult rather than read straight through.

> **This structure is a working draft (Pass 1).** The module grouping and chapter
> titles below are the source of truth for how the book reads; the *filenames*
> (`NN-name.md`) are stable IDs and deliberately lag the titles for now. A later pass
> will rename files to match their chapter titles and migrate the `docs/NN-*.md`
> references embedded in Go source and steering. Until then, trust the **title +
> module** here over the filename.

---

## How this book is written (governing rules)

These rules are what keep the guide readable and stop it decaying back into a pile of
change-logs. Apply them to every chapter, new or edited:

1. **A chapter describes the system as it is, in the present tense.** "The Router caches
   merged fundamentals" — not "Phase 10e made the Router cache fundamentals." A reader
   should learn *how the system works today* without reconstructing its history.
2. **Status and history are not chapter content.**
   - *What's done / in progress / next* → the **Roadmap** (Ch. 3).
   - *What changed and when* → **git history** (commits, PRs).
   - No `Status:` headers, no "shipped in Phase N," no strikethrough `~~was broken~~ ✅
     FIXED` archaeology inside a chapter. If you're tempted to write it, it belongs in
     the roadmap or is already in git.
3. **One chapter per subsystem or concept — never per phase, refactor, or change.** A
   document titled by a *process* (`refactor`, `migration`, `phase-N`) is a smell:
   retitle it by the *subsystem* it documents, or fold it into the roadmap if it's
   purely status.
4. **The module a chapter lives in supplies its context.** A terse title (`Value`,
   `Rendering`, `Screener`) is fine because its module disambiguates it. Don't pad names
   to be self-describing in isolation.
5. **New durable docs join the guide as a numbered chapter in the right module and get
   listed here.** Don't spawn satellite plan / per-feature process docs (impl plans, bug
   trackers, one-off audits) — record plans in the roadmap; document a feature in its
   chapter; let git hold the process.

---

## Module A — Foundations

*Why the system exists and the rules it's built on.*

| Ch. | Chapter | What it covers |
|----:|---------|----------------|
| 1 | [Vision](01-vision.md) | Who it's for, the problem, what it sets out to build |
| 2 | [Principles](02-principles.md) | Durable architectural principles — the review rubric for changes and merges |
| 27 | [Philosophy](27-Philosophy.md) | Momentum/Quality vs. Deep Value — reconciling market downturns, quality premiums, value traps, and the Golden Intersection |
| 3 | [Roadmap](03-roadmap.md) | **Canonical status + plan.** Summary of what's shipped (with chapter pointers) + detail for upcoming phases. The one status document; not part of the front-to-back read |

## Module B — Architecture & Platform

*How the system is structured and the cross-cutting machinery every subsystem uses.*

| Ch. | Chapter | What it covers |
|----:|---------|----------------|
| 4 | [Architecture Reference](04-architecture.md) | Layers, `cmd/pkg` breakdown, data flow, design decisions (D-decisions), package layering rule |
| 6 | [Rendering](06-render.md) | CLI output (`pkg/render`) — tables, formatters, TTY-aware color |
| 24 | [Logging & Observability](24-logging.md) | Two-channel logging (`pkg/logging`, slog) + the raw-response archive (`pkg/rawcapture`/`rawstore`, `mycase raw` triage) |

> Diagrams: `architecture-overview.d2` (source) / `architecture-overview.svg` (rendered)
> — layer/package overview accompanying Ch. 4.

## Module C — Data & Persistence

*Where the numbers come from and how they're stored.*

| Ch. | Chapter | What it covers |
|----:|---------|----------------|
| 7 | [Data Sources](07-datasources.md) | Schwab / EDGAR / Yahoo, provenance, gap analysis |
| 8 | [EDGAR](08-edgar-design.md) | SEC EDGAR client + XBRL concept mapper |
| 9 | [EDGAR Facts Reference](09-edgar-facts-reference.md) | The EDGAR XBRL fact universe — what we extract + future-factor candidates |
| 10 | [Storage & Pipeline Persistence](10-duckdb-migration.md) | DuckDB-backed pipeline state — schema, run/proposal/selection tables, data flow |
| 11 | [Data Directory Inventory](11-data-inventory.md) | Every file/dir under `data/`, provenance, keep/delete status |

## Module D — Strategies

*How stocks are scored and selected.*

| Ch. | Chapter | Strategy |
|----:|---------|----------|
| 12 | [Multibagger](12-multibagger.md) | India micro/small/mid-cap — 11 hard filters + 100-pt scoring |
| 13 | [Early Multibagger](13-early-multibagger.md) | `earlymb` regime-gated pre-breakout engine (VCP/RVOL/pocket-pivot/delivery) |
| 14 | [Value](14-value.md) | Large-cap Value — EPV-based, dual-path BFSI/industrial filters |
| 26 | [Predictive Fair Price](26-fairprice.md) | 5-Model Ensemble Intrinsic Valuation & Cross-Strategy Enrichment |
| 28 | [Golden Triangle](28-golden.md) | Multi-Strategy Quantitative Convergence Engine (EMB + MB + Fair Price) |
| 15 | [Exit & Addition Rationale](15-exit-addition-rationale.md) | Portfolio exit vetting & addition-driver tracking |
| 16 | [Scuttlebutt Research](16-scuttlebutt.md) | Qualitative research reporting pipeline |
| 17 | [Screener / nselib Integration](17-screener.md) | NSE `nselib` + Screener.in enrichment (India) |

> The active strategy, **US Quality-Momentum**, is specced inline in Ch. 4
> (Architecture) rather than a standalone chapter. The India strategies above belong to the
> **India-Path** — the other supported market path, distinct from the current US focus.

## Module E — Execution & Operations

*Turning selections into orders, and running the system.*

| Ch. | Chapter | What it covers |
|----:|---------|----------------|
| 18 | [Runbook](18-runbook.md) | Operator manual — every command with realistic examples and workflows |
| 19 | [Feature Specs](19-feature-specs.md) | Tax-optimized rebalancing (FIFO), options overlay |
| 20 | [Order Execution & Retry](20-executor-retry.md) | Rate limiting & failure recovery (`pkg/executor`, `cmd/retry`) |
| 21 | [Testing](21-testing.md) | The test pyramid, conventions, coverage, how to run each tier |

## Module F — India-Path Subsystems

*The India market path: the subsystems that run when the India-Path is used. Distinct from
the active US-Path focus.*

| Ch. | Chapter | Subsystem |
|----:|---------|-----------|
| 22 | [Themes](22-themes.md) | Theme lifecycle DB (`pkg/themedb`) + exact-return engine (`pkg/themereturn`, `mycase returns`) |
| 23 | [Static IP Setup](23-staticip.md) | Zerodha Kite static-IP (staticip.in) — the proxy `pkg/yfinance` bypasses for Yahoo |

---

*Retired: the R-numbered refactor history (formerly `05-refactor.md`) is not a chapter —
structural-change history lives in git, and the durable rules it once held now live in
Ch. 2 (Principles), Ch. 4 (Architecture), and `.kiro/steering/`.*
