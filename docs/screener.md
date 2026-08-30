# Indian Equity Data Integration Guide & Strategy Roadmap (NSE `nselib` & Screener.in)

This document outlines the **NSE (`nselib`)** and **Screener.in** data integration in `mycase`, environment setup, CLI tools, implemented features, and strategic roadmap enabled by Indian equity datasets.

---

## 1. Environment Setup

Python 3.9+ environment dependencies are specified in `requirements.txt`:

```text
nselib>=2.5.1
pandas>=2.0.0
pandas_market_calendars<5.0.0
requests>=2.30.0
```

Virtual environment initialization:
```bash
python3 -m venv .venv
.venv/bin/pip install -r requirements.txt
```

---

## 2. Implemented Data Engines & Go Architecture

### **A. Primary Official NSE Engine (`scripts/fetch_nse_data.py`)**
To eliminate reliance on fragile HTML scraping and prevent WAF/rate-limiting blocks on third-party web applications, `mycase` integrates official NSE data directly via Python [`nselib`](https://pypi.org/project/nselib/).

The CLI script `scripts/fetch_nse_data.py` supports 4 modes:
1. `earnings_dates`: Board meeting & earnings release dates (`capital_market.event_calendar_for_equity` & `capital_market.financial_results_for_equity`).
2. `delivery_data`: Traded volume, deliverable volume, and delivery % (`capital_market.price_volume_and_deliverable_position_data`).
3. `financial_results`: XBRL quarterly financial statements (`capital_market.financial_results_for_equity`).
4. `corporate_actions`: Dividends, splits, bonuses, and buybacks (`capital_market.corporate_actions_for_equity`).

### **B. Screener.in Fallback Engine (`pkg/yfinance/screener.go`)**
`FetchScreenerEarningsDates` acts as a resilient dual-engine pipeline:
1. **Primary (`FetchNselibEarningsDates`)**: Searches for `.venv/bin/python3` or system `python3` and executes `scripts/fetch_nse_data.py --symbol <SYMBOL> --mode earnings_dates` to retrieve official dates directly from NSE.
2. **Fallback**: If Python or `nselib` is unavailable or returns no data, it falls back to querying Screener.in (`https://www.screener.in/company/{symbol}/`) to extract:
   - **Quarterly Results Headers (`#quarters`)**: Scrapes `data-date-key` attributes (e.g. `2026-06-30`, `2026-03-31`).
   - **Corporate Disclosures & Board Meetings**: Parses company announcement feeds for board meeting intimations.
3. **Selection Reasons Integration (`_01_selection_reasons.txt`)**:
   - Computes `Result Prev -> Coming` (format: `DD-MM-YY -> DD-MM-YY`) and populates the **Result Prev -> Coming** column in the Stock Selection & Rejection report.

**Example Selection Report Output**:
```text
Ticker           | Sector               | Score  | Raw Rank | Weight Decided | Result Prev -> Coming | Selection Reason
---------------------------------------------------------------------------------------------------------------------------------------------
NSE:CHENNPETRO   | Energy               |  58.0  | 4        | 20.00%         | 23-07-26 -> N/A       | New addition (Rank 4) | Drivers: TTM Growth: +7.2% (3Y: -6.0%)
NSE:NETWEB       | Technology           |  84.0  | 1        | 20.00%         | 02-05-26 -> 28-07-26  | New addition (Rank 1) | Drivers: TTM Growth: +90.0% (3Y: +70.4%)
```

---

## 3. CLI Script Usage (`scripts/fetch_nse_data.py`)

### A. Earnings Announcement & Board Meeting Dates
```bash
.venv/bin/python3 scripts/fetch_nse_data.py --symbol NETWEB --mode earnings_dates
```
*Sample Output*:
```json
{
  "symbol": "NETWEB",
  "earnings_dates": [
    {
      "date": "2026-07-28",
      "purpose": "Financial Results/Dividend/Other business matters",
      "description": "To consider and approve the financial results for the period ended Jun 30, 2026",
      "source": "NSE Event Calendar"
    }
  ],
  "dates_only": ["2026-07-28"]
}
```

### B. Price, Volume & Deliverable Position Data
```bash
.venv/bin/python3 scripts/fetch_nse_data.py --symbol NETWEB --mode delivery_data --period 1W
```
*Sample Output*:
```json
{
  "symbol": "NETWEB",
  "records_count": 5,
  "records": [
    {
      "date": "2026-07-28",
      "close_price": 4395.7,
      "total_traded_qty": 960643,
      "deliverable_qty": 234146,
      "delivery_pct": 24.37
    }
  ]
}
```

### C. Quarterly Financial Results (XBRL)
```bash
.venv/bin/python3 scripts/fetch_nse_data.py --symbol TATAMOTORS --mode financial_results --period 3M
```

### D. Corporate Actions (Dividends, Splits, Bonuses)
```bash
.venv/bin/python3 scripts/fetch_nse_data.py --symbol RELIANCE --mode corporate_actions --period 3M
```

---

## 4. Proposed Future Enhancements Roadmap

Screener.in and NSE `nselib` provide rich sources for Indian corporate data. Here are strategic enhancements that can be integrated into `mycase`:

### A. **Deliverable Volume Position & High Conviction Signal (`nselib`)**
* **Data Available**: Historical and daily delivery percentage and deliverable volume via `nselib.capital_market.price_volume_and_deliverable_position_data`.
* **Proposed Enhancement**:
  - **Institutional Delivery Surge Filter**: Detect stocks where delivery % exceeds 50% alongside a price increase, indicating strong institutional accumulation.

### B. **Quarterly Financial Acceleration (QoQ & YoY Filters)**
* **Data Available**: 12+ recent quarters of Sales, Operating Profit, OPM %, Net Profit, EPS, and Other Income via Screener & `nselib`.
* **Proposed Enhancement**:
  - **QoQ / YoY Sales Acceleration**: Check if the latest quarter's YoY revenue growth exceeds the 4-quarter average growth (inflection indicator).
  - **OPM Margin Expansion**: Filter for companies where Operating Profit Margin (OPM %) expanded YoY for 2 consecutive quarters.

### C. **Quarterly Shareholding Pattern & Institutional Flow**
* **Data Available**: Quarter-by-quarter breakdown of Promoters, FIIs, DIIs, Government, and Public holding percentages.
* **Proposed Enhancement**:
  - **FII / DII Accumulation Driver**: Award bonus points in Multibagger scoring if combined FII + DII stake increased over the last 2 quarters.
  - **Promoter Pledge Reduction Tracker**: Detect reduction in promoter pledging quarter-over-quarter.

### D. **Capital Work-In-Progress (CWIP) Inflection**
* **Data Available**: Balance sheet breakdown showing Net Block vs Capital Work-In-Progress (CWIP).
* **Proposed Enhancement**:
  - **Upcoming Capacity Expansion Trigger**: Calculate `CWIP / Net Block`. A high ratio (> 20-30%) indicates a major new plant or capacity coming online soon, signaling future revenue expansion.

### E. **Concall Transcripts & Investor PPT Insights (AI Scuttlebutt)**
* **Data Available**: Direct links to Earnings Call Transcripts, Investor Presentations, and Credit Rating Rationale documents.
* **Proposed Enhancement**:
  - **Automated Management Guidance Extraction**: Feed concall transcripts into LLM/AI to extract management guidance on revenue growth, margin targets, and capex timelines for the Scuttlebutt report (`*_scuttlebutt.txt`).

### F. **Peer Comparison & Relative Industry Ranking**
* **Data Available**: Peer comparison tables containing sector P/E, Median P/E, Market Cap, ROCE, and Sales Growth.
* **Proposed Enhancement**:
  - **Sector Leader Filter**: Verify if a candidate stock ranks in the top 25% of its industry peers by ROCE and Sales Growth.

### G. **Cash Flow Quality (CFO / EBITDA Ratio)**
* **Data Available**: 10-year Cash Flow Statements (Cash from Operating Activities, Investing Activities, Financing Activities).
* **Proposed Enhancement**:
  - **Earnings Quality Hard Filter**: Ensure `Cumulative 3Y CFO / Cumulative 3Y Operating Profit > 75%` to filter out aggressive accounting or cash traps.
