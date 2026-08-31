# Mycase — Roadmap

**Goal**: An automated US equity system that delivers slight but consistent outperformance over the S&P 500 while eliminating emotional decision-making and manual busy work.

**Updated**: August 2026

**Target investor**: US-based individual investor using Schwab. The India market components exist as legacy code from an earlier multi-market design but are not part of the active strategy.

---

## Table of Contents

1. [Philosophy](#1-philosophy)
2. [Current State](#2-current-state)
3. [Architecture Vision](#3-architecture-vision)
4. [Phased Roadmap](#4-phased-roadmap)
5. [Success Metrics](#5-success-metrics)
6. [Anti-Goals](#6-anti-goals)

---

## 1. Philosophy

### The honest premise

Most active strategies fail to beat a broad index over 10 years after costs. The few that succeed share common traits: systematic factor tilts, disciplined rebalancing, tax efficiency, and — above all — behavioral consistency. The investor who stays fully invested through a -30% drawdown outperforms the one who panic-sells and waits for "clarity" to re-enter.

Mycase is not a stock-picking edge machine. It is a **behavioral discipline engine** that happens to also pick stocks intelligently. The system's primary job is:

1. **Prevent the investor from doing something stupid** during drawdowns (automation doesn't feel fear)
2. **Harvest mechanical premiums** that require no insight — rebalancing premium, tax-loss harvesting, factor mean-reversion
3. **Apply systematic factor tilts** (quality + momentum) that have academic support for long-term outperformance

### The realistic edge

| Source of alpha | Expected contribution | Academic support |
|----------------|----------------------|-----------------|
| Behavioral discipline (no panic sell/FOMO buy) | 1–2% / year | Dalbar study: avg investor underperforms by 3–4% due to timing |
| Rebalancing premium (sell winners, buy losers mechanically) | 0.3–0.8% / year | Bernstein, Perold & Sharpe |
| Tax-loss harvesting (systematic loss realization) | 0.5–1.5% / year | Wealthfront, Betterment published data |
| Factor tilts (quality + momentum) | 0.5–2% / year | Fama-French, AQR, Cliff Asness |

**Realistic combined target**: 1–3% annualized outperformance over SPY (passive S&P 500), with comparable or lower max drawdown. Over 20 years at $500K invested, even 1.5% alpha compounds to $200K+ additional wealth.

### Why automation wins

The enemy is not bad stock picks — it's bad behavior:

- **Recency bias**: Selling quality stocks after a 15% drawdown, right before recovery
- **Anchoring**: Holding a broken stock because "it was at ₹500 once"
- **Action bias**: Trading too frequently because it feels like you're "doing something"
- **Analysis paralysis**: Spending 10 hours/week reading stock tips, getting conflicting signals

Automation eliminates all four. The system runs quarterly, follows its rules, and sends you a Telegram message when it's done. Your job is to confirm execution — not to second-guess the methodology every quarter.

---

## 2. Current State

### What's built and working

| Component | Status | Coverage |
|-----------|--------|----------|
| Stock selection — Multibagger | ✅ Production | Indian micro/small/mid-cap, 11 hard filters + 100-pt scoring |
| Stock selection — MFS multi-factor | ✅ Production | 16-factor scoring, 4 strategy presets |
| Stock selection — Value | ✅ Implemented | Indian large-cap, EPV-based, dual-path BFSI/industrial filters |
| Stock selection — US Quality-Momentum | ✅ Production | S&P 500, 6-factor quality+momentum scoring, 3 hard filters |
| Weight optimization | ✅ Production | Inverse-volatility, MFS-proportional, equal-weight |
| Sector caps & redistribution | ✅ Production | Iterative 25% sector cap, 3 stocks/sector, per-stock cap |
| Backtesting engine | ✅ Production | Date-aligned, sell-then-buy, slippage, 7 metrics |
| Monitoring (4-pillar) | ✅ Production | Revenue, cash flow, technical, capital allocation |
| Drift daemon | ✅ Production | Launchd/systemd, 15:45 IST, Telegram/Discord alerts |
| Execution — Zerodha | ✅ Production | Basket orders, GTT, cost model, micro-transaction filter |
| Transaction cost model (India) | ✅ Production | STT, stamp, DP, SEBI, Finance Act 2024 |
| DuckDB cache | ✅ Production | Price + fundamentals, smart expiry |
| Web dashboard | ✅ Production | 5-tab, ECharts, SSE, embedded binary |
| Hysteresis protection | ✅ Production | Prevents churn from small rank fluctuations |
| Excel/CSV ingestion | ✅ Production | ETF/broker file → clean ticker CSV |
| Screener.in integration | ✅ Production | Quarterly result dates, earnings calendar |
| Quarterly autopilot | ✅ Production | Non-interactive pipeline, launchd scheduling, proposal→confirm→execute workflow |
| Schwab API (US broker + market data) | ✅ Production | OAuth2 auth, real-time quotes, price history, order execution, ticker routing |
| Tax-loss harvesting (FIFO + TLH) | ✅ Production | FIFO lot tracking, harvest candidates, wash-sale detection, `basket --tax-optimize`, dashboard Tax tab |
| CLI rendering layer (`pkg/render`) | ✅ Production | stdlib tabwriter tables, color (TTY-aware), formatters (Pct, Currency, Sparkline), panic-safe fallback |

### What's specced but not built

| Component | Spec location | Blocking? |
|-----------|---------------|-----------|
| Tax-optimized rebalancing (FIFO engine) | `docs/feature.md` Feature 1 | ✅ Built for US (`pkg/tax`) — India variant still specced only |
| Options overlay | `docs/feature.md` Feature 2 | No — post-maturity optimization |
| Screener.in deep integration (QoQ, shareholding, CWIP) | `docs/screener.md` | No — enrichment |

### What's missing entirely

| Gap | Impact |
|-----|--------|
| Live performance attribution (benchmark tracking) | Cannot measure if we're actually outperforming |

---

### Known technical debt

| Debt | Location | Impact | Fix effort |
|------|----------|--------|-----------|
| ~~Stockpicker calls `yfinance.FetchFundamentals` directly~~ | ~~`pkg/stockpicker/run.go:93`, `cmd/pick.go`~~ | ✅ Fixed — `stockpicker.DataFetcher` interface added; `opts.DataFetcher` (a `datafetcher.Router`) routes US tickers to Schwab, others to Yahoo | Done |
| ~~`cmd/pick.go` `runPickWithOpts` duplicates `stockpicker.Run`~~ | ~~`cmd/pick.go:100-220`~~ | ✅ Fixed — `runPickWithOpts` now wires the router and delegates to `stockpicker.Run`; `us_quality_momentum` branches moved into `RunWithResult` | Done |
| `yfinance.GetCache()` still exists (deprecated) | `pkg/yfinance/duckdbcache.go` | Confusing API — external code should use `cache.GetDB()` | Remove once no callers remain |

---

## 3. Architecture Vision

The system should operate as a **6-layer stack** where each layer has a clear responsibility and can be tested independently:

```
┌─────────────────────────────────────────────────────────┐
│  Layer 6: Performance Audit & Attribution                │
│  Benchmark tracking, alpha decomposition, annual report  │
├─────────────────────────────────────────────────────────┤
│  Layer 5: Autopilot & Scheduling                ✅ BUILT │
│  Quarterly pipeline, drift-trigger, alert→confirm→exec   │
├─────────────────────────────────────────────────────────┤
│  Layer 4: Execution & Tax Awareness             ✅ BUILT │
│  Zerodha (India), Schwab (US), FIFO lots ✅, TLH ✅      │
├─────────────────────────────────────────────────────────┤
│  Layer 3: Portfolio Construction                         │
│  Cross-market allocation, per-market optimization,       │
│  sector caps, rebalancing bands                          │
├─────────────────────────────────────────────────────────┤
│  Layer 2: Strategy Engine                                │
│  Multibagger (India S/M), Value (India L), US Factor,    │
│  MFS scoring, hard filters                               │
├─────────────────────────────────────────────────────────┤
│  Layer 1: Multi-Market Data                      ✅ BUILT │
│  Yahoo Finance (India), Schwab API (US), DuckDB cache,   │
│  Screener.in (enrichment), NSE/BSE constituents          │
└─────────────────────────────────────────────────────────┘
```

### Data flow in autopilot mode

```
Quarterly trigger (launchd/cron)
  │
  ├─ India leg
  │   ├─ Fetch microcap250 + small250 constituents
  │   ├─ Pick (multibagger, top 20)
  │   ├─ Optimize (inverse-vol, sector caps)
  │   └─ Generate India basket
  │
  ├─ US leg
  │   ├─ Fetch S&P 500 constituents (or custom universe)
  │   ├─ Pick (quality-momentum factor tilt, top 15-20)
  │   ├─ Optimize (inverse-vol, sector caps)
  │   └─ Generate US basket
  │
  ├─ Cross-market allocation
  │   ├─ Apply India/US target split (e.g., 60/40)
  │   ├─ Adjust for drift since last rebalance
  │   └─ Tax-loss harvest: identify loss-making lots to realize
  │
  ├─ Consolidate orders
  │   ├─ Filter micro-transactions
  │   ├─ Classify STCG/LTCG (India), long/short-term (US)
  │   └─ Generate combined order sheet
  │
  └─ Alert investor
      ├─ Telegram: summary + link to dashboard
      ├─ Dashboard: full order table, cost breakdown, tax impact
      └─ Await confirmation before executing
```

### Ticker convention

| Market | Prefix | Example | Data source | Broker |
|--------|--------|---------|-------------|--------|
| India (NSE) | `NSE:` | `NSE:RELIANCE` | Yahoo Finance (`.NS` suffix) | Zerodha |
| India (BSE) | `BSE:` | `BSE:500325` | Yahoo Finance (`.BO` suffix) | Zerodha |
| US (NYSE/NASDAQ) | `US:` | `US:AAPL` | Schwab Market Data API | Schwab |

The router in `pkg/yfinance/router.go` (new) selects data source based on prefix. DuckDB cache stores all markets identically — only the fetch path differs.

---

## 4. Phased Roadmap

### ~~Phase 1: Quarterly Autopilot Pipeline~~ ✅ Completed (R10)

Implemented as `mycase autopilot {run, install, uninstall, status, dismiss}`. Non-interactive pipeline generates a proposal file, sends Telegram/Discord alert, and waits for investor confirmation via web dashboard or CLI. Scheduling via launchd `StartCalendarInterval` plist. See `docs/architecture.md` D7–D9 for design decisions, `docs/runbook.md` §7b for usage.

---

### ~~Phase 2: Schwab Integration — US Market Access (R9)~~ ✅ Completed

Implemented as `pkg/schwab/` (auth, client, market data, broker) + `pkg/datafetcher/router.go` (ticker routing) + `pkg/costs/us.go` (US cost model). CLI: `mycase auth --broker schwab`. See `docs/refactor.md` R9 in Completed Phases and `docs/architecture.md` D6 for broker interface design.

**Prerequisite for live use**: Schwab developer app registration + approval (1–3 business days), then `mycase auth --broker schwab` to complete OAuth flow.

---

### ~~Phase 3: US Factor Strategy~~ ✅ Completed

Implemented as `mycase pick --index sp500 --method us_quality_momentum --top 20`. Six-factor 100-point scoring model (ROIC, FCF Yield, 12-month Momentum skip-1-month, Earnings Quality, Shareholder Yield, Low Volatility) with US-specific hard filters (market cap > $10B, ADV > $50M, positive FCF). Scoring in `pkg/stockpicker/scoring_us.go`, config in `config/mfs.json` under `us_quality_momentum`. S&P 500 constituents fetched from GitHub dataset. Sector caps (max 4/sector), hysteresis, and rebalancing bands apply identically to India strategies.

---

### ~~Phase 4: Strategic Asset Allocation Layer~~ ❌ Dropped

**Reason**: The system is now focused on US-only investing via Schwab. Cross-market India/US allocation requires the investor to have brokerage access in both markets (Zerodha + Schwab), which is impractical for a US-based investor. Indian markets are also underperforming, making the complexity unjustified. The India code remains as legacy but is not part of the active strategy.

---

### Phase 7: DuckDB Intermediate Pipeline Migration

**What**: Move pipeline intermediate data (index picks, proposals, selection tracker state) from CSV files to DuckDB tables. Adds run tracking, atomic writes, and cross-run diffs.

**Why**: The pipeline currently passes data between stages via CSV files in `data/candidates/`. This is fragile (half-written CSV on crash), opaque (can't query historical runs), and messy (stale files accumulate). The selection tracker parses its own `.txt` output to extract previous-run metrics — brittle. Moving to DuckDB gives atomic transactions, run history, and a foundation for lot tracking (Phase 4 TLH) and performance attribution (Phase 5).

**What moves**:
- `data/candidates/index_picks/*.csv` → `index_picks` table
- `data/candidates/proposals/*.csv` → `proposals` table (with stage column)
- `data/candidates/temp/combine_*.csv` → eliminated (in-memory DuckDB join)
- Selection tracker cross-run state → `selections` table

**What stays as files**:
- `data/{name}.csv` (golden copy) — requires manual editing until SwiftUI UI exists
- Human-readable `.txt` reports — opened in editor
- Backup CSVs — manual disaster recovery
- Config files — version-controlled

**Schema**: `pipeline_runs` (run tracking), `index_picks` (per-index scored candidates), `proposals` (draft/optimized/final stages), `selections` (final portfolio with driver metrics for cross-run comparison).

**Deliverables**:
- Schema additions to `pkg/cache/db.go` (`ensureSchema()`)
- `pipeline_runs` tracking (run_id, status, portfolio, method)
- Pipeline write path: stages insert to DB instead of writing CSV
- Pipeline read path: combine step is a query, not file parsing
- `mycase pipeline history` — list past runs
- `mycase pipeline diff <run1> <run2>` — cross-run comparison
- `mycase pipeline show <run_id>` — inspect a specific run
- `--legacy-csv` flag during transition for belt-and-suspenders

**Effort**: ~10 days (4 sub-phases: schema 1d, write path 3–4d, read path 2–3d, CLI+cleanup 2–3d). Each sub-phase is independently deployable.

**Detailed design**: See `docs/duckdb-migration.md` for full schema DDL, implementation phases, and risk mitigations.

---

### ~~Phase 4: Tax-Loss Harvesting Engine~~ ✅ Completed

**What**: FIFO lot tracking and systematic tax-loss harvesting for the US portfolio.

Implemented as the `pkg/tax` package (FIFO engine + TLH logic, broker-agnostic and unit-tested) plus supporting infrastructure:

- **`pkg/tax/fifo.go`** — `BuildLots` replays a chronological transaction history with FIFO matching (oldest lots consumed first), producing open lots and per-lot realized gains. Buy fees increase cost basis; sell fees reduce proceeds; oversells are flagged as warnings rather than fabricating zero-basis lots.
- **`pkg/tax/tlh.go`** — `FindHarvestCandidates` identifies loss-making lots worth harvesting (only losing lots within a mixed position), estimates federal tax savings (ST 37% / LT 20%), suggests same-sector substitutes that avoid the wash-sale rule, and flags wash-sale risk. `DetectWashSales` and `SummarizeRealized` (ST/LT split) round out reporting.
- **`pkg/tax/sequence.go`** — `TaxOptimizeOrders` reorders a batch for execution: loss-sells → gain-sells → buys, and flags any buy that would repurchase a loss-sold security (wash sale).
- **`pkg/broker/schwab/transactions.go`** — `FetchTransactions` (`GET /accounts/{hash}/transactions?types=TRADE`, chunked by year to respect the API window) + `NormalizeTransactions` mapping Schwab records to broker-agnostic `tax.Transaction`.
- **DuckDB** — `tax_transactions`, `tax_lots`, `realized_gains` tables (additive `CREATE TABLE IF NOT EXISTS`) with round-tripped Insert/Get methods in `pkg/cache/tax.go`. Lots and realized gains are derived state (recomputed from transactions on each import).
- **CLI** — `mycase tax import --broker schwab` (bootstraps lots), `mycase tax status` (open lots + YTD/all-time realized summary), `mycase tax harvest` (harvest candidates). `mycase basket --tax-optimize` sequences orders and surfaces wash-sale warnings; the basket's US tax warnings now use real FIFO purchase dates instead of "Unknown".
- **Dashboard** — new Tax tab (`/api/portfolio/{name}/tax` + `tax-tab.js`) showing realized gains/losses (YTD + all-time), harvest candidates, wash-sale calendar, and open lots with unrealized P&L.

**US tax rules honored**: short-term (< 1 year, up to 37%) vs long-term (≥ 1 year, 15/20%), 30-day wash-sale window, substitute must differ from the harvested security and its sector peers already held.

---

### Phase 5: Live Performance Attribution

**What**: Continuously track the actual portfolio's performance against SPY and decompose returns into their sources.

**Why**: Without this, you don't know if the system is working. "I think I'm beating the market" is not the same as "I'm beating the market by 1.7% annualized with a 0.82 information ratio." You need hard numbers to decide whether to continue, adjust, or simplify to pure index funds.

**Deliverables**:
- `pkg/attribution/tracker.go` — daily NAV computation from live Schwab holdings
- Benchmark tracked: SPY (S&P 500 ETF) — the "do nothing" baseline
- Monthly attribution report: how much came from stock selection vs rebalancing vs tax savings
- `mycase performance --vs-benchmark` — show cumulative alpha chart
- Dashboard performance tab: equity curve overlaid with SPY, rolling 1Y alpha
- Alert: if trailing 12-month alpha is significantly negative, send a "review your strategy" nudge

**Attribution decomposition**:
- **Selection effect**: did factor-tilted picks beat SPY?
- **Rebalancing effect**: did quarterly rebalancing add value vs buy-and-hold?
- **Tax effect**: how much did TLH save vs a no-TLH baseline?

**Effort**: ~2 weeks. The hard part is getting consistent NAV history; the math is standard.

**Implementation split** (see `docs/refactor.md` Phase 5 for full design):
- **Phase 5a** (in progress) — NAV foundation: `pkg/attribution` (daily NAV via `datafetcher.Router`, alpha/beta/information ratio vs benchmark), `nav_history` DuckDB table, `mycase performance --vs-benchmark`. Written slog-native (R14.3).
- **Phase 5b** — return decomposition (selection/rebalancing/tax), dashboard performance tab, negative-alpha alert nudge.
- **Benchmark**: `US:SPY` (the actual ETF, routed through Schwab) rather than the `^GSPC` index the backtest uses — SPY is the honest "you-could-have-bought-this" baseline. Configurable.

---

### Phase 8: Structured Selection History & Cross-Run Attribution Trail

**Status**: ⬜ Not started (deferred feature — replaces removed dead code)

**Origin**: Phase 7 (DuckDB migration) created a `selections` table + `Selection` struct + `InsertSelections`/`GetSelections`/`GetPreviousSelections` methods intended to hold a **structured, queryable audit trail of the final portfolio with per-stock driver metrics and cross-run deltas**. That persistence was never wired into the pipeline (nothing ever called `InsertSelections`), so `pipeline show`'s "Final Selections" section always rendered empty. During the "fix before feature" cleanup (loose end **L2**) the unused scaffolding was **deleted** rather than left as dead code — with the design intent captured here so it can be built deliberately when it earns priority.

**What it is**: Today the pipeline persists `index_picks` and `proposals(draft/optimized)` — but proposals only carry `{ticker, weight, score, rank, sector}`. The richer picture of *why* each stock was selected and *how the roster changed vs last quarter* lives only in the human-readable text report produced by `pkg/selectiontracker` (`report/*/executions/*_01_selection_reasons.txt`), which the tracker then re-parses from text to compute deltas — brittle. This phase makes that a first-class structured record.

**Why it's worth building** (when prioritized):
- **Behavioral discipline** (the core thesis): a durable, queryable answer to "why is this stock in my portfolio and what changed since last rebalance?" — reviewable without re-running anything.
- **Feeds performance attribution (Phase 5)**: the selection effect decomposition benefits from knowing which stocks were newly added vs retained, and their entry driver metrics.
- **Kills the text-parsing hack**: `selectiontracker.SaveReport()` currently parses its own prior `.txt` output to compute cross-run deltas. A `selections` table makes `GetPreviousSelections` the clean source of truth.

**Data model** (what the richer record captures beyond `proposals`):
- Per-stock driver metrics at selection time: `ttm_growth`, `revenue_cagr`, `dso_delta`, `rsi`, `momentum_1y`, `fcf_yield`, `roic`
- Cross-run delta fields: `action` ("new" / "retained" / "removed"), `prev_rank`, `prev_weight`

**Deliverables**:
- Re-add the `selections` table DDL (`pkg/cache/db.go`), `Selection` struct, and `Insert/Get/GetPrevious` methods (the deleted code is recoverable from git history — commit that closed L2).
- **Wire the write path**: at pipeline finalization, build `[]cache.Selection` from the in-memory `selectiontracker.Tracker` state + the fundamentals already fetched during scoring, compute `action`/`prev_rank`/`prev_weight` via `GetPreviousSelections(portfolio, method)`, and call `InsertSelections`. This is the missing step that was never built.
- Rewire `selectiontracker.SaveReport()` to source previous-run deltas from `GetPreviousSelections` instead of parsing the prior text file.
- Restore the "Final Selections" section in `mycase pipeline show` (driver metrics + action/prev-rank columns) — now backed by real data.
- Optionally extend `mycase pipeline diff` to compare selection-level driver metrics between runs (today it diffs proposals only).

**Effort**: ~3–4 days. The schema and methods are trivial to restore; the real work is (1) assembling `Selection` records from tracker + fundamentals at the right pipeline stage, and (2) migrating the tracker's delta computation off text parsing.

**Dependency**: Best sequenced with or after Phase 5 (Performance Attribution), which is the primary consumer of the structured selection history.

---

### Phase 9: Proposal `final` Stage Lifecycle

**Status**: ⬜ Deferred (defined-but-unimplemented — loose end **L4**, kicked to roadmap for hindsight)

**Origin**: The DuckDB `proposals` table carries a `stage` column, and the pipeline writes two stages: `draft` (raw pick) and `optimized` (post weight-optimization + sector caps). A third stage, `"final"`, is *referenced* on the read side — `mycase pipeline show` iterates `draft/optimized/final`, and `pipeline diff`'s `--stage` flag lists `final` as a valid value — but **nothing ever writes it**. `GetProposals(runID, "final")` always returns empty. It is aspirational surface area with no consumer, so rather than invent a lifecycle speculatively during the "fix before feature" cleanup (loose end **L4**), the intent is captured here to be built deliberately (or dropped) with hindsight.

**What it would be**: A `final` stage closes the loop between *what the system proposed* and *what actually executed*. After the investor confirms a proposal and orders fire (`mycase basket --live` / autopilot execution), the executed basket — real fill quantities and prices, post micro-transaction filtering and any manual edits — is written back as the `final` stage for that run. This makes the run record a complete audit trail: proposed (`optimized`) vs executed (`final`).

**Why it's worth building** (when prioritized):
- **Honest audit trail**: today the DB records what was *proposed*, never what was *executed*. Drift between the two (partial fills, skipped micro-transactions, manual removals) is invisible after the fact.
- **Feeds performance attribution (Phase 5)**: the selection/rebalancing decomposition currently reconstructs history from `optimized` proposals — i.e. intended weights, not realized ones. A `final` stage lets attribution use *actual* executed weights, tightening the "did rebalancing add value" measurement.
- **Removes a misleading affordance**: `pipeline show`/`diff` currently advertise a `final` stage that never has data — either back it with real data or trim the references.

**Deliverables**:
- Define the trigger: write `final` at execution confirmation (`pkg/executor` / autopilot post-confirm path), sourced from executed order results rather than the proposal.
- Persist executed quantities/prices (may need a richer `Proposal` shape or a sibling table if fill data doesn't fit the current `{ticker, weight, score, rank, sector}`).
- Update attribution's `LoadRebalanceHistory` to prefer `final` when present, falling back to `optimized` for runs that predate the stage.
- Restore/confirm the `pipeline show` + `diff` `final` references now that they have data.

**Alternative (if not built)**: drop `"final"` from the `pipeline_show` loop and the `pipeline diff --stage` usage string, and document `optimized` as the terminal stage — so the code stops implying a feature that doesn't exist.

**Effort**: ~2–3 days. The persistence is easy; the real work is threading executed fill data out of the executor and deciding the schema for realized (vs proposed) weights.

**Dependency**: Best done with or after Phase 5 (the primary consumer of realized rebalance history). Independent of Phase 8.

---

### Phase 6: Options Overlay (Post-Maturity)

**What**: Once the portfolio is stable and well-tracked (6+ months live), add an options overlay for income generation and tail-risk hedging.

**Why**: For a portfolio of 15-20 quality stocks, covered calls on over-weight positions generate 2-5% additional annual income. Protective puts on concentrated positions hedge black-swan events. This is an optimization on a working system — not a priority for a system that's still being built.

**Prerequisite**: Schwab options chain API (included in Market Data Production bundle) + 6 months of live portfolio tracking to understand position sizing and volatility profiles.

**Deliverables** (future):
- `pkg/options/overlay.go` — strike/expiry selection engine
- Covered call candidates: positions > 100 shares, IV rank > 30, overweight vs target
- Protective put candidates: concentrated positions (> 8% weight), earnings approaching
- `mycase options suggest` — weekly option overlay recommendations
- Integration with Schwab order API for option execution

**Effort**: ~4 weeks. Deferred because it requires a stable, tracked portfolio and options expertise.

---

### Timeline Summary

| Phase | Target | Dependency | Core value delivered | Status |
|-------|--------|------------|---------------------|--------|
| 1. Quarterly Autopilot | ~~Sep 2026~~ | None | Removes all manual busy work | ✅ Done |
| 2. Schwab Integration | ~~Oct 2026~~ | Schwab app approval | US market access | ✅ Done |
| 3. US Factor Strategy | ~~Nov 2026~~ | Phase 2 ✅ | US stock picking with factor edge | ✅ Done |
| ~~4. Asset Allocation~~ | — | — | ~~India+US portfolio~~ | ❌ Dropped |
| 7. DuckDB Pipeline Migration | Sep 2026 | None | Atomic pipeline, run history, query-based diffs | ✅ Done |
| 4. Tax-Loss Harvesting | ~~Oct 2026~~ | Phase 7 ✅ | 0.5-1.5% tax alpha | ✅ Done |
| 5. Performance Attribution | ~~Nov 2026~~ | Phase 3 ✅ | Know if system works | ✅ Done (5a+5b) |
| 8. Structured Selection History | Post Phase 5 | Phase 7 ✅ | Queryable "why held + what changed" audit trail | ⬜ Deferred (replaces removed L2 dead code) |
| 9. Proposal `final` Stage | Post Phase 5 | Phase 7 ✅ | Executed-vs-proposed audit trail | ⬜ Deferred (defined-but-unimplemented, ex-L4) |
| 6. Options Overlay | H2 2027 | Phase 5 + 6mo live data | Income optimization | ⬜ |

---

## 5. Success Metrics

### Primary metric: Net-of-cost, net-of-tax CAGR vs benchmark

**Benchmark**: SPY (S&P 500 ETF). This is what you'd get from one index fund with zero effort.

**Target**: Outperform SPY by 1–3% annualized over a rolling 3-year window.

**Measurement**: Begin tracking from the first fully-automated quarterly rebalance. Report monthly. Meaningful statistical significance requires 3+ years of data.

### Secondary metrics

| Metric | Target | Why it matters |
|--------|--------|---------------|
| Max Drawdown | < benchmark drawdown | If we take more risk for the same return, the system is broken |
| Sharpe Ratio | > benchmark Sharpe | Risk-adjusted return, not just absolute return |
| Turnover | < 30% annually | Low turnover = low costs + low tax drag |
| Time spent by investor | < 1 hour/quarter | The entire point is removing busy work |
| Rebalance discipline | 100% execution rate | Never skip a scheduled rebalance (emotional override = system failure) |
| Tax savings | Track $ saved via TLH annually | Concrete, measurable benefit |

### Failure conditions (when to simplify to pure index funds)

- 3 consecutive years of negative alpha after costs → the factor tilts aren't working in the current regime
- Max drawdown exceeds benchmark by > 10% → the concentration risk isn't worth it
- System requires > 2 hours/quarter of manual intervention → automation has failed
- Investor overrides the system more than once per year → behavioral discipline has broken down

If any failure condition triggers: simplify to VOO/VTI (passive S&P 500 / total market index) and stop trying to outperform. Knowing when to quit is part of the system.

---

## 6. Anti-Goals

These are things we explicitly will **not** build or pursue:

### Won't build

| Anti-goal | Why |
|-----------|-----|
| Day trading or intraday strategies | Negative-sum after costs; requires constant attention (opposite of our goal) |
| Crypto/NFT/speculative assets | Outside competence; no fundamental valuation framework applies |
| Leverage / margin trading | Amplifies behavioral errors; can blow up the portfolio |
| AI/ML black-box stock prediction | Violates transparency principle; overfits to noise; can't be trusted in drawdowns |
| Social/sentiment trading signals | Twitter tips, Reddit sentiment, etc. — noise, not signal, at our timescale |
| High-frequency or latency-sensitive execution | We rebalance quarterly; microsecond edge is irrelevant |
| Multi-user SaaS | This is a personal tool; no desire to manage other people's money |

### Won't pursue

| Anti-goal | Why |
|-----------|-----|
| > 5% annual alpha | Unrealistic target leads to over-trading, excessive risk, and eventual blowup |
| Perfect market timing | Impossible; system stays fully invested through all conditions |
| Zero drawdowns | Drawdowns are the price of equity returns; we accept them, we don't avoid them |
| Beating the market every quarter | Factor tilts underperform for years at a time; the edge is long-term |
| Complex derivatives strategies | Covered calls/puts (Phase 7) are the ceiling; no multi-leg spreads, no straddles |

### Design constraints (inherited from vision.md)

- **Transparency**: Every score, filter, and weight must be explainable from the output
- **Local-first**: No cloud service, no subscription, no external dependencies beyond broker APIs and market data
- **Investor-in-the-loop for execution**: System recommends; investor confirms; orders fire. Never auto-execute without explicit opt-in
- **No black boxes**: If the investor can't explain why a stock is in the portfolio by reading the report, the system has failed

---

## Appendix: How Each Phase Maps to Alpha Sources

| Alpha source | Which phase delivers it | Expected contribution |
|--------------|------------------------|----------------------|
| Behavioral discipline | Phase 1 (autopilot removes temptation to override) | 1–2% / year |
| Rebalancing premium | Phase 1 (quarterly rebalance) | 0.3–0.8% / year |
| Factor tilts (US) | Phase 3 (quality + momentum) | 0.5–1.5% / year |
| Pipeline reliability | Phase 7 (DuckDB migration — atomic writes, run history) | Operational quality (no lost data) |
| Tax-loss harvesting | Phase 4 | 0.5–1.5% / year (tax savings) |
| Performance awareness | Phase 5 (know when to simplify) | Prevents compounding losses from a broken strategy |
| Options income | Phase 6 | 1–3% / year on mature portfolio |

**Net expected alpha (Phases 1–5, conservative)**: 1.5–3% annually over SPY, at comparable max drawdown.

**Net expected alpha (all phases, optimistic)**: 3–5% annually. This is the upper bound; don't plan around it.

---

## Appendix B: Future Explorations (Parked)

Ideas worth revisiting once the core system (Phases 1–7) is stable and the ecosystem matures.

### Native macOS App (SwiftUI + DuckDB)

**Revisit when**: macOS 27, DuckDB 2.0, and Phases 4–5 are complete.

**Why it might be better**: The current web dashboard (`mycase serve` + browser) works but has friction — starting a server, opening a tab, no native notifications. More critically, the pipeline's "pause for human editing" step currently requires opening a raw CSV in a text editor — zero context, zero validation, easy to break.

A SwiftUI app solves both problems:

#### Core Value: Portfolio Editing UI

The pipeline currently does:
```
Pipeline runs → writes proposal CSV → prints "remove unwanted stocks"
→ user opens CSV in TextEdit/Excel → deletes rows → saves
→ user presses Enter in terminal → pipeline resumes
```

This is fragile: wrong column deleted, accidental formatting, no context while editing. A native editing view replaces this with:

- Table view with ticker, weight, sector, score, momentum — full context visible
- Checkbox/swipe to exclude stocks (not raw row deletion)
- Color-coded flags: "dropped from index", "failed hard filter", "new addition"
- Inline sparklines for price history
- Confirm button that writes back to the golden copy (or signals the pipeline via API callback)
- Validation: can't accidentally remove all stocks, can't break CSV structure

**This eliminates the golden copy's "must be a file" constraint** — once a proper editing UI exists, the golden copy can move to DuckDB too. The pipeline "pause" becomes: write proposal to DB → push SSE event → SwiftUI shows approval UI → user confirms → pipeline resumes via HTTP callback.

#### Additional Capabilities

- Menu bar presence showing portfolio value / drift status at a glance
- Native notifications for autopilot proposals with confirm/dismiss action buttons
- No server process needed — read DuckDB directly from Swift
- Single `.app` bundle distribution (drag to Applications)
- Swift Charts for equity curves, sector donut, weight comparison
- Shortcuts / Siri integration for hands-free status checks
- Rebalance proposals with approve/reject per trade
- Push notifications for drift alerts (daemon already dispatches them)
- Touch Bar widget for quick portfolio health

#### Architecture

```
┌─────────────┐         ┌──────────────┐         ┌─────────────┐
│  SwiftUI    │◄──JSON──►│  pkg/server  │◄────────►│  DuckDB     │
│  (macOS)    │   HTTP    │  (existing)  │          │  + golden   │
└─────────────┘         └──────────────┘         └─────────────┘
                              │
                         SSE quotes
```

- The Go server already serves JSON APIs for holdings, weights, drift, orders, monitor
- SwiftUI app is a native client to the **same API** the web dashboard uses — no new server work
- Pipeline "pause for human edit" becomes an API-driven approval flow
- Swift reads DuckDB directly for read-heavy display (portfolio value, historical charts)
- Shells out to `mycase` CLI for all mutations (pick, optimize, basket)
- Go binary remains the source of truth for logic; Swift is pure presentation + OS integration
- The web dashboard stays for headless/remote scenarios

#### Phased Build Plan

| Step | What | Effort |
|------|------|--------|
| 1. Proposal approval view | The one screen that replaces CSV editing. `URLSession` + `Codable` against existing API | 1 week |
| 2. Holdings dashboard | Table + donut chart. Read-only. Same data as web dashboard | 1 week |
| 3. Menu bar widget | Portfolio value + drift %. Background polling or SSE | 3 days |
| 4. Native notifications | Wire to daemon alerts. macOS `UserNotifications` framework | 2 days |
| 5. Performance charts | Swift Charts equity curve overlaid with SPY | 1 week |
| 6. Full migration | Replace web dashboard as primary UI. Server stays for API | 2 weeks |

#### Why Not Now

Phases 4–5 (TLH, Performance Attribution) are higher priority — they generate alpha; the UI doesn't. The CSV workflow is ugly but works for quarterly rebalance (4×/year). Defer until:
- The system is stable enough that UX is the bottleneck, not the strategy
- DuckDB intermediate migration is done (Phase 7, see `docs/duckdb-migration.md`) — the golden copy can then move to DB
- Swift Charts and DuckDB Swift bindings are mature enough for production use
