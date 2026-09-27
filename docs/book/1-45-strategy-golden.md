# Golden Triangle

The Golden Triangle fuses the three independent strategies — [Multibagger](1-10-strategy-multibagger.md)
quality, [Early Multibagger](1-20-strategy-early-multibagger.md) timing, and
[Fair Price](1-35-strategy-fair-price.md) valuation — into one cross-strategy audit. Rather
than picking stocks itself, it reads back what those three engines have already written to
DuckDB, aligns each stock across all three dimensions, classifies it into a regime, and ranks
the universe by a single composite score. The point is to surface the stocks where quality,
accumulation, and a valuation cushion coincide — and to flag the ones where momentum has run
far ahead of intrinsic worth.

The engine is `pkg/golden` (`RunAnalysis`, `ClassifyRegime`, `ComputeCompositeScore` in
`analytics.go`; the report and regime types in `types.go`; terminal rendering in
`render.go`), driven by the `mycase golden` command (`cmd/golden.go`). It is a decoupled
consumer of the point-in-time tables — it writes nothing back.

---

## 1. The three vertices

Each vertex is an orthogonal question about a stock, and each is answered by an existing
strategy:

- **Fair Price — *what is it worth?*** The five-model ensemble, its upside against market
  price, and the MOS band. Protects against paying bubble multiples for a momentum name.
- **Early Multibagger — *is smart money coiling it now?*** VCP tightness, delivery-volume
  delta, pocket-pivot intensity, relative volume, and composite relative strength. Provides
  entry timing near a pivot low.
- **Multibagger — *can it compound?*** The 100-point quality matrix — ROCE, ROE, revenue and
  PAT CAGR, low leverage, margin expansion, institutional footprint. Ensures the portfolio
  holds genuine high-return-on-capital businesses.

No single vertex is complete: momentum alone buys 75× P/E bases into drawdowns, quality alone
enters late, deep value alone traps capital in dead money. The triangle is the intersection.

---

## 2. The composite score

Every constituent is scored 0–100 (`ComputeCompositeScore`):

$$\text{GCS} = 0.35 \cdot S_{\text{mb}} + 0.40 \cdot S_{\text{emb}} + 0.25 \cdot S_{\text{fp}}$$

The Multibagger and Early Multibagger inputs are the effective scores each engine persisted.
The valuation input is a piecewise-linear mapping of fair-price upside to a bounded 0–100
score (`ComputeValuationScore`), so a deep-value trap with +250% theoretical upside cannot
dominate on valuation alone:

| Upside | Valuation score $S_{\text{fp}}$ |
|--------|---------------------------------|
| ≥ +50% | 100 |
| −20% to +50% | $50 + \text{upside}$ (e.g. 0% → 50, +30% → 80) |
| −50% to −20% | $30 + (\text{upside} + 20) \times 0.6$ |
| < −50% | $\max(0, \; 12 + (\text{upside} + 50) \times 0.3)$ |

A missing fair price scores a neutral 20.

---

## 3. The four regimes

`ClassifyRegime` places each stock into one regime by combining its Multibagger score,
Early Multibagger Stage-1 status, and fair-price upside. The thresholds as enforced in code:

| Regime | Criteria | Interpretation & action |
|--------|----------|-------------------------|
| **`GOLDEN_CORE`** | EMB Stage-1 passed, and (MB score ≥ 50 or MB Stage-1 passed), and upside ≥ −20% | The full trinity: a quality compounder coiling on the launchpad with a genuine valuation cushion. Maximum conviction. |
| **`HIGH_VELOCITY`** | EMB Stage-1 passed, upside < −20% | Elite momentum leader carrying a bull-market multiple overhang. Trade with a strict trailing stop; a ≤ −60% overhang is flagged a multiple bubble. |
| **`VALUE_BREAKOUT`** | EMB Stage-1 passed, upside ≥ +20%, MB score < 50 | Deep-value turnaround showing early accumulation. Asymmetric speculative bet. |
| **`COMPOUNDER_SALE`** | EMB Stage-1 *not* passed, MB score ≥ 60, upside ≥ +10% | Pristine business at a discount but with no active base yet. Systematic accumulation while awaiting a launchpad. |
| **`MONITOR`** | everything else | Cohort constituent under observation. |

The `HIGH_VELOCITY` names with upside ≤ −60% are additionally collected as overhang alerts,
the watchlist of momentum favourites at the greatest risk of multiple contraction.

---

## 4. How the analysis is built

`RunAnalysis` runs one DuckDB query against the consolidated point-in-time tables and
guarantees reproducibility without lookahead:

1. Find the latest `as_of_date` independently in `pit_candidate_scores` for `earlymb` and
   `multibagger`, and in `pit_fairprice_scores`.
2. Take the highest effective score and any Stage-1 pass per ticker for each method.
3. Deduplicate fair-price rows with `ARG_MAX(..., created_at)`, recovering CMP from the
   stored price, the latest close, or the fair-price/upside relationship as fallbacks.
4. Full-outer-join `earlymb` and `multibagger`, left-join fair price and latest prices.
5. Score, classify, and sort every constituent by composite score.

The result is a `GoldenReport`: a regime census (counts and cohort median upside) plus the
ranked lists for each regime and the overhang alerts, rendered as sections by `render.go`.

---

## 5. Reading the census across a market cycle

The size of the Golden Core is not fixed — it breathes with market breadth. In a severe
drawdown, momentum leaders hold near their highs on institutional accumulation while their
multiples never compress to intrinsic levels, so few names clear both the launchpad gate and
the valuation cushion, and the core is small. As breadth recovers, volatility-contraction
breakouts and delivery accumulation widen across sectors, more quality names clear the gate
with reasonable multiples, and the core expands. A thin Golden Core is itself a signal about
the regime, not a failure of the screen.

This is the operational counterpart to the tension described in [Philosophy](1-48-philosophy.md):
Early Multibagger and Fair Price disagree by construction, and the Golden Triangle is where
their disagreement is resolved into a single ranked view.

---

## 6. Running it

```bash
# Full Nifty Total Market cross-strategy audit (default)
mycase golden --analysis

# Target a specific index
mycase golden --analysis --index niftysmallcap250
mycase golden --analysis --index nifty500
```

The report prints, in order: the regime census; the Golden Core (full-trinity candidates);
high-velocity leaders with their overhang warnings; the value-breakout watchlist; and the
valuation risk alerts. Because it reads the point-in-time tables, a meaningful run depends on
having first populated them with `earlymb`, `multibagger`, and fair-price picks for the
target index.
