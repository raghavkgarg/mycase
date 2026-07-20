# Mycase — Product Vision

---

## What We Set Out to Build

An Indian retail investor who manages their own equity portfolio is underserved by current tooling. Advisory platforms charge for curated portfolios without transparency into methodology. Raw broker APIs surface data without helping the investor make decisions. Spreadsheets work until they don't — they can't fetch live prices, can't run backtests, and can't alert on drift.

Mycase is the tool we wanted: a local, self-hosted engine that automates the mechanical parts of self-directed equity investing — picking stocks by a consistent methodology, sizing positions, executing orders, auditing past performance, and watching the portfolio while you're not looking.

The design constraint that shaped everything: it must be transparent enough that the investor understands and trusts every decision it makes. No black-box scores, no unexplained recommendations. Every weight, every filter, every cost calculation should be auditable from the output.

---

## What We've Built

The core workflow is implemented and working end-to-end:

**Stock Selection**: Two strategies — Multibagger (11 hard filters + 100-point scoring, focused on small/mid-cap quality compounders) and MFS multi-factor (16-factor scoring covering return quality, risk, valuation, and fundamentals). Both strategies are parameterized, not hardcoded — factor weights live in `config/mfs.json` and can be adjusted without touching code.

**Weight Optimization**: Inverse-volatility and MFS-score-proportional weighting, both with iterative sector caps (25% max per sector, max 3 stocks per sector) and per-stock caps. The optimizer prevents concentration in any single sector or stock without requiring the investor to manually balance.

**Execution**: Basket order generation via Zerodha Kite Connect, with a complete Indian equity cost model (STT, stamp duty, CDSL DP charge, SEBI fee, Finance Act 2024 tax rates). The micro-transaction filter prevents paying more in DP charges than a small sell order is worth.

**Backtesting**: Full historical simulation with date-aligned common calendar, sell-then-buy rebalancing with slippage, and seven performance metrics (CAGR, Max Drawdown, Sharpe, Sortino, Calmar, Beta, Alpha). DuckDB-backed date-range cache means a 5-year backtest across 15 tickers fetches each ticker once.

**Monitoring**: 4-pillar health scoring (revenue momentum, cash flow quality, technical trend, capital allocation) with three pre-configured style presets from Hyper-Aggressive to Passive. Verdict per stock: KEEP HOLD, HIGH ALERT, or AUTO EXIT.

**Drift Daemon**: Runs at 15:45 IST daily via launchd (macOS) or systemd. Computes drift index (total variation distance between actual and target weights) and sends Telegram/Discord alerts when drift exceeds the configured threshold.

All of this runs as a single binary with no external services, no subscriptions, and no network dependencies beyond Yahoo Finance and the Zerodha API.

---

## What We'd Build Next

### Near-term: Web Dashboard (R8)

The biggest usability gap is that the tool is CLI-only. The investment thesis, portfolio health, and backtest results are all in the terminal. A local web dashboard would make the tool useful to an investor who doesn't think in command lines.

The dashboard is designed — views for holdings (live prices via SSE), backtesting (streaming equity curve), rebalance preview (order table + tax warnings), monitoring verdicts, and drift history. Tech: native Web Components, ECharts, Go stdlib HTTP, no framework, no build pipeline. The backend is a `mycase serve` subcommand; all assets are embedded in the binary.

### Medium-term: Multi-Broker Support

Zerodha is the primary broker today. The `Broker` interface already abstracts order placement and holdings — adding AngelOne SmartAPI, Fyers, or Upstox is a new `pkg/broker/{name}/` implementation, nothing else changes. Most Indian retail investors have accounts at multiple brokers; a unified view of all holdings across brokers is the natural next step.

### Medium-term: Fundamentals from Primary Sources

Yahoo Finance fundamentals are scraped, not sourced from the exchange. They have a 2–4 quarter lag, miss some balance sheet line items, and can be wrong. For the monitoring pillars (especially DSO and ROCE trends) and the multibagger hard filters, primary source data from NSE/BSE XBRL filings or Screener.in API would materially improve reliability.

### Longer-term: Tax-Optimized Rebalancing

The basket command today shows tax warnings but does not optimize for them. An investor approaching the LTCG exemption limit (₹1.25L) might choose to defer a sell, or realize a gain before year-end to reset the clock. A tax-aware rebalancer would integrate the investor's realized gains YTD and recommend the sequencing of sells that minimizes tax drag.

### Longer-term: Options Overlay

For a portfolio of 15–20 quality stocks, covered calls on over-weighted positions generate income while the market waits to recognize value. Protective puts on concentrated positions hedge tail risk. An options overlay module would identify strike/expiry candidates based on the portfolio's target weights and the investor's income vs. protection preference.

---

## What We Won't Build

**A recommendation engine that hides its reasoning.** Every score, filter, and weight must be explainable from the output. If a stock is ranked first, the investor should be able to see which factors drove it and by how much.

**A cloud service.** The tool runs locally, handles no one else's financial data, and has no subscription model. The investor controls the code, the config, and the data.

**A trading bot.** Execution is always gated on the investor's explicit approval — either `mycase basket --live` with a prompt, or a button in the web dashboard. The system recommends; the investor decides.
