# System Configuration Parameters Reference Guide

This document provides a premium, structured reference for all configuration parameters, filters, weights, and constants used across the portfolio management system.

---

## 1. Pipeline Workflow Configuration (`config/pipeline.yaml`)

These settings govern the high-level automated execution flow, performance backtest settings, and rebalancing parameters.

| Parameter | Default Value | Financial/Operational Purpose |
| :--- | :--- | :--- |
| **`indices`** | `microcap250`, `small250` | The list of stock indices to scan for strategy candidates. |
| **`golden_copy_path`** | `data/microsmall.csv` | File path of your active portfolio. Used for order basket generation and monitoring. |
| **`strategy`** | `multibagger` | Target stock selection model strategy. |
| **`top_n`** | `20` | Portfolio target size. The stock picker optimizes and selects exactly $N$ stocks. |
| **`capital`** | `100000` | Default investment capital (INR) used for performance backtests and share calculation. |
| **`purchase_date`** | `2026-01-01` | The starting date of the performance simulation backtest (bought at Close price). |
| **`rebalance_tolerance_pct`** | `0.10%` | **Tolerance Band**: Locking threshold to avoid minor weight changes (saves brokerage/slippage). |
| **`hysteresis_rank_buffer`** | `5` | **Hysteresis Zone**: Extends rank exit threshold to `TopN + 5` (rank 25) to reduce portfolio churn. |

---

## 2. Strategy Filters & Scoring (`config/mfs.json`)

These parameters govern the safety checks and scoring weights for the **Multibagger** strategy.

### A. Safety Filters (Hard Selection Gates)
Every candidate stock must pass all these conditions before it is considered for relative scoring.

| Filter Parameter | Configuration Value | Financial Meaning & Risk Mitigation |
| :--- | :--- | :--- |
| **`min_market_cap`** | `Rs. 500 Cr` (`5,000,000,000`) | Excludes highly illiquid nano-cap stocks to mitigate market execution risk. |
| **`max_market_cap`** | `Rs. 500,000 Cr` (`5,000,000,000,000`) | Sets a size ceiling to target only small/mid-caps with high growth potential. |
| **`min_adv`** | `Rs. 1 Cr` (`10,000,000`) | Average Daily Volume. Ensures sufficient daily liquidity for clean basket orders. |
| **`min_cfo_pat`** | `0.25` | Cash Flow from Operations / PAT. Verifies at least 25% of profits are actual cash flows. |
| **`min_promoter_percent`** | `25.0%` (`0.25`) | Minimum promoter skin-in-the-game to align interest with public shareholders. |
| **`check_earnings_trend`** | `false` | Bypassed by default to allow turnaround/cyclical recovery stories to pass. |
| **`check_200day_sma`** | `true` | Trend filter. The stock must be trading above its 200 SMA (uptrend confirmation). |
| **`max_pledged_percent`** | `5.0%` (`0.05`) | Promoter pledging cap. Restricts high-debt risk and hostile margin call takeovers. |
| **`min_roce`** | `12.0%` (`0.12`) | Return on Capital Employed. Enforces minimum 12% efficiency on deployed capital. |
| **`max_debt_to_equity`** | `1.50` | Leverage limit. Total debt cannot exceed 1.5x equity, keeping balance sheet flexible. |
| **`min_interest_coverage`** | `3.00` | Operating profits must service annual interest payments at least 3 times over. |
| **`min_croic`** | `6.0%` (`0.06`) | Cash Return on Invested Capital. FCF / (Equity + Debt) must be $\ge$ 6%. |
| **`max_capex_yoy_multiplier`**| `2.00` | YoY capital expenditure increase ceiling to prevent aggressive over-expansion cash traps. |
| **`volume_breakout_lookback_days`**| `60` days | Historical lookback window used to establish baseline average volumes. |
| **`volume_breakout_multiplier`**| `1.50` | Today's volume must be 1.5x of the 60-day average to trigger volume breakouts. |

### B. Portfolio Diversification Caps
These constraints ensure the final portfolio is not excessively concentrated in a single sector.

| Diversification Parameter | Configuration Value | Operational Purpose |
| :--- | :--- | :--- |
| **`max_stocks_per_sector`** | `3` | Caps the number of stocks selected from a single industry sector. |
| **`max_sector_weight_cap`** | `25.0%` (`0.25`) | Maximum portfolio weight exposure allowed for any single industry sector. |

### C. Relative Scoring Model (100-Point Multibagger Matrix)
Candidates that pass all safety filters are scored relative to each other using this allocation:

| Factor Weight Parameter | Target Points | Financial/Momentum Evaluation |
| :--- | :--- | :--- |
| **`peg_floor`** | `0.1` | Floor limit for the PEG ratio to prevent mathematical division distortions. |
| **`score_weight_rev_acc`** | `20.0` points | Sales acceleration: TTM growth accelerating over the 3-Year CAGR trend. |
| **`score_weight_asset_turnover`**| `20.0` points | Efficiency expansion: Improving asset turnover YoY. |
| **`score_weight_peg`** | `15.0` points | Valuation check: Growth valued at a reasonable price (PEG ratio). |
| **`score_weight_roce`** | `15.0` points | Capital efficiency: Return on Capital Employed score. |
| **`score_weight_volume_breakout`**| `15.0` points | Buying momentum: Volume breakout intensity relative score. |
| **`score_weight_relative_strength`**| `15.0` points | Price momentum: 52-Week Relative Strength outperforming Nifty 50. |

---

## 3. Hardcoded (Non-Configurable) System Constants

The following constants are coded directly into the Go source files:

### Data Cleansing & Session Rules
* **`Market Close Cutoff` (`15:30` IST)**: Located in [types.go](file:///Users/raghavgarg/Projects/myGo/mycase/pkg/yfinance/types.go). Used by the EOD Protection filter (`CleanIntradayNoise()`). If the pipeline is run before 15:30 IST on a trading day, the current live-updating day's data is discarded to avoid intraday volatility.

### Portfolio Monitoring Simulator Constants (`pkg/monitoring/simulator.go`)
* **`Warmup Days` (`200` days)**: Historical price data required to calculate the initial 200 SMA trend before simulator starts.
* **`Rebalancing Schedule` (`126` trading days)**: Represents the 6-month rebalancing window to reset portfolio weights.
* **`Quarterly Review Schedule` (`63` trading days)**: Represents the 3-month cycle to check quarters exits, DSO shifts, and CapEx.
* **`Volume Average Lookback` (`20` days)**: Used to compute the short-term volume baseline on below-SMA watchlist days.

### High-Fidelity Mock Generator Fallbacks (`cmd/monitoring/main.go`)
Fallback parameters activated when the Yahoo Finance API is rate-limited or unavailable:
* **Simulated Price Paths**:
  * **`Default stock price` (`500.0` to `1500.0` INR)**: `500.0 + rand.Float64() * 1000.0` baseline generator.
  * **`Volatility` (`0.02` / 2%)**: Standard deviation used in the simulated log-normal daily price walks.
  * **`Starting Index` (`18500.0`)**: Baseline benchmark starting level.
* **Policy Presets**:
  * **Moderate/Balanced**: `6` months rebalance, `15%` weight drift limit, `10` consecutive days SMA exit.
  * **Hyper-Aggressive**: `3` months rebalance, `12%` weight drift limit, `5` consecutive days SMA exit.
  * **Passive**: `12` months rebalance, `20%` weight drift limit, `20` consecutive days SMA exit.
