# Session Achievements: Intraday Portfolio Performance Analyzer & Simulator Corrections

We implemented a command-line tool in Go to analyze the intraday/historical performance of a stock portfolio starting from a specific target date and time down to the latest market close, and resolved alignment bugs in the historical monitoring simulator.

## What We Achieved

### 1. Extended yfinance API
- Added `FetchIntradayData(ticker string, rangeStr string) (*IntradayData, error)` in [prices.go](file:///Users/raghavgarg/Projects/myGo/mycase/pkg/yfinance/prices.go) to fetch minute-by-minute historical data for range options (like `1d` or `7d`) from Yahoo Finance.

### 2. Created Intraday Performance Command-line Subcommand
- Created the CLI subcommand at [cmd/performance.go](file:///Users/raghavgarg/Projects/myGo/mycase/cmd/performance.go) and valuation logic in [valuation.go](file:///Users/raghavgarg/Projects/myGo/mycase/pkg/performance/valuation.go).
- Supported CLI flags:
  - `-file`: Path to the portfolio CSV file containing `ticker` and `weight`.
  - `-capital`: Total capital invested (e.g. `100000.0`).
  - `-date`: Purchase date in `YYYY-MM-DD` or `YYYYMMDD` formats (default: today).
  - `-time`: Purchase time in `HH:MM` IST format (default: `09:30`).
- Core logic handles matching closest minutes on target dates. If the target purchase date is older than 7 days, it automatically switches to fetching daily Close prices (ignoring the time flag, since Yahoo Finance only supports up to 7 days of 1-minute interval data).

`./dist/mycase performance -file data/mymicro.csv -capital 100000 -date 2026-07-10 -time 09:30`

---

## Simulation & Alignment Corrections (Latest Updates)

During this session, we identified and corrected critical date alignment bugs in the **Portfolio Monitoring Simulator (Step 8)** that caused severe mismatches in historical backtests:

### 1. Right-Aligned Dataset Indexing (Fixed Date Drift)
* **The Bug**: The simulator previously aligned price histories by the start index of their slices (left-alignment). Because different stocks have varying numbers of trading days (e.g. 495 vs 499), a fixed index step resulted in a massive date drift. For instance, the simulator was accessing prices from different days (or several days ago) across the portfolio tickers, and today's final valuation did not use the actual latest close prices.
* **The Fix**: We refactored `RunSimulation` in [simulator.go](file:///Users/raghavgarg/Projects/myGo/mycase/pkg/monitoring/simulator.go) to align all time-series data from the **end of their slices** (right-alignment). The index on day `d` of the simulation is now computed as `len(h.Closes) - simDays + d`. This guarantees that:
  * Every stock's final valuation uses its actual latest close price (`len(h.Closes) - 1`).
  * Every stock's initial price is synchronized to exactly `simDays` trading days before today.

### 2. Synchronized Mock Timestamps & Spans
* **The Bug**: When Yahoo Finance APIs failed or returned incomplete data for a ticker (e.g., if a ticker has less than 200 days of history like `NSE:ATLANTAELE`), the simulator generated mock price histories spanning consecutive calendar days (504 days). Mixing calendar days (which include weekends) with trading days (which exclude weekends) caused months of mismatch. Furthermore, the mock data counted forward from 2 years ago, meaning it ended in December 2025 (well before today).
* **The Fix**: 
  * Aligned the mock data generator in [mock.go](file:///Users/raghavgarg/Projects/myGo/mycase/pkg/monitoring/mock.go) to count **backwards from today**, ensuring all mock histories extend to the current date.
  * Synchronized the mock timestamps to copy the exact trading day dates from `benchData.Timestamps` if available. This ensures mock tickers have the exact same length and trading days as live tickers.

### 3. Integrated Timeline Selection in Step 8
* **The Update**: We updated the automatic pipeline runner in [cmd/pipeline.go](file:///Users/raghavgarg/Projects/myGo/mycase/cmd/pipeline.go) to prompt the user before running Step 8:
  ```text
  Choose Monitoring Simulator timeframe:
  1. 1 Year Historical Backtest [Default]
  2. Same as performance simulation date (2026-01-01)
  ```
  * Choosing option 2 forwards the `-date` parameter to `mycase monitor`, making the simulation run on the exact same window as Step 7.