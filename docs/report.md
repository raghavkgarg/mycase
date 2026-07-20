# Portfolio Selection Explanation Report Generator

The Report Generator is a CLI tool that parses a stockpicker output CSV, fetches financial fundamentals and price histories, calculates key metrics, and generates a structured explanation report for the portfolio's contents.

## Location
- CLI entrypoint: [cmd/report.go](file:///Users/raghavgarg/Projects/myGo/mycase/cmd/report.go)
- Heuristics generator: [heuristics.go](file:///Users/raghavgarg/Projects/myGo/mycase/pkg/report/heuristics.go)
- Output location: Reports are saved under `report/[universe]_[strategy]/executions/`

---

## Usage

Run the report generator subcommand:

```bash
./dist/mycase report -file <path_to_csv> -method <strategy_preset>
```

### CLI Parameters

| Flag | Description | Default | Required |
|------|-------------|---------|----------|
| `-file` | Path to the stockpicker output CSV file | `""` | Yes |
| `-method` | Weighting strategy preset (`balanced`, `aggressive`, `conservative`, `multibagger`) | `"balanced"` | No |

---

## Core Functionality

### 1. CSV Parsing
The tool expects a CSV with at least:
*   `ticker` column (containing ticker strings like `NSE:NETWEB`)
*   `weight` column (containing the allocated weight decimals)

### 2. Data Fetching
For all parsed tickers, the tool concurrently fetches:
*   **3-Month close prices** for short-term momentum calculation.
*   **1-Year historical data with timestamps** for long-term returns, 200-day Simple Moving Average (SMA), and Relative Strength Index (RSI).
*   **Financial fundamentals** (annual revenue, CapEx, DSO, profit margins, Forward P/E, debt ratios, institutional holdings, etc.).

### 3. Report Output Modes

Based on the `-method` flag, the generated report includes different overview tables and heuristics:

#### A. Multibagger Mode (`-method multibagger`)
Prints a table containing:
*   **TTM Growth / 3Y CAGR**: Sales growth accelerator metrics.
*   **DSO (L/P)**: Working Capital Days Sales Outstanding (Latest / Previous).
*   **RSI**: 14-day Relative Strength Index.
*   **Inst %**: Institutional ownership percentage.
*   **Final Weight**: Target allocation weight.

*Heuristics mapped to each stock:*
*   **Sales Growth Accelerator**: Evaluates if revenue growth is accelerating (TTM growth exceeding 3-year CAGR).
*   **Asset Turnover & CapEx Inflection**: Detects stabilization of capital expenditures alongside rising asset turnover efficiency.
*   **Working Capital DSO**: Tracks year-over-year efficiency in accounts receivable collections.
*   **Institutional Sponsorship**: Documents validation of equity stakes from smart money.
*   **Technical Stage Analysis**: Checks if the stock is in a Stage 2 markup phase (above its 200-SMA) with volume breakout indicators.

#### B. Standard Modes (`balanced`, `aggressive`, `conservative`)
Prints a table containing:
*   **Ticker**
*   **Final Weight**
*   **1-Year Return**

*Heuristics mapped to each stock:*
*   **Performance/Momentum**: Highlights short-term momentum (3-month returns) versus long-term trend (1-year return).
*   **Valuation**: Analyzes Forward P/E levels to call out high-value or unprofitable segments.
*   **Debt/Solvency**: Examines Net Debt/EBITDA ratios to flag high leverage or cash-rich balance sheets.
*   **Efficiency**: Evaluates Return on Equity (ROE) and Operating Margins for business stability.
