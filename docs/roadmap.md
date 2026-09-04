# Mycase — Roadmap

**Goal**: An automated US equity system that delivers slight but consistent outperformance over the S&P 500 while eliminating emotional decision-making and manual busy work.

**Updated**: September 2026

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
| Authoritative US fundamentals (SEC EDGAR) | Schwab fundamentals are thin TTM only — no cash-flow statement, no annual series; US scoring degrades to proxies (see `docs/datasources.md`) |
| US sector classification | Schwab returns no sector → US stocks collapse to "Unknown" → sector caps silently disabled |
| Data-source provenance in cache | Cannot audit which source produced a number, or invalidate one source selectively |

---

### Known technical debt

| Debt | Location | Impact | Fix effort |
|------|----------|--------|-----------|
| `yfinance.GetCache()` still exists (deprecated) | `pkg/yfinance/duckdbcache.go` | Confusing API — external code should use `cache.GetDB()` | Zero callers remain; delete in Phase 10a |
| Seven command paths bypass `datafetcher.Router` | `cmd/report.go`, `cmd/monitor.go`, `cmd/optimize.go`, `pkg/server/handlers.go`, `pkg/executor/executor.go`, `pkg/backtest/valuation.go`, `pkg/autopilot/schedule.go` | US holdings get Yahoo data even when Schwab is configured — "use Schwab for US" is only true in `pick` today | Refactor R17 (Phase 10b) |
| Schwab fundamentals mapper drops derivable fields | `pkg/broker/schwab/market.go` `mapSchwabFundamentals` | `Sector`/`RegularPrice`/`NetIncome` left empty though derivable | Phase 10a |

---

## 3. Architecture Vision

The system is a 6-layer responsibility stack (market data → strategy → portfolio construction → execution & tax → autopilot → audit & attribution), US-only via Schwab. For the system design — conceptual layers, the concrete `cmd/pkg/` package breakdown, data flow, ticker routing (`US:`→Schwab, else→Yahoo), and design decisions — see **`docs/architecture.md`** §2 (Inputs), §4 (System Design), and §11 (Design Decisions). This roadmap covers only *what* is being built and *when*.

---

## 4. Phased Roadmap

Completed and dropped phases have been removed from this roadmap; their design detail lives in `docs/architecture.md` (design decisions), `docs/refactor.md` (Completed Phases ledger), and `docs/duckdb-migration.md`. Only active and planned work remains below.

### Carried-over follow-ups (non-blocking)

Small items left open by shipped phases, not yet scheduled:
- Plumb `rsi` / `momentum_1y` from the scoring pass to the `selectiontracker.RecordDriverMetrics` site so the `selections` columns persist non-zero (they exist, currently zero).
- Extend `mycase pipeline diff` to compare selection-level driver metrics between runs (today it diffs proposals only).

---

### Phase 10: Data Source Resilience

**What**: Source each data type from the most authoritative provider that can supply it, with deterministic logged fallback, and record provenance. Today the clean `pick`/autopilot pipeline routes US data through Schwab, but seven other command paths bypass the router and hit Yahoo directly, Schwab's fundamentals are a thin TTM snapshot (no sector, no cash-flow statement, no annual series), and the benchmark is always Yahoo `^GSPC`. Full design, API shapes, provenance chain, and gap analysis live in **`docs/datasources.md`**.

**Why**: Yahoo is a free aggregator reselling a vendor's parse of SEC filings — it is neither authoritative nor stable (unofficial endpoints, legally a scrape). The real origins are: **exchanges** for prices (Schwab is broker-direct, closer than Yahoo), **SEC EDGAR XBRL** for fundamentals (the filing itself), and **GICS/constituents-CSV** for sector. Sourcing authoritatively removes a fragile dependency, fixes silently-broken US sector caps, and upgrades the earnings-quality and ROIC factors from proxies to real inputs. This directly serves the "no black boxes / transparency" design constraint.

**Sub-phases** (each independently shippable, ordered by value-per-effort):

- **Phase 10a — Cheap correctness wins** (~1–2 days): populate `Fundamentals.Sector` from the constituents CSV (fixes broken US sector caps); enrich `mapSchwabFundamentals` to derive `NetIncome` and wire `RegularPrice` from quotes; delete the dead `yfinance.GetCache()` (zero callers). No new source.
- **Phase 10b — Router-bypass cleanup** (refactor **R17**, ~3–5 days): thread a `datafetcher.Router`/provider set into the seven bypass paths so every US command routes through Schwab; switch the benchmark to `US:SPY` via Schwab with `^GSPC`/Yahoo fallback; add a `source` column + `slog` which-source-served logging.
- **Phase 10c — SEC EDGAR fundamentals source** (~5–8 days): new `pkg/edgar` client (ticker→CIK map cached, `companyfacts` fetch, XBRL concept mapper with ordered candidate tags, mandatory `User-Agent` + 10 req/s limiter); populate operating cash flow, net income, and all annual series from EDGAR; a `FundamentalsMerger` composes Schwab ratios + EDGAR statements + CSV sector; per-source cache freshness (EDGAR facts stable until next quarterly filing).
- **Phase 10d — Provider abstraction hardening** (optional, ~2–3 days): split `DataFetcher` into capability interfaces (`PriceSource`, `FundamentalsSource`, `SectorSource`); formalize the ordered fallback chain; surface provenance in `pipeline show`/reports ("FCF: $2.1B [source: EDGAR 10-K 2025-Q4]").

**Deliverables**:
- `pkg/edgar/` — SEC EDGAR client + XBRL concept mapper (Phase 10c)
- `datafetcher.FundamentalsMerger` — composite Schwab + EDGAR + CSV fundamentals (Phase 10c)
- Sector-carrying constituents CSVs + loader wiring (Phase 10a)
- Router wired into `report`/`monitor`/`optimize`/`serve`/`executor`/`backtest`/`autopilot-schedule` (Phase 10b / R17)
- `source` provenance column in the price + fundamentals cache with per-source freshness (Phase 10b/10c)
- `US:SPY`-via-Schwab benchmark with Yahoo fallback (Phase 10b)

**Effort**: ~2–3 weeks total across the four sub-phases. The hard part is Phase 10c's XBRL parsing — filers use custom taxonomy extensions and tags drift over time, so the concept mapper must try an ordered list of candidate tags per concept. The open question (see `docs/datasources.md` §10) is whether to parse EDGAR ourselves or pay a commercial fundamentals vendor to skip it.

**Dependency**: Phase 10a and 10b are independent and can ship immediately. Phase 10c depends on 10b (the merger plugs into the routed path). Phase 10d depends on 10c.

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

Active and planned phases only (completed/dropped phases removed):

| Phase | Target | Dependency | Core value delivered | Status |
|-------|--------|------------|---------------------|--------|
| 10. Data Source Resilience | Q4 2026 | Phase 2 (Schwab) | Authoritative US data (SEC EDGAR), Schwab everywhere, provenance | ⬜ |
| 6. Options Overlay | H2 2027 | 6mo live data | Income optimization | ⬜ |

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
- The golden copy can move to DuckDB (the pipeline migration is done — see `docs/duckdb-migration.md`)
- Swift Charts and DuckDB Swift bindings are mature enough for production use
