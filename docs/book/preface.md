# The Mycase Guide

Mycase is an automated engine for self-directed investing: it picks holdings by a
transparent, quantitative strategy, sizes and rebalances them under discipline, executes
through a broker, and audits the result — all as a single local binary with a human in the
loop before any order fires.

This is its book. Read front-to-back, it takes you from *why the system exists* through
*what it invests in*, *how it's built*, and *how to run it*. Read as a reference, each
chapter stands alone. Either way, a chapter describes the system **as it is, in the present
tense** — how it came to be is in git, what's planned is in the Roadmap.

## How the book is organized

The chapters are grouped into three parts, by the reader they serve:

- **Part I — Product** is for the investor and operator: the strategies the engine can run,
  the portfolio decisions it makes, and how to drive it.
- **Part II — Architecture** is for the engineer: how the system is structured, where its
  data comes from, and how it stores, executes, and renders.
- **Part III — Operations** is for whoever runs it in anger: logging, testing, and
  environment setup.

Two ideas shape the organization. First, **strategies are an open-ended family of
investment approaches** — each tuned to a universe or asset class (US quality-momentum,
India micro-cap multibagger, large-cap value, and more to come). They are peers, not a
primary path with a legacy annex; a new strategy joins the family as an equal. Second,
**where markets genuinely differ, the difference lives in the architecture** — SEC EDGAR
serves US filings, NSE/Screener serves Indian equities — described by the universe it serves
rather than ranked against the others.

Ahead of the parts sit the **front matter** (the book's own rules and reasons); after them,
the **Roadmap** as the one appendix you consult rather than read.

> Filenames (`NN-slug.md`) are stable IDs — the number fixes identity, not reading order;
> this contents defines the order. **[Chapter 0 — The Style Guide](0-10-style-guide.md)** is
> how the book is written (voice, present-tense rule, the ban on status furniture and
> absolute-path links) and the rubric every chapter is reviewed against — read it before
> adding or editing a chapter.

---

## Front matter

The book about itself, and the reasons behind everything after it.

- **[0 · The Style Guide](0-10-style-guide.md)** — how every chapter is written: the book's
  voice, structure, and aesthetic, and the checklist a chapter is reviewed against.
- **[1 · Vision](0-20-vision.md)** — who the product is for, the problem it solves, and what
  it sets out to build.
- **[2 · Principles](0-30-principles.md)** — the durable architectural principles the system
  is built on, and the rubric changes are judged against.

## Part I — Product

*What the engine invests in and the decisions it makes — for the investor and operator.*

The **strategies** are the heart of the product: a family of quantitative approaches, each
tuned to a universe or asset class, sharing one scoring-and-selection engine.

- **[12 · Multibagger](1-10-strategy-multibagger.md)** — India micro/small/mid-cap growth: hard quality
  filters plus a 100-point multi-factor score.
- **[13 · Early Multibagger](1-20-strategy-early-multibagger.md)** — the `earlymb` regime-gated
  pre-breakout engine (VCP tightness, relative strength, delivery accumulation), catching
  compounders 1–3 weeks before markup.
- **[14 · Value](1-30-strategy-value.md)** — large-cap value: EPV-based intrinsic valuation
  with dual-path BFSI/industrial filters, avoiding value traps.
- **[26 · Fair Price](1-35-strategy-fair-price.md)** — a five-model intrinsic-valuation
  ensemble: fair price in currency terms, upside, and a margin-of-safety verdict, run both
  standalone and as an overlay on the growth strategies.
- **[19 · Feature Specs](1-40-strategy-emb-feature-specs.md)** — the deep engineering specification behind
  the Early Multibagger family: detection models, the three-tier lifecycle, and PIT research.
- **[28 · Golden Triangle](1-45-strategy-golden.md)** — the cross-strategy convergence engine
  that fuses quality, timing, and valuation into one regime classification and composite rank.

And the reasoning that ties the family together:

- **[27 · Philosophy](1-48-philosophy.md)** — why momentum and deep value select opposite
  stocks, and how the Golden Triangle reconciles them into a single view.

> The active US strategy, **US Quality-Momentum**, is currently specced inline in
> [Architecture](2-10-architecture.md); extracting it into its own chapter here is a tracked
> follow-up, so the family reads complete.

Around the strategies sit the **portfolio decisions and their audit trail**:

- **[15 · Exit & Addition Rationale](1-50-exit-addition-rationale.md)** — why each holding
  leaves, enters, or changes weight at a rebalance, recorded for the investor to audit.
- **[16 · Scuttlebutt Research](1-60-scuttlebutt-research.md)** — the qualitative research
  pipeline: governance, stability, and operational indicators compiled per stock.
- **[22 · Themes](1-70-themes.md)** — the theme lifecycle database and exact-return engine
  (`mycase returns`) that track investment themes over time.

And, to drive it all:

- **[18 · Runbook](1-80-runbook.md)** — the operator's manual: every command with realistic
  examples and the common workflows end to end.

## Part II — Architecture

*How the system is structured, sourced, and stored — for the engineer.*

- **[4 · Architecture Reference](2-10-architecture.md)** — the spine: conceptual layers, the
  `cmd/pkg` breakdown, data flow, and the design decisions (D-decisions) behind them.

**Data & sourcing** — where the numbers come from, each source described by the universe it
serves:

- **[7 · Data Sources](2-20-data-sources.md)** — sourcing each data type from the most
  authoritative provider that can supply it; the data model, API shapes, and provenance.
- **[8 · EDGAR](2-30-edgar.md)** — the SEC EDGAR client and XBRL concept mapper (US
  fundamentals), merged onto Schwab's TTM ratios.
- **[9 · EDGAR Facts Reference](2-40-edgar-facts-reference.md)** — the EDGAR XBRL fact universe:
  what we extract today and the candidates for future factors.
- **[17 · Screener / nselib Integration](2-50-screener-nselib.md)** — NSE `nselib` and
  Screener.in enrichment for the Indian-equity universe.

**Storage** — how state persists:

- **[10 · Storage & Pipeline Persistence](2-60-storage.md)** — DuckDB-backed pipeline state:
  runs, per-index picks, proposals, and final selections.
- **[11 · Data Directory Inventory](2-70-data-inventory.md)** — every file and directory under
  `data/`: what it is, where it comes from, and whether it's safe to delete.
- **[Configuration Directory Inventory](2-75-config-inventory.md)** — every file under
  `config/`: the two-file YAML core, Schwab credentials, and the `reference/` tree.

**Execution & output** — turning selections into orders and results into readable output:

- **[20 · Order Execution & Retry](2-80-execution-retry.md)** — rate-limited, failure-recovering
  order placement (`pkg/executor`, `mycase retry`).
- **[6 · Rendering](2-90-rendering.md)** — the CLI output layer (`pkg/render`): tables,
  formatters, and TTY-aware color, with a text fallback that never loses data.

> A hand-authored layer/package overview accompanies this part:
> `architecture-overview.d2` (source) / `architecture-overview.svg` (rendered).

## Part III — Operations

*Running and observing the system — for whoever operates it.*

- **[24 · Logging & Observability](3-10-logging.md)** — the two-channel logging model
  (`pkg/logging`, slog) and the raw-response archive (`mycase raw`) that make a
  mostly-headless system debuggable.
- **[21 · Testing](3-20-testing.md)** — the test pyramid, conventions, coverage by tier, and
  how to run each.
- **[23 · Static IP Setup](3-30-static-ip-setup.md)** — configuring the static IP that Zerodha
  Kite Connect requires (Indian-equity execution).

---

## Appendix A — Roadmap

**[Roadmap](9-10-roadmap.md)** is the one status-and-plan document — what's shipped (with
chapter pointers) and the detail for upcoming work. Consult it; don't read it as part of the
spine. Its filename stays `9-10-roadmap.md` as a stable ID.

Standalone working documents (issue trackers and the like) live one level up at the top of
`docs/`, outside the book — see [`docs/README.md`](../README.md).

(That top-level `docs/README.md` is the landing page; this file, `preface.md`, is the book's
own front page.)

---

*Retired: the R-numbered refactor history (formerly `05-refactor.md`) is not a chapter —
structural-change history lives in git, and the durable rules it once held now live in the
[Principles](0-30-principles.md) and [Architecture](2-10-architecture.md) chapters and
`.kiro/steering/`. The resolved-bug tracker (formerly `25-Bugs.md`) moved out of the book to
`docs/emb-bug-tracker.md` — a ledger of fixed bugs is a working document, not a chapter.*
