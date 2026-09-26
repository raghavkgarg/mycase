# The Mycase Guide

This is the book-form table of contents for `docs/`. Each document is a **chapter**;
chapters are grouped into **modules** and ordered so the guide reads front-to-back —
from *why the system exists* through *how it's built*, *what it does*, and *how to run
it*, ending with the India-Path material. **Status and plans are not part of the reading
spine** — they live in the Roadmap ([Appendix A](03-roadmap.md)), the one document you
consult rather than read straight through.

Filenames are stable numeric IDs (`NN-slug.md`) and the slug matches the chapter title.
Renaming a chapter is a deliberate, wholesale change (rename + migrate every `docs/NN-*.md`
reference + build), never piecemeal.

> **How the book is written lives in [Chapter 0 — The Style Guide](00-style-guide.md).**
> Title convention, the present-tense rule, the ban on status/history furniture and
> absolute-path links, structure and voice — Chapter 0 is the single source of truth and the
> rubric every chapter is reviewed against. Read it before adding or editing a chapter.

---

## Module A — Foundations

*Why the system exists and the rules it's built on.*

| Ch. | Chapter | What it covers |
|----:|---------|----------------|
| 0 | [The Style Guide](00-style-guide.md) | How every chapter is written — the book's voice, structure, and aesthetic; the review rubric |
| 1 | [Vision](01-vision.md) | Who it's for, the problem, what it sets out to build |
| 2 | [Principles](02-principles.md) | Durable architectural principles — the review rubric for changes and merges |

> Chapter 0 governs the book itself; Chapters 1–2 are where the front-to-back read begins.
> The **Roadmap** — the one status-and-plan document — sits outside the numbered spine as
> [Appendix A](03-roadmap.md).

## Module B — Architecture & Platform

*How the system is structured and the cross-cutting machinery every subsystem uses.*

| Ch. | Chapter | What it covers |
|----:|---------|----------------|
| 4 | [Architecture Reference](04-architecture.md) | Layers, `cmd/pkg` breakdown, data flow, design decisions (D-decisions), package layering rule |
| 6 | [Rendering](06-rendering.md) | CLI output (`pkg/render`) — tables, formatters, TTY-aware color |
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
| 10 | [Storage & Pipeline Persistence](10-storage.md) | DuckDB-backed pipeline state — schema, run/proposal/selection tables, data flow |
| 11 | [Data Directory Inventory](11-data-inventory.md) | Every file/dir under `data/`, provenance, keep/delete status |

## Module D — Strategies

*How stocks are scored and selected.*

| Ch. | Chapter | Strategy |
|----:|---------|----------|
| 12 | [Multibagger](12-multibagger.md) | India micro/small/mid-cap — 11 hard filters + 100-pt scoring |
| 13 | [Early Multibagger](13-early-multibagger.md) | `earlymb` regime-gated pre-breakout engine (VCP/RVOL/pocket-pivot/delivery) |
| 14 | [Value](14-value-strategy.md) | Large-cap Value — EPV-based, dual-path BFSI/industrial filters |
| 15 | [Exit & Addition Rationale](15-exit-addition-rationale.md) | Portfolio exit vetting & addition-driver tracking |
| 16 | [Scuttlebutt Research](16-scuttlebutt-research.md) | Qualitative research reporting pipeline |
| 17 | [Screener / nselib Integration](17-screener-nselib.md) | NSE `nselib` + Screener.in enrichment (India) |

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
| 23 | [Static IP Setup](23-static-ip-setup.md) | Zerodha Kite static-IP (staticip.in) — the proxy `pkg/yfinance` bypasses for Yahoo |

---

## Appendices

*Consulted, not read front-to-back.*

| App. | Document | What it covers |
|----:|----------|----------------|
| A | [Roadmap](03-roadmap.md) | **Canonical status + plan.** What's shipped (with chapter pointers) + detail for upcoming phases. The one status document; not part of the reading spine. (Filename stays `03-roadmap.md` as a stable ID.) |

---

*Retired: the R-numbered refactor history (formerly `05-refactor.md`) is not a chapter —
structural-change history lives in git, and the durable rules it once held now live in the
[Principles](02-principles.md) and [Architecture](04-architecture.md) chapters and
`.kiro/steering/`. The resolved-bug tracker (formerly `25-Bugs.md`) is likewise gone — a
ledger of fixed bugs is git history, not a chapter.*
