# Known System Issues & Bug Tracker For EMB

## Bug-001: EarlyMB Section 5 Cross-Run Score Shifts Display Overflow & Calibration Discontinuity

- **Status:** Resolved (2026-09-14)
- **Component:** [`pkg/pithistory/analytics.go`](file:///Users/raghavgarg/Projects/myGo/mycase/pkg/pithistory/analytics.go) (Section 5: `SIGNIFICANT SCORE SHIFTS & TRAJECTORY`)
- **Strategy:** Early Multibagger (`earlymb`)
- **Reported In:** `mycase --index niftytotalmarket --method earlymb --analysis`
- **Execution Date:** 2026-09-11 (comparing `2026-09-09 -> 2026-09-11`)

---

### 1. Symptoms & Observed Behavior

When executing the PIT deep analysis report (`--analysis`), Section 5 dumped an unbounded list of **65 candidates** (over 67% of the 97 Stage-1 survivors):

```
--- 5. SIGNIFICANT SCORE SHIFTS & TRAJECTORY (|Δ| >= 4.0 pts: 2026-09-09 -> 2026-09-11) ---
  Ticker          | Sector                   | Prev Score | Curr Score | Score Shift  | % Price Chg | VCP ATR  | Comp RS  | Deliv Δ  
  -----------------------------------------------------------------------------------------------------------------------------
  NSE:NETWEB      | Technology               |       21.5 |       30.9 |       +9.5pt |      +2.62% |     0.79 |   +26.2% |     +6.9%
  NSE:PREMIERENE  | Technology               |       17.0 |       24.9 |       +7.9pt |      -0.17% |     0.78 |    -2.7% |     +6.5%
  ... [10 gainers total]
  NSE:LGEINDIA    | Technology               |       34.5 |       29.4 |       -5.1pt |      -0.67% |     0.78 |    +6.0% |     +9.6%
  ...
  NSE:PIDILITIND  | Basic Materials          |       28.1 |       12.6 |      -15.6pt |      -0.32% |     1.34 |    +1.7% |     -5.6%
  NSE:INDHOTEL    | Consumer Cyclical        |       32.7 |       17.1 |      -15.6pt |      -0.59% |     1.47 |    +2.3% |     -1.1%
  NSE:CRISIL      | Financial Services       |       36.2 |       20.3 |      -15.9pt |      +0.16% |     0.77 |    +9.3% |     -1.0%
  NSE:NYKAA       | Consumer Cyclical        |       38.3 |       22.4 |      -16.0pt |      +0.44% |     1.17 |   +26.7% |     -2.1%
  NSE:METROPOLIS  | Healthcare               |       42.3 |       26.2 |      -16.1pt |      -2.76% |     0.80 |   +13.7% |     +4.6%
  NSE:EICHERMOT   | Consumer Cyclical        |       35.9 |       19.6 |      -16.2pt |      -2.64% |     1.06 |    +4.5% |     +1.0%
  NSE:MAHSEAMLES  | Basic Materials          |       33.9 |       17.3 |      -16.6pt |      +7.79% |     1.51 |   +21.1% |     -9.4%
  NSE:UNITDSPR    | Consumer Defensive       |       33.0 |       15.7 |      -17.3pt |      -0.73% |     1.00 |    +5.1% |     -3.0%
  NSE:NH          | Healthcare               |       27.4 |        9.6 |      -17.8pt |      -1.13% |     1.13 |    +6.8% |    -12.7%
  NSE:THELEELA    | Consumer Cyclical        |       39.1 |       20.9 |      -18.2pt |      +2.04% |     1.06 |   +21.2% |     -2.0%
  NSE:MANKIND     | Healthcare               |       30.3 |       10.2 |      -20.2pt |      +0.66% |     1.05 |    -2.4% |     +1.5%
  NSE:TECHM       | Technology               |       29.0 |        8.5 |      -20.4pt |      +2.19% |     0.90 |    +4.2% |    -13.0%
```

Out of 65 candidates, **55 exhibited severe negative drops (-5.1 pt to -20.4 pt)**, even when prices were flat or strongly rising (e.g. `NSE:MAHSEAMLES` gained +7.79% in price yet dropped -16.6pt in score; `NSE:TECHM` gained +2.19% in price yet dropped -20.4pt in score).

---

### 2. Root Cause Analysis

#### A. Presentation / UX Defect (Unbounded Output)
In [`pkg/pithistory/analytics.go:502-532`](file:///Users/raghavgarg/Projects/myGo/mycase/pkg/pithistory/analytics.go#L502-L532), `shiftsQuery` has no `LIMIT` clause:
```sql
WHERE curr.as_of_date = ? 
  AND prev.as_of_date = ?
  AND curr.index_name = ? 
  AND curr.method = ?
  AND curr.passed_stage1 = true
  AND prev.passed_stage1 = true
  AND curr.raw_score > 0.0
  AND prev.raw_score > 0.0
  AND ABS(curr.raw_score - prev.raw_score) >= 4.0
ORDER BY diff DESC;
```
Because the threshold is a flat `|diff| >= 4.0` on a 100-point composite score, any cross-sectional shift or formula calibration floods the terminal with dozens of rows.

#### B. Statistical & Domain Defect (Methodology Discontinuity)
The runs being compared span the **Pillar 4 (Delivery Delta) Canonical Migration** (documented in [`docs/earlyMB.md#19`](file:///Users/raghavgarg/Projects/myGo/mycase/docs/earlyMB.md)):
1. **2026-09-09 Run:** Calculated using legacy arbitrary baseline `(DeliveryPct - 35%)`, granting an artificial **+10 to +20 point subsidy** out of 25 to high-delivery stocks. In the database, these records are marked with `pillar4_uncalibrated = true`.
2. **2026-09-11 Run:** Calculated using canonical self-relative disjoint window ($\overline{\text{Deliv}}_{5\text{D}} - \overline{\text{Deliv}}_{20\text{D Baseline}}$), which mathematically centers around **`0.0%`**.
3. **The Discontinuity:** Joining `prev` (uncalibrated) with `curr` (calibrated) produced a one-time artificial universe-wide drop averaging `-8.1 pts` (Pillar 4 alone accounted for 98% of the shift). The table incorrectly presents this methodology recalibration as genuine technical breakdown across 55 stocks.

---

### 3. Planned Remediation

> **Note on Timing:** Implementation is deferred until after one more full cycle of `pillar4_uncalibrated = true` / calibrated data is processed and updated in the DuckDB database to verify clean consecutive comparisons.

When ready to implement, apply the following updates in [`pkg/pithistory/analytics.go`](file:///Users/raghavgarg/Projects/myGo/mycase/pkg/pithistory/analytics.go):

1. **Partition into Top 10 Gainers & Top 10 Decliners:**
   - Instead of an unbounded dump, show:
     - Universe summary: `Total Shifts (|Δ| >= 4.0 pts): X candidates (Y gainers, Z decliners)`
     - Top 10 Positive Gainers (ordered by `diff DESC LIMIT 10`)
     - Top 10 Negative Decliners (ordered by `diff ASC LIMIT 10`)
2. **Calibration Discontinuity Warning Banner:**
   - Inspect `prev.pillar4_uncalibrated` vs `curr.pillar4_uncalibrated`.
   - If `prev` is uncalibrated while `curr` is calibrated, display an explicit sentry alert:
     ```
     🚨 [CALIBRATION NOTICE] Runs span Pillar 4 methodology upgrade (uncalibrated -> calibrated).
        Negative score shifts reflect removal of legacy delivery subsidy, not technical breakdown.
     ```

---
---

## Bug-002: Stale Delivery Thresholds in Pre-Breakout Signatures & Accumulation Velocity (Sections 7 & 8)

- **Status:** Resolved (2026-09-14)
- **Component:** [`pkg/pithistory/analytics.go:630-642`](file:///Users/raghavgarg/Projects/myGo/mycase/pkg/pithistory/analytics.go#L630-L642) (Section 7) & [`pkg/pithistory/analytics.go:717-719`](file:///Users/raghavgarg/Projects/myGo/mycase/pkg/pithistory/analytics.go#L717-L719) (Section 8)
- **Strategy:** Early Multibagger (`earlymb`)

### 1. Symptoms & Observed Behavior

In Section 7 (`PRE-BREAKOUT INCUBATOR`):
- `NSE:IPCALAB` shows **`Deliv Δ = +10.0%`** (top institutional accumulation standout in the market, `VCP ATR = 0.50`), but gets categorized as **`Extreme Volatility Coil`**.
- `NSE:AGARWALEYE` shows **`Deliv Δ = +9.2%`** (`VCP ATR = 0.63`), but falls all the way down to the generic fallback **`Base Consolidating`**.
- In Section 8 (`MULTI-RUN ACCUMULATION VELOCITY`), no stock ever qualifies for **`Stealth Institutional Absorption`**.

### 2. Root Cause Analysis

In `analytics.go`:
```go
// Section 7 (lines 630-641)
if vcp < 0.65 && deliv > 0.20 {
    sig = "Tight VCP Coil + Heavy Deliv"
} else if vcp < 0.55 {
    sig = "Extreme Volatility Coil"
} else if deliv > 0.30 {
    sig = "Stealth Institutional Accum"
} else if vcp < 0.75 && rs > 0.20 {
    sig = "Tight Base + Relative Outperf"
} else if gap <= 8.0 {
    sig = "Runway Trigger Imminent"
}

// Section 8 (line 717)
} else if deliv > 0.30 {
    pattern = "Stealth Institutional Absorption"
}
```
The conditions `deliv > 0.20` (+20%) and `deliv > 0.30` (+30%) were hardcoded under the **legacy uncalibrated delivery formula** (where deltas frequently reached +20% to +35%). Under the canonical calibrated formula, the mean delivery delta is 0.0% and the empirical maximum across the entire 750-stock universe is `~+11.9%` (`IPCALAB`). Consequently:
- `deliv > 0.20` and `deliv > 0.30` are **dead code** that can never be reached.
- Real institutional accumulation leaders (`deliv >= +8%` to `+10%`) have their institutional footprints masked and fall back to purely technical or generic labels.

### 3. Planned Remediation

Recalibrate the delivery thresholds to match the empirical distribution:
- `deliv >= 0.08` (+8.0%, $\approx +1.5\sigma$ in cross-section) for heavy accumulation signatures.
- `deliv >= 0.05` (+5.0%, $\approx +1.0\sigma$) for moderate accumulation.
- In Section 7:
  ```go
  if vcp < 0.65 && deliv >= 0.06 {
      sig = "Tight VCP Coil + Heavy Deliv"
  } else if deliv >= 0.08 {
      sig = "Stealth Institutional Accum"
  } else if vcp < 0.55 {
      sig = "Extreme Volatility Coil"
  ...
  ```
- In Section 8:
  ```go
  } else if deliv >= 0.08 {
      pattern = "Stealth Institutional Absorption"
  }
  ```

---
---

## Bug-003: Stage-1 Elimination Bottleneck Blindspot: 205 Stocks (33.3%) Dumped in `Other Hard Filter` (Section 1)

- **Status:** Resolved (2026-09-14)
- **Component:** [`pkg/pithistory/analytics.go:340-360`](file:///Users/raghavgarg/Projects/myGo/mycase/pkg/pithistory/analytics.go#L340-L360) (Section 1: `rejectionQuery`)
- **Strategy:** Early Multibagger (`earlymb`) & All Strategies

### 1. Symptoms & Observed Behavior

In Section 1 (`STAGE-1 HARD FILTER ATTRITION & ELIMINATION BOTTLENECKS`):
```
Disqualification Category                | Count    | % of Eliminated | % Total Pool
-----------------------------------------------------------------------------------------
Other Hard Filter                        |      205 |           33.3% |        27.3%
Downtrend (< 200-Day SMA)                |      199 |           32.3% |        26.5%
...
```
`Other Hard Filter` is the single largest disqualification category (**205 stocks, 33.3% of all eliminated stocks**), hiding what actually eliminated one-third of the universe.

### 2. Root Cause Analysis

In `analytics.go`, the SQL `CASE` statement classifying rejection causes only accounts for:
`200-SMA`, `ROCE`, `52W High`, `Base duration`, `Promoter stake`, `DSO`, `Debt/Equity`, `Interest Coverage`, `Market Cap`, `ADV`, and `Earnings`.

It completely lacks branches for several major production hard filters:
1. `Low Cash Conversion CFO/PAT` / `Cash Flow Quality check failed` (as observed in Sections 9 & 10 for `NSE:HFCL`, `NSE:SAPPHIRE`, `NSE:DEVYANI`, `NSE:DIACABS`, `NSE:SWANCORP`, `NSE:TI`, `NSE:GRASIM`).
2. `Financial Services weak relative strength / ROE` (observed in Section 10 for `NSE:AIIL`).
3. `Promoter Pledging (> 20%)`.
4. `Operating Margin Deterioration` / `Negative Operating Margin`.

All stocks failing these gates fall into `ELSE 'Other Hard Filter'`, obscuring the actual gate bottlenecks.

### 3. Planned Remediation

Add explicit `WHEN` branches in `rejectionQuery`:
```sql
WHEN rejection_reason LIKE '%Cash Flow%' OR rejection_reason LIKE '%CFO/PAT%' OR rejection_reason LIKE '%Cash Conversion%' THEN 'Weak Cash Conversion (CFO < PAT)'
WHEN rejection_reason LIKE '%Financial Services%' OR rejection_reason LIKE '%Banking%' THEN 'Financial Services Gated (ROE/RS)'
WHEN rejection_reason LIKE '%Pledg%' THEN 'High Promoter Pledging (> 20%)'
WHEN rejection_reason LIKE '%Margin%' THEN 'Operating Margin Deterioration'
```

---
---

## Bug-004: Daily Top Price Gainers Table Fails to Link Graduated Radar Stocks (Section 10)

- **Status:** Resolved (2026-09-14)
- **Component:** [`pkg/pithistory/analytics.go:1008-1011`](file:///Users/raghavgarg/Projects/myGo/mycase/pkg/pithistory/analytics.go#L1008-L1011) (Section 10)
- **Strategy:** Early Multibagger (`earlymb`)

### 1. Symptoms & Observed Behavior

In Section 10 (`DAILY TOP PRICE GAINERS`), every single stock displays `-` in the `Radar` column:
```
  Ticker          |    Prev (₹) |   Close (₹) | 1D Gain  | Deliv Δ | Accum | Comp RS | Gate Type    | Stage-1 Bottleneck           | Radar    
  -----------------------------------------------------------------------------------------------------------------------------------
  ...
  NSE:BLACKBUCK   |     ₹580.05 |     ₹628.15 |   +8.29% |   +5.8% | NO    |  +11.8% | [CLEARED]    | Stage-1 Qualified            | -        
  NSE:WABAG       |   ₹2,130.90 |   ₹2,277.90 |   +6.90% |  -10.5% | NO    |  +35.8% | [CLEARED]    | Stage-1 Qualified            | -        
```
Yet in Section 9 (`Graduated Stocks Performance`), `NSE:WABAG` (+15.09% return) and `NSE:BLACKBUCK` (+6.80% return) are the top featured graduated radar success stories! The report fails to correlate the top price gainers with the radar.

### 2. Root Cause Analysis

In `analytics.go`:
```go
overlapStr := "-"
if radarTickers[ticker] || (hasScore && deliv >= 0.08 && rs >= 0.0) {
    overlapStr = "DUAL HIT"
}
```
`radarTickers` is populated strictly from the *active* near-miss radar table (`passed_stage1 = false`). Once a stock clears Stage-1 (like `WABAG` and `BLACKBUCK`), it is removed from active radar. Section 10 does not check the `graduates` set, so it prints `-` instead of indicating that the gainer was an incubated radar pick that just graduated.

### 3. Planned Remediation

Store graduated tickers in a set (`graduatedTickers[ticker] = true`) and check:
```go
overlapStr := "-"
if graduatedTickers[ticker] {
    overlapStr = "GRADUATED"
} else if radarTickers[ticker] {
    overlapStr = "ACTIVE RADAR"
} else if hasScore && deliv >= 0.08 && rs >= 0.0 {
    overlapStr = "DUAL HIT"
}
```

---
---

## Bug-005: Float Precision Inversion & Rule Ordering in Multi-Run Accumulation Velocity (Section 8)

- **Status:** Resolved (2026-09-14)
- **Component:** [`pkg/pithistory/analytics.go:713-719`](file:///Users/raghavgarg/Projects/myGo/mycase/pkg/pithistory/analytics.go#L713-L719) (Section 8)
- **Strategy:** Early Multibagger (`earlymb`)

### 1. Symptoms & Observed Behavior

In Section 8 (`MULTI-RUN ACCUMULATION VELOCITY`):
```
Ticker      | Score (T-2) | Score (T-1) | Score (T) | Pattern
-----------------------------------------------------------------------------
NSE:VIYASH  |        32.6 |        32.6 |      39.5 | 3-Session Consecutive Surge
```
Between T-2 and T-1, `VIYASH` was essentially flat (`32.6 -> 32.6`), but between T-1 and T it experienced a massive `+6.9 pt` surge (`32.6 -> 39.5`). Calling this a "3-Session Consecutive Surge" is misleading; its true market footprint is `Velocity Breakout (+5pt Δ)`.

### 2. Root Cause Analysis

In `analytics.go`:
```go
if sT2 > 0 && sT0 > sT1 && sT1 > sT2 {
    pattern = "3-Session Consecutive Surge"
} else if sT0 > sT1+5.0 {
    pattern = "Velocity Breakout (+5pt Δ)"
} else if deliv > 0.30 {
    pattern = "Stealth Institutional Absorption"
}
```
1. `sT1 > sT2` checks raw unrounded floats. A negligible noise difference (e.g. `32.61 > 32.58` = +0.03 pt) satisfies `sT1 > sT2`, triggering consecutive surge even though the scores are practically identical.
2. The consecutive surge branch takes precedence over `sT0 > sT1 + 5.0`, suppressing the more descriptive breakout classification.

### 3. Planned Remediation

1. Require a meaningful minimum increment for consecutive surge: `sT1 >= sT2 + 0.5`.
2. Prioritize explosive velocity breakout or distinguish true 3-session growth:
   ```go
   if sT0 >= sT1+5.0 {
       pattern = "Velocity Breakout (+5pt Δ)"
   } else if sT2 > 0 && sT0 > sT1+0.5 && sT1 > sT2+0.5 {
       pattern = "3-Session Consecutive Surge"
   } else if deliv >= 0.08 {
       pattern = "Stealth Institutional Absorption"
   }
   ```

---
---

## Bug-006: Hardcoded `1D Gain` Column Label for Multi-Day or Non-Consecutive Date Spans (Section 10)

- **Status:** Resolved (2026-09-14)
- **Component:** [`pkg/pithistory/analytics.go:986-987`](file:///Users/raghavgarg/Projects/myGo/mycase/pkg/pithistory/analytics.go#L986-L987) (Section 10)
- **Strategy:** Early Multibagger (`earlymb`) & All Strategies

### 1. Symptoms & Observed Behavior

In Section 10 (`DAILY TOP PRICE GAINERS (2026-09-09 -> 2026-09-11)`):
The column header is hardcoded as **`1D Gain`**, even though the date range spans 2 trading days / 48 hours (from Wednesday 2026-09-09 to Friday 2026-09-11, skipping Thursday 2026-09-10).

### 2. Root Cause Analysis

The column header in `analytics.go` is hardcoded:
```go
fmt.Printf("  %-15s | %11s | %11s | %-8s | ...\n",
    "Ticker", "Prev (₹)", "Close (₹)", "1D Gain", ...)
```
When runs skip dates (due to holidays, missed execution, or multi-day intervals), the percentage change represents the full span between runs, not 1 single day.

### 3. Planned Remediation

Dynamically label the column based on date distance:
- If consecutive trading calendar days: `1D Gain`
- If multi-day gap: `% Price Chg` or `Δ Price`

---
---

## Bug-007: Radar Graduation `Days` Ambiguity & Legacy Uncalibrated Run Leak (Section 9)

- **Status:** Resolved (2026-09-14)
- **Component:** [`pkg/pithistory/analytics.go:867-975`](file:///Users/raghavgarg/Projects/myGo/mycase/pkg/pithistory/analytics.go#L867-L975) (Section 9) & [`pkg/pithistory/db.go`](file:///Users/raghavgarg/Projects/myGo/mycase/pkg/pithistory/db.go)
- **Strategy:** Early Multibagger (`earlymb`)

### 1. Symptoms & Observed Behavior

In Section 9 (`Graduated Stocks Performance (Radar Alpha Audit: First-Seen Date -> Clear Date)`):
```text
Ticker          | Sector          | Channel          | First Seen | Clear Date | Days |   Entry (₹) |   Clear (₹) | Radar Return
--------------------------------------------------------------------------------------------------------------------------------
NSE:JAMNAAUTO   | Cons Cyclical   | base_duration    | 2026-08-28 | 2026-09-11 |   1d |     ₹120.80 |     ₹135.46 |      +12.14%
NSE:MANORAMA    | Cons Defensive  | base_duration    | 2026-08-28 | 2026-09-11 |   1d |   ₹1,889.40 |   ₹2,055.70 |       +8.80%
```
1. **Mathematical Inconsistency in `Days`:** `First Seen` is 2026-08-28 and `Clear Date` is 2026-09-11 (a 14-calendar-day interval), but `Days` printed as `1d`.
2. **False Phantom Graduates:** `JAMNAAUTO` and `MANORAMA` were not genuine radar accumulation candidates; their delivery deltas were below the 8% threshold throughout early September.

### 2. Root Cause Analysis

1. **Metric Discrepancy:** The query calculated `COUNT(DISTINCT as_of_date) AS days_on_radar` (number of sessions where `delivery_delta >= +8.0%`), but printed it under a generic column named `Days` between `First Seen` and `Clear Date`. Because `JAMNAAUTO` only met the radar condition on 1 single session, it printed `1d`.
2. **Uncalibrated Historical Data Leak:** Historical runs before 2026-08-31 were uncalibrated (`pillar4_uncalibrated = true`). On 2026-08-28 under the legacy arbitrary subsidy formula, `JAMNAAUTO` had an artificial `+16.09%` delivery delta (calibrated value was `+4.65%`). `gradQuery` had no `pillar4_uncalibrated = false` guard, causing historical uncalibrated anomalies to trigger false graduation entries.

### 3. Remediation & Fix

1. **Calibrated Historical Data Backfill:** Backfilled calibrated delivery deltas across all pre-Aug-31 runs (`2026-08-26`, `2026-08-27`, `2026-08-28`). 100% of candidate scores and runs in `mycase.db` are now calibrated.
2. **Filter Guard:** Added `AND (pillar4_uncalibrated = false OR pillar4_uncalibrated IS NULL)` to `prior_radar`.
3. **Elapsed Incubation + Radar Persistence:** Computed `CAST((curr.as_of_date - pr.first_seen_date) AS INT) AS elapsed_days` and formatted column as `Incub (Hits)`: e.g. `14d (5x)`, `10d (5x)`, `1d (1x)`.

---
---

## Bug-008: PIT Screening Pipeline Failure on Ephemeral Network/DNS Outage & Deceptive Fallback in Unified DB Update

- **Status:** Resolved (2026-09-21)
- **Component:**
  - [`cmd/db.go`](file:///Users/raghavgarg/Projects/myGo/mycase/cmd/db.go) (Verified snapshot exists on disk, honest error exit codes, multi-strategy unified `db update`)
  - [`pkg/stockpicker/run.go`](file:///Users/raghavgarg/Projects/myGo/mycase/pkg/stockpicker/run.go) & [`pkg/stockpicker/loader.go`](file:///Users/raghavgarg/Projects/myGo/mycase/pkg/stockpicker/loader.go) (Local air-gapped constituent mirror `data/cache/constituents/<index>.csv`, benchmark fetch 3-attempt backoff retry + DuckDB prices fallback)
  - [`pkg/stockpicker/loader.go`](file:///Users/raghavgarg/Projects/myGo/mycase/pkg/stockpicker/loader.go) (Restored exponential backoff retries to `fetchHistoricalPricesWithFetcher`)
  - [`scripts/daily_sync.sh`](file:///Users/raghavgarg/Projects/myGo/mycase/scripts/daily_sync.sh) (Consolidated Step 6 to `--method earlymb,multibagger`, removed redundant Step 7)
- **Strategy:** Early Multibagger (`earlymb`), Multibagger (`multibagger`), and Unified Database Update (`db update`)
- **Reported In:** `scripts/daily_sync.sh` / `logs/pit_update.log`
- **Execution Date:** 2026-09-17 21:12:07 -> 22:05:46 IST

---

### 1. Symptoms & Observed Behavior

During the automated daily sync run on 2026-09-17, the execution failed to complete today's screening, leaving `niftytotalmarket` (`earlymb` and `multibagger`) unpopulated in the database, while falsely logging that the database update completed successfully:

```text
[2026-09-17 21:12:07] Starting unified EOD database update for data/mycase.db...
====================================================================
        MYCASE UNIFIED EOD DATABASE UPDATE (data/mycase.db)         
====================================================================
Execution Time: 2026-09-17 21:12:07 IST | Target As-Of Date: 2026-09-17

▶ STAGE 1/3: Point-in-Time Research Screening [niftytotalmarket | earlymb]...
time=2026-09-17T21:12:07.324+05:30 level=INFO msg=prices.fetch_start req_id=db-211207 range=1y count=750 source=router
time=2026-09-17T22:05:44.559+05:30 level=INFO msg=prices.fetch_complete req_id=db-211207 active=689 total=750 source=router
time=2026-09-17T22:05:44.559+05:30 level=INFO msg=pick.benchmark_fetch req_id=db-211207 symbol=^NSEI range=1y
⚠️  Warning during PIT update: fetching benchmark prices: failed to fetch benchmark ^NSEI: network error: Get "https://query1.finance.yahoo.com/v8/finance/chart/%5ENSEI?range=1y&interval=1d": dial tcp: lookup query1.finance.yahoo.com: no such host (continuing with self-healing pass)

▶ STAGE 2/3: Verifying Snapshot Completeness & Self-Healing...
time=2026-09-17T22:05:44.561+05:30 level=WARN msg=pit.snapshot_date_fallback req_id=db-211207 requested=2026-09-17 using=niftytotalmarket_earlymb_2026-09-16.json
No data fetch failures found in snapshot niftytotalmarket_earlymb_2026-09-16.json. All 750 constituents have valid data.
   ✓ Data Integrity Verified: 750 / 750 candidates clean (0 unverified).

▶ STAGE 3/3: Synchronizing Theme Lifecycles & Return Intelligence...
====================================================================
             EOD DATABASE UPDATE COMPLETED SUCCESSFULLY             
====================================================================
data/mycase.db is 100% updated and cached for today.
All queries ('returns', 'theme show', 'pit stats', web dashboard) are now instant & offline.

[2026-09-17 22:05:46] Synchronizing Multibagger PIT factor scores on warmed Nifty Total Market cache...
Executing Point-in-Time Daily Screening Update for niftytotalmarket (multibagger) [Target: 2026-09-17]...
time=2026-09-17T22:05:46.433+05:30 level=INFO msg=constituents.download req_id=pit-220546 index=niftytotalmarket
Error: daily pit update failed: loading constituents: failed to download index 'niftytotalmarket': network error: Get "https://www.niftyindices.com/IndexConstituent/ind_niftytotalmarket_list.csv": dial tcp: lookup www.niftyindices.com: no such host
```

#### Observable Anomalies:
1. **53-minute fetch discarded entirely:** 689 out of 750 stock price series fetched successfully over 53 minutes were discarded because a single HTTP call for `^NSEI` failed at the end.
2. **Deceptive success reporting:** `mycase db update` reported "EOD DATABASE UPDATE COMPLETED SUCCESSFULLY", but the data was actually stale (fell back to `2026-09-16` from yesterday).
3. **Hard crash on Step 7:** 2 seconds after Step 6, Step 7 (`pit update`) attempted to re-download index constituents from NSE India, failed on DNS lookup, exited with code 1, and caused `daily_sync.sh` (`set -euo pipefail`) to abort immediately.

---

### 2. Root Cause Analysis

The incident was triggered by a temporary local network / DNS resolution dropout (`lookup ...: no such host`) at 22:05:44 IST, but exposed **five critical architectural fragilities**:

#### A. Monolithic Uncheckpointed Stage 1 (`pkg/stockpicker/run.go:446-449`)
Stage 1 executes as an all-or-nothing monolithic operation. It concurrently fetches prices for 750 tickers, then immediately calls `getBenchmarkAndSlicedPricesVia(ctx, fetcher, ...)`. 
- There is **no intermediate checkpoint** or partial snapshot persistence.
- There are **zero retries** on the benchmark fetch call (`^NSEI`).
- If this single endpoint fails, `RunWithResult` immediately returns an error. The 689 valid stock series already in memory are thrown away, and no snapshot is created for today's date (`2026-09-17`).

#### B. Error Swallowing & Deceptive Fallback in `cmd/db.go:143-157`
In `RunDBUpdateDirect`:
```go
if err := runPickWithOpts(ctx, opts); err != nil {
    fmt.Printf("⚠️  Warning during PIT update: %v (continuing with self-healing pass)\n", err)
}
```
`cmd/db.go` suppresses the Stage 1 error as a warning and enters Stage 2 (`RetryFailedSnapshotCandidates`). However, `RetryFailedSnapshotCandidates` assumes a snapshot file for `targetDateStr` exists on disk. When `niftytotalmarket_earlymb_2026-09-17.json` is not found, it falls back to globbing the latest available snapshot (`2026-09-16.json` from yesterday):
```go
snapPath = files[len(files)-1]
slog.WarnContext(ctx, "pit.snapshot_date_fallback", "requested", asOfDate, "using", filepath.Base(snapPath))
```
It verifies that yesterday's snapshot has 0 failed tickers, completes Stage 3, and returns `nil` (exit code `0`), falsely claiming the database is 100% up to date.

#### C. Injected Router Bypasses Retry Passes (`pkg/stockpicker/loader.go:476-526`)
The legacy `FetchHistoricalPrices` function implements multi-pass retries with exponential backoff (`maxRetries := 2`, `1500ms`, `3000ms`). 
However, when `runPickWithOpts` injects `opts.DataFetcher` (using `fetchHistoricalPricesWithFetcher`), all retry logic is completely bypassed. It directly dumps 750 tickers across 15 un-paced worker goroutines. When Yahoo Finance throttles, resets TCP connections, or times out, failed tickers are marked as permanent errors with no retry.

#### D. Redundant Constituent Downloads & Missing Local Cache (`pkg/stockpicker/loader.go:193-206`)
`downloadConstituents` makes an outbound HTTP GET request to `www.niftyindices.com` with a 15-second timeout and **no local disk caching or fallback**:
- Step 6 (`db update`) downloaded `ind_niftytotalmarket_list.csv`.
- Step 7 (`pit update`) ran 2 seconds later and downloaded the exact same CSV again.
NSE India constituent lists change at most twice a year (March and September rebalances). Making the live NSE web endpoint a mandatory runtime dependency on every single command run creates unnecessary single points of failure.

#### E. Pipeline Fragmentation in `scripts/daily_sync.sh`
`daily_sync.sh` executes two separate CLI processes back-to-back:
1. `mycase db update --all --index niftytotalmarket --method earlymb --top 10`
2. `mycase pit update --index niftytotalmarket --method multibagger --top 20`
Because `db update` only accepted a single `--method` flag, scoring `multibagger` required a second CLI execution. This forced constituent downloading and price loading to re-run from scratch, multiplying network exposure and runtime.

---

### 3. Comprehensive Remediation & Fix Specifications

#### Fix 1: Local Air-Gapped Mirror for Index Constituents
- In `pkg/stockpicker/loader.go`, implement a local cache mirror directory: `data/cache/constituents/<index>.csv`.
- When `downloadConstituents` is invoked:
  1. If the network request fails (DNS error, timeout, HTTP non-200), check if a local mirror file exists on disk.
  2. If found, load the constituent list from disk, log a clear notice (`WARN: Network constituent fetch failed; using cached offline mirror`), and proceed safely.
  3. On successful download, save the fresh CSV to disk.

#### Fix 2: Resilient Benchmark Fetching with DuckDB Fallback
- In `pkg/stockpicker/run.go` / `pkg/stockpicker/loader.go`:
  1. Add automatic retries with exponential backoff (3 attempts: 2s, 4s, 8s) when fetching `^NSEI`.
  2. If all retries fail, query `data/mycase.db` (`prices` table) for the latest available `^NSEI` series.
  3. If today's bar is missing in the fallback, synthesize the close or use the latest settled close rather than terminating the entire 750-stock candidate pool.

#### Fix 3: Partial Snapshot Checkpointing in Stage 1
- In `stockpicker.RunWithResult`:
  - If constituent prices succeeded (e.g. 689/750), never discard the results.
  - Save the PIT snapshot for the successful candidates and mark the missing candidates as `DataFetchFailed = true`.
  - Stage 2 (`RetryFailedSnapshotCandidates`) can then pick up today's partial snapshot and retry the missing candidates, rather than falling back to yesterday's data.

#### Fix 4: Restore Backoff Retries to `fetchHistoricalPricesWithFetcher`
- Align `fetchHistoricalPricesWithFetcher` with the legacy `FetchHistoricalPrices` retry logic:
  - Run initial pass with worker pool.
  - Collect failed tickers and perform up to 2 retry passes with backoff (`1500ms`, `3000ms`).

#### Fix 5: Multi-Strategy Unified Execution in `db update`
- Extend `mycase db update` to accept multiple comma-separated methods:
  ```bash
  mycase db update --all --index niftytotalmarket --method earlymb,multibagger
  ```
- Fetch the constituent list and market data **once**, then execute scoring passes for both `earlymb` and `multibagger` consecutively in-memory before proceeding to theme sync.
- Eliminates Step 7 in `scripts/daily_sync.sh` entirely.

#### Fix 6: Honest Exit Codes & Sentry Verification in `cmd/db.go`
- If Stage 1 fails and no snapshot can be generated for today's date, `cmd/db.go` must:
  - Print an explicit error banner: `❌ [EOD UPDATE FAILED]: Unable to generate fresh PIT snapshot for <date>`.
  - Refuse to report false 100% completion.
  - Exit with a non-zero exit code (e.g., `exit code 2` for incomplete data) so shell schedulers and monitoring catch the condition immediately.

---
---

## Bug-009: Missing Fundamental Data Sentry Blindspot & Stage-2 Self-Healing Bypass (78 Stocks in `Other Hard Filter`)

- **Status:** Resolved (2026-09-21)
- **Component:**
  - [`pkg/stockpicker/filters.go`](file:///Users/raghavgarg/Projects/myGo/mycase/pkg/stockpicker/filters.go) (`applyHardFilters` flags missing fundamental payload as fetch failure via `RecordFetchFailure`)
  - [`pkg/stockpicker/run.go`](file:///Users/raghavgarg/Projects/myGo/mycase/pkg/stockpicker/run.go) (Propagates fetch failures to candidate `DataFetchFailed = true`)
  - [`pkg/stockpicker/retry.go`](file:///Users/raghavgarg/Projects/myGo/mycase/pkg/stockpicker/retry.go) (Stage 2 self-healing enables retry pass on missing fundamentals and blocks prior-day fallback when target date specified)
  - [`pkg/pithistory/analytics.go`](file:///Users/raghavgarg/Projects/myGo/mycase/pkg/pithistory/analytics.go) (Section 1 explicitly maps missing fundamentals to `Data Fetch Failed`, and `CheckDataIntegrity` counts missing fundamentals)
- **Strategy:** Early Multibagger (`earlymb`) & All Strategies
- **Reported In:** `mycase --index niftytotalmarket --method earlymb --analysis`
- **Execution Date:** 2026-09-18

---

### 1. Symptoms & Observed Behavior

In the PIT deep analysis report for **2026-09-18**, `Other Hard Filter` suddenly reappeared at **78 stocks (12.3% of eliminated pool, 10.4% of total universe)**:

```text
--- 1. STAGE-1 HARD FILTER ATTRITION & ELIMINATION BOTTLENECKS (2026-09-18) ---
  * Total Constituents Processed: 750
  * Stage-1 Hard Gate Survivors : 117 (15.6% Pass Rate)
  * Eliminated in Stage 1       : 633 (84.4% Elimination Rate)

  Disqualification Category                | Count    | % of Eliminated | % Total Pool
  -----------------------------------------------------------------------------------------
  Downtrend (< 200-Day SMA)                |      172 |           27.2% |        22.9%
  Weak Cash Conversion (CFO < PAT)         |      159 |           25.1% |        21.2%
  Other Hard Filter                        |       78 |           12.3% |        10.4%
  Low ROCE (< 12%)                         |       65 |           10.3% |         8.7%
  Far from 52W High (< 85%)                |       60 |            9.5% |         8.0%
  ...
```

Simultaneously, the execution logs and integrity checks claimed total perfection:
```text
No data fetch failures found in snapshot niftytotalmarket_earlymb_2026-09-18.json. All 750 constituents have valid data.
   ✓ Data Integrity Verified: 750 / 750 candidates clean (0 unverified).
```

---

### 2. Root Cause Analysis

A forensic query into [`data/pit_snapshots/niftytotalmarket_earlymb_2026-09-18.json`](file:///Users/raghavgarg/Projects/myGo/mycase/data/pit_snapshots/niftytotalmarket_earlymb_2026-09-18.json) revealed **exactly 78 candidates** with:
```json
"rejection_reason": "Missing fundamental data"
```

During the 2026-09-18 EOD run, Yahoo Finance throttled, timed out, or reset connections on 78 out of 750 tickers during the fundamental fetch phase (`fetchFundamentals`). In [`pkg/stockpicker/filters.go:856-859`](file:///Users/raghavgarg/Projects/myGo/mycase/pkg/stockpicker/filters.go#L856-L859), when a ticker's fundamental payload was absent:
```go
for _, t := range activeKeys {
    f, ok := fundamentals[t]
    if !ok {
        tracker.RecordElimination(t, "Missing fundamental data", "Fixable")
        continue
    }

This specific string slipped through a **three-sentry blindspot**:

#### Sentry 1: Section 1 SQL Aggregator (`pkg/pithistory/analytics.go:361`)
```sql
CASE 
    WHEN data_fetch_failed = true OR rejection_reason LIKE 'DATA_FETCH_FAILED%' 
         OR rejection_reason IS NULL OR rejection_reason = '' 
         THEN 'Data Fetch Failed (Upstream Drop)'
    ...
    ELSE 'Other Hard Filter'
END
```
The query checked for `DATA_FETCH_FAILED%`, but not `Missing fundamental%`. Consequently, all 78 dropped candidates bypassed the upstream error category and fell into `ELSE 'Other Hard Filter'`.

#### Sentry 2: Stage 2 Automated Self-Healing (`pkg/stockpicker/retry.go:53`)
```go
if c.DataFetchFailed || (!c.PassedStage1 && c.RejectionReason == "") || strings.HasPrefix(c.RejectionReason, "DATA_FETCH_FAILED") {
    failedTickers = append(failedTickers, t)
}
```
Stage 2 self-healing only targets candidates marked with `DataFetchFailed = true` or `strings.HasPrefix(c.RejectionReason, "DATA_FETCH_FAILED")`. It did not recognize `"Missing fundamental data"` as a recoverable upstream drop. Thus, Stage 2 reported 0 failures and never attempted to re-fetch the fundamentals for those 78 stocks.

#### Sentry 3: Pre-Flight Data Integrity Audit (`pkg/pithistory/analytics.go:1423`)
```go
if fetchFailed || strings.Contains(strings.ToLower(reason), "unverified") || strings.Contains(strings.ToLower(reason), "fetch failure") {
    res.FailedCandidates++
}
```
`CheckDataIntegrity` checked for `"unverified"` or `"fetch failure"`, but lacked a check for `"missing fundamental"`. Thus, it reported `0 FailedCandidates` (0.0% failure rate) and bypassed the 5% threshold warning banner, despite 10.4% of the universe missing fundamental data.

---

### 3. Remediation & Fix Specification

Unify the detection vocabulary across all three sentries:

1. **Enable Self-Healing for Missing Fundamentals (`pkg/stockpicker/retry.go:53`):**
   ```go
   if c.DataFetchFailed || (!c.PassedStage1 && c.RejectionReason == "") || 
      strings.HasPrefix(c.RejectionReason, "DATA_FETCH_FAILED") || 
      strings.Contains(c.RejectionReason, "Missing fundamental") {
       failedTickers = append(failedTickers, t)
   }
   ```
   This ensures Stage 2 automatically queues the dropped tickers for fundamental retry with backoff.

2. **Transparent Section 1 Attribution (`pkg/pithistory/analytics.go:361`):**
   ```sql
   WHEN data_fetch_failed = true OR rejection_reason LIKE 'DATA_FETCH_FAILED%' 
        OR rejection_reason LIKE '%Missing fundamental%' 
        OR rejection_reason IS NULL OR rejection_reason = '' 
        THEN 'Data Fetch Failed (Upstream Drop)'
   ```
   Eliminates the blindspot in Section 1, restoring `Other Hard Filter` to 0.

3. **Accurate Data Integrity Warning (`pkg/pithistory/analytics.go:1423`):**
   ```go
   if fetchFailed || strings.Contains(strings.ToLower(reason), "unverified") || 
      strings.Contains(strings.ToLower(reason), "fetch failure") || 
      strings.Contains(strings.ToLower(reason), "missing fundamental") {
       res.FailedCandidates++
   }
   ```
   Ensures `CheckDataIntegrity` correctly catches unhealed drops $>5\%$ and fires the warning banner before analytics runs.

---
---

## Bug-010: Hardcoded 10-Run Lookback Cap in analytics.go:306 Truncates PIT Longitudinal Depth & Masks Historical Runs

- **Status:** Resolved (2026-09-21)
- **Component:** [`pkg/pithistory/analytics.go`](file:///Users/raghavgarg/Projects/myGo/mycase/pkg/pithistory/analytics.go) (`RunDeepAnalysis` & `RunPitAnalysisWithOptions`), [`cmd/pit.go`](file:///Users/raghavgarg/Projects/myGo/mycase/cmd/pit.go) (`--days` CLI flag parameterization)
- **Strategy:** Early Multibagger (`earlymb`)
- **Execution Evidence:** Verified in live codebase on 2026-09-21; `--days 60` analyzes full multi-month history across all 13 sections.

---

### 1. Symptoms & Observed Behavior

Every execution of PIT deep analysis (`mycase -i microcap250_smallcap250 -m earlymb -a`) outputs:
```
Total PIT Runs: 10 recorded runs (Latest As-Of: 2026-09-18)
```
Even though daily runs have executed continuously since late August (generating 15–18+ distinct daily runs in `data/mycase.db`), the displayed history in the report header and longitudinal streak computations is hard-capped at exactly 10 runs. 

Furthermore, [`docs/Feature_EMB.md §10`](file:///Users/raghavgarg/Projects/myGo/mycase/docs/Feature_EMB.md) had marked this task `p1a` as `:done, 2026-09-19`, creating an active documentation-vs-reality mismatch.

---

### 2. Root Cause Analysis

In [`pkg/pithistory/analytics.go:306`](file:///Users/raghavgarg/Projects/myGo/mycase/pkg/pithistory/analytics.go#L306):
```go
func (p *DB) RunDeepAnalysis(ctx context.Context, indexName, method string, marketOverride ...string) error {
	indexName = NormalizeIndexName(indexName)
	runs, err := p.GetRunHistory(ctx, indexName, method, 10) // <-- Hardcoded 10
```
While `GetRunHistory` has logic defaulting non-positive limits to 30 (`if limit <= 0 { limit = 30 }`), `RunDeepAnalysis` explicitly passes `10`. Consequently, `runs` is truncated to the 10 most recent runs, and `fmt.Printf("Total PIT Runs: %d recorded runs...", len(runs))` displays 10.

---

### 3. Remediation & Fix Specification

1. **Update `pkg/pithistory/analytics.go:306`:**
   Change the hardcoded limit from `10` to `30` (or `60` / wire to CLI `--days` parameter) to allow the full historical window to be analyzed.
2. **Synchronize Documentation:**
   Keep the item flagged as `🟡 Pending` in status matrices and active/critical in roadmaps until the code edit is committed.

---
---

## Bug-011: Legacy Delivery Delta (Pillar 4) Empirical Calibration Numbers Retained Post-Disjoint Baseline Migration

- **Status:** Open / Documentation Caveat Added (Pending Recalibration Run)
- **Component:** [`docs/earlyMB.md §6.4`](file:///Users/raghavgarg/Projects/myGo/mycase/docs/earlyMB.md#L237), [`docs/Feature_EMB.md §3B`](file:///Users/raghavgarg/Projects/myGo/mycase/docs/Feature_EMB.md#L127)
- **Strategy:** Early Multibagger (`earlymb`)

---

### 1. Symptoms & Observed Behavior

`docs/earlyMB.md §6.4` and `docs/Feature_EMB.md §3B` cite out-of-sample empirical metrics for Pillar 4 (Delivery Delta):
- **Mean IC**: $+0.0171$
- **Information Ratio (IR)**: $+0.160$
- **Positive IC % (Hit Rate)**: $55.6\%$

These metrics are cited in `Feature_EMB.md §3C` as justification for demoting Delivery Delta from a ranking multiplier to a binary overlay (Coiled Spring Index). However, forensic investigation established that the original calibration was conducted on the legacy formula `(DeliveryPct - 35%)`, which contaminated ~46% of records by handing out artificial subsidies.

---

### 2. Root Cause Analysis

When the canonical disjoint baseline ($\overline{\text{Deliv}}_{5\text{D}} - \overline{\text{Deliv}}_{20\text{D Baseline}}$) was introduced and legacy rows through Sep 10 were tagged `pillar4_uncalibrated = true`, the empirical calibration tables in the documentation were not re-derived from scratch. The existing figures describe the behavior of the deprecated, flawed metric rather than the clean disjoint baseline currently live in production.

Importantly, **Component 1 of the Coiled Spring Index (Setup Quality Score)** is defined purely as $\frac{\max(0.10, 1 + \text{Composite RS})}{\text{VCP Ratio} + 0.10}$, relying exclusively on Pillar 1 (Composite RS) and Pillar 2 (VCP Tightness). Setup Quality is therefore mathematically insulated from Pillar 4 calibration noise.

---

### 3. Remediation & Fix Specification

1. **Documentation Sentries:**
   Add explicit warning callouts in `docs/earlyMB.md` and `docs/Feature_EMB.md` flagging the $+0.017$ IC figure as legacy and uncalibrated.
2. **Scheduled Recalibration:**
   Once $\ge 30$ trading sessions have accumulated under `pillar4_uncalibrated = false` in `data/mycase.db`, execute a clean rolling OOS Spearman rank correlation test and update the canonical IC/IR tables.



