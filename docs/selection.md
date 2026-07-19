# Portfolio Selection & Stock Scoring Enhancements

This document details the quantitative philosophy and implementations added to the stock picker to reduce unnecessary portfolio churn, filter out short-term market noise, and stabilize target allocations.

---

## 1. The Hysteresis (Buffer Zone) Rule

### The Philosophy
In standard rank-based portfolios, stocks near the rank cutoff (e.g. #20 and #21) frequently swap places due to minor daily price fluctuations. This results in **portfolio churn** (unnecessary trading, slippage, and transaction costs).

To solve this, we implemented the **Hysteresis Buffer Zone** rule. An existing stock in your portfolio is not replaced unless a new candidate is *significantly* better, not just slightly better.

### Implementation
* Added the `-golden` CLI flag to [main.go](file:///Users/raghavgarg/Projects/myGo/mycase/cmd/stockpicker/main.go) to read the active holdings from the golden copy.
* Defined a **rank buffer limit of 25** (Top $N$ target = 20, plus a buffer of 5):
  * Any candidate ranking in the **Top 20** is automatically selected.
  * An existing constituent currently in the portfolio is kept as long as its rank remains within the **Top 25** (rank $\le 25$).
  * If an existing stock drops below rank 25, it is exited and replaced by the highest-ranking new candidate.
* The selection algorithm is implemented in [scoring.go](file:///Users/raghavgarg/Projects/myGo/mycase/pkg/stockpicker/scoring.go) via `ApplyHysteresisSelection`.

---

## 2. Market Hours EOD Protection (Intraday Noise Filter)

### The Philosophy
During active market hours, stock prices and volumes fluctuate second-by-second. Running a relative rank-based stock selection on live-updating intraday data causes candidate list recommendations to constantly shift throughout the day. 

For long-term portfolio selection, intraday noise is irrelevant. Only finalized End-of-Day (EOD) closing data should be utilized.

### Implementation
* Implemented the `CleanIntradayNoise()` method on the `HistoricalData` struct in [types.go](file:///Users/raghavgarg/Projects/myGo/mycase/pkg/yfinance/types.go).
* When historical charts are loaded from Yahoo Finance via `FetchHistoricalDataWithTimestamps` in [prices.go](file:///Users/raghavgarg/Projects/myGo/mycase/pkg/yfinance/prices.go):
  * The method checks if the current time is during active market hours (**before 15:30 IST** on a trading day).
  * If it is, **the current live-updating day's data point is discarded** from the price, volume, and timestamp slices.
  * The tool falls back to using the last finalized, fully closed trading day (e.g. yesterday's close, or Friday's close if run on a weekend).

---

## 3. Rebalancing Band / Tolerance Gate (0.10% Threshold)

### The Philosophy
Rebalancing a portfolio to adjust a stock's weight by a fraction of a percent (e.g., changing from 5.05% to 5.09%) is inefficient and generates transaction costs for no real benefit. 

To solve this, we implemented a **Rebalancing Band**. If a stock's new target weight is close to its existing weight, we keep the existing weight.

### Implementation
* Implemented the rebalancing band check in [scoring.go](file:///Users/raghavgarg/Projects/myGo/mycase/pkg/stockpicker/scoring.go) via `ApplyRebalancingBand`.
* Set a **0.10% (0.0010) weight difference tolerance**:
  * For each selected stock, if the absolute difference between the new target weight and its current weight in the golden copy is **less than 0.10%**, its current weight is locked.
  * The remaining weights are proportionally rescaled so that the overall portfolio weight sum remains exactly `1.0` (100%).
