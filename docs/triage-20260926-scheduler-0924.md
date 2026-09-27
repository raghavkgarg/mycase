# Triage: Questionable 09-24 Scheduler Output

**Date:** 2026-09-26
**Branch:** feat/data-layer
**Trigger:** The one successful scheduler pass (2026-09-24) executed cleanly end-to-end
but produced results that don't hold up: the ADV filter rejected 499/503 S&P 500 names,
`pillar4_insufficient_history` was true for the entire universe, and the drift check
reported `portfolio 0.00 across 20 holdings`.

## Scheduler status (context)

- launchd agent `com.mycase.scheduler` is **loaded**, `LastExitStatus = 0`, fires daily 16:15 local.
- Binary `dist/mycase` is fresh (rebuilt 2026-09-26).
- Run history: 09-23 failed (Schwab token `invalid_grant`), **09-24 succeeded** (only real run),
  09-25 / 09-26 correctly skipped as non-trading days.
- `rebalance` cadence has **never** fired — only `eod` and `drift` have run.
- State (`data/scheduler_state.json`): `eod` and `drift` both last completed 2026-09-24.

## Findings

### 1. ADV filter collapsed universe 503 → 4  — REAL BUG (root cause confirmed)

**Chain of causation:**
- 09-24 run fetched fundamentals from **Yahoo**, not Schwab
  (`pick.fundamentals_fetch source=yahoo count=503`).
- US ADV filter computes `adv = AverageVolume * RegularPrice`
  at `pkg/stockpicker/scoring_us.go:362`.
- Yahoo mapping sets `RegularPrice = sd.RegularMarketPrice.Raw`
  (`pkg/yfinance/yfinance.go:439`) — Yahoo's `summaryDetail.regularMarketPrice`,
  which frequently returns **0** (live price lives under the `price` module, not `summaryDetail`).
- With `RegularPrice == 0`, the filter hits its fallback branch
  (`scoring_us.go:363-367`) which treats the **raw share count as dollars**:
  `adv = f.AverageVolume`.

**Proof:** snapshot rejected AMZN at "ADV $45M". AMZN `avg3MonthVolume` = 44,869,534 → /1e6 = $45M exactly.
Reported "ADV" *is* the raw share count. Plugging real Schwab fields into the same formula
with a non-zero price yields correct ADVs: AMZN $11B, BAC $2B, AAPL $17B — all > $50M.

**Two defects:**
- (a) Yahoo fundamentals don't populate a usable `RegularPrice` for US tickers.
- (b) The fallback at `scoring_us.go:363-367` is dimensionally wrong (compares raw shares to a
  dollar threshold). Should derive price from `MarketCap / SharesOutstanding` (as the Schwab
  mapper does at `market.go:201-209`) or skip the ADV gate when price is unknown — never treat
  shares as dollars.

**Open decision:** should the US path use Schwab fundamentals (correct fields) instead of Yahoo?
Routing US tickers to Schwab fundamentals may be the more correct fix than patching the fallback.

**RESOLUTION (2026-09-26):** FIXED.
- Defect (a) is already addressed by a `RegularPrice` backfill introduced in the merged
  `origin/main` commit `52f6fb7` (`pkg/stockpicker/run.go:159-166`): when the fundamentals
  provider leaves `RegularPrice <= 0`, it is healed from the latest historical close
  *before* the ADV filter runs. This was NOT present on the pre-merge branch, which is why
  the 09-24 run (older binary) hit the bug.
- Defect (b) fixed directly in `pkg/stockpicker/scoring_us.go`: the shares-as-dollars fallback
  is removed. The ADV gate now runs only when `RegularPrice > 0`; when price is genuinely
  unknown it SKIPS the gate rather than guessing (an ADV floor is a liquidity safety gate — we
  don't reject a name on a number we can't compute). `SharesOutstanding` is not on the shared
  `marketdata.Fundamentals` struct, so deriving price at the filter layer isn't possible;
  the historical-close backfill is the correct heal.
- Regression test added: `TestApplyUSHardFilters_ADV` in `pkg/stockpicker/stockpicker_test.go`
  — liquid mega-cap (AMZN, real Schwab fields, ADV ≈ $11B) passes, illiquid name ($1M) drops,
  price-unknown name skips the gate. Passes.
- Verified: `make build` OK, `go vet` clean, `gofmt` clean. Only pre-existing date/data-dependent
  test failures remain (`TestLoadRecentExits_ArvindCooldown`, `pkg/cache/TestGetFundamentals_Stale`,
  `TestRangeKeyToStartDate`), all confirmed failing identically on the pre-merge branch.

### 2. `pillar4_insufficient_history: true` for all 503  — NOT A BUG (expected, mislabeled)

Pillar 4 is the NSE delivery-percentage signal — India-only, structurally absent for US equities,
never fetched on the `us_quality_momentum` path (US scoring uses six other factors). Flag being
universally set is correct. Only issue: the PIT snapshot records it per-stock as if it were a
data-fetch problem. Cosmetic.

Refs: `pkg/stockpicker/run.go:306-322`, `pkg/yfinance/metrics_delivery.go:27-60`,
`enrichDeliveryHistory` India-only gate at `run.go:534-543`.

### 3. Drift "portfolio 0.00 across 20 holdings"  — REAL BUG / mismatch

`CalculateDrift` (`pkg/daemon/drift.go:29-80`) values holdings only for symbols present in the
target basket CSV, keyed `US:<symbol>` (drift.go:44). "20 holdings" is actually `len(BasketKeys)`
— the CSV row count, not live positions (`pkg/scheduler/dispatch.go:63` mislabels it).
`portfolio 0.00` + `drift 0.5000` is the documented signature of `totalValue` collapsing to zero:
no basket key matched a valued holding — a symbol-format mismatch between the portfolio CSV and
the `US:`-prefixed Schwab holdings, or a basket that doesn't describe the live account. The
`0.5000` is a sentinel (`½·Σ|0 − target|`), not a real measurement.

## Fix priority

1. ~~**ADV/Yahoo price bug (#1)**~~ — **DONE** (see Resolution under Finding 1). Backfill from
   merge + fallback removed + regression test.
2. **Drift mismatch (#3)** — drift number is meaningless until the basket file and holdings keys
   reconcile. NEXT.
3. **Pillar-4 snapshot labeling (#2)** — cosmetic, low priority.

## Verification data (archived raw Schwab /instruments, 2026-09-18)

| Sym  | marketCap        | sharesOutstanding | avg3MonthVolume | derived px | code ADV (correct) |
|------|------------------|-------------------|-----------------|------------|--------------------|
| AMZN | 2,653,001,686,169 | 10,786,313,572   | 44,869,534      | $245.96    | $11.0B             |
| BAC  | 404,880,130,333  | 6,992,748,365     | 34,726,750      | $57.90     | $2.0B              |
| AAPL | 4,851,251,373,800 | 14,594,180,000   | 52,191,357      | $332.41    | $17.3B             |
