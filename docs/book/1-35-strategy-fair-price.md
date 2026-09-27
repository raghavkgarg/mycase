# Fair Price

The Fair Price strategy answers the one question the other strategies leave open: *what is
the business actually worth?* Where Multibagger, Early Multibagger, and Value rank stocks by
relative scoring matrices, Fair Price computes an intrinsic value in currency terms — a
five-model valuation ensemble that produces a single fair price per share, an upside
percentage against the current market price, and a margin-of-safety verdict.

It runs in two modes from the same engine. As a **standalone screener** (`--method
fairprice`) it ranks the most mispriced, cash-generative candidates across the broad market.
As a **cross-strategy overlay** it runs automatically for every `multibagger` and `earlymb`
candidate, injecting `Fair Price`, `Upside`, and an MOS verdict into those tables so a
technically-strong pick that has run far ahead of its fundamentals is visibly flagged rather
than silently sized.

The ensemble lives in `pkg/yfinance/metrics_fairprice.go` (`FairPriceResult`,
`CalculateFairPrice`); scoring and selection in `pkg/stockpicker/scoring_fairprice.go`; the
overlay is wired through `stockpicker.RunWithResult` (`pkg/stockpicker/run.go`) and rendered
by `PrintMultibaggerTable` / `PrintEarlyMultibaggerTable` (`pkg/stockpicker/io.go`). Every
evaluation is persisted to the `pit_fairprice_scores` table (`pkg/pithistory/db.go`).

---

## 1. The five valuation models

Every model is computed from fields already present in `yfinance.Fundamentals` plus the
one-year daily close history — no new data feed. Each returns a per-share fair value or
`nil` when its inputs are structurally invalid, and each carries a base weight in the
ensemble.

| Model | Category | Base weight | Primary source | Key metric |
|-------|----------|:-----------:|----------------|------------|
| 1. Two-Stage DCF | Cash-flow growth | 30% | `AnnualFreeCashFlow` | Sustainable FCF CAGR |
| 2. EPV (Greenwald) | Normalized yield | 25% | `AnnualOperatingIncome` | Zero-growth NOPAT |
| 3. Graham Number | Asset / earnings | 15% | `NetIncome`, `PBRatio` | EPS × BVPS floor |
| 4. Relative Comps | Market consensus | 15% | `TrailingPE`, `PB`, `EV/EBIT` | Sector-cohort median |
| 5. Quality PEG | Growth-adjusted | 15% | Sales & EPS growth | Quality-scaled PEG |

### Model 1 — Two-Stage DCF (30%)

Projects free cash flow over a five-year high-growth horizon and discounts the projection
plus a terminal value back at a sector-adjusted cost of capital (`CalculateDCFFairPrice`).

Base cash flow $FCF_0$ is chosen by a four-tier waterfall — latest annual FCF, else the
three-year positive average, else an operating-cash-flow-minus-maintenance-capex proxy
(maintenance taken as 60% of capex), else a conservative scalar fallback gated on positive
earnings. To stop one-off asset monetizations (InvIT divestments, HAM concession sales,
arbitration settlements) being projected forward as perpetual cash generation, $FCF_0$ is
sanity-capped:

$$FCF_0 \le \min\left(2.0 \times \text{NetIncome}, \; 1.8 \times \text{OperatingIncome}\right)$$

The projection growth rate is clamped by capital efficiency — an elite compounder
(ROCE ≥ 22%) may grow at up to 25%, a capital-destructive business (ROCE < 10%) at most 6% —
and floored at 4%. The valuation is standard two-stage:

$$\text{EV}_{\text{DCF}} = \sum_{t=1}^{5} \frac{FCF_0 (1+g)^t}{(1+\text{WACC})^t} + \frac{FCF_5 (1+g_{\text{term}})}{(\text{WACC}-g_{\text{term}})(1+\text{WACC})^5}$$

with $g_{\text{term}}$ = 5.0% for India, 2.5% for the US (nominal GDP), and equity value
recovered as $\text{EV} - \text{TotalDebt} + \text{Cash}$.

For `Financial Services`, corporate free cash flow is economically meaningless — deposits
are operating inflows, loan disbursals operating outflows — so the DCF model returns `nil`
and its 30% weight is redistributed across the surviving models. This is the single most
important sector rule in the engine; without it, banks post four-digit fair prices against
two-digit market prices.

### Model 2 — Earnings Power Value (25%)

Bruce Greenwald's zero-growth anchor (`CalculateEPVPerShare`): value the business on current
sustainable earnings alone, assuming no future growth.

$$\text{NOPAT} = \overline{\text{EBIT}}_{1..3} \times (1 - T), \qquad \text{Enterprise EPV} = \frac{\text{NOPAT}}{\text{WACC}}$$

with the effective tax rate $T$ defaulting to 25.17% (Indian statutory) unless a valid
`TaxRate` in $[0.15, 0.35]$ is present. Equity EPV subtracts debt and adds a conservative
cash proxy; if debt exceeds enterprise EPV the result is floored at $0.10 \times$ book value
per share and flagged distressed.

BFSI takes a direct-capitalization branch instead — subtracting deposits-as-debt would be
invalid — capitalizing sustainable net income: $\text{Equity EPV}_{\text{BFSI}} =
\text{NetIncome} / \text{WACC}_{\text{BFSI}}$, returning `nil` when net income is
non-positive.

### Model 3 — Graham Number (15%)

Graham's defensive ceiling (`CalculateGrahamNumber`): $\sqrt{22.5 \times \text{EPS} \times
\text{BVPS}}$, where $22.5 = 15 \times 1.5$ (his P/E and P/B ceilings) and BVPS is derived as
$\text{CMP} / \text{PBRatio}$.

The EPS input is **not** the latest single-year figure but a cyclically-adjusted
`normalizedEPS`. When the three-year average operating income is negative, a sudden positive
year is treated as an unproven windfall and capped at 25% of the latest EPS; when it is
positive but the latest EPS is more than double the implied operating EPS, the spike is
smoothed with a 70/30 blend. This is what keeps a biopharma out-licensing milestone or a
turnaround's first profitable quarter from ballooning the Graham Number, the PEG fair price,
and the relative P/E simultaneously.

### Model 4 — Relative Sector Comps (15%)

Prices the stock at the median multiples of its sector peers within the active cohort,
triangulating across P/E (on normalized EPS), P/B, and — for non-financials only — EV/EBITDA.
Multiples are winsorized (P/E to $[3, 120]$, P/B to $[0.3, 30]$, EV/EBITDA to $[2, 60]$),
negatives are excluded from the median, and a sector with fewer than three valid peers falls
back to the all-cohort market median. EV/EBITDA is excluded entirely for `Financial
Services`.

### Model 5 — Quality-Adjusted PEG (15%)

Peter Lynch's PEG = 1.0 principle, scaled by quality. Sustainable growth blends revenue
trajectory and profit growth ($0.60 \times$ revenue CAGR $+ 0.40 \times$ TTM earnings
growth, clamped to $[0.05, 0.40]$); the target PEG rises with capital efficiency (1.25 for a
low-debt, ROCE ≥ 22% compounder; 0.80 for lower-quality names). Fair P/E is
$\text{PEG}_{\text{target}} \times (g \times 100)$, applied to normalized EPS.

---

## 2. Sector cost of capital

Discount rates are not one-size-fits-all — a commodity cyclical carries a higher hurdle than
a consumer staple. The WACC matrix (India / NSE calibration, $R_f$ = 7.10%, ERP = 5.50%):

| Sector category | Applied WACC | Rationale |
|-----------------|:------------:|-----------|
| Defensive / FMCG (Consumer Defensive, Healthcare, Utilities) | 10.0% | Non-cyclical demand, pricing power |
| Stable industrials (Industrials, Basic Materials, Consumer Cyclical) | 11.0% | Moderate GDP elasticity |
| Technology & digital (Technology, Communication Services) | 12.0% | Rapid change, global exposure |
| Deep cyclicals (Energy, Real Estate, Mining) | 13.0% | Commodity swings, capital intensity |
| Financials (BFSI) | 11.5% | Leveraged balance sheets, credit risk |
| Unclassified | 11.5% | Conservative market baseline |

---

## 3. Ensemble, convergence, and Bayesian shrinkage

When a model returns `nil`, its base weight is redistributed proportionally across the
survivors, so the effective weights always sum to one:

$$W_m^{\text{eff}} = \frac{W_m^{\text{base}}}{\sum_{j \in \text{valid}} W_j^{\text{base}}}, \qquad \text{Fair Price}_{\text{raw}} = \sum_{m \in \text{valid}} W_m^{\text{eff}} \cdot \text{Fair Price}_m$$

A stock needs at least **two** valid models; fewer, and it is excluded as `UNVALUABLE`.

Internal agreement is measured by the coefficient of variation across the valid model prices
(CV < 0.15 is high conviction, ≥ 0.35 is high dispersion), and a confidence score in
$[20, 100]$ combines model count, convergence, and data completeness.

The confidence score then drives **empirical Bayesian shrinkage** toward the market prior.
The raw ensemble is the likelihood estimate; the current market price (CMP) is the collective
prior. The published fair price shrinks the raw intrinsic spread toward CMP in proportion to
confidence:

$$\text{Fair Price} = \text{CMP} + (\text{Fair Price}_{\text{raw}} - \text{CMP}) \times \frac{\text{Confidence}}{100}$$

So a unanimous five-model estimate keeps ~90% of its intrinsic spread, while a fragile
two-model, high-CV estimate keeps only ~40% — a distressed turnaround with two surviving
models can no longer post +300% upside and dominate the ranking. Both values are retained:
`RawEnsembleFairPrice` for the theoretical calculation, `EnsembleFairPrice` for every
downstream upside, band, rank, and weight. A final boundary clamp holds the published price
to $[0.20 \times \text{CMP}, \; 4.00 \times \text{CMP}]$ and flags any stock that hits a
boundary.

---

## 4. Margin-of-safety bands and verdicts

Three bands frame the estimate — a pessimistic floor at a 20% discount, the base fair price,
and an optimistic ceiling at a 15% premium — and the upside against CMP maps to an actionable
verdict:

| Upside | Verdict | Action |
|--------|---------|--------|
| ≥ +30% | `DEEPLY_UNDERVALUED` | Aggressive accumulation, maximum sizing |
| +12% to +30% | `UNDERVALUED` | Standard accumulation, strong margin of safety |
| −10% to +12% | `FAIRLY_VALUED` | Hold; do not chase new entries |
| −25% to −10% | `OVERVALUED` | Trim, tighten stops |
| < −25% | `DEEPLY_OVERVALUED` | Avoid new purchases; exit or reallocate |

---

## 5. Cross-strategy overlay (automatic)

Running `multibagger` or `earlymb` computes fair price for every surviving candidate in
`RunWithResult` and enriches the existing tables — `PrintMultibaggerTable` and
`PrintEarlyMultibaggerTable` gain `Fair Price`, `Upside`, and MOS-verdict columns without
disturbing their existing layout. A growth stock trading far above its base fair price is
tagged `⚠️ VAL_STRETCHED`; the scuttlebutt research report gains an intrinsic-valuation and
MOS breakdown block; and `FairPrice`, `UpsidePct`, and `MOSVerdict` are recorded on
`DriverMetrics` (`pkg/stockpicker/run.go`) for persistence. The point is a single glance:
a `LAUNCHPAD-ARMED` technical setup carrying −60% intrinsic upside has no valuation cushion,
and the column says so.

---

## 6. Standalone screener (`--method fairprice`)

Invoked directly, Fair Price is a value-discovery screener across the broad market. A
lightweight Stage-1 pre-screen (`isEligibleFairPrice`) casts a wide net while excluding shell
companies, illiquid counters, and distressed value traps: a market-cap band, a minimum
average daily value, positive net worth, at least two years of annual statements, and an
earnings-quality gate that rejects chronic loss-makers (non-financials need positive latest
operating income or positive ROCE; financials need positive net income and ROE).

Survivors are ranked on a 100-point matrix across four pillars:

| Pillar | Sub-metric | Points |
|--------|-----------|:------:|
| **I. Valuation discount** (40) | Ensemble upside % (min-max normalized) | 25 |
| | MOS band position (CMP vs pessimistic/base/optimistic) | 15 |
| **II. Model consensus** (20) | Count of models where fair price > CMP | 10 |
| | Convergence, $10 \times \max(0, 1 - \text{CV}/0.40)$ | 10 |
| **III. Quality anchor** (25) | Capital efficiency (ROCE, or ROE for BFSI) | 10 |
| | Cash realization (CFO / PAT) | 8 |
| | Balance-sheet safety (D/E, inverted) | 7 |
| **IV. Growth** (15) | Three-year revenue CAGR | 8 |
| | Year-on-year earnings acceleration | 7 |

Portfolio construction caps any sector at three stocks or 25% weight, any single stock at
8%, and spills unfilled allocation into `CASH_RESERVE`. When screening a large universe the
rejected-candidate table is truncated to the top 40 with a summary line, so sector-cap drops
among genuine contenders stay visible without a 700-row flood.

---

## 7. Persistence

Every evaluated stock — under any method — is written to `pit_fairprice_scores`
(`pkg/pithistory/db.go`): the CMP, each individual model price, the raw and shrunk ensemble,
upside, verdict, MOS band, valid model count, CV, confidence, and WACC used, keyed by
`(as_of_date, index_name, method, ticker)`. A companion analytical view joins each snapshot
to its forward realized price to score directional accuracy over time — the audit trail that
lets the model's calls be graded against what the market subsequently did, and the raw
material the [Golden Triangle](1-45-strategy-golden.md) reads back when it fuses valuation
with quality and timing.

---

## 8. Configuration

Fair Price parameters live under the active market's `strategies.fairprice` block in
`config/defaults.yaml` — model base weights, terminal growth rates, MOS discount and optimism
premium, the sector/stock caps, and the nine scoring-pillar weights — alongside every other
strategy (see [Configuration Directory Inventory](2-75-config-inventory.md)).

```bash
# Standalone: broad-market value discovery
mycase pick --index niftytotalmarket --method fairprice --top 15

# Overlay is automatic — fair-price columns appear in these runs:
mycase pick --index niftytotalmarket --method multibagger --top 15
mycase pick --file data/microsmall.csv --method earlymb --top 10

# Point-in-time valuation trajectory for one ticker
mycase pit stats --ticker VARROC --method fairprice
```
