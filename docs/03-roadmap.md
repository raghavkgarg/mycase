# Mycase — Roadmap

**Goal**: An automated US equity system that delivers slight but consistent outperformance over the S&P 500 while eliminating emotional decision-making and manual busy work.

**Updated**: September 2026

**Target investor**: US-based individual investor using Schwab. The system supports two
**market paths** — the **US-Path** (Schwab + SEC EDGAR, the active strategy focus) and the
**India-Path** (Zerodha + NSE/Yahoo, from the earlier multi-market design). The
market-parameterized design (`marketcal`, `pkg/eod`, the strategy engine) runs either path.

---

## ⏭️ Next up (operator TODO)

> **Run the first EDGAR-enabled S&P 500 pick with fresh data** (deferred to get a clean, fully-settled US EOD rather than a cold run tonight):
>
> ```bash
> make run ARGS="pick --index sp500 --method us_quality_momentum --top 20 --force"
> ```
>
> - Run it after the prior US session has settled (NYSE close 16:00 ET ≈ 01:30–02:30 IST next morning), so the EOD-settlement freshness picks up the latest data.
> - Keep `--force`: an earlier same-day snapshot (the pre-EDGAR `0/N` result) would otherwise short-circuit the recompute.
> - **This run will make live Schwab + EDGAR calls** — it is the cold-cache first fetch (~500 prices + ~500 fundamentals + ~500 EDGAR companyfacts). It *populates* the DuckDB cache as it goes (`source="schwab"` / `"schwab+edgar"`), so every same-day re-run after it is warm (zero Schwab/EDGAR calls).
> - Expected result now that EDGAR is enabled: real FCF values, so `us_quality_momentum`'s positive-FCF hard filter passes real names instead of eliminating the whole index (`0/N`).
> - Sanity-check EDGAR is live on the run: `data/raw/` should start showing files after the CIK-map download, and the funnel should no longer report every ticker as "FCF ≤ 0".
>
> _Context: EDGAR was enabled and verified live (commit `3d774d8`, Phase 10c/10e). Remove this note once the first successful run is done._

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

### Shipped and in production

Each capability below is live; the **Chapter** column points to where it's documented in
the guide (see `docs/README.md`).

| Capability | Chapter |
|-----------|---------|
| US Quality-Momentum selection (S&P 500, 6-factor quality+momentum, hard filters) | Ch. 4 Architecture |
| Weight optimization (inverse-vol, MFS-proportional, equal-weight) + iterative sector caps | Ch. 4 Architecture |
| Schwab API — OAuth2 auth, quotes, price history, order execution, ticker routing | Ch. 7 Data Sources |
| SEC EDGAR fundamentals — XBRL concept mapper, Schwab+EDGAR merge, provenance | Ch. 7–9 |
| DuckDB pipeline persistence — runs / proposals / selections, `pipeline history\|show\|diff` | Ch. 10 Storage |
| Tax-loss harvesting — FIFO lots, TLH candidates, wash-sale, `basket --tax-optimize` | Ch. 4, Ch. 19 |
| Live performance attribution — NAV vs SPY, alpha/IR/beta, decomposition, dashboard tab | Ch. 4 Architecture |
| Quarterly autopilot — non-interactive pipeline, launchd scheduling, proposal→confirm→execute | Ch. 18 Runbook |
| Drift monitoring daemon — Telegram/Discord alerts | Ch. 18 Runbook |
| Backtesting engine — date-aligned, sell-then-buy, slippage, metrics | Ch. 4 Architecture |
| 4-pillar monitoring (revenue, cash flow, technical, capital allocation) | Ch. 4 Architecture |
| Web dashboard — 5-tab, ECharts, SSE, embedded binary | Ch. 18 Runbook |
| Structured logging + raw-response capture/triage | Ch. 24 Logging & Observability |
| CLI rendering layer (`pkg/render`) | Ch. 6 Rendering |
| Hysteresis / anti-churn cooldown; market-aware EOD settlement; standardized rate limiting; home-relative config resolution | Ch. 2, Ch. 4 |
| **India-Path:** Multibagger, Early Multibagger, Value, MFS, Zerodha execution, India cost model, Screener/nselib, Themes | Module D, Module F |

### Specced but not built

| Component | Spec location | Notes |
|-----------|---------------|-------|
| Options overlay | Ch. 19 Feature Specs | Post-maturity optimization (Phase 6 below) |
| Tax-optimized rebalancing — India variant | Ch. 19 Feature Specs | US variant is built (`pkg/tax`); India variant still spec-only |
| Screener.in deep integration (QoQ, shareholding, CWIP) | Ch. 17 Screener | Enrichment, non-blocking |

---

## 3. Architecture Vision

The system is a 6-layer responsibility stack (market data → strategy → portfolio construction → execution & tax → autopilot → audit & attribution), US-only via Schwab. For the system design — conceptual layers, the concrete `cmd/pkg/` package breakdown, data flow, ticker routing (`US:`→Schwab, else→Yahoo), and design decisions — see **`docs/04-architecture.md`** §2 (Inputs), §4 (System Design), and §11 (Design Decisions). This roadmap covers only *what* is being built and *when*.

---

## 4. Phased Roadmap

Completed and dropped phases are not narrated here — shipped work is summarized in §2 with
chapter pointers, and its history lives in git. Design detail for shipped subsystems lives
in `docs/04-architecture.md` (design decisions) and `docs/10-duckdb-migration.md` (storage).
Only active and planned work remains below.

### Phase 10 — Data Source Resilience  *(shipped; Phase 10d provenance shipped, interface split open)*

**Goal**: source each data type from the most authoritative provider that can supply it,
with deterministic logged fallback, and record provenance. This is **shipped** — US data
routes through Schwab everywhere (Yahoo fallback), SEC EDGAR supplies authoritative
fundamentals merged onto Schwab's TTM ratios, sector comes from the constituents CSV, the
benchmark resolves to `US:SPY` via Schwab, and both cache tables carry a `source`
provenance column. All three API clients (Schwab, EDGAR, Yahoo) are cache-first and paced
by a shared rate limiter. See **Ch. 7 Data Sources**, **Ch. 8–9 EDGAR**, and **Ch. 10
Storage** for how it works.

**Phase 10d (partially shipped):**

- **Provenance surfacing — shipped.** Data provenance now flows end to end from the
  fetch/merge layer to the audit surfaces, at two granularities:
  - *Per-record* — `marketdata.Fundamentals` carries a `Source` tag
    (`schwab+edgar` / `schwab` / `edgar` / `yahoo`) set by the merger (US Schwab/EDGAR
    path) or the Yahoo path. It rides through the DuckDB cache blob, is persisted on the
    `selections` table (new `source` column + migration), and renders as a **Source
    column** in `mycase pipeline show` and in the selection-reasons report.
  - *Per-field* — `marketdata.Fundamentals.FieldSources` records which specific fields
    EDGAR authoritatively overlaid onto the Schwab base (FCF, operating cash flow, net
    income), **with the originating filing** — e.g. `edgar:10-K FY2025`. The EDGAR concept
    mapper captures each value's `Form`/`FP`/`FY`; the selection-reasons report annotates
    each pick with a compact marker, e.g. `… | [source: EDGAR FCF (10-K FY2025), OCF (10-K
    FY2025)]`. Populated only on the merged US path.

- **Open — interface split + fallback chain (optional):** split `DataFetcher` into
  capability interfaces (`PriceSource`, `FundamentalsSource`, `SectorSource`) and formalize
  the currently ad-hoc, per-method fallback (historical does not fall back; quotes/
  fundamentals do) into one explicit ordered chain. The consumer-defined-interface pattern
  already exists (`stockpicker.DataFetcher`, `server.MarketDataFetcher`,
  `attribution.PriceFetcher`), so this is largely mechanical. Depends on nothing further;
  deferred for lack of pressure.


---

### Phase 11 — Data & observability hygiene  *(mostly shipped; a few open items)*

The raw-response capture/triage store, market-aware EOD settlement (`pkg/marketcal`),
market-aware formatting (`pkg/marketfmt`), and the data-mapping bug fixes (`pick` `0/N`,
EDGAR FCF binding, sector-aware FCF exemption, CIK overrides, `edgar_facts` blob compaction)
are **shipped**. The logging + raw-store system is documented in **Ch. 24 Logging &
Observability**. Remaining open items:

- **R-store-6 (optional) — verdict-driven early deletion of raw captures.** Only if clean
  captures start crowding out interesting ones. Needs a run+outcome primitive (pin recent +
  flagged-broken runs, prune plausibly-fine old ones early), leaning on cheap "broken"
  signals (`0/N`, empty report, high fetch-failure rate) — never attempting to define
  "correct". Highest risk of deleting the wrong thing; defer until demonstrably needed.
- **R-store-7 — keep-or-cut review of the replay feature (`MYCASE_REPLAY`).** Capture +
  triage has carried the debugging load; replay has never been used in anger. Decide
  deliberately: if kept, define the concrete workflow it wins (e.g. deterministic
  regression of a whole pipeline run against a frozen fixture set) and add an end-to-end
  test so it can't rot; if dropped, remove the `Replay`/`findLatest` machinery and the
  per-client replay branches, keeping capture + triage. Not urgent.
- **Flatten `data/` + `report/`** *(deferred — destructive)*: collapse both nested trees
  into one flat `data/` plus a disposable `data/raw/`, with a filename naming convention
  (`<domain>__<portfolio>__<method>__<stamp>__<kind>.<ext>`) carrying the identity the
  paths used to. Centralize name construction in one `pkg/config` `DataPath` helper so
  writers compose and readers glob identically. `stockpicker.PickIdentity`/`SanitizeName`
  is the groundwork. Deferred deliberately: highest effort, only destructive open item, no
  functional pressure — needs sign-off and a clean cutover.
- **Golden-copy → DB** *(deferred, gated)*: the human-editable golden copy stays a file
  until an editing UI exists (see Appendix B); moving it into the `golden_portfolio` table
  is gated on that. See **Ch. 10 Storage**.

---
### Phase 12: Autonomous Scheduler (design — not yet built)

**Goal**: the system runs itself at the right cadence without a human remembering to
invoke commands — while preserving the investor-in-the-loop rule for anything that
places orders. Today three natural cadences exist but are unevenly automated and
uncoordinated (see "Current state" below). This phase designs a single Go-native
orchestrator that owns all three.

> **Status: shipped.** The scheduler is built and wired (`pkg/scheduler` + `mycase
> scheduler`). The sections below are the as-built design; the earlier per-cadence gaps are
> resolved.

#### Motivation (the pre-scheduler state)

Before the scheduler, three cadences existed but were unevenly automated and uncoordinated:

| Cadence | Mechanism (before) | Gap the scheduler closed |
|---|---|---|
| **(a) Daily EOD update** — `mycase db update` | only the stale `scripts/daily_sync.sh` (machine-specific path, hand-maintained holiday list) | No Go-native scheduling; logic was trapped in `cmd/db.go` → now `pkg/eod`, dispatched by the scheduler |
| **(b) Drift monitoring** — `pkg/daemon` | `daemon.RunLoop`, its own launchd/systemd unit | Fired *every calendar day*; now gated on the holiday-aware clock and sequenced after EOD |
| **(c) Quarterly rebalance** — `pkg/autopilot` | `autopilot install`, its own unit | Separate installer, no ordering; `auto_execute` was dead config → now one unit, EOD-first ordering, `auto_execute` consumed (gated) |

Cross-cutting problems the scheduler resolved: **no single orchestrator** (three separate
OS units with no ordering) → one process, EOD → drift → rebalance sequencing; **three
inconsistent "is it a trading day?" notions** (daemon every-day, autopilot live-probe,
`marketcal` weekend-only) → unified on the holiday-aware `marketcal` clock; **no holiday
calendar in Go** → `config/holidays.json` + `marketcal` holidays.

#### Design decisions (proposed)

1. **One long-lived scheduler process, not N OS units.** Replace the per-feature
   launchd/systemd installers with a single `mycase scheduler` daemon that owns a tick
   loop and dispatches all three cadences. One install (`mycase scheduler install`),
   one PID, one log. The OS unit is a **thin keep-alive** (launchd first; systemd
   supported for a future Linux server) — it only keeps the process alive; all cadence
   logic stays in Go. Rationale: coordination (ordering + shared "is it a trading day?"
   answer) is impossible across three independent OS-fired one-shots; it's trivial
   inside one process. The drift daemon's existing self-timed-loop model (L4
   `daemon.RunLoop`) is the proven pattern to generalize.

   > **Superseded in the as-built (see Progress below).** The shipped design keeps
   > the single-process *coordination* (one `scheduler run-now` runs the ordered pass)
   > but drops the resident keep-alive loop in favor of an OS **one-shot** timer
   > (`StartCalendarInterval` / `OnCalendar`) firing `mycase scheduler run-now` daily.
   > The "impossible across three one-shots" rationale held only for *three separate*
   > units; **one** OS timer running the single sequenced pass keeps coordination while
   > letting the OS own *when* (and sleep/wake). `scheduler daemon` (the loop) is retained
   > for a future intraday-reactive case.

2. **Layering: new `pkg/scheduler` at L6 (beside `server`).** It must invoke
   `autopilot.Run` (L5) and the EOD update, so it sits above autopilot. `cmd/scheduler.go`
   (composition root) wires it. This respects strictly-downward imports; a lower-layer
   scheduler can't call autopilot.

3. **Extract EOD logic out of `cmd/`.** `RunDBUpdateDirect` moves from `cmd/db.go`
   into a `pkg/` package (candidate: a new `pkg/eod` at L4, or fold into an existing
   domain) so the scheduler can call it without importing `cmd`. `cmd/db.go` becomes a
   thin wrapper (same pattern as the rest of `cmd/`).

4. **One trading-day authority — extend `marketcal` with a holiday calendar.** Collapse
   the three notions into `marketcal` (the L-1 pure floor). Add an exchange-holiday set
   per `Clock` (NSE, NYSE), sourced from a dedicated committed **`config/holidays.json`**
   (per-exchange arrays, hand-maintained yearly), so `IsTradingDay`/settlement math become
   holiday-aware. Autopilot's live benchmark probe and the daemon's every-calendar-day
   firing both switch to this. This is the prerequisite that unblocks correct scheduling
   around holidays.

5. **Cadence schema in config, one block.** A single `scheduler:` block in
   `config/defaults.json` (or the pipeline YAML) with per-cadence entries:
   `eod` (enabled, at = market-close + offset), `drift` (enabled, at, `drift_trigger_pct`
   — finally consumed), `rebalance` (enabled, `frequency`, `day`, `auto_execute`). Reuse
   the existing `config.ScheduleConfig`/`AlertConfig`; precedence flag > env > config >
   default as elsewhere.

6. **Preserve investor-in-the-loop.** The scheduler may run pick/optimize/propose and
   send the proposal alert automatically, but **must not place orders** unless
   `rebalance.auto_execute = true` *and* `--live`. Default off. When off, the quarterly
   tick produces a proposal + notification exactly like today; confirm/execute stays
   the dashboard action. `auto_execute` (currently dead config) gets its first real
   consumer here, gated hard.

7. **Drift-triggered rebalance (optional, later).** Once the orchestrator spans L6, a
   drift breach can *propose* a rebalance (not just alert) by invoking autopilot — the
   layering that made this impossible for the L4 daemon is resolved by the L6 placement.
   Still proposal-only unless `auto_execute`.

#### Resolved decisions (operator input)

The open questions have been decided:

1. **OS integration** — a **thin keep-alive OS unit + in-process tick loop**. The OS unit
   (launchd first; systemd supported for a future Linux server) only keeps the single
   `scheduler` process alive; all cadence logic stays in Go. Survives reboot, no OS-level
   cadence config.
2. **Holiday calendar source** — a **dedicated committed `config/holidays.json`** (per-exchange
   arrays), hand-maintained with a yearly update. It's data on a yearly cadence, so it gets
   its own file rather than an attribute inside `defaults.json`.
3. **Coordination semantics** — **yes**: the orchestrator sequences **EOD → drift** (the drift
   check waits for the same day's EOD update so it reads a fresh cache), and a **rebalance day
   implies an EOD update first**.
4. **Catch-up on missed ticks** — **yes**: if the machine was asleep at market close, the missed
   EOD update runs on wake (in-process detection of a stale last-run).
5. **`scripts/daily_sync.sh`** — **retire entirely**. The native `pkg/eod` + scheduler fully
   replace it; no shell fallback is kept.

#### Deliverables (once design is agreed)

- `pkg/scheduler/` (L6) — tick loop + cadence dispatch + coordination/ordering.
- `pkg/eod/` (or equivalent) — EOD-update logic lifted out of `cmd/db.go`.
- `marketcal` holiday calendar (NSE + NYSE) + committed holiday data; unify the three
  trading-day notions onto it.
- `mycase scheduler {run-now,daemon,status,install,uninstall}` CLI + one OS timer,
  replacing the separate `daemon install` / `autopilot install` units.
- `scheduler:` config block; first real consumer of `auto_execute` / `drift_trigger_pct`.

**Dependency**: the `marketcal` holiday calendar (#4) is the enabling prerequisite;
EOD extraction (#3) unblocks scheduler dispatch of cadence (a). Independent of Phase 10/11.

**Progress**: the holiday calendar (#4) is **shipped and unified**. `marketcal.Clock` gained
an injectable holiday set (`WithHolidays`) and a first-class `IsTradingDay`, with the
weekend rollback loops now holiday-aware; holiday data lives in the `holidays` table of
`mycase.db` (NYSE + NSE), read via the `DBHolidayProvider` (see the DB-only follow-up
below — the original `config/holidays.json` + `config.LoadHolidays` seam was retired).
`broker.TradingClock()` (L1, the one place that legally composes the holiday source +
`marketcal`) assembles the active market's
holiday-aware clock, and **both** former trading-day notions now consult it: the drift
daemon skips weekends/holidays instead of firing every calendar day, and autopilot's
`IsTradingDay` uses the calendar as its authority (keeping the live benchmark probe only
as a secondary cross-check for an unlisted holiday). The EOD update is **extracted**:
`pkg/eod` (L5) holds the daily screening + self-heal + theme-sync logic, lifted out of
`cmd/db.go` (now a thin wrapper), clock- and fetcher-injected; progress is slog. The
**scheduler is shipped**: `pkg/scheduler` (L6) runs the ordered EOD → drift → rebalance
pass with the coordination and catch-up described below, gated on the holiday-aware clock.
The installed model is a **one-shot** — `mycase scheduler install` writes a launchd
`StartCalendarInterval` LaunchAgent (systemd `oneshot` + `OnCalendar` timer on Linux) that
fires `mycase scheduler run-now` once per trading day at the active market's close+offset (in
local time); the process runs the sequenced pass and exits. This replaced the original
keep-alive tick-loop (letting the OS own *when*, and sleep/wake handling, while Go keeps the
ordering); `scheduler daemon` remains available for a future intraday-reactive case.
`mycase scheduler {run-now,daemon,status,install,uninstall}` installs a single unit that replaces
the separate daemon + autopilot units. The market path (US vs India) is a single-file switch
in `config/defaults.json` (committed `defaults.us.json` / `defaults.india.json` presets +
`make use-us` / `make use-india`); `config/defaults.json` gained a `scheduler` block; the
previously-dead `auto_execute` is now consumed (gated) and `scripts/daily_sync.sh` is retired.

**Follow-up — `HolidayProvider`, DB-only (shipped).** Holiday dates are attached to the
`marketcal.Clock` at the `broker.TradingClock()` composition point via a small provider
abstraction:

```go
type HolidayProvider interface {
    Holidays(exchange string) []string // ["2006-01-02", ...]
}
```

There is exactly **one** implementation — `DBHolidayProvider` (in `pkg/broker`, L1, the
consumer that owns the clock assembly) — which reads `SELECT date FROM holidays WHERE
exchange = ?` from the `holidays` table in `mycase.db`, owning that table via `cache.Conn()`
(the domains-own-their-tables pattern like `attribution.Store`/`tax.Store`). The **DuckDB
`holidays` table is the single source of truth.** `selectHolidayProvider` always returns the
DB provider; when the cache DB is not open, or the table is empty, it yields no holidays and
the clock degrades to weekend-only — a clock is always assembled, so order-placing paths are
never blocked by a missing calendar. `marketcal` stays a zero-import leaf (holidays arrive as
a value via `WithHolidays`).

The earlier file source was removed outright rather than kept as dead optionality: there is
no `config/holidays.json`, no `config.LoadHolidays`, no `FileHolidayProvider`, no
`holiday_source` switch, and no `--holiday-source` flag. The rationale: the JSON was *our*
intermediate format, not authoritative — the authoritative calendar is whatever each
exchange publishes (CSV, a circular, an HTML table), in a shape that varies by source and
year. Keeping a file provider + `file|db` switch would enshrine that intermediate format and
leave two coexisting sources to reconcile. Collapsing to DB-only makes provenance
operator-owned and unambiguous.

**Seeding is operational, not a product feature.** The `holidays` table is populated and
refreshed by an operator from each exchange's official calendar — landed via direct SQL (the
`duckdb` CLI) or the operator's own tooling — documented in the runbook (Ch. 18). The product
ships **no** importer: an `UpsertHolidays` write primitive exists on `DBHolidayProvider` for
operator tooling/tests, but nothing hard-codes a `<some-format> → DB` path. The initial rows
(NYSE + NSE, 2026–2027) were a one-time load from the retired `holidays.json` via the CLI.
Consequence to note: a fresh checkout/machine has an empty `holidays` table until seeded, so
first-run trading-day logic is weekend-only until the operator loads the calendar — the
runbook makes this the explicit first-run step.

**Holiday-awareness on the pick path (fixed).** The strategy pipeline's settlement
decisions are now holiday-aware end to end, resolved the *sound* way — the holiday-aware
`marketcal.Clock` flows in as an explicit value, never a global. `stockpicker.Options`
gained a `Clock` field (with a `clock()` accessor defaulting a zero value to the bare
`marketcal.NSE`, preserving prior behavior); `stockpicker`'s as-of / based-on decisions now
read that one clock instead of calling the package-level bare `marketdata.*` helpers, so its
settlement authority is a single injected value (it still cannot import `broker` — the clock
arrives as data). The composition-root callers inject the active market's aware clock:
`cmd/pick` and `cmd/pit` build it from `broker.TradingClock()` (the weekend-only IST
normalization in `pick` is replaced by the clock's holiday-aware `IsTradingDay`/
`SettlementDate`), `cmd`'s `runPickWithOpts` defaults `Options.Clock` from
`broker.TradingClock()` for the pipeline/pit callers, and `pkg/eod` injects `cfg.clock()`.
This collapsed the previous three ways stockpicker could pick an as-of date into one.

What deliberately stays **bare**: the package-level `marketdata` NSE helpers still exist
(`EODSettlementDate`, `NextEODAvailableDate`, `IsFreshEOD`, `IsNSEHoliday` — bound to
`marketcal.NSE`) for the low layers that cannot reach the holiday source. Their only
remaining live caller is `pkg/yfinance/prices.go` (legacy file `.cache` day-bucketing +
freshness), which is at L1 and cannot import `broker`; cache **freshness** is
holiday-insensitive (an entry is fresh until the next settlement cutoff regardless of an
intervening holiday), so the bare clock is correct there. Making that legacy cache
market-aware (NYSE vs NSE day bucketing via `marketcal.ClockForTicker`) is a separate,
optional market-correctness cleanup — not a holiday concern. The duplicate, unwired
`config/nse_holidays.json` was removed in favor of the single `config/holidays.json`.

**Follow-up — scheduler run reporting (shipped).** The scheduler now appends a
human-readable maintenance-log block per pass to `data/logs/scheduler-runs.log` (distinct
from the machine-readable JSONL slog file — the two-channel rule holds). Each `scheduler
run-now` / tick renders one dated block modeled on the operator maintenance-log convention:

```
──── Wed 2026-09-23 20:15 ────
  [eod]     3 method(s) screened; 2/500 integrity flags; 4 theme(s) synced in 5m12s
  [drift]   drift index 0.0830; portfolio 512340.00 across 20 holdings in 1.4s
  ⚠ drift: alert dispatch failed — telegram 429
  ✓ SUCCESS in 5m14s
```

Mechanically: the `Runner` interface widened from returning bare `error` to `(StageResult,
error)` so each cadence contributes detail lines + non-fatal warnings; `runCadence` times
every stage and records the outcome into a `RunReport` accumulator (`pkg/scheduler/
report.go`); `RunOnce`/`Run` open one report per pass and flush a single block at the end
with a `✓ SUCCESS in <dur>` or `✗ FAILED — <cadence>: <err> (<dur>)` summary. To feed real
counts, `eod.Run` now returns an `*eod.Result` (methods screened, integrity failed/total,
themes synced, warnings) instead of only `error`, and the `defaultRunner` also surfaces the
`daemon.DriftResult` (index, portfolio value, holdings) and the autopilot `RunResult.Proposal`
(entries/exits/reweights/orders + tax warnings + report path) that were previously discarded
at the scheduler boundary. Reporting is an explicit opt-in (`scheduler.enable_report`, path
override `scheduler.report_path`), gated so an empty catch-up tick writes nothing, and a
report-write failure is logged-and-swallowed — it never fails the run.

---




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
| 10. Data Source Resilience | Q4 2026 | Schwab API | Authoritative US data (SEC EDGAR), Schwab everywhere, provenance | 🟩 shipped; 10d provenance shipped, interface split open |
| 11. Data & observability hygiene | Q4 2026 | none | Raw-response capture/triage, market-aware settlement/formatting, mapping-bug fixes | 🟩 shipped; R-store-6/7 + flatten open |
| 12. Autonomous Scheduler | Q1 2027 | `marketcal` holiday calendar | One Go-native orchestrator for all three cadences (EOD / drift / rebalance); investor-in-the-loop preserved | 🟩 shipped |
| 6. Options Overlay | H2 2027 | 6mo live data | Income optimization | ⬜ |
| Docs restructure — Pass 2 | — | none | Rename doc files to chapter titles + migrate `docs/NN-*.md` references | ⬜ (see Appendix C) |

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
| Complex derivatives strategies | Covered calls/puts (Phase 6) are the ceiling; no multi-leg spreads, no straddles |

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
| Data source integrity | Phase 10 (authoritative SEC EDGAR fundamentals, Schwab prices, provenance) | Operational quality (correct inputs, no silent Yahoo scrape drift) |
| Tax-loss harvesting | Phase 4 | 0.5–1.5% / year (tax savings) |
| Performance awareness | Phase 5 (know when to simplify) | Prevents compounding losses from a broken strategy |
| Options income | Phase 6 | 1–3% / year on mature portfolio |

**Net expected alpha (Phases 1–5, conservative)**: 1.5–3% annually over SPY, at comparable max drawdown.

**Net expected alpha (all phases, optimistic)**: 3–5% annually. This is the upper bound; don't plan around it.

---

## Appendix B: Future Explorations (Parked)

Ideas worth revisiting once the core system is stable and the ecosystem matures.

### Native macOS App (SwiftUI + DuckDB)

**Revisit when**: macOS 27, DuckDB 2.0, and the core system is stable with 6+ months of live tracking.

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

Data-source resilience (Phase 10) is higher priority — it improves the correctness of inputs the strategy depends on; the UI doesn't. The CSV workflow is ugly but works for quarterly rebalance (4×/year). Defer until:
- The system is stable enough that UX is the bottleneck, not the strategy
- The golden copy can move to DuckDB (the pipeline migration is done — see `docs/10-duckdb-migration.md`)
- Swift Charts and DuckDB Swift bindings are mature enough for production use


---

## Appendix C: Docs Restructure — Pass 2 (planned)

Pass 1 (done) made the guide read as a book: `docs/README.md` is the module-grouped index
and the source of truth for chapter titles, chapters are written in the present tense, and
the process-artifact docs were de-ledgered (the refactor ledger removed; the DuckDB
"migration" doc re-voiced as the Storage chapter). Filenames were deliberately left stable
so no `docs/NN-*.md` reference in Go source or steering had to move.

**Pass 2** does the deferred, higher-churn half: rename the files to match their chapter
titles and migrate every embedded reference in one deliberate sweep. Rename candidates
(terse-but-accurate names that only read correctly via their module today):

| Current file | Candidate title / name | Module |
|---|---|---|
| `06-render.md` | Rendering | B |
| `10-duckdb-migration.md` | Storage & Pipeline Persistence | C |
| `14-value.md` | Value Strategy | D |
| `16-scuttlebutt.md` | Scuttlebutt Research | D |
| `17-screener.md` | Screener / nselib Integration | D |
| `22-themes.md` | Themes | F |
| `23-staticip.md` | Static IP Setup | F |

The blocker that makes this a dedicated pass: several docs are hard-referenced by
`docs/…md` path from Go source comments and `.kiro/steering/*`, so renames must be paired
with a reference migration (grep every `docs/NN-*.md`, update in lockstep) and verified
with a build + `make check-deps`. Do it wholesale, guided by the module tree, not piecemeal.
