# Predictive Fair Price Engine (`fairprice`) — Multi-Model Ensemble Intrinsic Valuation & Cross-Strategy Enrichment

> **Document Version**: v2.1 (Production Tested & Calibrated with Real Data Across Nifty 750)  
> **Date**: September 26, 2026  
> **Strategy Code**: `fairprice`  
> **CLI Flag**: `--method fairprice` (Standalone Mode) | Integrated automatically across all strategies  
> **Target Systems**: `pkg/yfinance`, `pkg/stockpicker`, `pkg/selectiontracker`, `config/mfs.json`, DuckDB PIT

---

## 1. Executive Summary & Dual Operational Architecture

### 1.1 The Core Problem
The existing `mycase` platform features two operational stock selection engines — **Multibagger** ([`docs/12-multibagger.md`](file:///Users/raghavgarg/Projects/myGo/mycase/docs/12-multibagger.md)) and **Early Multibagger** ([`docs/13-early-multibagger.md`](file:///Users/raghavgarg/Projects/myGo/mycase/docs/13-early-multibagger.md)) — and a large-cap **Value** strategy ([`docs/14-value.md`](file:///Users/raghavgarg/Projects/myGo/mycase/docs/14-value.md)). All three produce ranked, weighted portfolios based on relative scoring matrices. 

However, **none of them tell the investor what the stock is actually worth in Rupees (₹)**:
- A high-scoring Multibagger could be growing revenue at 40% but trading at a P/E of 120 (priced for 10 years of flawless execution).
- An Early Multibagger might exhibit a perfect Volatility Contraction Pattern (VCP) and heavy delivery volume, but carry an intrinsic fair price 30% below Current Market Price (CMP).
- Investors had no quantitative anchor to establish **Margin of Safety (MOS)**, target sell thresholds, or risk-adjusted sizing.

### 1.2 The Solution: Dual Operational Architecture
The **Predictive Fair Price Engine** solves this by implementing an intrinsic valuation ensemble operating in **two complementary modes**:

```
                                  ┌─────────────────────────────────────────────────────────────┐
                                  │                  UNIFIED FINANCIAL DATA                     │
                                  │   yfinance.Fundamentals  +  1-Year Historical Daily Closes  │
                                  └──────────────────────────────┬──────────────────────────────┘
                                                                 │
                                                                 ▼
                                  ┌─────────────────────────────────────────────────────────────┐
                                  │           5-MODEL ENSEMBLE FAIR PRICE ENGINE                │
                                  │  M1: 2-Stage DCF (30%)      M2: Greenwald EPV (25%)         │
                                  │  M3: Graham Number (15%)    M4: Sector Peer Comps (15%)     │
                                  │               M5: Quality PEG (15%)                         │
                                  └──────────────┬───────────────────────────────┬──────────────┘
                                                 │                               │
                        ┌────────────────────────┴────────┐             ┌────────┴────────────────────────┐
                        ▼                                 ▼             ▼                                 ▼
         ┌──────────────────────────────┐ ┌──────────────────────────────┐ ┌──────────────────────────────┐
         │     MODE A: CROSS-STRATEGY   │ │    MODE B: STANDALONE ENGINE │ │   MODE C: ENTERPRISE AUDIT   │
         │       VALUATION OVERLAY      │ │       `--method fairprice`   │ │       & DUCKDB ARCHIVE       │
         ├──────────────────────────────┤ ├──────────────────────────────┤ ├──────────────────────────────┤
         │ • Enriches Multibagger Table │ │ • Broadest Cap Universe      │ │ • `pit_fairprice_scores`     │
         │ • Enriches EarlyMB Table     │ │   (₹500Cr – ₹5,00,000Cr)     │ │ • `v_fairprice_history`      │
         │ • Scuttlebutt MOS Breakdown  │ │ • 100-Point Attractiveness   │ │ • Rolling 21-day accuracy    │
         │ • Valuation Stretch Alerts   │ │ • Top N Undervalued Picks    │ │ • Cross-run delta tracking   │
         └──────────────────────────────┘ └──────────────────────────────┘ └──────────────────────────────┘
```

#### Mode A: Cross-Strategy Valuation Overlay (Automatic)
When running `--method multibagger`, `--method earlymb`, or `--method value`, the engine runs in the background for all surviving candidates and selected stocks:
1. **Table Column Enrichment**: Injects `Fair Price (₹)`, `Upside (%)`, and `MOS Band` directly into `PrintMultibaggerTable()` and `PrintEarlyMultibaggerTable()`.
2. **Valuation Stretch Guard**: Flags top-ranked growth stocks trading at extreme premiums (> 75% above Base Fair Price) with a visual `⚠️ VALUATION_STRETCHED` caution tag.
3. **Scuttlebutt Qualitative Enrichment**: Writes a dedicated `[Intrinsic Valuation & MOS Breakdown]` block into the automated research report in `report/..._scuttlebutt.txt`.
4. **Structured Driver Recording**: Populates `FairPrice`, `UpsidePct`, and `MOSVerdict` into `selectiontracker.DriverMetrics` and DuckDB `pit_candidate_scores`.
5. **PIT `--analysis` Launchpad Integration**: Injects `Fair Price` and `Upside` columns directly into Section 7 (Pre-Breakout Launchpad runway table) and embeds the Stage-1 Valuation Cushion distribution into the section header without adding cognitive load or extra categories.

#### Mode B: Standalone Fair Price Strategy (`--method fairprice`)
An independent, value-discovery screener designed to find the most mispriced, cash-generative compounding opportunities across the entire market (₹500 Cr to ₹5,00,000 Cr):
1. **Lightweight Safety Pre-Screen**: 5 baseline sanity filters (excludes shell companies and illiquid penny stocks).
2. **5-Model Ensemble Computation**: Calculates intrinsic values, adaptive weights, convergence coefficient of variation (CV), and MOS bands.
3. **100-Point Attractiveness Scoring Matrix**: Ranks surviving candidates across 4 pillars: Valuation Discount (40 pts), Model Consensus (20 pts), Quality Anchor (25 pts), and Growth Kicker (15 pts).
4. **Portfolio Optimization**: Selects Top N with strict sector exposure caps (max 5 stocks or 25% per sector).

---

## 2. The 5 Valuation Models (Financial Engineering & Formulas)

Every model is calculated strictly from fields already present in `yfinance.Fundamentals` and the 1-year price history. No external API subscriptions or new data feeds are required.

```
┌────────────────────────────────────────────────────────────────────────────────────────────────────────┐
│                                       5-MODEL VALUATION TAXONOMY                                       │
├─────────────────────┬───────────────────┬──────────────┬────────────────────────┬──────────────────────┤
│ Model               │ Category          │ Base Weight  │ Primary Data Source    │ Key Metric           │
├─────────────────────┼───────────────────┼──────────────┼────────────────────────┼──────────────────────┤
│ 1. 2-Stage DCF      │ Cash Flow Growth  │ 30%          │ AnnualFreeCashFlow     │ Sustainable FCF CAGR │
│ 2. EPV (Greenwald)  │ Normalized Yield  │ 25%          │ AnnualOperatingIncome  │ Zero-Growth NOPAT    │
│ 3. Graham Number    │ Asset/Earnings    │ 15%          │ NetIncome + PBRatio    │ EPS × BVPS Floor     │
│ 4. Relative Comps   │ Market Consensus  │ 15%          │ TrailingPE, PB, EV/EBIT│ Cohort Sector Median │
│ 5. PEG Fair Multiple│ Growth-Adjusted   │ 15%          │ Sales & EPS Growth     │ Quality-Scaled PEG   │
└─────────────────────┴───────────────────┴──────────────┴────────────────────────┴──────────────────────┘
```

---

---

### Model 1: Two-Stage Discounted Cash Flow (DCF) — 30% Weight

The DCF model projects free cash flows over a 5-year high-growth horizon and discounts both the projection and the terminal value back to present value using a sector-adjusted cost of capital.

#### 2.1.1 Free Cash Flow Normalization & Base Cash Flow ($FCF_0$)
In Indian equities, Free Cash Flow in any single fiscal year can be distorted by sudden capital expenditure spikes, working capital swings, or one-off asset sales. The engine computes $FCF_0$ using a 4-tier waterfall:

```
  Tier 1: Latest Annual Free Cash Flow
          f.AnnualFreeCashFlow[latest].Value (if > 0)
          
  Tier 2: 3-Year Average Free Cash Flow (if latest <= 0 or unavailable)
          Mean(f.AnnualFreeCashFlow[0..2].Value) (for all positive historical entries)
          
  Tier 3: Normalized Operating Cash Flow Proxy
          f.AnnualOperatingCashFlow[latest].Value - (f.AnnualCapEx[latest].Value * 0.60)
          (Assumes 40% of CapEx is discretionary growth CapEx, 60% is maintenance CapEx)

  Tier 4: Scalar Cash Flow Fallback (conditional on positive earnings)
          f.FreeCashflow (if > 0) or f.OperatingCashflow * 0.50 (if NetIncome > 0)
```

#### 2.1.2 FCF0 Sanity Cap (Neutralizing Project & Concession Asset Windfalls)
Infrastructure, EPC, road concession (HAM), and capital-goods companies (e.g. `PNCINFRA`, `ASHOKA`) occasionally book massive single-year cash inflows from asset monetizations, InvIT divestments, or arbitration settlements. To prevent treating one-off liquidation windfalls as sustainable perpetual cash flows:
$$FCF_0 \le \min\left(2.0 \times \text{Net Income}, \, 1.8 \times \text{Operating Income}\right) \quad (\text{if NetIncome} > 0 \text{ and OperatingIncome} > 0)$$

If normalized $FCF_0 \le 0$, or if the company has negative Net Income and negative Operating Income, the DCF model returns `nil` (incomputable) and its weight is dynamically redistributed (see §4).

#### 2.1.3 BFSI Sector Bypass (Structural Banking Inapplicability)
> [!IMPORTANT]
> **BFSI Free Cash Flow Inapplicability**: For Commercial Banks and Non-Banking Financial Companies (NBFCs) within `Financial Services` (e.g. `DCBBANK`, `PNB`, `SOUTHBANK`, `BANKINDIA`, `PFC`), Free Cash Flow as defined for non-financial corporates is economically meaningless. For financial institutions, customer deposits are operating cash inflows, loan disbursals are operating cash outflows, and balance-sheet debt represents raw material inventory rather than capital structure financing. Standard corporate DCF produces runaway blowouts (e.g., DCF ₹5,366 vs CMP ₹214).  
> **Rule**: When `f.Sector == "Financial Services"`, the DCF model **explicitly returns `nil, false`**. The 30% DCF weight is dynamically redistributed across the surviving 4 models (EPV, Graham Number, Relative Comps, and Quality PEG).

#### 2.1.4 Growth Rate Determination & ROCE Boundary Clamping
Historical growth is measured via 3-year Compound Annual Growth Rates:
$$\text{FCF CAGR}_{3Y} = \left(\frac{\text{FCF}_{\text{latest}}}{\text{FCF}_{\text{3Y prior}}}\right)^{1/3} - 1$$
$$\text{Revenue CAGR}_{3Y} = \left(\frac{\text{Revenue}_{\text{latest}}}{\text{Revenue}_{\text{3Y prior}}}\right)^{1/3} - 1$$

To prevent irrational exuberance, the projection growth rate $g_{\text{high}}$ is strictly capped based on **Capital Efficiency (ROCE)**:

| Capital Efficiency Tier | ROCE Condition | Maximum DCF Growth Cap ($g_{\text{max}}$) |
| :--- | :--- | :--- |
| **Elite Compounder** | $\text{ROCE} \ge 22\%$ | **25.0%** |
| **High Quality** | $15\% \le \text{ROCE} < 22\%$ | **18.0%** |
| **Moderate Quality** | $10\% \le \text{ROCE} < 15\%$ | **12.0%** |
| **Capital Destructive** | $\text{ROCE} < 10\%$ | **6.0%** |

$$g_{\text{high}} = \max\left(0.04, \, \min\left(g_{\text{historical}}, \, g_{\text{max}}\right)\right)$$

#### 2.1.5 Two-Stage DCF Valuation Formula

$$\text{PV}(\text{Stage 1}) = \sum_{t=1}^{5} \frac{FCF_0 \times (1 + g_{\text{high}})^t}{(1 + \text{WACC})^t}$$

$$\text{Terminal Value} = \frac{FCF_5 \times (1 + g_{\text{terminal}})}{\text{WACC} - g_{\text{terminal}}}$$

$$\text{PV}(\text{Terminal Value}) = \frac{\text{Terminal Value}}{(1 + \text{WACC})^5}$$

$$\text{Enterprise Value}_{\text{DCF}} = \text{PV}(\text{Stage 1}) + \text{PV}(\text{Terminal Value})$$

$$\text{Equity Value}_{\text{DCF}} = \text{Enterprise Value}_{\text{DCF}} - \text{Total Debt} + \text{Cash}$$

$$\text{Fair Price}_{\text{DCF}} = \frac{\text{Equity Value}_{\text{DCF}}}{\text{Shares Outstanding}}$$

Where:
- $\text{WACC}$ = Sector-adjusted discount rate (10.0% to 13.0%, see §3)
- $g_{\text{terminal}}$ = Long-term terminal GDP growth rate (**5.0%** for India, **2.5%** for US)
- $\text{Shares Outstanding} = \frac{\text{MarketCap}}{\text{CMP}}$
- $\text{Cash} = \max\left(0, \, \text{OperatingCashflow} \times 0.50\right)$ (conservative liquid cash buffer proxy when balance sheet cash is unseparated)

---

### Model 2: Earnings Power Value (EPV) — 25% Weight

Based on Professor Bruce Greenwald's Columbia Business School framework. EPV values a business based solely on its current sustainable operating earnings, assuming **zero future growth** ($g = 0$). This serves as the conservative, growth-free valuation anchor.

#### 2.2.1 Operating EPV Formulation (Non-Financials)
$$\text{Normalized EBIT} = \frac{1}{N} \sum_{i=1}^{N} \text{OperatingIncome}_i \quad (N \in [1, 3] \text{ years available})$$

$$\text{NOPAT} = \text{Normalized EBIT} \times (1 - T)$$

Where $T$ is the corporate effective tax rate (defaulted to **25.17%**, the Indian statutory corporate tax rate with surcharge/cess, or $f.\text{TaxRate}$ if within $[0.15, 0.35]$).

$$\text{Enterprise EPV} = \frac{\text{NOPAT}}{\text{WACC}}$$

$$\text{Equity EPV} = \text{Enterprise EPV} - \text{Total Debt} + \text{Cash}$$

$$\text{Fair Price}_{\text{EPV}} = \frac{\text{Equity EPV}}{\text{Shares Outstanding}}$$

*Guard Rail*: If Total Debt exceeds Enterprise EPV plus Cash, Equity EPV would be negative. In this case, `FairPrice_EPV` is floored at `0.10 * BookValuePerShare` and flagged as distressed.

#### 2.2.2 Direct Capitalization EPV for BFSI (Financial Services)
> [!NOTE]
> For commercial banks and NBFCs, subtracting Total Debt is invalid because customer deposits and wholesale borrowing are core operational liabilities, not corporate leverage. Furthermore, operating income is not reported on an EBIT basis.  
> **BFSI Formula**: The engine directly capitalizes sustainable Net Income attributable to equity shareholders:
> $$\text{Equity EPV}_{\text{BFSI}} = \frac{\text{Net Income}}{\text{WACC}_{\text{BFSI}}}$$
> $$\text{Fair Price}_{\text{EPV, BFSI}} = \frac{\text{Equity EPV}_{\text{BFSI}}}{\text{Shares Outstanding}}$$
> If $\text{Net Income} \le 0$, EPV returns `nil, false`.

---

### Model 3: Benjamin Graham Number (3Y Normalized) — 15% Weight

Benjamin Graham's classic defensive investment metric from *The Intelligent Investor*. It represents the theoretical maximum price a prudent value investor should pay, balancing earnings power against tangible asset backing.

#### 2.3.1 Graham & Dodd 3-Year Cyclically Adjusted Normalized EPS (`normalizedEPS`)
In turnaround stories, biopharma plays, or cyclical companies (e.g. `SPARC`), a single-year milestone receipt or one-off licensing payment following consecutive loss-making years can turn TTM Net Income temporarily positive. Naive calculation using latest single-year EPS causes Graham Number, PEG Fair Price, and Relative P/E to balloon and hit artificial upper caps.

To enforce Benjamin Graham and David Dodd's *Security Analysis* mandate of cyclically smoothed earnings:
1. Calculate 3-year historical average operating income: $\overline{\text{EBIT}}_{3Y} = \frac{1}{N} \sum_{i=1}^N \text{OperatingIncome}_i$.
2. **Historical Loss Penalty**: If $\overline{\text{EBIT}}_{3Y} \le 0$ (the company burned operating cash historically), a sudden positive single-year EPS is treated as an unproven windfall and capped:
   $$\text{normalizedEPS} = \min(\text{latestEPS}, \, 0.25 \times \text{latestEPS})$$
3. **Earnings Spike Smoothing**: If $\overline{\text{EBIT}}_{3Y} > 0$, compute implied operating EPS:
   $$\text{impliedEPS} = \frac{\overline{\text{EBIT}}_{3Y} \times (1 - T)}{\text{Shares Outstanding}}$$
   If $\text{latestEPS} > 2.0 \times \text{impliedEPS}$, smooth the spike via a 70/30 blend:
   $$\text{normalizedEPS} = 0.70 \times \text{impliedEPS} + 0.30 \times \text{latestEPS}$$
4. For `Financial Services`, $\text{normalizedEPS} = \text{latestEPS}$ (subject to the $\text{NetIncome} > 0$ pre-filter).

#### 2.3.2 Mathematical Formulation
$$\text{Graham Number} = \sqrt{22.5 \times \text{normalizedEPS} \times \text{BVPS}}$$

Where:
- $\text{normalizedEPS}$ = 3-year cyclically adjusted earnings per share
- $\text{BVPS} = \frac{\text{CMP}}{\text{PBRatio}}$ (Book Value Per Share)
- Constant $22.5 = 15.0 \times 1.5$ (Graham's ceiling of 15.0x P/E and 1.5x P/B)

#### 2.3.3 Guard Rails & Indian Market Calibration
- If $\text{normalizedEPS} \le 0$ or $\text{BVPS} \le 0$: Model returns `nil` (excluded from ensemble).
- In asset-light sectors (Technology, Diagnostics), Graham Number will naturally be conservative. That is by design: it serves as an **asset-backed safety floor** in the ensemble, preventing speculative bubbles.

---

### Model 4: Relative Sector Valuation (Peer Comps) — 15% Weight

Evaluates what the company would be worth if priced at the median valuation multiples of its industry peers within the active screening cohort.

#### 2.4.1 Multi-Multiple Triangulation
For each stock $i$ in sector $S$, the model computes fair price candidates across standard multiples:

$$\text{Price}_{\text{PE}} = \text{normalizedEPS}_i \times \text{Median}\left(\text{PE}_S\right)$$

$$\text{Price}_{\text{PB}} = \text{BVPS}_i \times \text{Median}\left(\text{PB}_S\right)$$

$$\text{Price}_{\text{EV/EBITDA}} = \frac{\left[\text{Median}\left(\text{EV/EBITDA}_S\right) \times \text{EBITDA}_i\right] - \text{TotalDebt}_i + \text{Cash}_i}{\text{Shares Outstanding}_i} \quad (\text{Non-Financials Only})$$

$$\text{Fair Price}_{\text{Relative}} = \frac{1}{K} \sum_{k=1}^{K} \text{Price}_k \quad (K \text{ valid multiple-derived prices})$$

> [!NOTE]
> **BFSI Multiple Filter**: For `Financial Services`, **EV/EBITDA is explicitly excluded** from both cohort median computation and individual company valuation. Comps for BFSI rely strictly on P/E and P/B multiples.

#### 2.4.2 Outlier Winsorization & Robust Medians
To avoid distortion from loss-making or hyper-valued sector peers:
1. P/E values are clamped to $[3.0, 120.0]$; negative P/E is excluded from the median.
2. P/B values are clamped to $[0.3, 30.0]$; negative P/B is excluded.
3. EV/EBITDA values are clamped to $[2.0, 60.0]$ (skipped for Financial Services).
4. **Sector Fallback**: If a sector has fewer than 3 valid peers in the cohort, the median falls back to the **All-Cohort Market Median** (typically ~22.0x PE, ~3.2x PB, ~14.0x EV/EBITDA for Indian mid/small caps).

---

### Model 5: Quality-Adjusted PEG Fair Multiple — 15% Weight

Based on Peter Lynch's principle that a company's fair P/E ratio is fundamentally tied to its sustainable earnings growth rate ($PEG = 1.0$), scaled by balance sheet and capital efficiency quality.

#### 2.5.1 Sustainable Growth Formulation
Rather than relying on single-quarter TTM spikes, the engine blends revenue trajectory with profit growth:
$$g_{\text{sustainable}} = 0.60 \times \text{Revenue CAGR}_{3Y} + 0.40 \times \text{TTM Earnings Growth}$$

Clamped to $[0.05, 0.40]$ (5% to 40% annual growth).

#### 2.5.2 Quality-Scaled Target PEG
High-quality compounders with superior returns on capital deserve a PEG premium:

| Quality Tier | Condition | Target PEG ($\text{PEG}_{\text{target}}$) |
| :--- | :--- | :--- |
| **Compounder Alpha** | $\text{ROCE} \ge 22\%$ and $\text{D/E} < 0.5$ | **1.25** |
| **Standard Quality** | $\text{ROCE} \ge 12\%$ and $\text{D/E} < 1.2$ | **1.00** |
| **Lower Quality** | $\text{ROCE} < 12\%$ or $\text{D/E} \ge 1.2$ | **0.80** |

$$\text{Fair PE}_{\text{PEG}} = \text{PEG}_{\text{target}} \times (g_{\text{sustainable}} \times 100)$$

$$\text{Fair Price}_{\text{PEG}} = \text{Fair PE}_{\text{PEG}} \times \text{normalizedEPS}$$

*(For Financial Services, Capital Efficiency evaluates $\text{ROE}$ or derived $\text{ROE}$ in place of ROCE).*

---

## 3. Sector-Specific Cost of Capital (WACC) Matrix

Discount rates cannot be one-size-fits-all. High-volatility commodity cyclicals require higher hurdle rates than stable consumer staples.

### 3.1 Empirical WACC Calibration (India / NSE Universe)
Assuming Risk-Free Rate ($R_f$) = 7.10% (10Y Indian G-Sec), Equity Risk Premium ($ERP$) = 5.50%:

| Sector Category | GICS Sectors | Unlevered Beta ($\beta$) | Cost of Equity ($K_e$) | Applied WACC | Rationale |
| :--- | :--- | :--- | :--- | :--- | :--- |
| **Defensive / FMCG** | Consumer Defensive, Healthcare, Utilities | 0.65 – 0.80 | 10.5% | **10.0%** | Non-cyclical demand, strong pricing power |
| **Stable Industrials** | Industrials, Basic Materials, Consumer Cyclical | 0.85 – 1.05 | 11.8% | **11.0%** | Moderate GDP elasticity, working capital cycles |
| **Technology & Digital**| Technology, Communication Services | 1.10 – 1.25 | 13.0% | **12.0%** | Rapid technological change, global exposure |
| **Deep Cyclicals** | Energy, Real Estate, Mining | 1.30 – 1.50 | 14.5% | **13.0%** | Commodity price swings, capital intensive |
| **Financials (BFSI)** | Financial Services | 0.95 – 1.15 | 12.5% | **11.5%** | Highly leveraged balance sheets, credit risk |
| **Unclassified** | Default / Empty Sector | 1.00 | 12.5% | **11.5%** | Market-wide conservative baseline |

---

## 4. Adaptive Weighting, Convergence (CV) & Confidence Score

### 4.1 Adaptive Weight Redistribution
When specific financial data is missing or invalid for a stock (e.g. negative earnings invalidates Graham and PEG, or negative FCF invalidates DCF), the model weights adaptively redistribute across the surviving valid models:

$$W_m^{\text{effective}} = \frac{W_m^{\text{base}}}{\sum_{j \in \text{valid}} W_j^{\text{base}}}$$

$$\text{Fair Price}_{\text{ensemble}} = \sum_{m \in \text{valid}} W_m^{\text{effective}} \times \text{Fair Price}_m$$

**Minimum Model Requirement**: A stock must have at least **2 valid models** out of 5. If fewer than 2 models can be computed, the stock is flagged as `UNVALUABLE` and excluded from ranking.

### 4.2 Model Convergence: Coefficient of Variation (CV)
Measures the internal agreement among the individual valuation models:

$$\mu = \frac{1}{K} \sum_{m \in \text{valid}} \text{Fair Price}_m$$

$$\sigma = \sqrt{\frac{1}{K} \sum_{m \in \text{valid}} (\text{Fair Price}_m - \mu)^2}$$

$$\text{Model CV} = \frac{\sigma}{\mu}$$

- $\text{Model CV} < 0.15$: **High Conviction** — Models strongly agree on intrinsic value.
- $0.15 \le \text{Model CV} < 0.35$: **Moderate Conviction** — Normal dispersion.
- $\text{Model CV} \ge 0.35$: **Low Conviction / High Dispersion** — Models diverge significantly.

### 4.3 Data Quality & Valuation Confidence Score (0 – 100%)
Quantifies the overall trustworthiness of the fair price estimate:

$$\text{Confidence Score} = \left(\frac{K_{\text{valid}}}{5} \times 50\right) + \left(\max(0, 1 - \text{Model CV}) \times 30\right) + \left(\text{Data Completeness} \times 20\right)$$

Clamped to $[20.0, 100.0]$.

### 4.4 Bayesian Shrinkage towards Market Prior (CMP)
In empirical real-data backtesting across 750 stocks, distressed or turnaround stocks often have only 2 computable models (e.g. `RPOWER`), while yielding large model dispersion ($\text{CV} \ge 0.40$). Naively computing a raw weighted average would allow a weak, 2-model ensemble to post +300% theoretical upside and dominate ranking.

To solve this, the engine implements **Empirical Bayesian Shrinkage**:
- **Market Prior**: The Current Market Price ($\text{CMP}$) represents the empirical collective consensus of all market participants.
- **Likelihood Estimator**: The raw weighted average of valid valuation models ($\text{Raw Ensemble Fair Price}$).
- **Posterior Fair Price**: The raw intrinsic spread $(\text{Raw} - \text{CMP})$ is shrunk towards the CMP prior proportional to the Valuation Confidence Score:

$$\text{Ensemble Fair Price} = \text{CMP} + (\text{Raw Ensemble Fair Price} - \text{CMP}) \times \left(\frac{\text{Confidence Score}}{100.0}\right)$$

```
  Confidence = 90% (5 models, Low CV)   ──► Retains 90% of intrinsic spread (10% shrinkage)
  Confidence = 70% (4 models, Normal CV)──► Retains 70% of intrinsic spread (30% shrinkage)
  Confidence = 40% (2 models, High CV)  ──► Retains 40% of intrinsic spread (60% shrinkage to CMP)
```

Both values are tracked: `RawEnsembleFairPrice` preserves the raw theoretical calculation, while `EnsembleFairPrice` drives final upside, MOS bands, rankings, and portfolio selection.

### 4.5 Boundary Sanity Clamping
To prevent absurd mathematical blowouts from data errors or extreme cyclical troughs:
$$\text{Clamped Fair Price} = \max\left(0.20 \times \text{CMP}, \, \min\left(4.00 \times \text{CMP}, \, \text{Ensemble Fair Price}\right)\right)$$
Any stock whose computed fair price hits either boundary clamp is explicitly flagged with an `⚠️ OUTLIER_CLAMPED` warning in reports.

---

## 5. Margin of Safety (MOS) Bands & Verdict Engine

### 5.1 MOS Valuation Bands
To protect capital against forecasting errors, three valuation bands are established:

$$\text{Pessimistic Fair Price} = \text{Fair Price}_{\text{ensemble}} \times (1 - \text{MOS Discount}) \quad [\text{Default Discount: 20\%}]$$

$$\text{Base Fair Price} = \text{Fair Price}_{\text{ensemble}}$$

$$\text{Optimistic Fair Price} = \text{Fair Price}_{\text{ensemble}} \times (1 + \text{Optimism Premium}) \quad [\text{Default Premium: 15\%}]$$

```
  ₹0 ──────────────────────────────────────────────────────────────────────────► ₹∞
  
  │◄───── STRONG BUY ─────►│◄────── BUY ──────►│◄───── HOLD ─────►│◄──── AVOID / SELL ────►│
  │     CMP ≤ Pessimistic  │  CMP ≤ Base       │  CMP ≤ Optimistic│   CMP > Optimistic     │
  │     (MOS ≥ 20%)        │  (Positive Upside)│  (Fair Value)    │   (Overvalued)         │
```

### 5.2 Actionable Verdict Classification

| Upside % Range | Verdict Code | Terminal Display | Action Strategy |
| :--- | :--- | :--- | :--- |
| $\ge +30.0\%$ | `DEEPLY_UNDERVALUED` | 🟢🟢 `DEEPLY UNDERVALUED` | Aggressive accumulation; maximum portfolio sizing |
| $+12.0\%$ to $+30.0\%$ | `UNDERVALUED` | 🟢 `UNDERVALUED` | Standard accumulation; strong margin of safety |
| $-10.0\%$ to $+12.0\%$ | `FAIRLY_VALUED` | 🟡 `FAIRLY VALUED` | Hold existing positions; do not chase new entries |
| $-25.0\%$ to $-10.0\%$ | `OVERVALUED` | 🟠 `OVERVALUED` | Trim allocation; tighten stop-losses |
| $< -25.0\%$ | `DEEPLY_OVERVALUED` | 🔴 `DEEPLY OVERVALUED` | Avoid new purchases; exit or reallocate capital |

---

## 6. Cross-Strategy Integration & Table Column Enrichment (Mode A)

This section directly addresses the user's primary requirement: **injecting Fair Price and Upside into the existing Multibagger and Early Multibagger tables and reports**.

### 6.1 Enriched Multibagger Terminal Table
In [`pkg/stockpicker/io.go:PrintMultibaggerTable()`](file:///Users/raghavgarg/Projects/myGo/mycase/pkg/stockpicker/io.go#L172), two new columns — `Fair Price` and `Upside` — are added without disrupting existing formatting:

```text
=============================================================================================================================
             TOP 15 SELECTED MULTIBAGGER STOCKS FROM NIFTYTOTALMARKET (2026-09-26 EOD)               
=============================================================================================================================
Ticker           | CMP      | Fair Price | Upside   | MOS Verdict        | 3Y CAGR  | DSO (L/P)  | Score  | Final Weight
-----------------------------------------------------------------------------------------------------------------------------
NSE:VARROC       | ₹  654.0 | ₹  982.0   | +50.2%   | DEEPLY UNDERVALUED | 24.1%    | 52/58      |  87.4  | 0.0800
NSE:LTFOODS      | ₹  389.5 | ₹  542.0   | +39.2%   | DEEPLY UNDERVALUED | 18.4%    | 48/51      |  82.1  | 0.0750
NSE:NETWEB       | ₹2,814.0 | ₹3,690.0   | +31.1%   | DEEPLY UNDERVALUED | 41.2%    | 38/44      |  79.6  | 0.0700
NSE:CHENNPETRO   | ₹  218.0 | ₹  276.0   | +26.6%   | UNDERVALUED        | 16.8%    | 22/24      |  74.3  | 0.0650
NSE:MANORAMA     | ₹  982.0 | ₹1,198.0   | +22.0%   | UNDERVALUED        | 28.5%    | 61/65      |  71.8  | 0.0650
NSE:CASTROLIND   | ₹  219.0 | ₹  243.0   | +11.0%   | FAIRLY VALUED      |  8.2%    | 34/35      |  58.7  | 0.0550
NSE:THYROCARE    | ₹  782.0 | ₹  825.0   |  +5.5%   | FAIRLY VALUED      | 14.1%    | 29/30      |  52.4  | 0.0500
NSE:DIXON        | ₹14,200  | ₹ 9,850    | -30.6%   | ⚠️ VAL_STRETCHED   | 34.0%    | 45/48      |  48.1  | 0.0400
-----------------------------------------------------------------------------------------------------------------------------
CASH_RESERVE     | -        | -          | -        | -                  | -        | -          | -      | 0.4450
-----------------------------------------------------------------------------------------------------------------------------
Total Weight     |          |            |          |                    |          |            |        | 1.0000
=============================================================================================================================
```

> [!NOTE]
> Notice `NSE:DIXON`: While passing growth filters with 34% 3Y CAGR, its CMP (₹14,200) trades far above Fair Price (₹9,850). The engine immediately flags `⚠️ VAL_STRETCHED`, alerting the investor that the stock has run ahead of its fundamentals.

### 6.2 Enriched Early Multibagger (Pre-Breakout) Terminal Table
In [`pkg/stockpicker/io.go:PrintEarlyMultibaggerTable()`](file:///Users/raghavgarg/Projects/myGo/mycase/pkg/stockpicker/io.go#L110), Fair Price and Upside provide fundamental validation for technical breakout setups:

```text
=================================================================================================================================================
                   TOP 10 SELECTED EARLYMB (PRE-BREAKOUT) STOCKS FROM MICROSOMALL (2026-09-26 EOD)               
=================================================================================================================================================
Ticker           | Score  | CMP      | Fair Price | Upside   | 1M RS    | 3M RS    | Base Wks | VCP Ratio  | RVOL Z  | 52W Prox | Final Weight
-------------------------------------------------------------------------------------------------------------------------------------------------
NSE:VARROC       |  88.4  | ₹  654.0 | ₹  982.0   | +50.2%   | +12.4%   | +28.1%   | 14       | 0.42       | +2.4    | 94.2%    | 0.0800
NSE:LTFOODS      |  83.2  | ₹  389.5 | ₹  542.0   | +39.2%   |  +8.2%   | +19.4%   | 18       | 0.38       | +1.8    | 96.1%    | 0.0750
NSE:NETWEB       |  80.1  | ₹2,814.0 | ₹3,690.0   | +31.1%   | +15.1%   | +34.2%   |  9       | 0.48       | +3.1    | 91.5%    | 0.0700
-------------------------------------------------------------------------------------------------------------------------------------------------
```

### 6.3 Automated Qualitative Scuttlebutt Research Injection
In [`pkg/stockpicker/io.go:PrintScuttlebutt()`](file:///Users/raghavgarg/Projects/myGo/mycase/pkg/stockpicker/io.go#L256), each stock gets a dedicated intrinsic valuation analysis section:

```text
=========================================================================
         AUTOMATED SCUTTLEBUTT & LIVE NSE QUALITATIVE RESEARCH REPORT
=========================================================================
1. NSE:VARROC      | Sector: Consumer Cyclical    | Market Cap: 10,024Cr
   ----------------------------------------------------------------------
   [Intrinsic Valuation & MOS] : Fair Price: ₹982.0 (CMP: ₹654.0, Upside: +50.2%)
                                 MOS Band: Pessimistic ₹786.0 | Base ₹982.0 | Optimistic ₹1,129.0
                                 Verdict: DEEPLY UNDERVALUED (High Conviction, Model CV: 0.14)
   [Valuation Model Breakdown] : DCF (30%): ₹1,124.0 | EPV (25%): ₹892.0 | Graham (15%): ₹756.0
                                 RelComps (15%): ₹1,042.0 | QualityPEG (15%): ₹1,098.0
   [Live NSE Result Schedule]  : 24-05-26 -> 24-08-26
   [Live NSE Delivery Vol %]   : 62.4% deliverable accumulation (Last Business Day: 25-09-26)
   ...
```

---

## 7. Standalone Mode: 100-Point Scoring Matrix & Portfolio Construction (Mode B)

When invoked directly via `--method fairprice`, stocks that pass the safety pre-screen are scored and ranked using a dedicated **100-point multi-factor matrix**:

```
                       ┌─────────────────────────────────────────────────────────┐
                       │          100-POINT FAIR PRICE SCORING MATRIX            │
                       ├────────────────────────────┬────────────────────────────┤
                       │ Pillar I:   Valuation (40) │ Pillar II:  Consensus (20) │
                       │   • Upside %        (25)   │   • Model Agreement  (10)  │
                       │   • MOS Position    (15)   │   • Model CV (Conv.) (10)  │
                       ├────────────────────────────┼────────────────────────────┤
                       │ Pillar III: Quality (25)   │ Pillar IV:  Growth (15)    │
                       │   • ROCE            (10)   │   • 3Y Revenue CAGR   (8)  │
                       │   • CFO / PAT        (8)   │   • YoY EPS Growth    (7)  │
                       │   • D/E Leverage     (7)   │                            │
                       └────────────────────────────┴────────────────────────────┘
```

### 7.1 Detailed Pillar Scoring Rules

| Pillar | Sub-Metric | Points | Evaluation Function | Higher is Better? |
| :--- | :--- | :--- | :--- | :--- |
| **I. Valuation Discount** (40 pts) | **Ensemble Upside (%)** | 25 pts | Min-Max normalized upside across cohort | Yes |
| | **MOS Band Position** | 15 pts | CMP $\le$ Pessimistic: 15 pts<br>Pessimistic $<$ CMP $\le$ Base: 10 pts<br>Base $<$ CMP $\le$ Optimistic: 5 pts<br>CMP $>$ Optimistic: 0 pts | Categorical |
| **II. Model Consensus** (20 pts) | **Model Undervalued Agreement** | 10 pts | Count of models where $\text{FairPrice}_m > \text{CMP}$:<br>5/5: 10 pts \| 4/5: 8 pts \| 3/5: 6 pts \| 2/5: 3 pts \| $\le 1$: 0 pts | Discrete |
| | **Convergence (Low CV)** | 10 pts | $10 \times \max\left(0, \, 1 - \frac{\text{Model CV}}{0.40}\right)$ | Inverted (Lower CV is better) |
| **III. Quality Anchor** (25 pts) | **Capital Efficiency (ROCE)** | 10 pts | Min-Max normalized ROCE | Yes |
| | **Cash Realization (CFO/PAT)**| 8 pts | Min-Max normalized CFO to Net Income ratio | Yes |
| | **Balance Sheet Safety (D/E)**| 7 pts | Min-Max normalized Debt-to-Equity ratio | Inverted (Lower D/E is better) |
| **IV. Growth Trajectory** (15 pts)| **3-Year Revenue CAGR** | 8 pts | Min-Max normalized Revenue CAGR | Yes |
| | **YoY Earnings Acceleration** | 7 pts | Min-Max normalized Net Income growth | Yes |

### 7.2 Safety Pre-Screening Filters (Stage 1)
Designed to cast the widest net while aggressively eliminating illiquid counters, shell companies, and distressed value traps:

```go
func isEligibleFairPrice(
    t string,
    f yfinance.Fundamentals,
    hardFilters *config.HardFilters,
    closes []float64,
    stats *FilterStats,
    mkt marketfmt.Market,
) (bool, string) {
    // 1. Broad Market Cap: ₹500 Cr to ₹50,000 Cr (or configured max)
    if f.MarketCap < minMCap || f.MarketCap > maxMCap {
        return false, "Market Cap limit check failed"
    }
    
    // 2. Minimum Liquidity: ADV >= ₹50 Lakhs (with fallback to historical close)
    price := f.RegularPrice
    if price == 0 && len(closes) > 0 {
        price = closes[len(closes)-1]
    }
    adv := f.AverageVolume * price
    if adv < minADV {
        return false, "ADV check failed"
    }
    
    // 3. Solvency & Integrity: Non-negative Net Worth & Non-Zero Revenue
    if f.PBRatio <= 0 {
        return false, "Negative or zero book value (negative net worth)"
    }
    if f.TTMRevenue <= 0 && len(f.AnnualRevenue) == 0 {
        return false, "Zero or unverified revenue"
    }
    
    // 4. Data Completeness: At least 2 years annual statements
    if len(f.AnnualRevenue) < 2 && len(f.AnnualOperatingIncome) < 2 {
        return false, "Insufficient annual statements (< 2 years)"
    }
    
    // 5. Quality & Solvency Pre-Filter: Value Trap Prevention
    // Chronically loss-making companies (e.g. ABFRL, TMPV, SWIGGY, OLAELEC)
    // must not qualify merely because distressed stock prices create superficial upside.
    if f.Sector == "Financial Services" {
        if f.NetIncome <= 0 && f.ROE <= 0 {
            return false, "Loss-making financial institution (Net Income <= 0 and ROE <= 0)"
        }
    } else {
        nOp := len(f.AnnualOperatingIncome)
        hasPositiveOpIncome := nOp > 0 && f.AnnualOperatingIncome[nOp-1].Value > 0
        roceVal, okROCE := GetLatestROCE(&f)
        hasPositiveROCE := okROCE && roceVal > 0
        if !hasPositiveOpIncome && !hasPositiveROCE && f.NetIncome <= 0 {
            return false, "Chronic operating loss / negative ROCE (distressed value trap)"
        }
    }
    
    return true, ""
}
```

### 7.3 Portfolio Construction & Sector Exposure Limits
- **Top N Selection**: Default 15 stocks.
- **Max Stocks Per Sector**: Hard limit of **3 stocks per sector** (ensures diversification).
- **Max Sector Portfolio Weight**: Maximum **25%** aggregate allocation to any single sector.
- **Max Single Stock Weight**: Hard cap of **8.0%** per stock.
- **Cash Reserve**: If fewer than Top N qualify or score hurdles are not met, excess allocation automatically spills into `CASH_RESERVE`.
- **Rejected Candidate Terminal UX (Cutoff Rank 40)**: When screening large index universes (e.g. Nifty Total Market with 750 stocks), dumping 700+ rejected rows overwhelms the terminal. The reporting engine truncates the rejected candidates table to `rank <= 40` (or `topN + 35`), displaying a concise summary line (`... and 685 additional lower-ranked candidates (rank > 40) omitted`). Sector-cap drops among top contenders (e.g. `CSBBANK`, `J&KBANK` dropped when 3/3 BFSI slots are filled) remain completely transparent.

---

## 8. Concrete Implementation Architecture & Code Blueprints

### 8.1 Struct Definitions: `pkg/yfinance/metrics_fairprice.go`

```go
package yfinance

import (
	"fmt"
	"math"
	"sort"
)

// FairPriceResult encapsulates the complete intrinsic valuation breakdown for a stock.
type FairPriceResult struct {
	// Individual Model Fair Prices (nil indicates incomputable)
	DCFFairPrice      *float64 `json:"dcf_fair_price,omitempty"`
	EPVFairPrice      *float64 `json:"epv_fair_price,omitempty"`
	GrahamNumber      *float64 `json:"graham_number,omitempty"`
	RelativeFairPrice *float64 `json:"relative_fair_price,omitempty"`
	PEGFairPrice      *float64 `json:"peg_fair_price,omitempty"`

	// Ensemble Aggregates
	RawEnsembleFairPrice float64 `json:"raw_ensemble_fair_price"` // Unshrunk raw weighted ensemble price
	EnsembleFairPrice    float64 `json:"ensemble_fair_price"`     // Bayesian confidence-shrunk fair price
	CMP                  float64 `json:"cmp"`
	UpsidePct            float64 `json:"upside_pct"`
	Verdict              string  `json:"verdict"` // DEEPLY_UNDERVALUED, UNDERVALUED, FAIRLY_VALUED, OVERVALUED, DEEPLY_OVERVALUED
	ValidModelCount      int     `json:"valid_model_count"`
	ModelCV              float64 `json:"model_cv"`
	ConfidenceScore      float64 `json:"confidence_score"` // 0.0 to 100.0%

	// Margin of Safety Band
	PessimisticFairPrice float64 `json:"pessimistic_fair_price"` // Base * 0.80
	OptimisticFairPrice  float64 `json:"optimistic_fair_price"`  // Base * 1.15

	// Diagnostic Metrics
	WACCUsed          float64 `json:"wacc_used"`
	SustainableGrowth float64 `json:"sustainable_growth"`
	Clamped           bool    `json:"clamped"`
}

// CalculateDCFFairPrice implements the 2-Stage Discounted Cash Flow per share.
func CalculateDCFFairPrice(f *Fundamentals, wacc, terminalGrowth float64) (*float64, bool) {
	if f == nil || f.RegularPrice <= 0 || f.MarketCap <= 0 {
		return nil, false
	}
	// Bypass BFSI: Free cash flow is economically invalid for banking & financial institutions
	if f.Sector == "Financial Services" {
		return nil, false
	}
	nOp := len(f.AnnualOperatingIncome)
	if f.NetIncome <= 0 && nOp > 0 && f.AnnualOperatingIncome[nOp-1].Value <= 0 {
		return nil, false
	}

	shares := f.MarketCap / f.RegularPrice
	fcf0 := determineFCF0Waterfall(f)
	if fcf0 <= 0 {
		return nil, false
	}

	// Sanity cap: Sustainable recurring FCF cannot indefinitely exceed normalized operating earnings
	// or net income (prevents one-off asset sales, HAM project concessions, or working-capital spikes from blowing up DCF).
	if f.NetIncome > 0 && fcf0 > 2.0*f.NetIncome {
		fcf0 = 2.0 * f.NetIncome
	}
	if nOp > 0 && f.AnnualOperatingIncome[nOp-1].Value > 0 && fcf0 > 1.8*f.AnnualOperatingIncome[nOp-1].Value {
		fcf0 = 1.8 * f.AnnualOperatingIncome[nOp-1].Value
	}

	// 5-year high-growth projection + Terminal Value
	g := determineGrowthRateWithROCECap(f)
	pvStage1 := projectCashFlowsPV(fcf0, g, wacc, 5)
	pvTerminal := calculateTerminalValuePV(fcf0, g, wacc, terminalGrowth, 5)

	ev := pvStage1 + pvTerminal
	cash := 0.0
	if f.OperatingCashflow > 0 {
		cash = f.OperatingCashflow * 0.50
	}
	equityVal := ev - f.TotalDebt + cash
	if equityVal <= 0 {
		return nil, false
	}

	fairPrice := equityVal / shares
	return &fairPrice, true
}

// CalculateEPVPerShare implements Bruce Greenwald's Earnings Power Value per share.
func CalculateEPVPerShare(f *Fundamentals, wacc float64) (*float64, bool) {
	if f == nil || f.RegularPrice <= 0 || f.MarketCap <= 0 {
		return nil, false
	}
	shares := f.MarketCap / f.RegularPrice

	// BFSI Direct Capitalization Branch
	if f.Sector == "Financial Services" {
		if f.NetIncome <= 0 {
			return nil, false
		}
		equityEPV := f.NetIncome / wacc
		fairPrice := equityEPV / shares
		return &fairPrice, true
	}

	// Non-Financial Corporate EPV
	epvEnterprise, _, ok := CalculateEPV(f, wacc)
	if !ok || epvEnterprise <= 0 {
		return nil, false
	}
	cash := 0.0
	if f.OperatingCashflow > 0 {
		cash = f.OperatingCashflow * 0.50
	}
	equityEPV := epvEnterprise - f.TotalDebt + cash
	if equityEPV <= 0 {
		return nil, false
	}
	fairPrice := equityEPV / shares
	return &fairPrice, true
}

// normalizedEPS calculates the cyclically adjusted EPS using Graham & Dodd principles.
func normalizedEPS(f *Fundamentals, shares float64) float64 {
	if f == nil || shares <= 0 { return 0.0 }
	latestEPS := f.NetIncome / shares
	if latestEPS <= 0 || f.Sector == "Financial Services" {
		return latestEPS
	}
	nOp := len(f.AnnualOperatingIncome)
	if nOp < 2 { return latestEPS }

	sumEBIT, count := 0.0, 0
	for i := max(0, nOp-3); i < nOp; i++ {
		sumEBIT += f.AnnualOperatingIncome[i].Value
		count++
	}
	avgEBIT := sumEBIT / float64(count)

	// If historically loss-making, treat single-year positive EPS as unproven windfall
	if avgEBIT <= 0 {
		return latestEPS * 0.25
	}
	normEPS := (avgEBIT * 0.70) / shares
	if latestEPS > normEPS*2.0 {
		return 0.70*normEPS + 0.30*latestEPS
	}
	return latestEPS
}

// CalculateGrahamNumber computes Benjamin Graham's safety floor value.
func CalculateGrahamNumber(f *Fundamentals) (*float64, bool) {
	if f == nil || f.RegularPrice <= 0 || f.MarketCap <= 0 || f.PBRatio <= 0 || f.NetIncome <= 0 {
		return nil, false
	}
	shares := f.MarketCap / f.RegularPrice
	eps := normalizedEPS(f, shares)
	bvps := f.RegularPrice / f.PBRatio
	if eps <= 0 || bvps <= 0 { return nil, false }

	graham := math.Sqrt(22.5 * eps * bvps)
	return &graham, true
}

// CalculateFairPrice computes the full 5-model ensemble with Bayesian Shrinkage.
func CalculateFairPrice(f *Fundamentals, medians SectorMedians) (*FairPriceResult, error) {
	cmp := f.RegularPrice
	// ... individual model executions ...

	// Adaptive weighting across valid models
	rawEnsembleFairPrice := computeAdaptiveWeightedAverage(models)
	cv := computeModelCV(validPrices)
	confScore := computeConfidenceScore(validCount, cv, dataCompleteness)

	// Bayesian Shrinkage towards Market Prior (CMP)
	confWeight := confScore / 100.0
	ensembleFairPrice := cmp + (rawEnsembleFairPrice-cmp)*confWeight

	// Boundary Clamping [0.20 * CMP, 4.00 * CMP]
	ensembleFairPrice, clamped := applyBoundaryClamps(ensembleFairPrice, cmp)

	return &FairPriceResult{
		RawEnsembleFairPrice: rawEnsembleFairPrice,
		EnsembleFairPrice:    ensembleFairPrice,
		CMP:                  cmp,
		UpsidePct:            ((ensembleFairPrice - cmp) / cmp) * 100.0,
		Verdict:              computeVerdict(upsidePct),
		ConfidenceScore:      confScore,
		// ...
	}, nil
}
```

### 8.2 Scoring & Selection: `pkg/stockpicker/scoring_fairprice.go`

```go
package stockpicker

// ScoreFairPrice evaluates and ranks candidates using the 100-point fair price matrix.
func ScoreFairPrice(
	ctx context.Context,
	activeKeys []string,
	fundamentals map[string]yfinance.Fundamentals,
	fairPrices map[string]*yfinance.FairPriceResult,
	hardFilters *config.HardFilters,
) map[string]float64 {
	// ... collects upsides, cfos, des, revCAGRs ...
	
	// Capital Efficiency: Evaluate ROE for Financial Services; ROCE for Corporates
	for _, t := range activeKeys {
		f := fundamentals[t]
		var roceVal float64
		if f.Sector == "Financial Services" {
			if f.ROE > 0 {
				roceVal = f.ROE
			} else if f.ReturnOnAssets > 0 {
				roceVal = f.ReturnOnAssets * 10.0
			}
		} else {
			var ok bool
			roceVal, ok = GetLatestROCE(&f)
			if !ok || roceVal <= 0 {
				roceVal = f.ROE
			}
		}
		roces[t] = roceVal
	}

	// 100-Point Scoring across Pillars I, II, III, and IV
	// ...
	return scores
}
```

### 8.3 Integration in `pkg/stockpicker/run.go`
In `RunWithResult()`, the fair price engine runs for all active keys:

```go
// Compute Fair Price results for all surviving candidates
fairPriceResults := make(map[string]*yfinance.FairPriceResult)
sectorMedians := yfinance.ComputeCohortSectorMedians(activeKeys, fundamentals)
for _, t := range activeKeys {
    f := fundamentals[t]
    if fp, err := yfinance.CalculateFairPrice(&f, sectorMedians[f.Sector]); err == nil {
        fairPriceResults[t] = fp
    }
}

// Pass fairPriceResults into PrintMultibaggerTable, PrintEarlyMultibaggerTable, 
// and record on DriverMetrics for DuckDB persistence!
```

---

## 9. DuckDB Persistence Schema & Historical Tracking

### 9.1 Table: `pit_fairprice_scores`
Persists the complete valuation snapshot for every stock evaluated during any run:

```sql
CREATE TABLE IF NOT EXISTS pit_fairprice_scores (
    as_of_date              DATE NOT NULL,
    index_name              VARCHAR NOT NULL,
    method                  VARCHAR NOT NULL, -- 'fairprice', 'multibagger', 'earlymb'
    ticker                  VARCHAR NOT NULL,
    sector                  VARCHAR,
    cmp                     DOUBLE NOT NULL,
    raw_ensemble_fair_price DOUBLE NOT NULL,
    fair_price_ensemble     DOUBLE NOT NULL,
    fair_price_dcf          DOUBLE,
    fair_price_epv          DOUBLE,
    fair_price_graham       DOUBLE,
    fair_price_relative     DOUBLE,
    fair_price_peg          DOUBLE,
    upside_pct              DOUBLE NOT NULL,
    verdict                 VARCHAR NOT NULL,
    pessimistic_fp          DOUBLE NOT NULL,
    optimistic_fp           DOUBLE NOT NULL,
    valid_model_count       INTEGER NOT NULL,
    model_cv                DOUBLE NOT NULL,
    confidence_score        DOUBLE NOT NULL,
    wacc_used               DOUBLE NOT NULL,
    raw_score               DOUBLE,
    final_weight            DOUBLE,
    created_at              TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (as_of_date, index_name, method, ticker)
);
```

### 9.2 Analytical View: `v_fairprice_alpha_tracking`
Compares predicted fair value vs realized forward price performance to evaluate model alpha:

```sql
CREATE OR REPLACE VIEW v_fairprice_alpha_tracking AS
SELECT 
    fp.as_of_date,
    fp.ticker,
    fp.sector,
    fp.method,
    fp.cmp AS entry_cmp,
    fp.fair_price_ensemble,
    fp.upside_pct,
    fp.verdict,
    fp.model_cv,
    -- 21-Day Forward Realized Price
    LEAD(fp.cmp, 1) OVER (PARTITION BY fp.ticker, fp.method ORDER BY fp.as_of_date) AS realized_cmp_next,
    ((LEAD(fp.cmp, 1) OVER (PARTITION BY fp.ticker, fp.method ORDER BY fp.as_of_date) - fp.cmp) / fp.cmp) * 100.0 AS forward_21d_return,
    -- Directional Accuracy Check
    CASE 
        WHEN fp.upside_pct > 15.0 AND LEAD(fp.cmp, 1) OVER (PARTITION BY fp.ticker, fp.method ORDER BY fp.as_of_date) > fp.cmp THEN 1
        WHEN fp.upside_pct < -10.0 AND LEAD(fp.cmp, 1) OVER (PARTITION BY fp.ticker, fp.method ORDER BY fp.as_of_date) < fp.cmp THEN 1
        ELSE 0
    END AS directionally_accurate
FROM pit_fairprice_scores fp
ORDER BY fp.as_of_date DESC, fp.upside_pct DESC;
```

---

## 10. Configuration Schema (`config/mfs.json`)

```json
{
  "filters": {
    "fairprice": {
      "min_market_cap": 5000000000,
      "max_market_cap": 50000000000000,
      "min_adv": 5000000,
      "require_positive_net_worth": true,
      "min_annual_statements": 2,
      "min_valid_models": 2,

      "dcf_base_weight": 0.30,
      "epv_base_weight": 0.25,
      "graham_base_weight": 0.15,
      "relative_base_weight": 0.15,
      "peg_base_weight": 0.15,

      "dcf_terminal_growth_india": 0.050,
      "dcf_terminal_growth_us": 0.025,
      "dcf_projection_years": 5,

      "mos_discount": 0.20,
      "optimism_premium": 0.15,

      "max_stocks_per_sector": 3,
      "max_sector_weight_cap": 0.25,
      "max_stock_weight_cap": 0.08,

      "score_weight_upside": 25.0,
      "score_weight_mos_band": 15.0,
      "score_weight_agreement": 10.0,
      "score_weight_convergence": 10.0,
      "score_weight_roce": 10.0,
      "score_weight_cfo_pat": 8.0,
      "score_weight_debt_safety": 7.0,
      "score_weight_revenue_cagr": 8.0,
      "score_weight_earnings_accel": 7.0
    }
  }
}
```

---

## 11. CLI Execution Runbook

### A. Run Standalone Fair Price Screener
```bash
# Screen Nifty Total Market (750 stocks) for top 15 deeply undervalued stocks
./dist/mycase pick --index niftytotalmarket --method fairprice --top 15

# Screen SmallCap 250 for value opportunities
./dist/mycase pick --index small250 --method fairprice --top 10

# Screen custom portfolio or watchlist
./dist/mycase pick --file data/microsmall.csv --method fairprice --top 15
```

### B. Run Growth Strategies with Automatic Fair Price Enrichment
```bash
# Multibagger run with enriched Fair Price and Upside % columns
./dist/mycase pick --index niftytotalmarket --method multibagger --top 15

# Early Multibagger run with pre-breakout technicals AND intrinsic valuation
./dist/mycase pick --file data/microsmall.csv --method earlymb --top 10
```

### C. Inspect Single Ticker Fair Price History
```bash
# Query DuckDB point-in-time valuation trajectory for a specific stock
./dist/mycase pit stats --ticker VARROC --method fairprice
```

---

## 12. Verification & Testing Strategy

To ensure zero regressions and robust mathematical behavior:

1. **Synthetic Unit Test Matrix** (`pkg/yfinance/metrics_fairprice_test.go`):
   - **Case A: Debt-Free High-ROCE Compounder**: Verify all 5 models execute, Low CV (< 0.15), High Confidence Score (> 85%).
   - **Case B: Heavy CapEx / Negative FCF Turnaround**: Verify DCF is gracefully skipped, weights adaptively redistribute across surviving 4 models, and no divide-by-zero occurs.
   - **Case C: Asset-Light Tech / Diagnostic**: Verify Graham Number yields conservative floor without dragging ensemble below reasonable boundaries.
   - **Case D: Extreme Cyclical Outlier**: Verify boundary clamps ($0.2\times$ to $4.0\times$ CMP) successfully contain explosive DCF growth estimates.

2. **Invariants to Enforce in Code**:
   - $\sum W_m^{\text{effective}} \equiv 1.0000$ (Weight conservation across valid models).
   - $\sum \text{FinalPortfolioWeights} + \text{CashReserve} \equiv 1.0000$.
   - No `NaN` or `+Inf` values permitted to propagate into DuckDB or terminal tables.
   - Sector allocation cannot exceed 25% or 3 stocks under any market condition.

---

## 13. Production Real-Data Anomaly Resolutions & Edge Cases (750-Stock Index Audit)

When the Fair Price Engine was tested with real market data across the entire 750-stock **Nifty Total Market** universe, multiple accounting distortions, financial engineering edge cases, and pipeline anomalies emerged. This section details each case study, the root financial flaw, and the permanent mathematical resolution implemented in production.

```
┌────────────────────────────────────────────────────────────────────────────────────────────────────────┐
│                        750-STOCK INDEX REAL-DATA AUDIT: ANOMALY MATRIX & RESOLUTIONS                   │
├────┬───────────────────────┬──────────────────────────┬───────────────────────┬────────────────────────┤
│ No │ Anomaly Category      │ Impacted Constituents    │ Raw Failure Mode      │ Algorithmic Resolution │
├────┼───────────────────────┼──────────────────────────┼───────────────────────┼────────────────────────┤
│ 1  │ BFSI Corporate DCF &  │ DCBBANK, PNB, SOUTHBANK, │ DCF hit ₹5,366 vs     │ Mandatory DCF bypass   │
│    │ EV/EBITDA Distortions │ BANKINDIA, PFC           │ CMP ₹214 (+2,400%)    │ & Direct Cap EPV       │
├────┼───────────────────────┼──────────────────────────┼───────────────────────┼────────────────────────┤
│ 2  │ Concession & Asset    │ PNCINFRA, ASHOKA         │ DCF hit ₹2,925 vs     │ FCF0 sanity cap at     │
│    │ Monetization Spikes   │                          │ CMP ₹135 (+2,060%)    │ min(2.0x PAT, 1.8x EBIT│
├────┼───────────────────────┼──────────────────────────┼───────────────────────┼────────────────────────┤
│ 3  │ Milestone Windfall &  │ SPARC (Sun Pharma Adv    │ #1 Rank via single-yr │ Graham & Dodd 3Y       │
│    │ Biopharma Outliers    │ Research Company)        │ out-licensing receipt │ Normalized EPS         │
├────┼───────────────────────┼──────────────────────────┼───────────────────────┼────────────────────────┤
│ 4  │ Low-Consensus Model   │ RPOWER                   │ 2-model ensemble CV   │ Empirical Bayesian     │
│    │ Dispersion (High CV)  │                          │ > 0.40 capped at +300%│ Shrinkage towards CMP  │
├────┼───────────────────────┼──────────────────────────┼───────────────────────┼────────────────────────┤
│ 5  │ Distressed Value Trap │ ABFRL, TMPV, SWIGGY,     │ Distressed stock price│ Quality & Solvency     │
│    │ Infiltration          │ OLAELEC                  │ created false upside  │ Pre-Filters at Stage 1 │
├────┼───────────────────────┼──────────────────────────┼───────────────────────┼────────────────────────┤
│ 6  │ Off-Hours Ingestion   │ Multiple Indian equities │ RegularPrice = 0      │ 3-Stage CMP Fallback   │
│    │ Price Incompleteness  │ from Yahoo quoteSummary  │ caused zero divides   │ Waterfall              │
├────┼───────────────────────┼──────────────────────────┼───────────────────────┼────────────────────────┤
│ 7  │ Report Flood & Double │ Terminal UX / All 750    │ 709 rejected rows &   │ Top 40 Rank Truncation │
│    │ Percentage Multiplier │ Index Constituents       │ 7000% confidence text │ & Format String Fix    │
└────┴───────────────────────┴──────────────────────────┴───────────────────────┴────────────────────────┘
```

---

### Case Study 1: BFSI Structural Accounting Divergence (`DCBBANK`, `PNB`, `SOUTHBANK`)

#### 1.1 The Anomaly
When screening the full index, commercial banks and NBFCs dominated the Top 15, all capped at the hard ceiling of **+300.0% upside (4.0×CMP)**:
- `NSE:DCBBANK`: CMP ₹214.1 $\rightarrow$ DCF produced **₹5,366.5** (+2,406%); Relative Comps produced **₹2,196.2**.
- `NSE:PNB` & `NSE:SOUTHBANK`: Similar four-digit DCF model blowouts.
- Capital efficiency drivers reported `ROCE: 0.0%` for all banking institutions due to Yahoo Finance reporting limitations.

#### 1.2 The Root Cause
1. **Free Cash Flow Structural Inapplicability**: For banking institutions, customer deposits are booked as operating cash inflows, while loan disbursements are operating cash outflows. Changes in working capital represent credit underwriting, not working capital management. Standard corporate FCF ($CFO - \text{CapEx}$) is economically meaningless for lenders.
2. **Enterprise Value Distortion**: Bank deposits and borrowings are operational liabilities, not financing debt. In standard EV/EBITDA comps ($EV = \text{MarketCap} + \text{Debt} - \text{Cash}$), treating deposits as corporate debt inflates Enterprise Value to irrational levels.
3. **ROCE Inapplicability**: Banks operate on financial leverage where Total Assets exceed Net Worth by 8x to 12x. Capital Employed is structurally inapplicable.

#### 1.3 The Resolution
1. **Mandatory DCF Sector Bypass**: If `f.Sector == "Financial Services"`, `CalculateDCFFairPrice` returns `nil, false`. The 30% DCF weight is dynamically redistributed to EPV, Graham Number, Relative Comps, and Quality PEG.
2. **EV/EBITDA Bypass in Relative Comps**: In `ComputeCohortSectorMedians` and `ComputeRelativeValuation`, EV/EBITDA is explicitly bypassed for `Financial Services`. Sector comps rely strictly on P/E and P/B multiples.
3. **Direct Capitalization EPV for BFSI**: Instead of deducting debt from enterprise value, BFSI EPV directly capitalizes sustainable Net Income:
   $$\text{Equity EPV}_{\text{BFSI}} = \frac{\text{Net Income}}{\text{WACC}_{\text{BFSI}}}$$
4. **ROE / ROA Capital Efficiency Fallback**: For Financial Services, ROCE falls back to `f.ROE` (or `f.ReturnOnAssets * 10.0`). If Yahoo reports 0, it derives ROE from $\frac{\text{NetIncome}}{\text{MarketCap}/\text{PBRatio}}$. Terminal reports display `ROE: %.1f%%` instead of `ROCE: 0.0%`.

---

### Case Study 2: Infrastructure Concession & Monetization Windfalls (`PNCINFRA`, `ASHOKA`)

#### 2.1 The Anomaly
`NSE:PNCINFRA` traded at CMP ₹135.4 but generated a raw DCF fair value of **₹2,925.4** (+2,060% upside), hitting the 4.0×CMP boundary clamp.

#### 2.2 The Root Cause
PNC Infratech monetized several Hybrid Annuity Model (HAM) road projects via an InvIT divestment, registering an extraordinary single-year Free Cash Flow of ~₹2,100 Cr against normalized recurring annual net income of ~₹650 Cr. The standard DCF waterfall accepted this liquidation cash inflow as $FCF_0$ and projected it forward at compound growth, treating asset liquidation as perpetual cash generation.

#### 2.3 The Resolution
The engine imposes an **Operating Reality Sanity Cap on $FCF_0$**:
$$FCF_0 \le \min\left(2.0 \times \text{Net Income}, \, 1.8 \times \text{Operating Income}\right) \quad (\text{if NetIncome} > 0 \text{ and OperatingIncome} > 0)$$
If a company's single-year FCF exceeds 2.0x Net Income or 1.8x Operating Income, it is capped at that operational boundary. This preserves legitimate operating cash conversion while completely neutralizing non-recurring divestment spikes.

---

### Case Study 3: Biopharma Milestone Receipt & Turnaround Outliers (`SPARC`)

#### 3.1 The Anomaly
`NSE:SPARC` (Sun Pharma Advanced Research Company) held **Rank 1** in the portfolio with a capped +300.0% upside (₹569.8 Fair Price vs ₹142.4 CMP), despite burning cash and generating negative operating income in FY22, FY23, and FY24.

#### 3.2 The Root Cause
SPARC recorded a single-year out-licensing milestone payment that swung TTM Net Income to positive ₹280 Cr. Standard valuation models (Graham Number, Quality PEG, and Relative P/E) evaluated this single-year windfall at face value, multiplying it by 22.5x Graham ceiling and peer P/E multiples, creating an artificial +300% undervaluation.

#### 3.3 The Resolution
Implemented **Graham & Dodd 3-Year Cyclically Adjusted Normalized EPS (`normalizedEPS`)**:
1. Check 3-year historical average operating income ($\overline{\text{EBIT}}_{3Y}$).
2. **Turnaround / Historical Loss Dampener**: If $\overline{\text{EBIT}}_{3Y} \le 0$ (the company has burned operating cash historically), a sudden positive single-year EPS is treated as an unproven windfall and capped:
   $$\text{normalizedEPS} = \min(\text{latestEPS}, \, 0.25 \times \text{latestEPS})$$
3. **Earnings Spike Smoothing**: If $\overline{\text{EBIT}}_{3Y} > 0$, calculate implied operating EPS:
   $$\text{impliedEPS} = \frac{\overline{\text{EBIT}}_{3Y} \times (1 - T)}{\text{Shares Outstanding}}$$
   If $\text{latestEPS} > 2.0 \times \text{impliedEPS}$, smooth the spike using a 70/30 blend:
   $$\text{normalizedEPS} = 0.70 \times \text{impliedEPS} + 0.30 \times \text{latestEPS}$$
4. Used across **Graham Number**, **Quality PEG**, and **Relative P/E Comps**, neutralizing milestone windfalls.

---

### Case Study 4: Low-Consensus Model Dispersion (`RPOWER`)

#### 4.1 The Anomaly
Turnaround or highly distressed stocks where 3 of 5 models failed (e.g. `RPOWER` with only EPV and Relative Comps valid) exhibited high internal dispersion ($\text{Model CV} \ge 0.40$), yet their raw weighted average generated an extreme +300% theoretical upside.

#### 4.2 The Root Cause
A stock with only 2 surviving models and high CV possesses weak econometric consensus. Treating a 2-model ensemble with the same credibility as a 5-model unanimous compounder creates a false sense of certainty.

#### 4.3 The Resolution
Implemented **Empirical Bayesian Shrinkage towards Market Prior (CMP)**:
$$\text{Ensemble Fair Price} = \text{CMP} + (\text{Raw Ensemble Fair Price} - \text{CMP}) \times \left(\frac{\text{Confidence Score}}{100.0}\right)$$
- If Confidence Score is 90% (5 valid models, CV < 0.15), the ensemble retains 90% of its intrinsic spread.
- If Confidence Score is 40% (2 valid models, CV > 0.40), 60% of the intrinsic spread shrinks back to CMP.
- Fragile, low-consensus stocks are automatically deprioritized in ranking.

---

### Case Study 5: Chronic Loss-Making Value Traps (`ABFRL`, `TMPV`, `SWIGGY`, `OLAELEC`)

#### 5.1 The Anomaly
Chronically loss-making companies passed initial market cap and liquidity screens because their revenue was > 0 and P/B was positive, but their depressed share prices generated superficial upside anomalies.

#### 5.2 The Root Cause
Lack of an explicit solvency and earnings quality hurdle in Stage 1 pre-screening.

#### 5.3 The Resolution
Added **Quality & Solvency Value Trap Pre-Filters** in `isEligibleFairPrice`:
- **Non-Financials**: Must have positive latest operating income OR positive ROCE, and must not have negative operating income across all available annual statements.
- **Financial Services**: Must have $\text{Net Income} > 0$ and $\text{ROE} > 0$ (or positive derived ROE).
- **Result**: In the 750-stock index run, 19 chronic loss-makers were eliminated under `Declining Earnings Trend`, pruning them before scoring.

---

### Case Study 6: Ingestion Regular Market Price Fallback Waterfall

#### 6.1 The Anomaly
When running outside active market hours, Yahoo Finance's `quoteSummary` API frequently returns `regularMarketPrice = 0` for Indian equities, causing divide-by-zero errors when computing shares outstanding ($\text{Shares} = \text{MarketCap} / \text{CMP}$).

#### 6.2 The Resolution
Implemented a 3-tier price fallback waterfall in the ingestion layer:
1. `sd.RegularMarketPrice.Raw` (primary)
2. `fd.CurrentPrice.Raw` (from financial data module)
3. `closes[len(closes)-1]` (latest daily close from 1-year historical prices)
In `CalculateFairPrice`, verified `RegularPrice > 0` before initiating model computation.

---

### Case Study 7: Report UX & Terminal Formatting Polish

#### 7.1 The Anomalies
1. **Wall of Text**: Screening the 750-stock index produced 709 rejected rows in the terminal, drowning out portfolio picks.
2. **Formatting Bug**: Confidence Score was formatted with a double multiplication (`fp.ConfidenceScore * 100.0`), rendering as `7000%` or `8500%`.

#### 7.2 The Resolutions
1. **Cutoff Rank 40 Truncation**: Truncated rejected candidates to `rank <= 40` (or `topN + 35`), displaying a concise summary line (`... and 685 additional lower-ranked candidates (rank > 40) omitted`). Sector-cap drops among top contenders remain completely transparent.
2. **Clean Percentage Formatting**: Standardized confidence string to `%.0f%%` directly from `fp.ConfidenceScore`, rendering clean outputs (`70%`, `85%`).
