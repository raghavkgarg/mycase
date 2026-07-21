# Feature Specification: 
---


## Feature 1 Tax-Optimized Rebalancing & FIFO Capital Gains Engine

## Overview

This specification details the design for **Tax-Optimized Rebalancing** as outlined in [`docs/vision.md`](file:///Users/raghavgarg/Projects/myGo/mycase/docs/vision.md#L51-L54) (*"Longer-term: Tax-Optimized Rebalancing"*).

While the `mycase basket` command provides general tax warnings, broker APIs do not expose historical tax lots. This feature introduces:
1. Zerodha Console Tradebook CSV parsing.
2. A **First-In, First-Out (FIFO)** capital gains matching engine tailored for Indian equity tax rules (Finance Act 2024).
3. Tax-aware order sequencing to minimize tax drag during rebalancing.

---

## 1. Tradebook Data Ingestion

Because standard broker APIs only expose active positions or single-day execution histories, full historical buy/sell lots are loaded via the user's exported Zerodha Console Tradebook.

### Step 1: Exporting Tradebook from Zerodha Console
1. **Log in**: Navigate to Zerodha Console (`console.zerodha.com`) with your Kite credentials.
2. **Access Tradebook**: Go to **Reports** → **Tradebook**.
3. **Apply Filters**: Select Segment: `Equity`, and set your target date range (e.g., past 2–3 years).
4. **Download**: Click **Download CSV** to save the file (e.g., `data/tradebook.csv`).

### Schema & Mandatory Columns
| Column Name | Description | Example |
|---|---|---|
| `Trade Date` / `Date` | Execution date | `2024-05-01` |
| `Symbol` / `Ticker` | Stock symbol | `RELIANCE` / `NSE:RELIANCE` |
| `Trade Type` / `Type` | Transaction type | `BUY` or `SELL` |
| `Quantity` | Number of shares executed | `100` |
| `Price` | Execution price per share | `1000.00` |

---

## 2. FIFO Capital Gains Calculation Engine

Under Section 45 of the Indian Income Tax Act, equity share transactions follow strict **First-In, First-Out (FIFO)** lot matching rules.

### Tax Classification (Finance Act 2024)
- **Holding Period Calculation**: Exact calendar days between `Trade Date (Buy)` and `Trade Date (Sell)`.
- **Long-Term Capital Gains (LTCG)**: $\text{Holding Days} > 365$. Taxed at 12.5% for gains exceeding the annual ₹1.25 Lakh threshold.
- **Short-Term Capital Gains (STCG)**: $\text{Holding Days} \le 365$. Taxed at 20%.

### Mathematical Formula
$$\text{Capital Gain / Loss} = \sum_{\text{matched lots}} \text{Quantity}_{\text{matched}} \times (\text{Sale Price} - \text{Buy Price}_{\text{lot}})$$

### Implementation Algorithm
1. **Isolate Asset History**: Filter rows for a specific stock ticker (e.g., `NSE:RELIANCE`).
2. **Sort Chronologically**: Sort records strictly from oldest to newest execution date.
3. **Queue Buy Orders**: Enqueue each `BUY` transaction into an active FIFO `TaxLot` queue (`BuyDate`, `Quantity`, `BuyPrice`).
4. **Offset Sell Orders**: When processing a `SELL` transaction:
   - Match the required sell quantity against the oldest available lot(s) in the `TaxLot` queue.
   - Calculate holding duration ($\text{SellDate} - \text{BuyDate}$).
   - Classify as **LTCG** if $> 365$ days, else **STCG**.
5. **Maintain Inventory**: Any unutilized shares remain in the queue to offset future sales.

---

## 3. Practical Calculation Example

Assume the following exported order history for **Stock X**:

| Date | Type | Quantity | Price (₹) |
|---|---|---|---|
| `01-May-2024` | BUY | 100 shares | ₹1,000 |
| `15-Aug-2024` | BUY | 50 shares | ₹1,200 |
| `10-Jun-2026` | SELL | 120 shares | ₹1,500 |

### Processing the 120-Share SELL on 10-Jun-2026:

#### Batch 1 (Oldest Lot: 01-May-2024)
- **Matched Quantity**: 100 shares
- **Holding Period**: ~770 days ($> 365$ days $\rightarrow$ **LTCG**)
- **Gain**: $100 \times (₹1,500 - ₹1,000) = \mathbf{₹50,000\text{ LTCG}}$

#### Batch 2 (Next Chronological Lot: 15-Aug-2024)
- **Matched Quantity**: 20 shares (from 50 shares lot)
- **Holding Period**: ~664 days ($> 365$ days $\rightarrow$ **LTCG**)
- **Gain**: $20 \times (₹1,500 - ₹1,200) = \mathbf{₹6,000\text{ LTCG}}$

#### Total Realized Capital Gains:
$$\text{Total Realized LTCG} = ₹50,000 + ₹6,000 = \mathbf{₹56,000}$$

*(The remaining 30 unutilized shares from the 15-Aug-2024 batch remain in the active inventory queue for future sales).*

---

## 4. Proposed Architecture & CLI Integration

### Data Structures (`pkg/tax/`)
```go
type TaxLot struct {
    Ticker   string
    BuyDate  time.Time
    Quantity float64
    BuyPrice float64
}

type RealizedGain struct {
    Ticker      string
    BuyDate     time.Time
    SellDate    time.Time
    Quantity    float64
    BuyPrice    float64
    SellPrice   float64
    Gain        float64
    IsLTCG      bool
}
```

### CLI Command Extensions
- `mycase tax import --file data/tradebook.csv`: Load tradebook and persist tax lots to DuckDB (`data/cache.db`).
- `mycase basket --tax-optimize`: Order basket executions to:
  1. Prioritize **tax-loss harvesting** (realizing STCL/LTCL to offset gains).
  2. Utilize remaining **annual ₹1.25L LTCG tax exemption limit**.
  3. Flag stocks near the **365-day boundary** to defer sells where STCG (20%) can turn into LTCG (12.5%).


  ## Feature 2 : Options Overlay Module

    