# System-Wide Data Audit & Pipeline Mitigation Guide

## 1. Executive Summary

During an operational audit of the `mycase` pipeline (triggered by anomalous readings in stocks like `NSE:DIACABS` having a stale `+13.4%` delivery delta and being gated out by `< 12%` ROCE), we conducted a full-system audit of all data sources, transformations, default assumptions, and filter gates across Go and Python components.

The investigation revealed that **silent default assumptions** (`0.0`, `null`, `NaN`, and missing keys) regularly alter system behavior without raising errors or warnings. In several critical cases, gates designed to enforce quality (such as Cash Flow Quality and Promoter Pledging) were effectively dormant or non-functional for up to 96–100% of the universe.

This document serves as the permanent reference for:
1. All identified data integrity vulnerabilities across the pipeline.
2. Root-cause analyses and quantifiable real-world impacts.
3. Concrete mitigation strategies applicable to all present and future trading strategies.

---

## 2. Comprehensive Vulnerability Catalog

```
                                    DATA PIPELINE VULNERABILITY MAP
                                    
  [NSE Python Scraper] -------------> [Go Cache / Structs] -------------> [Strategy Gates & Scoring]
          |                                    |                                    |
  1A. BE Series NaN -> null            2A. Timeseries OCF/FCF omitted       2B. ROE=0 passes financial gate
  1B. No recency validation            2C. D/E=0 for banks & -NW            2D. Cash flow gate 96% skipped
  1C. BL Block deals overwrite EQ      2E. Governance JSON dummy 0.0s       2F. Interest coverage silent pass
                                       3A. Recent IPO RS inflation          3C. 200 SMA bypass for IPOs
```

---

### Category 1: NSE Delivery & Market Data Pipeline

#### 1A. Trade-to-Trade (`BE/BZ/ST`) Series → `null` Delivery
- **Affected Components:** [`scripts/fetch_nse_data.py:276`](file:///Users/raghavgarg/Projects/myGo/mycase/scripts/fetch_nse_data.py#L276), [`pkg/yfinance/metrics_delivery.go:47`](file:///Users/raghavgarg/Projects/myGo/mycase/pkg/yfinance/metrics_delivery.go#L47)
- **Root Cause:** In the NSE Bhavcopy / security-wise delivery API, Trade-to-Trade (T2T) segment stocks have `DeliverableQty = NaN` and `%DlyQttoTradedQty = '-'` because intraday trading is prohibited by SEBI—**100% of all traded shares must be delivered**. Python's `sanitize_val()` converted `"-"` and `NaN` into `null`, which Go unmarshalled into `0.0`.
- **System Impact:**
  - `CalculateDeliveryDelta` ignored all `0.0` delivery rows.
  - When a stock entered the `BE` series, the system skipped all current trading days and reached back weeks or months into the past to find 5 historical `EQ` days to calculate the "recent 5-day delivery average."
  - **Live Impact:** 61 stocks in our cache had null delivery records. `NSE:DIACABS` showed a `+13.4%` delivery delta on Sept 12 using data from July 28 – Aug 3 (40 days stale).
- **Mitigation:**
  - In `fetch_nse_data.py`, if series is `BE`, `BZ`, or `ST`, set `DeliverableQty = TradedQty` and `DeliveryPct = 100.0%`.
  - Add explicit `Series` field in `DeliveryRecord` struct in [`pkg/marketdata/marketdata.go`](file:///Users/raghavgarg/Projects/myGo/mycase/pkg/marketdata/marketdata.go).

#### 1B. Delivery Staleness — Absence of Recency Invariant
- **Affected Component:** [`pkg/yfinance/metrics_delivery.go:55`](file:///Users/raghavgarg/Projects/myGo/mycase/pkg/yfinance/metrics_delivery.go#L55)
- **Root Cause:** `CalculateDeliveryDelta` verified that at least 25 valid records existed (`len(byDate) >= 25`), but never checked the timestamp of `byDate[0]`.
- **System Impact:** If an instrument was suspended, moved to T2T, or stopped updating, the system happily computed delivery deltas against ancient history, generating phantom institutional accumulation signals.
- **Mitigation:**
  - Enforce strict recency invariant: `byDate[0].Date` must be within 7 calendar days (accounting for long weekends/holidays) of the analysis reference date. If older, return `0.0, false` and log a staleness warning.

#### 1C. Block Deal (`BL`) Series Date Overwrite
- **Affected Component:** [`scripts/fetch_nse_data.py:287-289`](file:///Users/raghavgarg/Projects/myGo/mycase/scripts/fetch_nse_data.py#L287-L289)
- **Root Cause:** When NSE returns multiple series records for the same symbol on the same date (e.g. `LOTUSDEV` on Sep 11 has both an `EQ`/`BE` row and a `BL` block deal window row), the dictionary assignment `merged_by_date[r["date"]] = r` blindly assigns the last encountered row.
- **System Impact:** Block deal volume, price, and deliverable stats overwrite the regular continuous trading session data.
- **Mitigation:**
  - Prioritize `EQ` and `BE` series over `BL`. Only store `BL` as an auxiliary field or merge traded quantities additively.

---

### Category 2: Fundamentals & The "0.0 Default" Trap

#### 2A. Cash Flow Quality Gate Non-Functional (96.6% Silent Bypass)
- **Affected Components:** [`pkg/stockpicker/filters.go:244`](file:///Users/raghavgarg/Projects/myGo/mycase/pkg/stockpicker/filters.go#L244), [`pkg/yfinance/yfinance.go:220,394`](file:///Users/raghavgarg/Projects/myGo/mycase/pkg/yfinance/yfinance.go#L220)
- **Root Cause:**
  1. In `yfinance.go:220`, the `fundamentals-timeseries` query string omitted `annualOperatingCashFlow` and `annualFreeCashFlow`.
  2. In `yfinance.go:394-395`, the struct fields were populated exclusively from `res.FinancialData.OperatingCashflow.Raw` (the summary card). Yahoo Finance only populates this summary card for 22 Indian mega-caps.
  3. In `filters.go:244`, the gate logic was wrapped in:
     ```go
     if f.OperatingCashflow != 0 || f.FreeCashflow != 0 {
         // Cash flow quality check
     }
     ```
- **System Impact:**
  - **625 out of 647 stocks (96.6%)** in the database have `OperatingCashflow = 0.0 AND FreeCashflow = 0.0`.
  - The cash flow quality gate was completely skipped for 96% of candidate stocks, allowing low-quality earnings and cash-burning businesses to pass.
- **Verification & Mitigation:**
  - Live query testing of Yahoo's `fundamentals-timeseries` endpoint confirmed **100% coverage (15/15 test stocks)** when querying `annualOperatingCashFlow` and `annualFreeCashFlow`.
  - Include `annualOperatingCashFlow,annualFreeCashFlow` in `typesStr` in `yfinance.go`.
  - Map the latest timeseries entries into `OperatingCashflow` and `FreeCashflow` when `fd.OperatingCashflow.Raw == 0`.
  - If cash flow is truly missing for a non-financial stock, treat it as unverified rather than a silent pass.

#### 2B. Financial Sector ROE Gate Bypass (`ROE == 0.0` Passes)
- **Affected Component:** [`pkg/stockpicker/filters.go:312`](file:///Users/raghavgarg/Projects/myGo/mycase/pkg/stockpicker/filters.go#L312), [`pkg/stockpicker/filters.go:474`](file:///Users/raghavgarg/Projects/myGo/mycase/pkg/stockpicker/filters.go#L474)
- **Root Cause:**
  ```go
  if f.ROE > 0 && f.ROE < minROE {
      return false, "Low Financial ROE..."
  }
  ```
- **System Impact:**
  - The check `f.ROE > 0` was intended to avoid negative division, but it caused any financial stock with missing ROE (`0.0`) to **bypass the filter entirely**. Only stocks with positive-but-sub-par ROE (e.g. 7%) failed.
  - **79 Financial Services stocks** in the cache have `ROE == 0.0`, including `BAJFINANCE`, `KOTAKBANK`, `LICI`. All passed unchecked.
- **Mitigation:**
  - Enforce explicit ROE threshold for financials: if `f.ROE < minROE`, reject unless explicitly exempted with logged warning. Calculate ROE as `NetIncome / Equity` from balance sheet timeseries if Yahoo's summary ROE is missing.

#### 2C. Debt-to-Equity = 0.0 Distortion (Banks & Negative Net Worth)
- **Affected Component:** [`pkg/stockpicker/filters.go:354`](file:///Users/raghavgarg/Projects/myGo/mycase/pkg/stockpicker/filters.go#L354)
- **Root Cause:** Yahoo returns `0.0` for Debt-to-Equity when:
  1. The company is a bank/NBFC (deposits/borrowings distort standard D/E).
  2. The company has negative net worth (where D/E is mathematically negative/undefined).
- **System Impact:**
  - **35 stocks** have `DebtToEquity == 0` while carrying over ₹100 Cr in debt (e.g. `SBIN` with ₹8.2L Cr debt, `IDEA` with ₹1.9L Cr debt).
  - The filter treated `0.0` as "debt-free", granting distressed turnarounds or highly leveraged banks a clean bill of financial health.
- **Mitigation:**
  - Exempt financial sector stocks from D/E via `IsFinancialSector(f.Sector)` and evaluate them purely on ROE / Capital Adequacy.
  - For non-financials, if `TotalDebt > 0 && DebtToEquity == 0`, compute net worth (`TotalAssets - TotalLiabilities`). If net worth $\le 0$, fail the solvency gate immediately.

#### 2D. Interest Coverage Silent Pass
- **Affected Component:** [`pkg/stockpicker/filters.go:362-380`](file:///Users/raghavgarg/Projects/myGo/mycase/pkg/stockpicker/filters.go#L362-L380)
- **Root Cause:** `passedCoverage` starts as `true`. If `AnnualOperatingIncome` or `AnnualInterestExpense` slices are empty, the evaluation loop never runs and `passedCoverage` remains `true`.
- **System Impact:** Companies with missing debt service disclosures silently pass the solvency check.
- **Mitigation:**
  - If `TotalDebt > 100 Cr` and interest expense history is absent, flag coverage as unverified or fail solvency.

#### 2E. Promoter Pledging Gate Completely Inactive
- **Affected Components:** [`pkg/stockpicker/filters.go:65-68`](file:///Users/raghavgarg/Projects/myGo/mycase/pkg/stockpicker/filters.go#L65-L68), [`config/governance.json`](file:///Users/raghavgarg/Projects/myGo/mycase/config/governance.json)
- **Root Cause:** `config/governance.json` contained only 27 symbols, and all values were `0.0`. Stocks absent from the file defaulted to `0.0%` pledged.
- **System Impact:** The promoter pledging filter has never caught or eliminated a single stock in the history of the system.
- **Mitigation:**
  - Implement automated fetching of Promoter Pledged % via `scripts/fetch_nse_data.py` (using `nselib` shareholding pattern) or direct scraping of Screener.in's Shareholding section, populating `config/governance.json` on daily sync.

#### 2F. ROCE Calculation & Turnaround Discrepancies (e.g. `DIACABS`)
- **Affected Component:** [`pkg/stockpicker/filters.go:117-136`](file:///Users/raghavgarg/Projects/myGo/mycase/pkg/stockpicker/filters.go#L117-L136)
- **Root Cause:**
  - `getLatestROCE` uses the asset approach: $\text{Capital Employed} = \text{Total Assets} - \text{Current Liabilities}$.
  - For `DIACABS`, Yahoo reported FY26 Total Assets of ₹2,403 Cr and Current Liabilities of ₹514 Cr, giving Capital Employed = ₹1,890 Cr and ROCE = **10.28%** (3-year avg = 5.16%).
  - In contrast, Screener.in reported ROCE = **24.2%** because DIACABS completed an NCLT/CIRP restructuring where post-resolution clean standalone assets are ₹803 Cr (Capital Employed ~₹600 Cr against EBIT of ₹194 Cr). Yahoo's consolidated timeseries still carried un-restructured gross book items.
- **Mitigation:**
  - Cross-check Screener.in's top-level ROCE via [`pkg/yfinance/screener.go`](file:///Users/raghavgarg/Projects/myGo/mycase/pkg/yfinance/screener.go) as an enrichment/verification layer for high-momentum turnaround candidates.

---

### Category 3: Technical Metric Edge Cases

#### 3A. Composite RS Inflation for Recent IPOs / Listings
- **Affected Component:** [`pkg/yfinance/metrics.go:667`](file:///Users/raghavgarg/Projects/myGo/mycase/pkg/yfinance/metrics.go#L667)
- **Root Cause:** If a stock has fewer than 252 bars, `days >= n` clamps to `n - 1`, treating the full available window (e.g. 60 days) as the 12-month return (weighted 30% in composite RS).
- **Mitigation:** Annualize or proportionally discount momentum scores when history is $< 252$ trading days.

#### 3B. Benchmark Return Defaults to 0.0% on Fetch Failure
- **Affected Component:** [`pkg/yfinance/metrics.go:682-687`](file:///Users/raghavgarg/Projects/myGo/mycase/pkg/yfinance/metrics.go#L682-L687)
- **Root Cause:** If Nifty 50 fails to fetch, benchmark returns default to 0.0%, turning Relative Strength into absolute price return without alerting the user.
- **Mitigation:** Throw an error or explicitly abort run if benchmark data has $< 200$ valid bars.

#### 3C. 200 SMA Downtrend Guard Bypass for Stocks with < 200 Bars
- **Affected Component:** [`pkg/stockpicker/filters.go:667,683`](file:///Users/raghavgarg/Projects/myGo/mycase/pkg/stockpicker/filters.go#L667)
- **Root Cause:** `if len(prices) < 200 { return true, "" }`.
- **Mitigation:** For stocks with $< 200$ bars, evaluate trend against the 50-day SMA or 20-day EMA instead of giving an automatic pass.

---

## 3. Systematic Mitigation Framework Across All Strategies

To ensure that existing strategies (`stockpicker`, `velocity`, `incubator`, `scoring_us`) and future strategies do not suffer from these failure modes, the pipeline adopts three fundamental design rules:

```
                            THREE PILLARS OF DATA HYGIENE
                            
      ┌─────────────────────────┬─────────────────────────┬─────────────────────────┐
      │   1. EXPLICIT NULLS     │  2. RECENCY INVARIANTS  │   3. PROVENANCE TAGS    │
      ├─────────────────────────┼─────────────────────────┼─────────────────────────┤
      │ • No float64 0.0 default│ • Every metric must have│ • Track source (Yahoo,  │
      │   masking missing data  │   an asOf timestamp     │   NSE, Screener)        │
      │ • Use explicit HasValue │ • Max staleness bounds  │ • Track Series (EQ,     │
      │   or nullable types     │   strictly enforced     │   BE, BZ, BL)           │
      └─────────────────────────┴─────────────────────────┴─────────────────────────┘
```

1. **Explicit Data Presence Over Silent 0.0s:** Never interpret uninitialized `float64` as a legitimate financial reading. If a metric cannot be fetched, it must be flagged as missing.
2. **Mandatory Recency Invariants:** Every timeseries calculation (delivery, price, volume, financials) must enforce a hard threshold on the latest available data point.
3. **Market Segment Awareness:** The data ingestion layer must preserve exchange series metadata (`EQ` vs `BE` vs `BL`) so downstream filters understand the regulatory trading mechanics (e.g. 100% delivery for T2T).

---

## 4. Implementation Phasing

- **Phase 1 (Immediate - High Impact):**
  - Fix Cash Flow ingestion in `pkg/yfinance/yfinance.go` (add timeseries types & mapping).
  - Fix NSE Delivery T2T/BE 100% rule in `scripts/fetch_nse_data.py`.
  - Add 7-day recency invariant in `pkg/yfinance/metrics_delivery.go`.
- **Phase 2 (Filter Integrity - Medium Impact):**
  - Fix Financial ROE gate bypass in `pkg/stockpicker/filters.go`.
  - Fix Debt-to-Equity logic for banks and negative net worth companies.
  - Implement 50 SMA trend fallback for stocks with $< 200$ bars.
- **Phase 3 (Governance & Enrichment):**
  - Automate Promoter Pledging data collection into `config/governance.json`.
  - Enrich Screener.in cross-checks for turnaround / restructuring balance sheets.
