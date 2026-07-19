# Portfolio Monitoring Policy & Simulator

The portfolio monitoring policy module is designed as an operational filter for managing hyper-growth portfolios (like [microsmall.csv](file:///Users/raghavgarg/Projects/myGo/mycase/data/microsmall.csv)). Rather than a traditional "buy and forget" strategy, this monitoring policy systematically cuts companies whose structural tailwinds fade and retains top performers.

It implements a 4-Pillar framework executed across distinct time horizons and provides an interactive backtest simulator to evaluate parameter styles (Hyper-Aggressive, Moderate/Balanced, Passive, or Custom).

---

## The 4-Pillar Framework

### 1 & 2. Quarterly Operational Reviews (Growth, DSO, Asset Turnover, CapEx)
* **Rule**: Every quarter, candidates are monitored across 3 operational checkpoints plus a CapEx reinvestment gate:
  1. **Sales Growth**: TTM Revenue Growth $\ge$ 3-Year CAGR.
  2. **Working Capital (DSO)**: DSO collection is flat or improving YoY (`latestDSO < previousDSO` or `latestDSO < DSO 2 years ago`).
  3. **Asset Turnover**: Asset Turnover expands YoY (`atLatest > atPrev`).
  4. **CapEx Reinvestment Sentry**: YoY CapEx growth remains within the configurable threshold (default `2.00`x).
* **Action**:
  - **CapEx Gate (Hard Trigger)**: If a company's YoY CapEx growth exceeds the limit, it is automatically exited (`⚠️ AUTO EXIT`).
  - **2-out-of-3 Check (Combined Trigger)**: The stock must satisfy at least **2 out of the 3** operational criteria. If it fails more than one, it triggers an automatic exit (`⚠️ AUTO EXIT`). Exited positions are liquidated and the proceeds are reallocated proportionally back to active positions.

### 3. Allocation Drift & Rebalancing (Semi-Annual & Dynamic)
* **Rule**: Track target baseline weights.
* **Action**: 
  - **Scheduled**: Rebalance weights back to baseline targets exactly every `RebalanceMonths` (default: 6).
  - **Dynamic**: If any single stock drifts to encompass more than a threshold percentage of the total portfolio value (configurable via `MaxWeightDrift`, default: 15.0%), trim it back to target and reallocate the excess profit across the trailing laggards in the basket.

### 4. Technical Trend & Stage Analysis (Monthly Review)
* **Rule**: Track the stock price relative to its 200-day Simple Moving Average (SMA).
* **Action**: If a stock closes below its 200-day SMA for more than a threshold of consecutive trading days (configurable via `SMADays`, default: 10) on **rising volume** (daily volume > 20-day average volume), it signals institutional distribution. It is put on a `👀 HIGH ALERT` watch list and future capital additions are paused.

---

## File Architecture

- **[types.go](file:///Users/raghavgarg/Projects/myGo/mycase/pkg/monitoring/types.go)**: Houses parameter configurations (`PolicyParams`), position state structures, and simulation results container formats.
- **[simulator.go](file:///Users/raghavgarg/Projects/myGo/mycase/pkg/monitoring/simulator.go)**: Performs the daily backtest simulation loop, calculates Cap Stall Severity, rebalances holdings, evaluates indicator triggers, and logs verdicts.
- **[main.go](file:///Users/raghavgarg/Projects/myGo/mycase/cmd/monitoring/main.go)**: Command-line entrypoint. Reads input portfolios, fetches live metrics or falls back to simulated mocks, manages the interactive menu, and outputs report files.

---

## Cap Stall Severity Classification
Severity is determined by combining the growth velocity and working capital sentry triggers:
* **None**: DSO Delta is $\le 5\%$ YoY **and** TTM Growth $\ge$ 3Y CAGR.
* **Mild**: DSO Delta is $\le 10\%$ YoY.
* **Moderate**: DSO Delta is between $10\%$ and $20\%$ YoY.
* **Severe**: DSO Delta is $> 20\%$ YoY.

---

## Style Presets

The simulator supports three default presets:

| Parameter | Hyper-Aggressive | Moderate (Balanced) | Passive |
| :--- | :--- | :--- | :--- |
| **Consecutive Slowdowns** | 1 Quarter | 2 Quarters | 3 Quarters |
| **DSO Deterioration Limit** | 10% YoY | 15% YoY | 25% YoY |
| **Days below SMA 200** | 5 Days | 10 Days | 20 Days |
| **Rebalance Period** | 3 Months | 6 Months | 12 Months |
| **Max Weight Drift Limit** | 12% | 15% | 20% |

---

## How to Run

### Basic Execution (Standard Preset)
Compile the module and run with a specific style preset:
```bash
# Build the binary
go build -o bin/monitoring cmd/monitoring/main.go

# Run using the moderate style (Default)
./bin/monitoring -file data/microsmall.csv -style moderate

# Run using the hyper-aggressive style
./bin/monitoring -file data/microsmall.csv -style hyper-aggressive
```

### Interactive Simulator (Tuning Parameters)
Run the CLI in interactive mode to test custom rules:
```bash
./bin/monitoring -file data/microsmall.csv -interactive
```

The terminal will prompt you to pick a style or specify custom thresholds:
```text
=====================================================
        PORTFOLIO MONITORING POLICY SIMULATOR        
=====================================================
Choose a monitoring style:
1. Hyper-Aggressive (Strict triggers, frequent rebalancing)
2. Moderate / Balanced (Standard guidelines, 6m rebalance) [Default]
3. Passive (Loose triggers, annual rebalancing)
4. Custom Parameters (Specify your own thresholds)
-----------------------------------------------------
Enter choice (1-4): 4
Enter consecutive quarters of growth slowdown to trigger exit [current: 2]: 2
Enter DSO YoY deterioration % threshold (e.g. 15 for 15%) [current: 15.0]: 12
...
```

The simulator prints the report to the console and saves a record file to `report/report_monitoring_<indexName>_<YYMMDD>.txt`.

---

## Gotchas & Behavioral Learnings

When analyzing and debugging discrepancies between the **Stockpicker selection** and the **Monitoring Simulator verdicts**, keep these behaviors in mind:

### 1. Stockpicker (1y) vs. Simulator (2y) Data Length Mismatch
* **Stockpicker**: Requests a 1-year historical price history to maintain quick execution. If a newer stock listing has slightly fewer than 200 trading days in that year, it bypasses the SMA check and passes by default to avoid rejecting candidates with incomplete histories.
* **Simulator**: Requests a 2-year history to warm up SMA calculations, meaning it will compute the actual 200-day SMA, which may cause a stock to fail the SMA rule.

### 2. High Alert Persistence
* The `👀 HIGH ALERT` flag is triggered during the daily review loop if a stock stays below its 200-day SMA for consecutive days on high volume.
* Once triggered, the flag remains active **until the stock's closing price recovers and crosses back above the 200-day SMA**. If the stock remains below the SMA line (even on subsequent low-volume days), it stays flagged.

### 3. Data Source Indicators (Live vs. Mock)
* If the Yahoo Finance API rate-limits or fails to return data for *any single stock* in your batch, the simulator switches to a mock data fallback for that specific ticker.
* In the output report, look at the **`Data Source`** column:
  * **`Live`**: Evaluated on actual market prices and fundamentals.
  * **`Mock`**: Simulated via a random walk, which can sometimes result in simulated SMA violations that differ from actual live charts.

### 4. Deterministic Mock Generation via Isolated Local Generator (Resolved)
* **Historical Issue**: Previously, the mock generator used Go's global `rand` package (`rand.Seed(42)` and `rand.Float64()`). Since the global generator is shared across the entire Go process, background goroutines inside Go's HTTP/network library (managing connection pooling, keep-alives, DNS lookups) would concurrently consume random numbers, shifting the sequence and causing mock price paths to differ slightly across runs.
* **Resolution**: The mock generator now uses an isolated local random generator instance (`rand.New(rand.NewSource(42))`). This ensures that mock price generation is completely independent of background runtime tasks and remains 100% deterministic and reproducible across multiple runs.
* **Warmup Length Warning**: Note that if a stock (like `NSE:PARKHOSPS` with 146 closes or `NSE:ATLANTAELE` with 199 closes) has less than 200 trading days of cached historical data, it will always fail the `>= 200` closes safety requirement for the 200-day SMA warmup, causing it to fall back to mock data generation.

---

## Recommended Review Schedule & Operational Frequency

To optimize portfolio health while balancing transaction costs, slippage, and short-term capital gains tax (STCG), follow this dual-layered execution schedule:

### 1. Daily/Monthly Technical Sentry Checks (Alerting)
* **Frequency:** Run every **15 to 30 days**.
* **Objective:** Scan the active portfolio to check for technical breakdowns (e.g. 200-day SMA violations on rising volume).
* **Action:** Flagged tickers (e.g., `👀 HIGH ALERT`) should be closely watched; if they breach the consecutive SMA days limit, they should be exited immediately without waiting for a quarterly rebalance.

### 2. Quarterly Fundamental Reviews (Churn & Candidate Replacements)
* **Frequency:** Run every **90 days**, aligned with standard SEBI quarterly reporting deadlines.
* **Execution Schedule:**
  * **Q1 (Apr–Jun) Review:** Run on **August 16** (filing deadline August 14).
  * **Q2 (Jul–Sep) Review:** Run on **November 16** (filing deadline November 14).
  * **Q3 (Oct–Dec) Review:** Run on **February 16** (filing deadline February 14).
  * **Q4 / Full Year (Jan–Mar) Review:** Run on **June 1** (filing deadline May 30).
* **Action:** Run the automated stockpicker pipeline to re-rank the candidate universe, check for new potential multibaggers, and identify/exit stocks with deteriorating quarterly fundamentals.

### 3. Actioning Cap Stall Warnings
When a quarterly review flags a holding with `Severe` or `Moderate` Cap Stall Severity (e.g., a massive YoY jump in Days Sales Outstanding or revenue deceleration):
1. **Auditing Accounts:** Immediately perform the qualitative checks in the `report/<universe>_multibagger/research/<date>_scuttlebutt.txt` checklist.
2. **Operational Checks:** Check cash flow from operations (CFO) and management comments on margins. If the receivables stretch represents a structural decline in credit quality, prepare to rotate capital into healthier candidates during the next rebalance.
