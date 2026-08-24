# Mycase — Roadmap

**Goal**: An automated equity system that delivers slight but consistent outperformance over broad-market indices (S&P 500, Nifty 50) while eliminating emotional decision-making and manual busy work.

**Updated**: August 2026

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
3. **Apply systematic factor tilts** (quality + value + momentum) that have academic support for long-term outperformance
4. **Diversify across uncorrelated markets** (India + US) to reduce portfolio-level drawdowns without sacrificing returns

### The realistic edge

| Source of alpha | Expected contribution | Academic support |
|----------------|----------------------|-----------------|
| Behavioral discipline (no panic sell/FOMO buy) | 1–2% / year | Dalbar study: avg investor underperforms by 3–4% due to timing |
| Rebalancing premium (sell winners, buy losers mechanically) | 0.3–0.8% / year | Bernstein, Perold & Sharpe |
| Tax-loss harvesting (systematic loss realization) | 0.5–1.5% / year | Wealthfront, Betterment published data |
| Factor tilts (quality + value + small-cap) | 0.5–2% / year | Fama-French, AQR, Cliff Asness |
| Geographic diversification (India + US) | Risk reduction, not alpha per se | Correlation < 0.6 historically |

**Realistic combined target**: 1–3% annualized outperformance over a passive 60/40 India/US equity split, with lower max drawdown. Over 20 years at ₹50L invested, even 1.5% alpha compounds to ₹20L+ additional wealth.

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

### What's specced but not built

| Component | Spec location | Blocking? |
|-----------|---------------|-----------|
| Tax-optimized rebalancing (FIFO engine) | `docs/feature.md` Feature 1 | No — India-only enhancement |
| Options overlay | `docs/feature.md` Feature 2 | No — post-maturity optimization |
| Screener.in deep integration (QoQ, shareholding, CWIP) | `docs/screener.md` | No — enrichment |

### What's missing entirely

| Gap | Impact |
|-----|--------|
| US market strategy (factor tilt on S&P 500 / Russell) | Cannot diversify geographically |
| Strategic asset allocation layer (India/US split) | No cross-market portfolio construction |
| Live performance attribution (benchmark tracking) | Cannot measure if we're actually outperforming |

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
│  Layer 4: Execution & Tax Awareness                      │
│  Zerodha (India), Schwab (US) ✅, FIFO lots, TLH engine │
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

### Phase 3: US Factor Strategy

**What**: Apply systematic factor scoring to US equities (S&P 500 or Russell 1000 universe).

**Why**: A pure index fund already "beats most people." A factor-tilted selection (overweight quality + momentum, underweight expensive/low-quality) has historically added 1-2% over decades. This is our marginal alpha on the US side.

**Strategy design** — simpler than Multibagger because US large-cap data is cleaner:

| Factor | Weight | Direction | Rationale |
|--------|--------|-----------|-----------|
| ROIC (Return on Invested Capital) | 20 pts | Higher = better | Capital efficiency — the Buffett metric |
| Free Cash Flow Yield (FCF/EV) | 20 pts | Higher = better | Actual cash generation vs enterprise value |
| 12-month Momentum (skip last month) | 15 pts | Higher = better | Jegadeesh-Titman momentum, skip recent month to avoid reversal |
| Earnings Quality (CFO/Net Income) | 15 pts | Higher = better | Accruals anomaly — cash-backed earnings persist |
| Shareholder Yield (div + buyback) | 15 pts | Higher = better | Total capital return to shareholders |
| Low Volatility | 15 pts | Lower vol = better | Low-vol anomaly: less risk, not less return |

**Hard filters** (US):
- Market cap > $10B (liquid large-cap only)
- Average daily volume > $50M
- Positive trailing FCF
- No ADRs (avoid dual-listing complexity)

**Universe**: S&P 500 constituents fetched from a maintained source (Wikipedia table, or Schwab instrument search filtered by index membership).

**Top N**: 20-25 stocks (enough diversification, few enough to have meaningful factor tilt)

**Rebalance**: Quarterly, aligned with India rebalance

**Deliverables**:
- `mycase pick --index sp500 --method us_quality_momentum --top 20`
- New scoring function in `pkg/stockpicker/scoring.go` (or new file `scoring_us.go`)
- S&P 500 constituent fetcher (Schwab instruments endpoint or CSV)
- Backtest against SPY to validate factor tilt adds value historically

**Effort**: ~2 weeks. The scoring engine is generic — this is mostly mapping Schwab fundamentals to factors and tuning thresholds.

---

### Phase 4: Strategic Asset Allocation Layer

**What**: A top-level portfolio construction layer that manages the India/US split, enforces target allocations, and handles cross-market rebalancing.

**Why**: Without this, India and US portfolios are two separate things. The investor has to manually decide "how much in India vs US." This layer makes that systematic and handles the drift between the two.

**Design**:

```yaml
# config/pipeline.yaml addition
allocation:
  india:
    target: 0.60                # 60% target allocation
    strategies:
      - index: microsmall       # existing
        method: multibagger
        top_n: 20
        weight_in_india: 0.70   # 70% of India allocation → micro/small
      - index: nifty50
        method: value
        top_n: 10
        weight_in_india: 0.30   # 30% of India allocation → large-cap value
    broker: zerodha

  us:
    target: 0.40                # 40% target allocation
    strategies:
      - index: sp500
        method: us_quality_momentum
        top_n: 20
        weight_in_us: 1.0
    broker: schwab

  rebalance_band: 0.05          # rebalance when actual drifts > 5% from target
  tax_aware: true               # consider tax impact before cross-market rebalance
```

**Rebalancing logic**:
1. Compute current India% vs US% from live holdings (both brokers)
2. If drift > `rebalance_band`, propose cross-market rebalance
3. Tax-aware: prefer deploying new capital to the underweight market over selling the overweight market (avoids taxable events)
4. Within each market: run the existing per-market rebalance logic

**Deliverables**:
- `pkg/portfolio/allocator.go` — cross-market allocation engine
- `mycase pipeline` updated to run both legs and produce a unified report
- Dashboard shows combined portfolio (India + US) with geographic allocation donut
- Drift daemon monitors cross-market drift in addition to per-stock drift

**Effort**: ~2 weeks.

---

### Phase 5: Tax-Loss Harvesting Engine

**What**: Implement the FIFO capital gains engine (specced in `docs/feature.md`) and add systematic tax-loss harvesting to the rebalance workflow.

**Why**: Tax-loss harvesting is the closest thing to a free lunch in investing. By systematically selling positions at a loss (and immediately replacing them with a correlated substitute), you realize tax deductions without changing portfolio exposure. This is worth 0.5–1.5% per year in tax savings.

**India-specific rules**:
- STCG (< 12 months): 20% tax → harvesting a ₹10,000 loss saves ₹2,000 in tax
- LTCG (≥ 12 months): 12.5% tax above ₹1.25L exemption
- Wash sale: no explicit wash sale rule in India (unlike US 30-day rule), but income tax officers may challenge artificial losses. Conservative approach: replace with sector peer, not same stock

**US-specific rules**:
- Short-term (< 1 year): taxed as ordinary income (up to 37%)
- Long-term (≥ 1 year): 15% or 20%
- Wash sale rule: cannot buy "substantially identical" security within 30 days before or after the sale
- TLH substitute must be a different stock in the same sector/factor exposure

**Deliverables**:
- `pkg/tax/fifo.go` — FIFO lot matching engine
- `pkg/tax/tlh.go` — tax-loss harvesting logic (identify harvest candidates, select substitutes)
- `mycase tax import --file data/tradebook.csv` — load Zerodha tradebook
- `mycase basket --tax-optimize` — orders sequenced to maximize TLH + use LTCG exemption
- Dashboard tax tab: YTD realized gains/losses, available harvest candidates, exemption utilization

**Effort**: ~3 weeks. The FIFO engine is the complex part; TLH logic is straightforward once lots are tracked.

---

### Phase 6: Live Performance Attribution

**What**: Continuously track the actual portfolio's performance against benchmarks and decompose returns into their sources.

**Why**: Without this, you don't know if the system is working. "I think I'm beating the market" is not the same as "I'm beating the market by 1.7% annualized with a 0.82 information ratio." You need hard numbers to decide whether to continue, adjust, or simplify to pure index funds.

**Deliverables**:
- `pkg/attribution/tracker.go` — daily NAV computation from live holdings
- Benchmarks tracked: Nifty 50, S&P 500, a 60/40 Nifty/SPY blend (the "do nothing" baseline)
- Monthly attribution report: how much came from stock selection vs asset allocation vs rebalancing vs tax savings
- `mycase performance --vs-benchmark` — show cumulative alpha chart
- Dashboard performance tab: equity curve overlaid with benchmarks, rolling 1Y alpha
- Alert: if trailing 12-month alpha is significantly negative, send a "review your strategy" nudge

**Attribution decomposition** (Brinson model):
- **Allocation effect**: did India/US split help vs 60/40 benchmark?
- **Selection effect**: did stock picks within each market beat the market's index?
- **Interaction effect**: combination of both
- **Tax effect**: how much did TLH save vs a no-TLH baseline?

**Effort**: ~2 weeks. The hard part is getting consistent NAV history; the math is standard.

---

### Phase 7: Options Overlay (Post-Maturity)

**What**: Once the portfolio is stable and well-tracked (6+ months live), add an options overlay for income generation and tail-risk hedging.

**Why**: For a portfolio of 15-20 quality stocks, covered calls on over-weight positions generate 2-5% additional annual income. Protective puts on concentrated positions hedge black-swan events. This is an optimization on a working system — not a priority for a system that's still being built.

**Prerequisite**: Schwab options chain API (included in Market Data Production bundle) + 6 months of live portfolio tracking to understand position sizing and volatility profiles.

**Deliverables** (future):
- `pkg/options/overlay.go` — strike/expiry selection engine
- Covered call candidates: positions > 100 shares, IV rank > 30, overweight vs target
- Protective put candidates: concentrated positions (> 8% weight), earnings approaching
- `mycase options suggest` — weekly option overlay recommendations
- Integration with Schwab order API for option execution

**Effort**: ~4 weeks. Deferred to Phase 7 because it requires a stable, tracked portfolio and options expertise.

---

### Timeline Summary

| Phase | Target | Dependency | Core value delivered | Status |
|-------|--------|------------|---------------------|--------|
| 1. Quarterly Autopilot | ~~Sep 2026~~ | None | Removes all manual busy work | ✅ Done |
| 2. Schwab Integration | ~~Oct 2026~~ | Schwab app approval | US market access | ✅ Done |
| 3. US Factor Strategy | Nov 2026 | Phase 2 ✅ | US stock picking with factor edge | ⬜ Next |
| 4. Asset Allocation | Nov 2026 | Phase 3 | Unified India+US portfolio | ⬜ |
| 5. Tax-Loss Harvesting | Jan 2027 | Phase 1 ✅ (India), Phase 2 ✅ (US) | 0.5-1.5% tax alpha | ⬜ |
| 6. Performance Attribution | Feb 2027 | Phase 4 | Know if system works | ⬜ |
| 7. Options Overlay | H2 2027 | Phase 6 + 6mo live data | Income optimization | ⬜ |

---

## 5. Success Metrics

### Primary metric: Net-of-cost, net-of-tax CAGR vs benchmark

**Benchmark**: A 60% Nifty 50 / 40% S&P 500 blend, rebalanced annually. This is what you'd get from two index funds with zero effort.

**Target**: Outperform the blend by 1–3% annualized over a rolling 3-year window.

**Measurement**: Begin tracking from the first fully-automated quarterly rebalance. Report monthly. Meaningful statistical significance requires 3+ years of data.

### Secondary metrics

| Metric | Target | Why it matters |
|--------|--------|---------------|
| Max Drawdown | < benchmark drawdown | If we take more risk for the same return, the system is broken |
| Sharpe Ratio | > benchmark Sharpe | Risk-adjusted return, not just absolute return |
| Turnover | < 30% annually | Low turnover = low costs + low tax drag |
| Time spent by investor | < 1 hour/quarter | The entire point is removing busy work |
| Rebalance discipline | 100% execution rate | Never skip a scheduled rebalance (emotional override = system failure) |
| Tax savings | Track ₹ saved via TLH annually | Concrete, measurable benefit |

### Failure conditions (when to simplify to pure index funds)

- 3 consecutive years of negative alpha after costs → the factor tilts aren't working in the current regime
- Max drawdown exceeds benchmark by > 10% → the concentration risk isn't worth it
- System requires > 2 hours/quarter of manual intervention → automation has failed
- Investor overrides the system more than once per year → behavioral discipline has broken down

If any failure condition triggers: simplify to 3-fund portfolio (Nifty 50 index + S&P 500 index + debt fund) and stop trying to outperform. Knowing when to quit is part of the system.

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
| Rebalancing premium | Phase 1 (quarterly rebalance) + Phase 4 (cross-market) | 0.3–0.8% / year |
| Geographic diversification | Phase 2 + 4 (India + US) | Risk reduction (lower drawdowns) |
| Factor tilts (India) | Already built (multibagger + value) | 0.5–1.5% / year |
| Factor tilts (US) | Phase 3 (quality + momentum) | 0.5–1% / year |
| Tax-loss harvesting | Phase 5 | 0.5–1.5% / year (tax savings) |
| Performance awareness | Phase 6 (know when to simplify) | Prevents compounding losses from a broken strategy |
| Options income | Phase 7 | 1–3% / year on mature portfolio |

**Net expected alpha (Phases 1–6, conservative)**: 1.5–3% annually over the 60/40 blend, at lower max drawdown.

**Net expected alpha (all phases, optimistic)**: 3–5% annually. This is the upper bound; don't plan around it.
