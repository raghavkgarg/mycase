# Phase 10c — SEC EDGAR Fundamentals: Design

**Status**: ✅ IMPLEMENTED (Phase 10c shipped). This document records the design as built; see `pkg/edgar`, `pkg/datafetcher/merger.go`, and the Router `WithEDGAR` wiring.
**Scope**: `pkg/edgar` client + XBRL concept mapper + `datafetcher.FundamentalsMerger`
**References**: `docs/roadmap.md` Phase 10c, `docs/datasources.md` §4.3/§7, `docs/architecture.md` layering (R16), `docs/api-rules.md`

---

## 1. Decision on the open question (datasources.md §8.1)

**Parse EDGAR ourselves. No commercial vendor.** Rationale, grounded in the roadmap's own design constraints (§6 "local-first / no external dependencies beyond broker APIs and market data", "no black boxes / transparency"):

- A paid vendor reintroduces exactly the thing Phase 10 exists to remove — trusting someone else's opaque parse of the SEC filing.
- EDGAR is free + authoritative + no API key. The cost is XBRL tag drift, which is a bounded, well-understood problem (ordered candidate-tag lists per concept).
- Fetch volume is tiny: `companyfacts.json` is quarterly-stable, so ~1 fetch/company/quarter — trivially inside the 10 req/s budget.

If the concept mapper proves too brittle in practice, swapping in a vendor later is a localized change behind the same `FundamentalsSource` interface. Starting free keeps optionality.

---

## 2. Package placement & layering

New package **`pkg/edgar`** at **Layer 1** (stores/low-level impls over leaves).

- Imports: `pkg/marketdata` (leaf, for the `Fundamentals`/`AnnualMetric` DTOs it populates) and `pkg/cache` (leaf, for its own persistence via `cache.Conn()`). Both are L0 → edgar at L1 is a legal downward import.
- **Owns its persistence** per the R16 doctrine: edgar defines its own tables (ticker→CIK map, per-company facts cache) via `cache.Conn()`; `pkg/cache` never imports `edgar`.
- Mirrors the existing `themedb` precedent (L1, imports only `cache`), so the layering pattern is already established and enforced by `devtools/checkdeps`.

`layers.go` change: add `"edgar": 1` with a comment. `datafetcher` (L3) will import `edgar` (L1) — legal.

**Why not put EDGAR fetch logic in `datafetcher` directly?** `datafetcher` is L3 and composes sources; the *client* (HTTP, CIK map, XBRL parsing, its own cache tables) is a self-contained low-level concern that belongs in its own L1 package, exactly like `yfinance` (L1) and `schwab` (L2). The **merger** lives in `datafetcher` (L3) because merging is a consumer-side composition concern.

---

## 3. `pkg/edgar` internals

### 3.1 HTTP client (`client.go`)

```go
type Client struct {
    httpClient *http.Client   // 15s timeout
    limiter    *rate.Limiter  // 10 req/s (golang.org/x/time/rate), burst 10
    cache      *cache.Cache   // for CIK map + facts persistence (may be nil in tests)
    logger     *slog.Logger
    userAgent  string         // MANDATORY per EDGAR; validated at construction
    wwwBase    string         // overridable for tests (httptest)
    dataBase   string
    factsTTL   time.Duration
    cikTTL     time.Duration
}
```

- **User-Agent**: mandatory. Sourced from config (`config/defaults.json` `edgar.user_agent`). Validated at construction (`validateUserAgent`): rejects empty, the shipped placeholder, and UAs without a contact (`@` or URL) — a missing/generic UA gets the host IP-blocked by SEC.
- **Rate limit**: ≤ 10 req/s via **`golang.org/x/time/rate`** (`rate.NewLimiter(rate.Limit(10), 10)`, `limiter.Wait(ctx)` before each request). Rationale: `golang.org/x/*` is Go-team-maintained (effectively first-party) and `golang.org/x/sync`/`mod`/`tools` are already in `go.mod`; `golang.org/x/time` adds one direct require with no transitive deps. A token bucket models the per-*second* EDGAR limit correctly (smooth pacing + burst cap), whereas Schwab's hand-rolled per-*minute* sliding window would let 10 requests fire in the same instant. Follow-up (out of 10c scope): the Schwab client's hand-rolled window could later migrate to `rate.Limiter` (120/min = `rate.Every(500*time.Millisecond)`, burst 120) for consistency — noted, not done here.
- **Logging**: `logging.LogRequest`/`LogResponse` on the EDGAR HTTP path (the API-rules enforcement seam), plus `slog.*Context` with dotted events (`edgar.cik_map_refreshed`, `edgar.facts_fetched`, `edgar.cik_unknown`, …). Never logs response bodies. A 404 is a sentinel (`errNotFound`) → graceful skip, not an error.

### 3.2 Ticker → CIK map (`cik.go`)

- Source: `https://www.sec.gov/files/company_tickers.json` (flat `{cik_str, ticker, title}`).
- EDGAR is CIK-keyed; every facts call needs a 10-digit zero-padded CIK.
- Cache in a DuckDB table `edgar_cik_map(ticker VARCHAR PRIMARY KEY, cik VARCHAR, title VARCHAR, refreshed_at BIGINT)`; **weekly** freshness (new listings are rare).
- Lookup strips the `US:`/`NYSE:`/`NASDAQ:` prefix via a local `stripUSPrefix` helper (4 lines) rather than importing `schwab.StripUSPrefix` — schwab is L2, so importing it would violate edgar's downward-only layering.
- On refresh failure with a stale-but-present map, edgar keeps serving the stale map rather than failing (logged `edgar.cik_map_refresh_failed`).

### 3.3 Facts fetch + XBRL concept mapper (`facts.go`, `concepts.go`)

- Fetch `https://data.sec.gov/api/xbrl/companyfacts/CIK##########.json` — one call returns every us-gaap concept for the company.
- Cache the **raw JSON blob** in a DuckDB table `edgar_facts(cik VARCHAR PRIMARY KEY, raw_json VARCHAR, fetched_at BIGINT)`; freshness = **until next quarterly filing**. Simple policy: treat as fresh for 80 days (a quarter is ~91 days; 80 leaves margin to pick up a new 10-Q shortly after it files). Revisit if too coarse.
- **Concept mapper**: for each target field, an **ordered list of candidate us-gaap tags**; take the first tag present, then select the most recent annual (`fp == "FY"`, `form == "10-K"`) fact for annual series, or the latest fact for TTM-ish point values. Candidate lists (from datasources.md §4.3):

  | `marketdata.Fundamentals` field | Candidate tags (ordered) |
  |---|---|
  | `OperatingCashflow` | `NetCashProvidedByUsedInOperatingActivities`, `NetCashProvidedByUsedInOperatingActivitiesContinuingOperations` |
  | `NetIncome` | `NetIncomeLoss`, `ProfitLoss` |
  | `AnnualOperatingIncome` | `OperatingIncomeLoss` |
  | `AnnualTotalAssets` | `Assets` |
  | `AnnualCurrentLiabilities` | `LiabilitiesCurrent` |
  | `AnnualRevenue` | `RevenueFromContractWithCustomerExcludingAssessedTax`, `Revenues`, `SalesRevenueNet` |
  | `AnnualGrossProfit` | `GrossProfit` |
  | `AnnualNetPPE` | `PropertyPlantAndEquipmentNet` |
  | `AnnualAccountsReceivable` | `AccountsReceivableNetCurrent` |
  | `AnnualCapEx` | `PaymentsToAcquirePropertyPlantAndEquipment` |
  | `AnnualInterestExpense` | `InterestExpense`, `InterestExpenseDebt` |

- Annual series → `[]marketdata.AnnualMetric{Date, Value}` sorted ascending by fiscal year end, deduped by `end` date (a fact can appear in multiple filings; keep the most recently `filed`).
- FCF: EDGAR gives authoritative operating cash flow and capex → `FreeCashflow = OperatingCashflow_latest − CapEx_latest` when both present (more authoritative than Schwab's FCF/share × shares).

### 3.4 Public API

```go
// EDGARFundamentals returns only the statement-level fields EDGAR can supply,
// as a partial marketdata.Fundamentals (ratios/sector left zero).
func (c *Client) FetchFundamentals(ctx context.Context, tickers []string) (map[string]marketdata.Fundamentals, error)
```

Returns a **partial** `Fundamentals` — only the fields EDGAR authoritatively supplies (op cash flow, net income, annual series, derived FCF). Ratios (ROE, margins, P/E, beta, market cap) stay zero; the merger overlays Schwab for those. Structurally satisfies the `datafetcher.FundamentalsSource` interface.

Per API-rules "fail gracefully": a ticker whose CIK is unknown or whose facts are missing a concept is **skipped/left partial**, never aborts the batch.

---

## 4. `datafetcher` merger (`merger.go`, L3)

Per datasources.md §7, merge — don't just fall back. `mergeFundamentals(base, edgarPartial, hasSchwab, hasEDGAR)` applies a per-field source-of-record precedence:

1. **Schwab TTM ratios** are the base: ROE, ROA, margins, P/E, P/B, beta, market cap, div yield, shares, average volume — copied through unchanged.
2. **Overlay EDGAR statement facts**: `OperatingCashflow`, `NetIncome`, all `Annual*` series, and authoritative `FreeCashflow` (OCF − capex) — EDGAR wins for these because they're the filing itself.
3. **Sector is left untouched** — it is backfilled downstream by `stockpicker.InjectSectors` (Phase 10a) from the constituents CSV (GICS), a better source than EDGAR's coarse SIC. The merger neither reads nor writes `Sector`.

The overlay is **field-level and non-destructive**: an EDGAR field overrides only when non-zero (or a non-empty series via `pickSeries`), so a concept EDGAR couldn't map never zeroes a good Schwab value. The function returns the merged struct plus a `source` provenance tag (`"schwab+edgar"` / `"edgar"` / `"schwab"`).

**Scope note (as built):** the Schwab-derived `NetIncome`/`RegularPrice` floor (Phase 10a) already lives in `mapSchwabFundamentals`, so it is present in `base` before the merge — the merger does not re-derive it. Yahoo fallback for sparse/errored tickers remains the Router's existing responsibility (the merger operates only on the Schwab-success path); it is **not** part of the merger.

### Wiring into the Router — option (B), as built

The Router gained an optional `edgarSource` (a consumer-defined `fundamentalsSource` interface, so `*edgar.Client` satisfies it structurally and tests use a fake), attached via a chainable, nil-safe `WithEDGAR(src)`. A nil source ⇒ behavior is exactly as before EDGAR existed. On the US-fundamentals path, after a successful Schwab fetch the Router calls `overlayEDGAR`, which fetches EDGAR partials and merges them in place; an EDGAR fetch error is logged and the Schwab result is kept (fail-gracefully). This matches the existing "nil client ⇒ fall back" pattern and keeps the blast radius minimal.

---

## 5. Cache & provenance

- Two new edgar-owned tables (`edgar_cik_map`, `edgar_facts`) created via `cache.Conn()` with `CREATE TABLE IF NOT EXISTS` — edgar owns them, cache stays a leaf.
- The merged fundamentals continue to flow through the existing `fundamentals` cache blob; tag `source` = `"schwab+edgar"` (or `"edgar"` / `"schwab"` / `"yahoo"` depending on what actually contributed) so provenance is auditable — extends the R17 `source` column already in place.
- Per-source freshness: CIK map weekly, EDGAR facts ~quarterly (80-day), unchanged for Schwab TTM (24h). These live inside edgar's own cache reads, independent of the 24h `fundamentals` blob TTL.

---

## 6. Config

New `config/defaults.json` block:
```json
"edgar": {
  "enabled": false,
  "user_agent": "mycase/1.0 (set-your-contact@example.com)",
  "facts_ttl_days": 80,
  "cik_ttl_days": 7
}
```
- `enabled: false` by default ⇒ zero behavior change until deliberately turned on (safe rollout; nil edgar client in the Router).
- Precedence flag > env > config > default, consistent with the logging config convention. `MYCASE_EDGAR_USER_AGENT` env override for the UA.

---

## 7. Testing strategy (hermetic by default)

Per the convention I just enforced (network tests behind `//go:build integration`):

- **Unit (default `make test`, hermetic)** — all shipped and green:
  - Concept mapper against a saved `companyfacts.json` fixture (`pkg/edgar/testdata/companyfacts_TESTCO.json`) exercising ordered-tag selection, annual-series dedup (a later-filed `10-K/A` restatement wins), ascending sort, FCF derivation (annual OCF − capex), and latest-reported point selection.
  - Client/CIK behavior against an `httptest` server that also asserts the mandatory `User-Agent` on every request; UA-validation table; unknown-ticker and cache-blob round-trip (real temp DuckDB).
  - Merger precedence: table-driven synthetic Schwab + EDGAR partials asserting per-field winner and non-destructive overlay.
  - Router overlay via a fake `fundamentalsSource`: enrichment, EDGAR-error-keeps-Schwab, and `WithEDGAR(nil)` nil-safety.
- **Integration (`make test-integration`, `//go:build integration`)**: live AAPL CIK+facts fetch against `data.sec.gov`, skipped unless `MYCASE_EDGAR_USER_AGENT` is set. Gated so `make test` stays offline-safe.

---

## 8. Decision record (rationale as built)

The five decisions below were resolved in review before implementation; kept here as the rationale of record.

1. **Rate limiter** → **`golang.org/x/time/rate`** token bucket (10 req/s, burst 10). `golang.org/x/*` is effectively first-party and other `x/` modules are already required; token bucket fits the per-second limit better than a minute-window. Schwab-limiter migration noted as a separate follow-up.
2. **Router wiring** → **option (B)**: Router gains an optional `*edgar.Client` (nil ⇒ exactly current behavior). Merger is an unexported helper invoked on the US-fundamentals path.
3. **Sector** → **CSV-GICS only; do NOT fetch EDGAR submissions/SIC.** SIC is a coarser, semantically different taxonomy (~10 divisions) that doesn't map cleanly to GICS's 11 sectors; mixing taxonomies would corrupt the sector-cap buckets and require a hand-maintained SIC→GICS crosswalk. The rare CSV-uncovered ticker landing in "Unknown" is already handled gracefully and is the lesser evil. Universal sector coverage, if ever needed, is a proper-GICS-mapping decision, not coarse SIC. One fewer endpoint is a bonus, not the reason.
4. **First-cut scope** → Schwab+EDGAR merge, `enabled:false` default. Capability-interface split + provenance-in-reports deferred to Phase 10d per roadmap sequencing.
5. **FreeCashflow** → **EDGAR-preferred, Schwab-TTM fallback, guarded.** Use EDGAR `OperatingCashflow − CapEx` only when *both* concepts are present on a consistent annual basis (authoritative, textbook FCF); otherwise keep Schwab's TTM `FCF/share × shares` (smoother/more current but vendor-derived). Field-level non-destructive: a missing capex concept never zeroes Schwab's value. Record the winner via the `source` provenance tag.

---

## 9. Deliverables checklist (all shipped)

- [x] `pkg/edgar/client.go` — HTTP client, UA validation, rate limiter
- [x] `pkg/edgar/cik.go` — ticker→CIK map + weekly cache table
- [x] `pkg/edgar/facts.go` — companyfacts fetch + ~quarterly cache table
- [x] `pkg/edgar/concepts.go` — ordered candidate-tag concept mapper
- [x] `pkg/edgar/*_test.go` + `testdata/` fixture (hermetic) & integration test (tagged)
- [x] `pkg/datafetcher/merger.go` — field-level, non-destructive merge with provenance tag
- [x] `pkg/datafetcher/router.go` — optional `edgarSource` via `WithEDGAR`, US path overlays
- [x] `devtools/internal/layers/layers.go` — register `edgar` at L1
- [x] `config/defaults.json` + `pkg/config` — `edgar` block, UA, TTLs, `enabled`
- [x] `.kiro/steering/architecture.md` layer table — `edgar` L1 (also synced `themedb`/`portfolio`/`themereturn`)
- [x] `docs/datasources.md` §5/§6/§7 — 10c marked done; `docs/roadmap.md` Phase 10c → done
- [x] `make cleanup` + `make test` green; `make check-deps` intact
- [x] wired opt-in into `cmd/broker.go` + `pkg/autopilot` router factories (`MYCASE_EDGAR_USER_AGENT` env override)
