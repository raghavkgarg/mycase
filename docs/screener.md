# Screener.in Integration Guide & Strategy Roadmap

This document outlines the **Screener.in** data integration in `mycase`, what has been implemented, and potential future enhancements enabled by Screener's rich Indian equity dataset.

---

## 1. What We Implemented

### **Quarterly Earnings Date Extraction (`pkg/yfinance/screener.go`)**
To solve missing or sparse quarterly result dates for Indian stocks (NSE/BSE) from Yahoo Finance, `mycase` queries Screener.in (`https://www.screener.in/company/{symbol}/`) to extract:

1. **Quarterly Results Headers (`#quarters`)**:
   - Scrapes `data-date-key` attributes (e.g. `2026-06-30`, `2026-03-31`, `2025-12-31`) from the `#quarters` HTML table header.
2. **Corporate Disclosures & Board Meetings**:
   - Parses the company announcements feed for board meeting intimations and financial result filings (e.g., `"Board meeting on 23 July 2026 to approve Q1 results"`).
3. **Selection Reasons Integration (`_01_selection_reasons.txt`)**:
   - Computes `Result Prev -> Coming` (format: `DD-MM-YY -> DD-MM-YY`) and populates the **Result Prev -> Coming** column in the Stock Selection & Rejection report.

**Example Output**:
```text
Ticker           | Sector               | Score  | Raw Rank | Weight Decided | Result Prev -> Coming | Selection Reason
---------------------------------------------------------------------------------------------------------------------------------------------
NSE:CHENNPETRO   | Energy               |  58.0  | 4        | 20.00%         | 23-07-26 -> N/A       | New addition (Rank 4) | Drivers: TTM Growth: +7.2% (3Y: -6.0%)
NSE:NETWEB       | Technology           |  84.0  | 1        | 20.00%         | 02-05-26 -> 28-07-26  | New addition (Rank 1) | Drivers: TTM Growth: +90.0% (3Y: +70.4%)
```

---

## 2. Proposed Future Enhancements Using Screener.in

Screener.in is one of the richest sources for Indian corporate data. Here are strategic enhancements that can be integrated into `mycase`:

### A. **Quarterly Financial Acceleration (QoQ & YoY Filters)**
* **Data Available**: 12+ recent quarters of Sales, Operating Profit, OPM %, Net Profit, EPS, and Other Income.
* **Proposed Enhancement**:
  - **QoQ / YoY Sales Acceleration**: Check if the latest quarter's YoY revenue growth exceeds the 4-quarter average growth (inflection indicator).
  - **OPM Margin Expansion**: Filter for companies where Operating Profit Margin (OPM %) expanded YoY for 2 consecutive quarters.

### B. **Quarterly Shareholding Pattern & Institutional Flow**
* **Data Available**: Quarter-by-quarter breakdown of Promoters, FIIs, DIIs, Government, and Public holding percentages.
* **Proposed Enhancement**:
  - **FII / DII Accumulation Driver**: Award bonus points in Multibagger scoring if combined FII + DII stake increased over the last 2 quarters.
  - **Promoter Pledge Reduction Tracker**: Detect reduction in promoter pledging quarter-over-quarter.

### C. **Capital Work-In-Progress (CWIP) Inflection**
* **Data Available**: Balance sheet breakdown showing Net Block vs Capital Work-In-Progress (CWIP).
* **Proposed Enhancement**:
  - **Upcoming Capacity Expansion Trigger**: Calculate `CWIP / Net Block`. A high ratio (> 20-30%) indicates a major new plant or capacity coming online soon, signaling future revenue expansion.

### D. **Concall Transcripts & Investor PPT Insights (AI Scuttlebutt)**
* **Data Available**: Direct links to Earnings Call Transcripts, Investor Presentations, and Credit Rating Rationale documents.
* **Proposed Enhancement**:
  - **Automated Management Guidance Extraction**: Feed concall transcripts into LLM/AI to extract management guidance on revenue growth, margin targets, and capex timelines for the Scuttlebutt report (`*_scuttlebutt.txt`).

### E. **Peer Comparison & Relative Industry Ranking**
* **Data Available**: Peer comparison tables containing sector P/E, Median P/E, Market Cap, ROCE, and Sales Growth.
* **Proposed Enhancement**:
  - **Sector Leader Filter**: Verify if a candidate stock ranks in the top 25% of its industry peers by ROCE and Sales Growth.

### F. **Cash Flow Quality (CFO / EBITDA Ratio)**
* **Data Available**: 10-year Cash Flow Statements (Cash from Operating Activities, Investing Activities, Financing Activities).
* **Proposed Enhancement**:
  - **Earnings Quality Hard Filter**: Ensure `Cumulative 3Y CFO / Cumulative 3Y Operating Profit > 75%` to filter out aggressive accounting or cash traps.
