# Golden Triangle Strategy (`golden`) — Multi-Strategy Quantitative Convergence Engine

> **Document Version**: v1.0  
> **Date**: September 26, 2026  
> **Strategy Code**: `golden`  
> **CLI Flag**: `mycase golden --analysis` (Default `--index niftytotalmarket`)  
> **Target Systems**: `pkg/golden`, `pkg/pithistory`, `pkg/stockpicker`, DuckDB (`data/mycase.db`)  
> **Cross-Referenced Documents**:  
> - Ch. 12: [Multibagger (`mb`)](12-multibagger.md)  
> - Ch. 13: [Early Multibagger (`earlymb`)](13-early-multibagger.md)  
> - Ch. 26: [Predictive Fair Price (`fairprice`)](26-fairprice.md)  
> - Ch. 27: [Philosophy: Reconciling Growth & Intrinsic Value](27-Philosophy.md)  

---

## 1. Executive Summary & Foundational Mandate

Institutional quantitative research across global and Indian equity markets demonstrates that **single-style strategies inevitably fail during adverse regime shifts**:
* **Momentum/Trend Alone (`earlymb`)** excels during market liquidity expansions, but can blindly enter high-multiple stocks at 75x P/E, resulting in severe 40%–60% drawdowns when liquidity contracts.
* **Fundamental Compounding Alone (`mb`)** discovers companies with pristine ROCE and clean balance sheets, but often enters late in Stage 3 after the story is already fully priced in and widely held.
* **Deep Value Alone (`fairprice`)** preserves downside capital via DCF/EPV cash flow floors, but repeatedly traps capital in cyclical commodity refiners, debt-laden EPCs, or holding company discounts with zero price velocity ("dead money").

The **Golden Triangle (`golden`)** solves this fundamental problem by fusing all three independent analytical disciplines into a unified quantitative synthesis engine:

```
                                  ▲
                       Predictive Fair Price
                       [docs/26-fairprice.md]
                     (Valuation Floor & Gravity)
                           /             \
                          /   THE GOLDEN  \
                         /       CORE      \
                        /   (All 3 Stars)   \
                       /                     \
                      /                       \
   Early Multibagger ◄─────────────────────────► Multibagger
[docs/13-early-multibagger.md]             [docs/12-multibagger.md]
  (Timing & Launchpad)                      (Compounding Quality)
```

---

## 2. The Three Vertices: Formal Definitions

Each vertex in the Golden Triangle represents an orthogonal market dimension:

### Vertex 1: Predictive Fair Price (`fairprice`) — *Valuation Gravity & Downside Anchor*
* **Primary Question**: *"What is the business intrinsically worth in tangible cashflows?"*
* **Metrics**: 5-Model Ensemble Intrinsic Fair Price, Current Market Price (CMP), Upside Percentage ($\text{Upside} = \frac{\text{FairPrice} - \text{CMP}}{\text{CMP}} \times 100\%$), Margin of Safety (MOS) Band (Pessimistic vs. Optimistic), and Model Count ($3\text{--}5$ models).
* **Guarantees**: Protects the investor from paying bubble valuations for hype-driven momentum.

### Vertex 2: Early Multibagger (`earlymb`) — *Pre-Breakout Timing & Accumulation Velocity*
* **Primary Question**: *"Is smart money quietly coiling this stock right now before a Stage-2 expansion?"*
* **Metrics**: Volatility Contraction Pattern (VCP Ratio $< 1.0$), Delivery Volume Delta ($\Delta > +5\%$), Pocket Pivot Intensity, Relative Volume ($RVOL_z$), and Composite Relative Strength ($RS \ge +20\%$).
* **Guarantees**: Provides precision entry timing near pivot lows, minimizing portfolio drawdown risk.

### Vertex 3: Multibagger (`mb`) — *Capital Compounding & Fundamental Moat*
* **Primary Question**: *"Is this business capable of compounding intrinsic value at 20%+ over 3 to 5 years?"*
* **Metrics**: 100-Point Quality Matrix ($\text{ROCE} \ge 18\%$, $\text{ROE} \ge 15\%$, 3Y Revenue CAGR $\ge 15\%$, 3Y PAT CAGR $\ge 18\%$, Debt-to-Equity $< 0.5$, Margin Expansion, and Institutional Footprint).
* **Guarantees**: Ensures the portfolio holds legitimate, high-return-on-capital businesses rather than low-quality junk.

---

## 3. Triangle Regimes & Classification Matrix

Every constituent evaluated across the universe is classified into one of **Four Core Regimes** based on its positioning across the triangle:

```
┌─────────────────────────────────────────────────────────────────────────────────────────────────┐
│                                   THE FOUR TRIANGLE REGIMES                                     │
├────────────────────┬─────────────────────────────┬──────────────────────────────────────────────┤
│ Regime             │ Mathematical Criteria       │ Tactical Interpretation & Portfolio Action   │
├────────────────────┼─────────────────────────────┼──────────────────────────────────────────────┤
│ 1. GOLDEN_CORE     │ • MB Score >= 60.0          │ The Nirvana Setup: High-quality compounder   │
│    (Full Trinity)  │ • EMB Stage-1 = true        │ quietly coiling on the launchpad with an     │
│                    │ • Fair Price Upside >= -20% │ authentic valuation cushion.                 │
│                    │                             │ Action: MAXIMUM POSITION SIZING              │
├────────────────────┼─────────────────────────────┼──────────────────────────────────────────────┤
│ 2. HIGH_VELOCITY   │ • MB Score >= 55.0          │ Elite growth leader with massive momentum,   │
│    (Quality +      │ • EMB Stage-1 = true        │ but carrying a bull-market multiple overhang.│
│     Momentum)      │ • Fair Price Upside < -20%  │ Action: TRADE WITH STRICT TRAILING STOP-LOSS │
├────────────────────┼─────────────────────────────┼──────────────────────────────────────────────┤
│ 3. VALUE_BREAKOUT  │ • Fair Price Upside >= +20% │ Deep-value turnaround showing initial signs  │
│    (Value +        │ • EMB Stage-1 = true        │ of smart-money accumulation and base coiling.│
│     Accumulation)  │ • MB Score < 60.0           │ Action: ASYMMETRIC SPECULATIVE BET           │
├────────────────────┼─────────────────────────────┼──────────────────────────────────────────────┤
│ 4. COMPOUNDER_SALE │ • MB Score >= 65.0          │ Pristine business trading at a discount, but │
│    (Quality +      │ • Fair Price Upside >= +10% │ currently ignored by institutions (no base). │
│     Value)         │ • EMB Stage-1 = false       │ Action: SYSTEMATIC ACCUMULATION (SIP)        │
└────────────────────┴─────────────────────────────┴──────────────────────────────────────────────┘
```

---

## 4. The Golden Composite Score ($\text{GCS}$)

To rank stocks across the entire multi-strategy spectrum, `mycase golden` calculates a normalized **Golden Composite Score (0 to 100)**:

$$\text{GCS} = w_{\text{mb}} \cdot S_{\text{mb}} + w_{\text{emb}} \cdot S_{\text{emb}} + w_{\text{fp}} \cdot S_{\text{fp}}$$

Where:
* $S_{\text{mb}} = \text{Normalized Multibagger Score} \in [0, 100]$ (Default weight: $w_{\text{mb}} = 0.35$).
* $S_{\text{emb}} = \text{Effective Early Multibagger Score} \in [0, 100]$ (Default weight: $w_{\text{emb}} = 0.40$).
* $S_{\text{fp}} = \text{Normalized Valuation Score} \in [0, 100]$ (Default weight: $w_{\text{fp}} = 0.25$).

### The Valuation Score Mapping Function ($S_{\text{fp}}$)
Valuation Upside ($\%$) is bounded and mapped through a sigmoid-like piecewise linear function to prevent deep-value traps (e.g. $+250\%$ upside on a dying PSU) from skewing the composite score:

$$S_{\text{fp}} = \begin{cases}
100 & \text{if } \text{Upside} \ge +50\% \\
50 + (\text{Upside} \times 1.0) & \text{if } -20\% \le \text{Upside} < +50\% \\
30 + ((\text{Upside} + 20) \times 0.6) & \text{if } -50\% \le \text{Upside} < -20\% \\
\max(0, 12 + ((\text{Upside} + 50) \times 0.3)) & \text{if } \text{Upside} < -50\%
\end{cases}$$

This mathematical formulation ensures that:
1. Companies with severe multiple overhangs ($-65\%$ upside) receive minimal valuation points ($S_{\text{fp}} \approx 7.5$).
2. Companies near fair value ($-10\%$ to $+15\%$) receive a solid baseline ($S_{\text{fp}} \approx 40\text{--}65$).
3. Deep-value assets receive high valuation points, but cannot dominate without momentum or quality.

---

## 5. System Architecture & DuckDB Integration

The `pkg/golden` engine operates as an independent, decoupled consumer of the consolidated Point-in-Time database (`data/mycase.db`):

```
┌────────────────────────────────────────────────────────────────────────┐
│                        DUCKDB PERSISTENCE LAYER                        │
├─────────────────────────┬────────────────────────┬─────────────────────┤
│ `pit_candidate_scores`  │ `pit_candidate_scores` │ `pit_fairprice_     │
│ (method = 'earlymb')    │ (method = 'multibagger')│  scores`           │
└────────────┬────────────┴───────────┬────────────┴──────────┬──────────┘
             │                        │                       │
             └────────────────────────┼───────────────────────┘
                                      ▼
                        ┌───────────────────────────┐
                        │   `pkg/golden` ENGINE     │
                        │   • Deduplication         │
                        │   • Point-in-Time Align   │
                        │   • Regime Classification │
                        │   • Composite Scoring     │
                        └─────────────┬─────────────┘
                                      ▼
                        ┌───────────────────────────┐
                        │  TERMINAL RENDER OUTPUT   │
                        │  `mycase golden --analysis`│
                        └───────────────────────────┘
```

### The Analytical Query Pipeline
To guarantee complete reproducibility without point-in-time lookahead bias:
1. Identify the latest `as_of_date` independently across `earlymb`, `multibagger`, and `fairprice`.
2. Extract the highest score and stage-1 filter status per ticker.
3. Deduplicate `pit_fairprice_scores` via `ARG_MAX(..., created_at)`.
4. Perform full-outer and left joins across all 750 index constituents.
5. Classify every stock into its respective regime and sort by $\text{GCS}$.

---

## 6. CLI Command & Operational Workflows

### 6.1 Running the Golden Analysis
```bash
# Default analysis against the full Nifty Total Market (750 stocks)
mycase golden --analysis

# Target specific indices
mycase golden --analysis --index niftysmallcap250
mycase golden --analysis --index nifty500
```

### 6.2 Output Sections in `mycase golden --analysis`
1. **Section 1: Golden Triangle Regime Census**: Cohort distribution count and percentages across `GOLDEN_CORE`, `HIGH_VELOCITY`, `VALUE_BREAKOUT`, and `COMPOUNDER_SALE`.
2. **Section 2: The Golden Core (Full Trinity Candidates)**: Top priority institutional candidates satisfying Quality, Launchpad, and Fair Price cushion.
3. **Section 3: High-Velocity Growth Leaders**: Momentum leaders with multiple overhang warnings and strict trailing stop-loss guidelines.
4. **Section 4: Value Breakout Watchlist**: Deep-value candidates exhibiting early volume accumulation.
5. **Section 5: Valuation Risk Alerts**: High-RS favorites flagged for severe multiple contraction risk ($\text{Upside} \le -60\%$).

---

## 7. Golden Triangle Decision Heuristic (Operator's Card)

```
When reviewing any stock:
1. Is it on the EarlyMB Launchpad?
   ├── NO  ──> Look at Fair Price. If deeply undervalued, place on SIP Watchlist.
   └── YES ──> Check its Multibagger Score.
               ├── < 50 ──> Low-quality spike. Avoid or trade micro-size.
               └── >= 50 ──> Check Fair Price Upside:
                             ├── <= -60% ──> MULTIPLE OVERHANG: Tight trailing stop, small size.
                             ├── -20% to +10% ──> VALUATION CUSHION: Standard position.
                             └── >= +10% ──> GOLDEN CORE: Maximum conviction allocation.
```

---

## 8. The Multibagger Lifecycle on the Golden Triangle

Every legendary multibagger in modern Indian market history (e.g., Titan, Trent, Astral, Dixon, Bajaj Finance) traverses a distinct, predictable quantitative trajectory across the Golden Triangle:

```
                  STAGE 1: TURNAROUND                   STAGE 2: THE GOLDEN CORE
               [ASYMMETRIC VALUE BREAKOUT]                [MAXIMUM CONVICTION ENTRY]
                  • Cheap: Upside > +40%                    • Quality: ROCE > 20%
                  • Volume: Early Delivery Delta            • Base: VCP Ratio < 0.90
                  • Example: PNB, SOUTHBANK                 • Valuation: Upside -15% to +35%
                               │                            • Example: LTFOODS, KRBL
                               ▼                                       ▲
                   ┌──────────────────────┐                           │
                   │  THE GOLDEN TRIANGLE │───────────────────────────┘
                   └──────────────────────┘
                               │
                               ▼
                   STAGE 3: HIGH-VELOCITY                   STAGE 4: THE EXIT
                  [MOMENTUM COMPOUNDING]               [VALUATION BUBBLE / OVERHANG]
                  • Score: RS > +40%                        • Valuation: Upside < -65%
                  • Multiples: Expanding (P/E 50x)          • Multiple Overhang: Extreme
                  • Action: Ride with trailing stop         • Action: Trim / Book Profits
                  • Example: MCX, NAVINFLUOR                • Example: CUPID, SOLARINDS
```

### The 4 Evolutionary Phases:
1. **Phase 1: Asymmetric Value Turnaround (`VALUE_BREAKOUT`)**: The company trades below book value or single-digit P/E due to historical neglect. Smart money begins stealth accumulation (Delivery Delta $> +5\%$, pocket pivots), but retail and institutions are not yet aware.
2. **Phase 2: The Golden Core Nirvana (`GOLDEN_CORE`)**: Earnings inflect, ROCE crosses $18\%$, and the stock forms a tight Volatility Contraction Pattern (VCP). Valuation is still fair ($\text{Upside} \ge -20\%$). **This is where 80% of long-term wealth is generated.**
3. **Phase 3: High-Velocity Compounding (`HIGH_VELOCITY`)**: The story becomes institutional consensus. Multiple expands from 20x to 55x. Momentum is explosive, but intrinsic DCF models begin flagging valuation stretch ($\text{Upside} < -20\%$).
4. **Phase 4: Valuation Bubble Overhang (`VALUATION_RISK`)**: The stock trades at 80x+ P/E. Upside compresses to $\le -65\%$. Any earnings deceleration causes violent multiple contraction.

---

## 9. Macro Regime Elasticity & Market Turn Dynamics

A critical insight of the Golden Triangle is that **the size of the Golden Core expands and contracts elastically with macro breadth**:

$$\text{Active Hurdle Gap} = \text{Base Score} \times \text{Macro Regime Multiplier}$$

* **In a Severe Market Drawdown (Current State)**:
  - Macro Regime Multiplier is compressed to **`0.2000`**.
  - Hurdle remains high ($150\text{ pts}$), breadth is narrow, and only **5 stocks** qualify for the Golden Core (`KRBL`, `GOKULAGRO`, `LTFOODS`, `KPIL`, `CYIENT`).
  - Intrinsic valuations of momentum stocks look stretched because defensive institutional money refuses to sell them down to Graham levels.
* **When the Market Bottoms & Recovers**:
  - Macro Regime Multiplier expands: $0.20 \to 0.50 \to 1.00$.
  - Volatility contraction breakouts succeed across broader sectors.
  - Pocket pivots and delivery accumulation surges widen across mid-caps.
  - The **Golden Core expands to 15–25 elite compounders**, providing abundant high-conviction deployment opportunities.

---

## 10. Quantitative Position Sizing Matrix

To remove emotion from portfolio sizing, the Golden Triangle assigns capital weights inversely to multiple overhang risk:

| Regime | Conviction Level | Portfolio Sizing Multiplier | Target Single-Stock Weight | Trailing Stop Rule |
| :--- | :---: | :---: | :---: | :--- |
| **`GOLDEN_CORE`** | **Maximum (3 Stars)** | **1.5x Base Size** | **8.0% – 10.0%** | Loose: 50-day SMA or Base Pivot Floor |
| **`HIGH_VELOCITY`** | **High Momentum** | **1.0x Base Size** | **4.0% – 6.0%** | Standard: 21-day EMA Trailing Stop |
| **`VALUE_BREAKOUT`** | **Speculative Turn** | **0.5x Base Size** | **2.0% – 3.0%** | Structural: Prior Swing Low |
| **`OVERHANG_ALERT`** | **Extreme Bubble** | **0.0x (No New Buys)** | **Trim / 0%** | Hard Profit-Booking Trigger |

---

## 11. The Profit-Booking / Exit Oracle

Knowing when to exit a winning multibagger is notoriously difficult. Selling too early cuts compounders short; holding too long round-trips massive gains during multiple contraction crashes.

The Golden Triangle solves this through a **Dual Valuation-Momentum Ceiling**:

```
                       EXIT / PROFIT-BOOKING MATRIX
┌──────────────────────────────────────┬──────────────────────────────────────┐
│ Technical State                      │ Valuation State                      │
├──────────────────────────────────────┼──────────────────────────────────────┤
│ Case A: RS Strong, VCP Holding       │ Upside <= -65% (Multiple Bubble)     │
│ ──> ACTION: HOLD & RIDE with tight   │ Let the momentum run; do not sell    │
│     trailing stop at 21-EMA.         │ prematurely on valuation alone.      │
├──────────────────────────────────────┼──────────────────────────────────────┤
│ Case B: VCP Breaks Down OR           │ Upside <= -65% (Multiple Bubble)     │
│         3D Score Drops >= -5.0 pts   │ ──> ACTION: HARD EXIT / PROFIT BOOK! │
│                                      │ Multiple contraction is confirmed.   │
└──────────────────────────────────────┴──────────────────────────────────────┘
```

---

## 12. Strategic Execution Roadmap

The implementation of `pkg/golden` opens four powerful capabilities for `mycase`:

1. **`mycase golden --analysis` (Shipped)**: Point-in-Time cross-strategy audit across the full 750-stock Nifty Total Market universe.
2. **`mycase pick --method golden` (Planned)**: Full native portfolio construction engine generating optimized rebalancing baskets directly from the Golden Core and High-Velocity leaders.
3. **Trajectory & Inflection Alerts (Planned)**: Tracking DuckDB state transitions to alert the operator the exact day a stock migrates from `VALUE_BREAKOUT` $\to$ `GOLDEN_CORE`.
4. **Automated Exit Triggers (Planned)**: Automated alerts when existing portfolio holdings breach the Case B Profit-Booking Matrix.

