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

