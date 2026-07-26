# Portfolio Exit Vetting & Addition Drivers Documentation

## Overview

This document details the enhanced vetting and rationale tracking system implemented in `mycase`. The system ensures complete transparency for all portfolio movements during rebalancing:
1. **Exiting Holdings**: Rigorously vetted with exact technical or fundamental safety filter failures or rank hysteresis drift reasons.
2. **New Additions**: Highlight key operational and financial catalysts explaining why they entered the Top 20.
3. **Weight Shifts (Increases & Reductions)**: Track rank and score deltas relative to golden copy holdings.

---

## 1. Exit Vetting Mechanism

When an existing portfolio holding (from the Golden Copy CSV, e.g., `data/microsmall.csv`) is removed or reduced to `0.00%` weight, the system vets the ticker through the following pipeline:

### Early Golden Copy Loading
- The `pick` command loads golden copy tickers early and merges them into the candidate fetch set.
- This guarantees that historical prices and fundamental metrics are fetched for all existing holdings even if they are not in the top index candidate feed.

### Filter & Drift Diagnostics
1. **Hard Safety Filter Failure**: If an existing holding fails any hard filter (e.g., 200-Day SMA, ROCE floor, Debt/Equity, DSO deterioration), the exact metric failure is logged.
   - *Example*: `Failed safety filter: Below 200-Day SMA (Downtrend)`
2. **Hysteresis Buffer Drift**: If a holding passes safety filters but its score rank drifts beyond the hysteresis threshold ($\text{Rank} > \text{TopN} + \text{HysteresisBuffer}$):
   - *Example*: `Removed: Rank 28 fell below hysteresis buffer limit (25)`
3. **Sector Cap Exclusion**: If a holding is pushed out due to sector allocation limits:
   - *Example*: `Dropped by sector cap: Sector cap for 'Industrials' exceeded (3/3 slots filled by ...)`
4. **Feed / Data Missing**: If data could not be retrieved from Yahoo Finance or the ticker is absent:
   - *Example*: `Missing from index dataset or fetch error`

> [!NOTE]
> Generic exit reasons like `"Dropped due to unknown reason"` are completely eliminated.

---

## 2. New Addition Positive Drivers

When a new stock enters the Top 20 portfolio selection, the selection tracker records its key financial and technical drivers.

### Multibagger Strategy Drivers
- **TTM Sales Growth & 3Y CAGR**: Accelerating revenue growth.
- **ROCE**: Return on Capital Employed.
- **Institutional Stake**: Holding percentage by institutional investors.
- *Example Report Output*: `New addition (Rank 20) | Drivers: TTM Growth: +4.7% (3Y: +15.9%), ROCE: 37.5%, Inst Stake: 1.3%`

### Value Strategy Drivers
- **Forward P/E Ratio**: Relative valuation.
- **Free Cash Flow (FCF) Yield**: FCF generation relative to Market Cap.
- **Institutional Stake**: Institutional endorsement.
- *Example Report Output*: `New addition (Rank 18) | Drivers: Forward PE: 14.2, FCF Yield: 6.8%, Inst Stake: 12.4%`

---

## 3. Weight Adjustment Shift Rationale

For holdings retained across rebalance runs, weight increases and reductions are tied to rank and score shifts:
- **Weight Increase**: Logged when a stock's raw rank or relative score improves significantly.
  - *Example*: `Increased Weight (3.43% -> 5.20%) | Rank jumped #23 -> #8 (Score 30.4 -> 49.7)`
- **Weight Reduction**: Logged when a stock's raw rank drifts slightly down within the hysteresis zone.
  - *Example*: `Reduced Weight (6.63% -> 6.26%) | Rank drifted #3 -> #4`

---

## 4. Code Architecture Summary

### `pkg/selectiontracker/tracker.go`
- `AdditionDrivers map[string]string`: Maps ticker to positive driver text.
- `WeightShiftReasons map[string]string`: Maps ticker to rank/score shift text.
- `RecordAdditionDriver(ticker, summary)`: Records catalyst summary.
- `RecordWeightShift(ticker, summary)`: Records weight shift rationale.
- Updated `SaveReport`: Formats detailed exit reasons and addition drivers into `report/*_selection_reasons.txt`.

### `pkg/stockpicker/scoring.go`
- Calculates metrics (TTM Growth, ROCE, FCF Yield, Institutional Stake) and logs drivers during constituent ranking.

### `cmd/pick.go`
- Merges golden copy tickers into constituent price/fundamental fetching to guarantee complete exit vetting.

---

## 5. Sample Output

### Selection Reasons Report (`*_selection_reasons.txt`)
```text
=============================================================================================
                               REMOVED ACTIVE HOLDINGS (EXITS)
=============================================================================================
Ticker           | Sector               | Score  | Raw Rank | Exit Reason
---------------------------------------------------------------------------------------------
NSE:CHALET       | Consumer Cyclical    | N/A    | N/A      | Failed safety filter: Below 200-Day SMA (Downtrend)
```

### Selected Stocks Table
```text
Ticker           | Sector               | Score  | Raw Rank | Weight Decided | Selection Reason
---------------------------------------------------------------------------------------------
NSE:SMLMAH       | Consumer Cyclical    |  41.5  | 20       | 3.91%          | New addition (Rank 20) | Drivers: TTM Growth: +4.7% (3Y: +15.9%), ROCE: 37.5%, Inst Stake: 1.3%
```
