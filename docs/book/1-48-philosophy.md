# Philosophy

Run a point-in-time audit across the market and two of the strategies appear to contradict
each other. The [Early Multibagger](1-20-strategy-early-multibagger.md) Stage-1 cohort — the
launchpad survivors — is dominated by names the [Fair Price](1-35-strategy-fair-price.md)
engine calls overvalued, while Fair Price's own top picks are stocks Early Multibagger would
never touch. Both engines are behaving with complete fidelity to their mandates; the tension
between them is the point, and reconciling it is the philosophy the whole system is built on.

This chapter is the *why* behind the strategy family and the [Golden Triangle](1-45-strategy-golden.md)
that fuses it. It carries no code of its own.

---

## 1. Two engines, two objective functions

Early Multibagger and Fair Price optimize for different things, so they select different
stocks — not by accident but by design.

**Early Multibagger** descends from O'Neil (CANSLIM), Minervini (VCP/SEPA), and Phelps
(*100 to 1*). Its hypothesis is that supernormal returns come from earnings acceleration
coupled with multiple *expansion*. It gates on capital efficiency (ROCE, ROE), structural
growth (multi-year sales and PAT CAGR), institutional accumulation (delivery-volume delta,
tight VCP), and relative strength — the stock must be outperforming its benchmark and trading
near its 52-week high. The output is high-multiple momentum leaders.

**Fair Price** descends from Graham & Dodd, Greenwald (EPV), and Damodaran. Its hypothesis is
that wealth is protected by buying assets below their reproducible earnings power and cash
generation, regardless of sentiment. It values on discounted cash flow, zero-growth earnings
power, Graham's asset-backed floor, sector comps, and quality-adjusted PEG. The output is
low-multiple deep-value assets.

---

## 2. Why launchpad survivors look expensive

Intrinsic models are bounded by discount rates (WACC ≈ 11–13%) and terminal growth capped at
nominal GDP (≤ 5.5%). Under those bounds the steady-state fair multiple of even a superb
business rarely exceeds 25–35× — yet the market awards its recognized compounders quality
premiums of 45–75×.

In a downturn this divergence widens rather than closes. Weak stocks crash and their
multiples collapse to single digits; elite compounders instead attract institutional
accumulation that holds their prices near the highs. Their relative strength therefore
*rises* — tripping Early Multibagger's Stage-1 gate — precisely because their prices did not
fall, which is also why their multiples never compressed to Graham-and-Dodd levels. When Fair
Price then values them against intrinsic cash flows it honestly reports a large negative
upside. The launchpad survivors are not victims of a falling market; they are momentum havens
carrying bull-market multiple overhangs.

---

## 3. Why deep-value picks never pass the launchpad

The reverse holds for Fair Price's favourites — refiners, PSU banks, EPC contractors,
fertiliser makers, holding companies — trading at large discounts to tangible assets. Fair
Price loves the cheapness; Early Multibagger disqualifies them because cheapness without a
catalyst is not outperformance. The recurring failure modes are the anatomy of a value trap:

- **Low reinvestment** — a business earning 8% ROCE cannot compound capital internally even
  if bought at 5× earnings.
- **Capital misallocation** — excess cash reinvested in sub-par capex rather than returned to
  shareholders.
- **Opportunity cost** — a stock with +150% theoretical upside that moves sideways for years
  produces zero alpha against an index fund.

And the deepest structural discounts (a holding-company discount, a subsidized-industry
overhang) can persist for decades without ever narrowing.

By strict intrinsic standards, elite compounders are cheap only under extreme capitulation —
the Covid crash offered roughly eighteen sessions, 2008 a few weeks. Waiting for a +50%
margin of safety on a quality leader under normal conditions means sitting in cash while it
compounds away.

---

## 4. The three zones and the intersection

Neither engine is complete alone: Early Multibagger has valuation blindness, Fair Price has
momentum blindness. Placing every stock on the spectrum of both produces three zones.

| Zone | Signature | Action |
|------|-----------|--------|
| **Momentum risk** | Strong RS, upside ≤ −60%, massive multiple overhang | Avoid or cut — no valuation cushion if selling intensifies |
| **The intersection** | High EMB score, ROCE > 18%, upside −20% to +30%, reasonable multiples with runway | Maximum size — accumulation footprint without bubble risk |
| **Value traps** | Deep upside (+100% to +250%), ROCE < 10%, weak RS, stagnant compounding | Skip or watch — cheap for a reason |

Three operating rules follow directly:

1. **Guardrail the launchpad.** A `LAUNCHPAD-ARMED` setup carrying ≤ −60% intrinsic upside
   does not get full portfolio weight — which is exactly why the fair-price overlay injects
   an upside column into the Early Multibagger table.
2. **Hunt the cushion.** Concentrate capital on Stage-1 survivors whose intrinsic upside gap
   is manageable (roughly −20% to +30%): accumulation without a severe multiple overhang.
3. **Watch the inflection bridge.** When a deeply undervalued value-trap candidate undergoes
   a genuine turnaround — debt reduction, ROCE expansion, new capacity — institutions notice;
   the moment its relative strength crosses into the Early Multibagger gate it stops being a
   value trap and becomes an early-stage compounder.

---

## 5. The strategies compared

| Criterion | Early Multibagger | Fair Price | Multibagger |
|-----------|-------------------|------------|-------------|
| Primary goal | Pre-breakout entry into emerging momentum leaders | Capital preservation, deep intrinsic discount | Ride established trend compounders |
| Holding period | 3–12 months | 12–36 months | 12–36 months |
| Typical multiples | Elevated (30–65× P/E) | Depressed (5–18× P/E) | Moderate to high (25–50× P/E) |
| Key metric | VCP ratio & accumulation velocity | Margin of safety & EPV | EPS growth consistency & trailing stop |

The synthesis is the [Golden Triangle](1-45-strategy-golden.md): the intersection of quality,
timing, and valuation, where the highest-conviction compounders live — run at any time with
`mycase golden --analysis`.
