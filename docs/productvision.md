# Product Vision: Go Mycase Basket & Rebalancing Engine

This document outlines the product roadmap, architectural expansion, and feature phases for the Go-based Mycase portfolio optimization tool.

---

## 🌟 Vision Statement
To build a high-performance, developer-first, and highly-customizable portfolio execution and rebalancing engine that minimizes transaction friction, monitors deviation, and integrates with major Indian brokerages seamlessly.

---

## 🛣️ Roadmap & Feature Phases

### Phase 1: Core Engine & CLI (Current State)
- **High-Performance Quotes**: Native concurrent HTTP client querying Yahoo Finance chart APIs in parallel.
- **Greedy Optimizer**: Smart allocation algorithm calculating optimal shares under a budget constraint.
- **Decoupled CLI Execution**: Segmented CLI commands (`holdings` and `mycase`) supporting both live Zerodha API execution and local mock/dry runs.
- **Robust Table Printer**: Multi-byte character alignment for clean terminal presentation of Indian Rupee (`₹`) metrics.

---

### Phase 2: Portfolio Drift Monitoring & Alerts
Add a background monitoring system to alert when a portfolio strays from its target.
- **Drift Metrics**: Calculate drift index:
  $$\text{Drift} = \frac{1}{2} \sum_{i} |w_{\text{actual}, i} - w_{\text{target}, i}|$$
- **Background Cron/Daemon**: A lightweight service running daily checks after market close.
- **Alert Integrations**: Multi-channel notifications via **Telegram Bots**, **Discord Webhooks**, or **Email (SMTP)** indicating when the drift exceeds a specific threshold (e.g., $5\%$).

---

### Phase 3: Tax & Transaction Cost-Aware Optimization
Ensure the optimizer accounts for real-world transaction friction under Indian market regulations.
- **Friction Constraints**:
  - Depository Participant (DP) charge estimation (flat fee per company sold).
  - Securities Transaction Tax (STT), Stamp Duty, and brokerage fees.
- **Micro-transaction Filtering**: Prevent execution of tiny transactions where trading costs exceed a threshold percentage of the trade value.
- **Tax Harvesting Indicators**: Provide warning banners if a proposed rebalancing sell is subject to Short-Term Capital Gains (STCG) vs. Long-Term Capital Gains (LTCG).

---

### Phase 4: Historical Backtesting Engine
Build a simulator to test basket performance historically before deploying capital.
- **Historical Scraper**: Scrape daily adjusted close historical prices from Yahoo Finance.
- **Simulation Parameters**: Define initial capital, rebalancing frequency (monthly, quarterly, or drift-triggered), and slippage percentages.
- **Performance Analytics**: Output comprehensive reports including CAGR, Max Drawdown, Sharpe Ratio, Sortino Ratio, and comparison against benchmark indices (e.g., Nifty 50, Nifty Smallcap 250).

---

### Phase 5: Multi-Broker Abstraction Layer
Decouple the application from Zerodha Kite to allow flexibility in execution platforms.
- **Broker Interface**: Introduce a clean Go interface (`pkg/broker`) defining common operations:
  ```go
  type Broker interface {
      GetHoldings() ([]portfolio.Holding, error)
      PlaceBasketOrders(orders []executor.Order) ([]string, error)
  }
  ```
- **Integrations**: Support popular alternative broker APIs (e.g., **Fyers API**, **AngelOne SmartAPI**, **Upstox API**).

---

### Phase 6: Modern Local Web Dashboard
Replace/supplement the CLI with a stunning local web dashboard.
- **Go Backend Server**: A lightweight REST API server built with standard library or Gin.
- **Interactive Web App**: A responsive single-page dashboard with:
  - Donut/pie charts comparing actual vs. target weights.
  - Interactive sliders to dynamically adjust investment amounts.
  - An "Execute Rebalance" button that initiates live execution.
