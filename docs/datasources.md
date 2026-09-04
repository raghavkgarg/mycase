# Mycase — Data Sources Reference

**Status**: Reference annex (API shapes, provenance, gap analysis). The *implementation plan* lives in `docs/roadmap.md` **Phase 10** (feature framing) and `docs/refactor.md` **R17** (Router-bypass cleanup); the design principle is `docs/architecture.md` **D15**.
**Updated**: September 2026
**Scope**: US equity data (India/Yahoo paths are legacy — see `docs/roadmap.md` Phase 4 "Dropped")

---

## Table of Contents

1. [Why This Doc Exists](#1-why-this-doc-exists)
2. [The Data Model We Populate](#2-the-data-model-we-populate)
3. [Provenance: Where Financial Data Actually Originates](#3-provenance-where-financial-data-actually-originates)
4. [Source Catalog: Real API Shapes](#4-source-catalog-real-api-shapes)
5. [Capability Matrix & Gap Analysis](#5-capability-matrix--gap-analysis)
6. [Current Wiring & Its Problems](#6-current-wiring--its-problems)
7. [Architecture Direction & Rollout](#7-architecture-direction--rollout)
8. [Open Questions](#8-open-questions)

---

## 1. Why This Doc Exists

Two questions drove this document:

1. **How much do we still depend on Yahoo Finance, and for data we could get from better sources?**
2. **Where does this data actually come from — exchanges, SEC, Treasury? Is Yahoo just the only *free* source?**

The short answers:

- The clean `pick`/autopilot pipeline routes US prices/quotes/fundamentals through Schwab, but **seven other command paths bypass the router and hit Yahoo directly** (`report`, `monitor`, `optimize`, `serve`, `executor`, `backtest`, `autopilot-schedule`). See [§6](#6-current-wiring--its-problems).
- Even the routed path degrades: Schwab's fundamentals are a **thin TTM snapshot** with no sector, no cash-flow statement, and no annual series. See [§5](#5-capability-matrix--gap-analysis).
- Yahoo is **not the origin** of any of this. Prices originate at the **exchanges**; fundamentals originate at the **SEC** (XBRL filings); sector is a **classification standard** (GICS, licensed; SIC, free). Yahoo is a *free, pre-cleaned aggregator* that resells a data vendor's normalized snapshot. So is Schwab's fundamentals endpoint. See [§3](#3-provenance-where-financial-data-actually-originates).

The goal is an architecture where **each data type is sourced from the most authoritative provider that can supply it**, with deterministic fallback when a provider is down, and where provenance is recorded so we can audit which source fed a given number. This document is the *reference*: the data model, the real API shapes, and the gap analysis. The *plan to get there* is tracked as roadmap **Phase 10** and refactor **R17**.

---

## 2. The Data Model We Populate

All sources normalize into two leaf DTOs in `pkg/marketdata/marketdata.go` (zero-import leaf; see `docs/architecture.md` layering). These are the shapes every provider must satisfy.

### `HistoricalData` (daily OHLCV series)

| Field | Type | Consumed by |
|-------|------|-------------|
| `Timestamps` | `[]int64` | momentum, volatility, backtest, charts |
| `Closes` | `[]float64` | momentum (`computeMomentumSkip1Mo`), vol (`computeAnnualizedVol`) |
| `Opens` | `[]float64` | backtest, intraday cleanup |
| `Volumes` | `[]float64` | liquidity checks |

### `Fundamentals` (the field set that matters for scoring)

The struct has ~40 fields. The ones the **US quality-momentum path** (`pkg/stockpicker/scoring_us.go`) actually reads:

| Field | Used for | Notes |
|-------|----------|-------|
| `MarketCap` | hard filter, FCF yield, shareholder yield | |
| `FreeCashflow` | FCF-yield factor, earnings-quality proxy, FCF filter | |
| `AverageVolume` | ADV liquidity filter | |
| `RegularPrice` | ADV filter (`vol × price`) | |
| `DividendYield` | shareholder-yield factor | |
| `ROE` | ROIC fallback, driver string | |
| `ReturnOnAssets` | ROIC fallback | |
| `OperatingMargins` | driver string | |
| `NetIncome` | earnings quality (CFO/NI) | |
| `OperatingCashflow` | earnings quality (CFO/NI) | |
| `Sector` | **sector caps + diversification** | |
| `AnnualOperatingIncome` | preferred ROIC (NOPAT/invested capital) | annual series |
| `AnnualTotalAssets` | preferred ROIC | annual series |
| `AnnualCurrentLiabilities` | preferred ROIC | annual series |

The remaining annual series (`AnnualRevenue`, `AnnualGrossProfit`, `AnnualNetPPE`, `AnnualAccountsReceivable`, `AnnualCapEx`, `AnnualInterestExpense`) feed the **India** metrics (`CalculateSalesGrowth`, `CalculateDSO`, `CalculateCROIC` in `pkg/yfinance/metrics.go`) — not the US path today, but they define the shape a full fundamentals provider must fill.

---

## 3. Provenance: Where Financial Data Actually Originates

Yahoo and Schwab are **aggregators**, not origins. The true chain:

| Data | True origin | How it flows to us today | Authoritative & free? |
|------|-------------|--------------------------|----------------------|
| Prices / quotes / volume | **Exchanges** (NYSE, NASDAQ, Cboe) → SIP consolidated feed | Yahoo (vendor redistribution license); Schwab (direct broker connectivity) | Schwab is *closer to source* |
| Income stmt / balance sheet / cash flow | **SEC** — 10-K/10-Q filings in **XBRL** (us-gaap taxonomy) | Yahoo & Schwab both resell a vendor's normalized parse | SEC EDGAR is free + authoritative |
| Sector / industry | **Classification standard** — GICS (S&P/MSCI, licensed), ICB (FTSE), or SIC/NAICS (SEC, free) | Yahoo licenses GICS-style; Schwab omits it | SIC free (coarse); GICS licensed |
| Earnings / result dates | **Company** 8-K filings (SEC) / IR calendars | Yahoo + Screener.in (India) | SEC 8-K free (needs parsing) |
| Risk-free rate (unused today) | **US Treasury** / FRED | not used | free |

**Conclusion:** Yahoo isn't special data — it's the *free, pre-parsed* aggregator. The authoritative free primary source for US fundamentals is **SEC EDGAR XBRL**; the cost is that we'd parse XBRL ourselves instead of consuming a vendor's clean snapshot. For prices, **Schwab is the better source** (broker-direct). For sector, the practical free source is the **constituents CSV** (which already carries GICS sector for S&P 500) or SEC's coarse SIC.

Treasury data is a non-issue: none of the current factors use a risk-free rate.

---

## 4. Source Catalog: Real API Shapes

### 4.1 Schwab Market Data API (current US primary)

`pkg/broker/schwab/` — OAuth2, 120 req/min ceiling (client-side sliding window), access token 30 min / refresh 7 days. See `docs/api-rules.md`.

| Capability | Method | Endpoint | Shape |
|-----------|--------|----------|-------|
| Historical OHLCV (range) | `FetchHistoricalDataWithTimestamps` | `/pricehistory` | `marketdata.HistoricalData` |
| Historical OHLCV (dates) | `FetchHistoricalByDateRange` | `/pricehistory` | `marketdata.HistoricalData` |
| Quotes (batch) | `FetchQuotes` | `/quotes?symbols=A,B,C` | `map[symbol]price` |
| Fundamentals (per ticker) | `FetchFundamentals` | `/instruments?symbol=X&projection=fundamental` | `marketdata.Fundamentals` (thin) |
| Holdings | `GetHoldings` | `/accounts/{hash}?fields=positions` | broker positions |
| Transactions | `FetchTransactions` | `/accounts/{hash}/transactions?types=TRADE` | tax lots |
| Orders | `PlaceOrder` | `POST /accounts/{hash}/orders` | order id |

**Fundamentals gap** — Schwab's `Fundamental` struct (`types.go`) has: `marketCap`, `peRatio`, `pegRatio`, `pbRatio`, `divYield`, `returnOnEquity`, `returnOnAssets`, `operatingMarginTTM`, `netProfitMarginTTM`, `grossMarginTTM`, `totalDebtToEquity`, `epsTTM`, `revenueTTM`, `vol3MonthAvg`, `beta`, `sharesOutstanding`, `freeCashFlowPerShare`. It has **no** operating cash flow, **no** annual series, **no** sector, **no** net income (derivable), **no** regular price (from quotes).

### 4.2 Yahoo Finance (current fallback + India primary)

`pkg/yfinance/` — unofficial endpoints, no key, cookie+crumb for some. ~2000 req/hr informal.

| Capability | Method | Endpoint |
|-----------|--------|----------|
| Quotes | `FetchQuotes` | `v8/finance/chart` (regularMarketPrice) |
| Historical OHLCV | `FetchHistoricalDataWithTimestamps` / `...ByDateRange` | `v8/finance/chart` |
| Intraday 1m | `FetchIntradayData` | `v8/finance/chart` |
| Fundamentals (rich) | `FetchFundamentals` | `quoteSummary` + `fundamentals-timeseries` |
| Earnings dates (India) | `FetchScreenerEarningsDates` | Screener.in scrape |

Yahoo's fundamentals are the **richest** we consume (sector, annual series, earnings history) — but they're unofficial, unstable, and legally a scrape. That's the risk we're trying to reduce.

### 4.3 SEC EDGAR (proposed — authoritative US fundamentals)

`data.sec.gov` — **no auth, no API key.** Two hard rules:

- **Mandatory `User-Agent`** header declaring identity + contact (e.g. `mycase/1.0 you@example.com`). Missing/generic UA → IP blocked.
- **Fair-access rate limit: ≤ 10 requests/second** per user across all machines. Excess → IP block.

| Capability | Endpoint | Notes |
|-----------|----------|-------|
| Ticker → CIK map | `https://www.sec.gov/files/company_tickers.json` | flat `{cik_str, ticker, title}`; cache it — EDGAR is CIK-keyed |
| Company metadata + SIC | `https://data.sec.gov/submissions/CIK##########.json` | 10-digit zero-padded CIK; carries `sic`, `sicDescription`, exchanges, tickers, recent filings |
| All facts (one call) | `https://data.sec.gov/api/xbrl/companyfacts/CIK##########.json` | every us-gaap concept for a company; best single-call fundamentals source |
| One concept | `https://data.sec.gov/api/xbrl/companyconcept/CIK##########/us-gaap/{Tag}.json` | facts per unit; each fact has `start/end/fy/fp/form/filed/val` |
| Cross-sectional | `https://data.sec.gov/api/xbrl/frames/us-gaap/{Tag}/USD/CY####Q#[I].json` | one fact per entity for a calendar period |
| Bulk | `companyfacts.zip`, `submissions.zip` | recompiled nightly ~3am ET; for mass backfill |

**Relevant us-gaap concept tags** (fills the fields Schwab can't):

| `marketdata.Fundamentals` field | us-gaap tag(s) |
|--------------------------------|----------------|
| `OperatingCashflow` | `NetCashProvidedByUsedInOperatingActivities` (or `...ContinuingOperations`) |
| `NetIncome` | `NetIncomeLoss` |
| `AnnualOperatingIncome` | `OperatingIncomeLoss` |
| `AnnualTotalAssets` | `Assets` |
| `AnnualCurrentLiabilities` | `LiabilitiesCurrent` |
| `AnnualRevenue` | `Revenues` / `RevenueFromContractWithCustomerExcludingAssessedTax` |
| `AnnualGrossProfit` | `GrossProfit` |
| `AnnualNetPPE` | `PropertyPlantAndEquipmentNet` |
| `AnnualAccountsReceivable` | `AccountsReceivableNetCurrent` |
| `AnnualCapEx` | `PaymentsToAcquirePropertyPlantAndEquipment` |
| `AnnualInterestExpense` | `InterestExpense` |

**Parsing risk:** filers use custom taxonomy extensions and tags drift over time; a robust mapper tries an ordered list of candidate tags per concept and picks the most recent 10-K/10-Q fact. This is the real engineering cost of EDGAR vs. a pre-cleaned vendor feed.

### 4.4 Constituents CSV (proposed sector source)

Already used for the universe (S&P 500 GitHub/datahub dataset). The S&P 500 constituent files carry **GICS sector** per ticker. Carrying that column through to `Fundamentals.Sector` fixes sector caps with **zero live fetch** and no Yahoo dependency.

---

## 5. Capability Matrix & Gap Analysis

Per data type, which source can supply it — and the honest quality.

| Data type | Schwab | Yahoo | SEC EDGAR | Constituents CSV | Best source |
|-----------|:------:|:-----:|:---------:|:----------------:|-------------|
| Prices / OHLCV | ✅ direct | ✅ | ❌ | ❌ | **Schwab** |
| Quotes | ✅ batch | ✅ | ❌ | ❌ | **Schwab** |
| Market cap, P/E, P/B, ROE, ROA, margins, beta, debt/equity | ✅ TTM | ✅ | ⚠️ derivable | ❌ | Schwab (fine) |
| Free cash flow | ⚠️ FCF/share × shares | ✅ | ✅ authoritative | ❌ | EDGAR > Yahoo > Schwab |
| **Operating cash flow** | ❌ | ✅ | ✅ authoritative | ❌ | **EDGAR** (Schwab has none) |
| **Net income** | ⚠️ derivable | ✅ | ✅ authoritative | ❌ | EDGAR / derive |
| **Annual series** (op income, assets, liabilities, revenue, PPE, AR, capex) | ❌ | ✅ | ✅ authoritative | ❌ | **EDGAR** (Schwab has none) |
| **Sector** | ❌ | ✅ GICS | ⚠️ SIC (coarse) | ✅ GICS | **CSV** (free GICS) |
| Regular price (for ADV) | ⚠️ via quotes | ✅ | ❌ | ❌ | Schwab quotes (wire it) |
| Earnings / result dates | ❌ | ✅ | ⚠️ 8-K parse | ❌ | Yahoo / EDGAR 8-K |

### The three real gaps

1. **Sector is empty for US** (`mapSchwabFundamentals` sets `Sector: ""`). Every routed US stock collapses to `"Unknown"`, silently disabling sector caps. **Cheapest fix: sector from constituents CSV.**
2. **Operating cash flow & annual series are absent from Schwab.** Earnings-quality degrades to an FCF proxy; ROIC always uses the ROA/ROE fallback instead of the preferred NOPAT/invested-capital calc. **Authoritative fix: SEC EDGAR.**
3. **Two fields Schwab could supply but the mapper drops:** `NetIncome` (derive from `revenueTTM × netProfitMarginTTM` or `epsTTM × sharesOutstanding`) and `RegularPrice` (from the quotes endpoint). **Fix: enrich the mapper — no new source needed.**

---

## 6. Current Wiring & Its Problems

### How routing works

`pkg/datafetcher/Router` (`router.go`) dispatches by ticker prefix: `US:`/`NYSE:`/`NASDAQ:` → Schwab (when `schwabClient != nil`), everything else → Yahoo. It structurally satisfies `stockpicker.DataFetcher` (interface defined by the consumer, per layering rules).

Fallback behavior differs by method:
- `FetchQuotes`, `FetchFundamentals`: US → Schwab, **fall back to Yahoo on Schwab error** or when no client.
- Historical methods: US → Schwab, **no error fallback** (Schwab error propagates); Yahoo only when no client.
- `GetBenchmarkSymbol`: always Yahoo string logic → benchmark prices always come from Yahoo (`^GSPC`).

### Problem 1 — Seven paths bypass the Router entirely

These construct no Router and call `yfinance.*` directly, so US holdings get Yahoo data even when Schwab is configured:

| Path | Direct Yahoo calls |
|------|-------------------|
| `cmd/report.go` | prices, historical, fundamentals |
| `cmd/monitor.go` | benchmark + per-holding prices + fundamentals |
| `cmd/optimize.go` | prices, benchmark, fundamentals |
| `pkg/server/handlers.go` (dashboard) | benchmark + per-holding history + fundamentals |
| `pkg/executor/executor.go` | quote fallback |
| `pkg/backtest/valuation.go` | historical + intraday |
| `pkg/autopilot/schedule.go` | benchmark trading-day probe |

### Problem 2 — Thin routed fundamentals

Even on the Schwab-routed happy path, US fundamentals lack sector, cash flow, and annual series ([§5](#5-capability-matrix--gap-analysis)).

### Problem 3 — Benchmark is always Yahoo

`^GSPC` comes from Yahoo everywhere, even inside the Router. Roadmap Phase 5 already flags the *correct* benchmark as `US:SPY` routed through Schwab (the honest "you could have bought this" baseline).

### Problem 4 — No provenance in the cache

The DuckDB cache (`pkg/cache/`) stores prices and a fundamentals JSON blob per ticker with freshness metadata, but **no column records which source produced a value.** We can't audit whether a number came from Schwab, Yahoo, or EDGAR — or selectively invalidate one source.

---

## 7. Architecture Direction & Rollout

> The detailed phase breakdown, effort, and sequencing live in **`docs/roadmap.md` Phase 10** and **`docs/refactor.md` R17**; the design principle is **`docs/architecture.md` D15**. This section keeps only the durable design shapes those phases implement.

### Principle: source per data type, not per market

Routing today is **market-keyed** (US vs India). The direction is **data-type-keyed with an ordered, logged provider chain**, because the best source differs by *what* is fetched, not just *where* it trades:

```
Prices/Quotes:   Schwab  → Yahoo (fallback)
Fundamentals:    EDGAR (statements) + Schwab (ratios/TTM) merged → Yahoo (fallback)
Sector:          Constituents CSV → EDGAR SIC → Yahoo
Earnings dates:  Yahoo/Screener (US: EDGAR 8-K later)
Benchmark:       Schwab US:SPY → Yahoo ^GSPC (fallback)
```

### Provider interfaces (defined by the consumer)

Keep the layering rule (D14): consumers define narrow interfaces; each source satisfies them structurally. Split the fat `DataFetcher` into capability interfaces so a source implements only what it can serve:

```go
// pkg/datafetcher (interfaces live with the router/consumer)
type PriceSource interface {
    FetchHistoricalDataWithTimestamps(ctx, ticker, rangeStr) (*marketdata.HistoricalData, error)
    FetchQuotes(ctx, tickers) (map[string]float64, error)
}
type FundamentalsSource interface {
    FetchFundamentals(ctx, tickers) (map[string]marketdata.Fundamentals, error)
}
type SectorSource interface {
    Sector(ctx, ticker) (string, bool)
}
```

- `schwab.Client` implements `PriceSource` + a partial `FundamentalsSource`.
- `yfinance` implements all (fallback).
- new `edgar.Client` implements `FundamentalsSource` (statement fields).
- `csvloader`/constituents implements `SectorSource`.

### Composite fundamentals (merge, don't just fall back)

The key insight from [§5](#5-capability-matrix--gap-analysis): no single source is complete for US fundamentals. A `FundamentalsMerger` (in `pkg/datafetcher`, composing sources with no upward imports) applies a per-field source-of-record precedence:

1. Schwab TTM ratios (ROE, margins, P/E, beta, market cap).
2. Overlay EDGAR statement facts (operating cash flow, net income, annual series).
3. Overlay sector from the constituents CSV.
4. Derive `NetIncome`/`RegularPrice` if still missing.
5. Fall back to Yahoo for any ticker where the merge is too sparse to score.

### Cache: provenance + per-source freshness

Add a `source` column to the price/fundamentals cache and a per-source freshness policy:

| Data | Freshness | Rationale |
|------|-----------|-----------|
| Prices (range ending today) | same IST/ET day | unchanged |
| Prices (historical) | never expire | unchanged |
| Schwab TTM ratios | 24h | unchanged |
| EDGAR statement facts | until next filing (quarterly) | filings don't change between quarters |
| Sector (CSV) | until constituents refresh | rarely changes |
| Ticker→CIK map | weekly | new listings are rare |

EDGAR facts being quarterly-stable means each company's `companyfacts.json` is fetched roughly **once per quarter** — trivially inside the 10 req/s budget.

### How this maps to tracked phases

| Direction element | Tracked as |
|-------------------|-----------|
| Sector from CSV; derive `NetIncome`/`RegularPrice`; delete dead `GetCache()` | roadmap **Phase 10a** |
| Wire the 7 bypass paths through the Router; `US:SPY` benchmark; `source` column + slog | roadmap **Phase 10b** / refactor **R17** |
| `pkg/edgar` client + XBRL mapper + `FundamentalsMerger` | roadmap **Phase 10c** |
| Capability interfaces + provenance surfaced in reports | roadmap **Phase 10d** |

---

## 8. Open Questions

1. **EDGAR effort vs. commercial vendor.** A clean commercial fundamentals API (FMP, Polygon, Tiingo, Intrinio) trades money for skipping XBRL parsing. Is the parsing cost (Phase 10c) worth avoiding a subscription? EDGAR is free + authoritative but is real engineering; a vendor is fast but paid and reintroduces "trust someone else's parse."
2. **Sector for non-S&P-500 universes.** The CSV fix works because S&P 500 constituents ship GICS sector. If the US universe ever broadens beyond indices with sector-carrying CSVs, we fall back to SEC SIC (coarse) or need a GICS mapping.
3. **India fundamentals.** This doc is US-focused. India (Screener.in + Yahoo `.NS`) has no EDGAR equivalent; its authoritative origin is BSE/NSE filings + MCA. Out of scope while India is legacy.
4. **Earnings dates for US.** Deferred to Yahoo for now. Worth an EDGAR 8-K parser later to remove the last unofficial-scrape dependency for US.
5. **Backtest historical fundamentals.** EDGAR's `frames` API gives point-in-time historical facts (no survivorship/restatement bias) — a potential future upgrade for backtesting accuracy that Yahoo can't cleanly provide.

