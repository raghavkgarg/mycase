# Mycase Rebalance & Weight Optimization Guide

This guide details the portfolio rebalancing process, the programmatic weight optimizer, and recent enhancements made to the basket engine.

---

## 1. Programmatic Weight Optimizer

We created a CLI tool to automate and normalize target weight allocation using **Inverse-Volatility Weighting** (Smart Beta). 

### How it Works
1. **Historical Data:** It fetches 3 months of daily closing prices from Yahoo Finance for all tickers.
2. **Volatility Calculation:** It calculates the daily percentage returns and the sample standard deviation (historical volatility $\sigma_i$) for each stock.
3. **Allocation:** Weights are distributed inversely proportional to volatility ($W_i \propto \frac{1}{\sigma_i}$). More stable stocks get higher weights, while volatile stocks are downscaled.
4. **Safeguard for Exits:** Tickers passed to be removed are assigned a weight of `0.0000` so that the rebalance tool knows they exist and generates `SELL` orders to liquidate them.

### Running the Optimizer
To optimize weights in your basket and remove/sell specific tickers:
```bash
go run ./cmd/optimize_weights -file data/basket.csv -remove "NSE:FCL,NSE:PARACABLES"
```
This updates [basket.csv](file:///Users/raghavgarg/Projects/myGo/mycase/data/basket.csv) directly with normalized weights summing to `1.0000`.

---

## 2. Rebalancing Engine Features

Run the main rebalancer with:
```bash
go run ./cmd/basket --live
```

### Action 1: Fresh Buy (Invest Dynamic Amount)
*   Computes the optimal quantity of additional shares to purchase using a greedy algorithm to match target weights as closely as possible.
*   **Zero-Weight Safeguard:** Skip adding any shares for stocks with `0.0000` target weight (ensures no new capital is deployed into stocks you are exiting).

### Action 2: Rebalance (Align Holdings to Targets)
*   Compares your current quantity against target quantities based on your current portfolio size.
*   Generates both **BUY** and **SELL** orders to align the portfolio.

---

## 3. Key Enhancements & Bug Fixes

### A. Net Cash Flow Output
The portfolio preview now displays three cash metrics:
1.  **Estimated Total Outflow (Sum of Buys):** Total cost of all purchase orders.
2.  **Estimated Total Inflow (Sum of Sells):** Cash generated from trims and exits.
3.  **Net Cash Flow (Buys - Sells):** Tells you exactly if additional cash is needed or if a refund/surplus will be credited back to your ledger.

### B. Settlement Holdings & CNC Positions Bug Fix
*   **The Bug:** Recently purchased stocks in T+1 or T+2 settlement (or CNC positions bought today) had a settled quantity of `0` in Kite Holdings, causing the engine to think you owned `0` shares (underestimating your portfolio value by ~₹12.8k).
*   **The Fix:** Integrated `portfolio.FetchAndMergeHoldings` inside the data fetcher, which correctly merges `Quantity + T1Quantity + T2Quantity`, matching your broker terminal's value exactly.

### C. Fresh Capital Injection during Rebalance
*   You are prompted to enter a **fresh cash injection** amount when choosing Rebalance.
*   This scales up the target portfolio size, allowing you to buy more of the underallocated assets while naturally selling *less* (or keeping) your overweight assets.

### D. Outflow Out-of-Bounds Fix
*   Fixed a division-by-zero bug where a target weight of `0.0000` resulted in an infinite portfolio size calculation (`1.5e23`).
*   Modified the minimum required portfolio size calculator to assume a minimum of 1 share instead of `currentQty`, making it correctly compute a realistic 5-digit minimum required portfolio value (approx. `₹45,500`).
