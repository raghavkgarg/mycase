# Mycase — Roadmap

**Goal**: An automated US equity system that delivers slight but consistent outperformance over the S&P 500 while eliminating emotional decision-making and manual busy work.

**Updated**: September 2026

**Target investor**: US-based individual investor using Schwab. The India market components exist as legacy code from an earlier multi-market design but are not part of the active strategy.

---

## ⏭️ Next up (operator TODO)

> **Run the first EDGAR-enabled S&P 500 pick with fresh data** (deferred to get a clean, fully-settled US EOD rather than a cold run tonight):
>
> ```bash
> make run ARGS="pick --index sp500 --method us_quality_momentum --top 20 --force"
> ```
>
> - Run it after the prior US session has settled (NYSE close 16:00 ET ≈ 01:30–02:30 IST next morning), so the EOD-settlement freshness picks up the latest data.
> - Keep `--force`: an earlier same-day snapshot (the pre-EDGAR `0/N` result) would otherwise short-circuit the recompute.
> - **This run will make live Schwab + EDGAR calls** — it is the cold-cache first fetch (~500 prices + ~500 fundamentals + ~500 EDGAR companyfacts). It *populates* the DuckDB cache as it goes (`source="schwab"` / `"schwab+edgar"`), so every same-day re-run after it is warm (zero Schwab/EDGAR calls).
> - Expected result now that EDGAR is enabled: real FCF values, so `us_quality_momentum`'s positive-FCF hard filter passes real names instead of eliminating the whole index (`0/N`).
> - Sanity-check EDGAR is live on the run: `data/raw/` should start showing files after the CIK-map download, and the funnel should no longer report every ticker as "FCF ≤ 0".
>
> _Context: EDGAR was enabled and verified live (commit `3d774d8`, Phase 10c/10e). Remove this note once the first successful run is done._

---

## Table of Contents

1. [Philosophy](#1-philosophy)
2. [Current State](#2-current-state)
3. [Architecture Vision](#3-architecture-vision)
4. [Phased Roadmap](#4-phased-roadmap)
5. [Success Metrics](#5-success-metrics)
6. [Anti-Goals](#6-anti-goals)

---

## 1. Philosophy

### The honest premise

Most active strategies fail to beat a broad index over 10 years after costs. The few that succeed share common traits: systematic factor tilts, disciplined rebalancing, tax efficiency, and — above all — behavioral consistency. The investor who stays fully invested through a -30% drawdown outperforms the one who panic-sells and waits for "clarity" to re-enter.

Mycase is not a stock-picking edge machine. It is a **behavioral discipline engine** that happens to also pick stocks intelligently. The system's primary job is:

1. **Prevent the investor from doing something stupid** during drawdowns (automation doesn't feel fear)
2. **Harvest mechanical premiums** that require no insight — rebalancing premium, tax-loss harvesting, factor mean-reversion
3. **Apply systematic factor tilts** (quality + momentum) that have academic support for long-term outperformance

### The realistic edge

| Source of alpha | Expected contribution | Academic support |
|----------------|----------------------|-----------------|
| Behavioral discipline (no panic sell/FOMO buy) | 1–2% / year | Dalbar study: avg investor underperforms by 3–4% due to timing |
| Rebalancing premium (sell winners, buy losers mechanically) | 0.3–0.8% / year | Bernstein, Perold & Sharpe |
| Tax-loss harvesting (systematic loss realization) | 0.5–1.5% / year | Wealthfront, Betterment published data |
| Factor tilts (quality + momentum) | 0.5–2% / year | Fama-French, AQR, Cliff Asness |

**Realistic combined target**: 1–3% annualized outperformance over SPY (passive S&P 500), with comparable or lower max drawdown. Over 20 years at $500K invested, even 1.5% alpha compounds to $200K+ additional wealth.

### Why automation wins

The enemy is not bad stock picks — it's bad behavior:

- **Recency bias**: Selling quality stocks after a 15% drawdown, right before recovery
- **Anchoring**: Holding a broken stock because "it was at ₹500 once"
- **Action bias**: Trading too frequently because it feels like you're "doing something"
- **Analysis paralysis**: Spending 10 hours/week reading stock tips, getting conflicting signals

Automation eliminates all four. The system runs quarterly, follows its rules, and sends you a Telegram message when it's done. Your job is to confirm execution — not to second-guess the methodology every quarter.

---

## 2. Current State

### What's built and working

| Component | Status | Coverage |
|-----------|--------|----------|
| Stock selection — Multibagger | ✅ Production | Indian micro/small/mid-cap, 11 hard filters + 100-pt scoring |
| Stock selection — MFS multi-factor | ✅ Production | 16-factor scoring, 4 strategy presets |
| Stock selection — Value | ✅ Implemented | Indian large-cap, EPV-based, dual-path BFSI/industrial filters |
| Stock selection — US Quality-Momentum | ✅ Production | S&P 500, 6-factor quality+momentum scoring, 3 hard filters |
| Stock selection — Early Multibagger (EBM) | 🟧 Integrated | Regime-gated pre-breakout scoring (VCP/RVOL/pocket-pivot/delivery), PIT snapshots; `TestPickDeterminism` skipped pending regime-cutoff-vs-topN decision |
| Weight optimization | ✅ Production | Inverse-volatility, MFS-proportional, equal-weight |
| Sector caps & redistribution | ✅ Production | Iterative 25% sector cap, 3 stocks/sector, per-stock cap |
| Backtesting engine | ✅ Production | Date-aligned, sell-then-buy, slippage, 7 metrics |
| Monitoring (4-pillar) | ✅ Production | Revenue, cash flow, technical, capital allocation |
| Drift daemon | ✅ Production | Launchd/systemd, 15:45 IST, Telegram/Discord alerts |
| Execution — Zerodha | ✅ Production | Basket orders, GTT, cost model, micro-transaction filter |
| Transaction cost model (India) | ✅ Production | STT, stamp, DP, SEBI, Finance Act 2024 |
| DuckDB cache | ✅ Production | Price + fundamentals, smart expiry |
| Web dashboard | ✅ Production | 5-tab, ECharts, SSE, embedded binary |
| Hysteresis protection | ✅ Production | Prevents churn from small rank fluctuations |
| Excel/CSV ingestion | ✅ Production | ETF/broker file → clean ticker CSV |
| Screener.in integration | ✅ Production | Quarterly result dates, earnings calendar |
| Quarterly autopilot | ✅ Production | Non-interactive pipeline, launchd scheduling, proposal→confirm→execute workflow |
| Schwab API (US broker + market data) | ✅ Production | OAuth2 auth, real-time quotes, price history, order execution, ticker routing |
| Tax-loss harvesting (FIFO + TLH) | ✅ Production | FIFO lot tracking, harvest candidates, wash-sale detection, `basket --tax-optimize`, dashboard Tax tab |
| CLI rendering layer (`pkg/render`) | ✅ Production | stdlib tabwriter tables, color (TTY-aware), formatters (Pct, Currency, Sparkline), panic-safe fallback |

### What's specced but not built

| Component | Spec location | Blocking? |
|-----------|---------------|-----------|
| Tax-optimized rebalancing (FIFO engine) | `docs/feature.md` Feature 1 | ✅ Built for US (`pkg/tax`) — India variant still specced only |
| Options overlay | `docs/feature.md` Feature 2 | No — post-maturity optimization |
| Screener.in deep integration (QoQ, shareholding, CWIP) | `docs/screener.md` | No — enrichment |

### What's missing entirely

| Gap | Impact |
|-----|--------|
| Authoritative US fundamentals (SEC EDGAR) | Schwab fundamentals are thin TTM only — no cash-flow statement, no annual series; US scoring degrades to proxies (see `docs/datasources.md`) |
| US sector classification | ✅ **Addressed (Phase 10a)** — `Fundamentals.Sector` backfilled from the constituents CSV's GICS Sector column, so US sector caps engage instead of collapsing to "Unknown" |
| Data-source provenance in cache | Cannot audit which source produced a number, or invalidate one source selectively |

---

### Known technical debt

| Debt | Location | Impact | Fix effort |
|------|----------|--------|-----------|
| ~~Schwab fundamentals mapper drops derivable fields~~ | ~~`pkg/broker/schwab/market.go` `mapSchwabFundamentals`~~ | **PARTLY RESOLVED (Phase 10a)** — `NetIncome` + `RegularPrice` now derived; `Sector` backfilled from constituents CSV via `stockpicker.InjectSectors` | 🟧 sector-via-CSV done, EDGAR statements → 10c |
| `pick` eliminates all constituents (`0 / N`) | `pkg/stockpicker/filters.go` `isEligible` ← `pkg/broker/schwab/market.go` fundamentals mapper | **RESOLVED (2026-09-18)** — root cause was three mapping bugs (MarketCap ×1e6 inflation, RegularPrice ×1e6, AverageVolume bound to `vol3MonthAvg`=0 instead of `avg3MonthVolume`), zeroing ADV → failing the *liquidity* gate for every ticker (not the size gate as first hypothesized). Fixed + verified via `raw inspect` on live-captured bodies. See Phase 11. | ✅ done |
| `pick` report dir/CSV naming ignores `--index` / `--method` flags | report/basket writers | **RESOLVED (2026-09-20)** — split identity: writer keyed off a `--file`-derived universe name (ignoring `--index`) and four call sites sanitized the `<name>_<method>` token differently. Fixed via a single `stockpicker` identity helper (`SanitizeName`/`DisplayName`/`PickIdentity`, `--name`>`--index`>file-name precedence) routed through writer + cached reader. See Phase 11. | ✅ done |
| Two divergent cache DBs coexist | `data/cache.db` (Sep 8, `source=NULL`, 507 tickers, tax tables) vs `data/mycase.db` (newer schema) | **RESOLVED (2026-09-20)** — verified no live reader (all of `cache`/`pithistory`/`themedb` default to `mycase.db`; only the on-demand `db migrate` reads `cache.db` read-only), and its only unique tables (`tax_lots`/`tax_transactions`/`realized_gains`) were all **empty** — `pkg/tax.Store` recreates them lazily in `mycase.db` via `cache.Conn()`. Backed up to `data/backups/cache_db_retired_20260920.tar.gz` and deleted. `mycase db stats` + `mycase tax status` verified green post-delete. | ✅ done |
| `data/` + `report/` are deeply nested with path-encoded identity | `data/**`, `report/**`; writers in `selectiontracker`, proposal/backup/monitor paths | **DEFERRED (2026-09-20)** — flatten to a single `data/` tree (+ disposable `data/raw/`) with a filename naming convention. Deferred deliberately: highest-effort + only destructive open item, no functional pressure; `stockpicker.PickIdentity` is groundwork for it. | 🟧 later |
| `docs/` sprawl (28 md files, heavy overlap) | `docs/**` | **RESOLVED (2026-09-20)** — consolidated 29 → 24 flat docs with a [`docs/README.md`](docs/README.md) index (start-here / design & subsystems / strategy specs / India-legacy / testing). Merged the two theme docs into one `themes.md`; deleted 5 point-in-time *process artifacts* (resolved bug tracker, one-off audit, completed impl plan, one-off prompt, generic git how-to) whose durable substance already lives in the feature docs (`earlyMB.md`/`value.md`) + git history. Kept every doc that documents running code (incl. India-legacy `themes.md`, `staticip.md`). | ✅ done |

---

## 3. Architecture Vision

The system is a 6-layer responsibility stack (market data → strategy → portfolio construction → execution & tax → autopilot → audit & attribution), US-only via Schwab. For the system design — conceptual layers, the concrete `cmd/pkg/` package breakdown, data flow, ticker routing (`US:`→Schwab, else→Yahoo), and design decisions — see **`docs/architecture.md`** §2 (Inputs), §4 (System Design), and §11 (Design Decisions). This roadmap covers only *what* is being built and *when*.

---

## 4. Phased Roadmap

Completed and dropped phases have been removed from this roadmap; their design detail lives in `docs/architecture.md` (design decisions), `docs/refactor.md` (Completed Phases ledger), and `docs/duckdb-migration.md`. Only active and planned work remains below.

### Carried-over follow-ups (non-blocking)

Small items left open by shipped phases:
- ✅ **DONE** — Plumb `rsi` / `momentum_1y` from the scoring pass to the `selectiontracker.RecordDriverMetrics` site so the `selections` columns persist non-zero. The US quality-momentum selector (`SelectTopNUSQMWithCooldown`) now receives `fullHistory` and records `Momentum1Y` (via `computeMomentumSkip1Mo`) and `RSI` (via `yfinance.CalculateRSI`) at the `RecordDriverMetrics` call site; the round-trip to the `selections` table was already wired.
- ✅ **DONE** — Extend `mycase pipeline diff` to compare selection-level driver metrics between runs. Added a `--metrics`/`-m` flag that appends a "Selection Driver Metrics" section diffing per-ticker RSI, momentum, FCF yield, ROIC, TTM growth, revenue CAGR, and DSO delta (via `cache.GetSelections`) for tickers held in both runs, rendered through `pkg/render`.
- ✅ **DONE (2026-09-20)** — Retired the `.txt`-parsing golden-copy comparison (DuckDB migration deferred item C3). `csvloader.PrintComparisonReport` no longer calls `parseSelectionReport` to read back `report/*_01_selection_reasons.txt`, nor locates the previous run by directory-listing + filename string-sort. Both are deleted. Because `csvloader` is an L0 leaf that must not import `pkg/cache`, the data flow was inverted: `PrintComparisonReport(src, dst, strategy, prevRanks, currRanks map[string]csvloader.RankScore)` now takes the prev/curr rank+score as caller-supplied maps (new exported leaf DTO `csvloader.RankScore`). The two callers, which sit at layers that legitimately import `cache`, source them structurally: **autopilot** (L5) from `db.GetPreviousSelections(goldenBase, strategy)` (previous) + `db.GetSelections(runID)` (this run's freshly-inserted selections); **pick** (`stockpicker` L3) mirrors the existing `loadPreviousDriverStrings` pattern — `db.GetPreviousSelections(displayName, method)` for previous and the in-memory `tracker.RawRanks`/`scores` for current (the pick path has no run_id of its own). Previous-run selection now comes from `LatestRun`'s `started_at DESC` run-identity ordering instead of a filename guess. Output format/colors unchanged; nil maps degrade action strings gracefully. Added `pkg/csvloader/comparison_test.go` (the function had zero coverage): DB-sourced rank/score rationale + nil-degradation. `make build`/`test`/`check-deps`/`cleanup` green; layering stayed intact (csvloader acquired no `cache` import).

---

### Phase 10: Data Source Resilience

**What**: Source each data type from the most authoritative provider that can supply it, with deterministic logged fallback, and record provenance. Today the clean `pick`/autopilot pipeline routes US data through Schwab, but seven other command paths bypass the router and hit Yahoo directly, Schwab's fundamentals are a thin TTM snapshot (no sector, no cash-flow statement, no annual series), and the benchmark is always Yahoo `^GSPC`. Full design, API shapes, provenance chain, and gap analysis live in **`docs/datasources.md`**.

**Why**: Yahoo is a free aggregator reselling a vendor's parse of SEC filings — it is neither authoritative nor stable (unofficial endpoints, legally a scrape). The real origins are: **exchanges** for prices (Schwab is broker-direct, closer than Yahoo), **SEC EDGAR XBRL** for fundamentals (the filing itself), and **GICS/constituents-CSV** for sector. Sourcing authoritatively removes a fragile dependency, fixes silently-broken US sector caps, and upgrades the earnings-quality and ROIC factors from proxies to real inputs. This directly serves the "no black boxes / transparency" design constraint.

**Sub-phases** (each independently shippable, ordered by value-per-effort):

- **Phase 10a — Cheap correctness wins** ✅ **DONE**: populate `Fundamentals.Sector` from the constituents CSV via `stockpicker.InjectSectors` (fixes broken US sector caps that collapsed to "Unknown"); enrich `mapSchwabFundamentals` to derive `NetIncome` (net-profit-margin × TTM revenue) and `RegularPrice` (market cap ÷ shares); deleted the dead `yfinance.GetCache()`. No new source. Note: `RegularPrice`/`NetIncome` are *derived* here; authoritative statement-level figures still arrive in 10c (EDGAR).
- **Phase 10b — Router-bypass cleanup** (refactor **R17**) ✅ **DONE**: threaded a `datafetcher.Router` into the seven bypass paths so every US command routes through Schwab (Yahoo fallback); `cmd/*` uses the `newDataRouter()` factory, `pkg/server` gained a `MarketDataFetcher`/`WithRouter` seam, and `pkg/backtest`/`pkg/autopilot/schedule` use consumer-side interfaces over the `marketdata` leaf to stay within the layering. The benchmark now resolves to `US:SPY` via Schwab (`Router.GetBenchmarkSymbol`/`NormalizeBenchmarkSymbol`) with `^GSPC`/Yahoo fallback. Added `Router.FetchIntradayData` (Yahoo-only), `slog` which-source-served logging on the fallback branches, and a `source` provenance column on the cache `prices`+`fundamentals` tables (idempotent `ADD COLUMN IF NOT EXISTS`; yfinance path tags `"yahoo"`). Schwab-side `source` tagging + the composite merger are deferred to 10c.
- **Phase 10c — SEC EDGAR fundamentals source** ✅ **DONE**: new `pkg/edgar` client (L1; ticker→CIK map cached weekly in `edgar_cik_map`, `companyfacts` fetch cached ~quarterly in `edgar_facts`, both owned via `cache.Conn()`; mandatory `User-Agent` validated at construction + 10 req/s token-bucket limiter via `golang.org/x/time/rate`). The XBRL concept mapper tries an ordered list of candidate us-gaap tags per concept and populates operating cash flow, net income, authoritative FCF (annual OCF − capex), and the full annual series. A `datafetcher` `FundamentalsMerger` composes Schwab TTM ratios + EDGAR statement facts (field-level, non-destructive; EDGAR wins for statements, sector stays from the 10a CSV backfill) with a `source` provenance tag. Wired opt-in into the `Router` (`WithEDGAR`, nil-safe) and gated by `edgar.enabled` in `config/defaults.json` (**now default `true`** with a real `user_agent`; was `false` at ship time until validated live against SEC endpoints — see Phase 10e). Hermetic tests cover the mapper/merger/overlay against fixtures; a live EDGAR test is `//go:build integration`.
- **Phase 10e — Schwab cache-first parity** ✅ **DONE**: the Schwab provider was the only source not wired to the DuckDB serving cache (Yahoo was, so US re-runs re-hit the API every time despite the "cache is truth" rule and a full `data/raw` dump on disk). Fixed by mirroring the Yahoo seam: `pkg/broker/schwab/duckdbcache.go` makes `FetchHistoricalDataWithTimestamps`/`FetchHistoricalByDateRange` check-before-fetch / store-after (`source="schwab"`); **US fundamentals are cached in the Router** (`pkg/datafetcher/fundcache.go`), not the client, because the EDGAR overlay happens Router-side — caching the pre-overlay blob would persist FCF=0 and re-trip the FCF hard-filter. `overlayEDGARAndCache` stores the *merged* blob with its provenance tag, so a warm re-run of the same universe on the same day serves fundamentals warm and skips **both** the Schwab and EDGAR calls. `schwab.SetCache(c)` wired alongside `yfinance.SetCache(c)` in `main.go` + `cmd/serve.go`. Freshness stays market-aware via `marketcal.ClockForTicker` (NYSE for `US:`). `make check-deps` confirms `schwab` (L2) → `cache` (L0) is a legal downward import.
- **Phase 10d — Provider abstraction hardening** (optional, ~2–3 days): split `DataFetcher` into capability interfaces (`PriceSource`, `FundamentalsSource`, `SectorSource`); formalize the ordered fallback chain; surface provenance in `pipeline show`/reports ("FCF: $2.1B [source: EDGAR 10-K 2025-Q4]").

**Deliverables**:
- `pkg/edgar/` — SEC EDGAR client + XBRL concept mapper (Phase 10c)
- `datafetcher.FundamentalsMerger` — composite Schwab + EDGAR + CSV fundamentals (Phase 10c)
- Sector-carrying constituents CSVs + loader wiring (Phase 10a)
- Router wired into `report`/`monitor`/`optimize`/`serve`/`executor`/`backtest`/`autopilot-schedule` (Phase 10b / R17)
- `source` provenance column in the price + fundamentals cache with per-source freshness (Phase 10b/10c)
- `US:SPY`-via-Schwab benchmark with Yahoo fallback (Phase 10b)

**Effort**: ~2–3 weeks total across the four sub-phases. The hard part is Phase 10c's XBRL parsing — filers use custom taxonomy extensions and tags drift over time, so the concept mapper must try an ordered list of candidate tags per concept. The open question (see `docs/datasources.md` §10) is whether to parse EDGAR ourselves or pay a commercial fundamentals vendor to skip it.

**Dependency**: Phase 10a and 10b are independent and both shipped. Phase 10c (shipped) depended on 10b (the merger plugs into the routed path). Phase 10d depends on 10c.

#### Infrastructure & consistency (post-10c, shipped)

- **Market-aware EOD settlement — `pkg/marketcal`** ✅ **DONE**: extracted the EOD-settlement time math into a pure, stdlib-only leaf (`pkg/marketcal`, layer L-1 below the L0 leaves) parameterized by a market `Clock{Loc, CutoffHour}`. `NSE` = Asia/Kolkata @ 21:00 IST (India legacy), `NYSE` = America/New_York @ 16:00 ET (DST-correct via `LoadLocation`); `ClockForTicker` selects by ticker prefix (`US:`/`NYSE:`/`NASDAQ:` → NYSE, else NSE). `marketdata` keeps its four exported EOD funcs as thin NSE-default delegations (+ new `*ForTicker` variants), so existing call sites/tests are unchanged. **Bug fix**: cache freshness (`pkg/cache`) previously judged *all* rows — including US data — against the 21:00 IST clock; `isFreshToday`/`isFreshFundamentals` now take the ticker and use its market clock. Resolves the layering triplication that had copied the settlement math into `cache` and `selectiontracker`. Known limitation: weekend-aware only, no exchange holiday calendar yet.
- **Standardized external-API rate limiting on `golang.org/x/time/rate`** ✅ **DONE**: all three API clients now pace through the same token-bucket package. `pkg/edgar` (10 req/s, from 10c), `pkg/broker/schwab` (migrated off a hand-rolled sliding window → 120 req/min = 2/s, burst 120), and `pkg/yfinance` (new process-wide limiter, 10 req/s burst 20, gated at `executeYFinanceRequest` + the raw timeseries call; overridable via `SetRateLimiter`). Yahoo previously had *no* rate limit — only fixed worker-pool sizes — the 429 risk the API rules warn about.
- **Home-relative config/data resolution** ✅ **DONE**: config was loaded via CWD-relative paths, so the binary only worked from the repo root. Added a home resolver in `pkg/config` (`Home()`/`Path()`/`DataPath()`): precedence `$MYCASE_HOME` > binary-relative (follows a `/usr/local/bin` symlink back to the project tree via `EvalSymlinks`, validated by `config/` existing) > CWD; plus `$MYCASE_CONFIG_DIR`/`$MYCASE_DATA_DIR` overrides. `make install` now symlinks `/usr/local/bin/mycase → dist/mycase` so the installed binary resolves `config/`+`data/` from the tree. **Deferred (R15)**: package-level *write/state* dir constants (`logging.DefaultDir`, daemon PID/state, autopilot proposal dir, snapshot dirs) remain relative — those live in leaf packages and need the dir injected from the composition root (`MYCASE_DATA_DIR` write path).

---

### Phase 11: Data & observability hygiene (planned)

Foundational cleanup surfaced during the 2026-09-15 session. Ordered by dependency —
raw-capture unblocks the fundamentals-mapping fix; formatting groundwork already
shipped.

- **Market-aware formatting — `pkg/marketfmt`** ✅ **DONE (2026-09-15)**: pure,
  zero-import leaf (sibling to `marketcal`, L0) for currency/magnitude formatting —
  US `$` K/M/B/T, India `₹` L/Cr. Wired into `cmd/performance` and the stock-picker
  filter summary / eligibility reasons / rationale, replacing hardcoded `Rs. `/`%.0fCr`
  / `/1e7`. Fixes the nonsensical `500Cr–500000Cr` band label for US runs
  (`$5.0B–$5.0T`, "no cap" when max=0).
- **Raw API response capture + offline replay** ✅ **DONE (2026-09-15)**:
  the API rule "fetch once, analyze offline" is now enforceable end-to-end. New pure
  L0 leaf `pkg/rawcapture` (zero-import, MustBeLeaf; self-configures from
  `MYCASE_CAPTURE` / `MYCASE_REPLAY` toggles + `MYCASE_DATA_DIR` base — no `config`
  import, so it can be called from deep inside the L1/L2 clients).
  **Capture (Phase 1)**: `rawcapture.Capture(source, endpoint, symbol, body)` is wired
  into the single chokepoint of each client — `schwab.Client.executeRequest`
  (captures **2xx** market-data/trader bodies only) and yfinance
  `executeYFinanceRequest` (+ the fundamentals-timeseries `client.Do` that bypasses
  it). On a 2xx it buffers the body, writes raw bytes flat to a disposable
  `data/raw/<source>__<endpoint>__<symbol>__<YYYYMMDD-HHMMSS>.json`, and returns a
  fresh `io.ReadCloser` over the same bytes so callers are unchanged; disabled (the
  default) it returns the body untouched with zero overhead.
  **Replay (Phase 2)**: with `MYCASE_REPLAY` set, each chokepoint calls
  `rawcapture.Replay(source, endpoint, symbol)` *first* — on a hit it returns the
  newest matching archive file's body as a synthetic 200 and **short-circuits the
  network, token fetch, and rate limiter entirely** (Schwab logs `schwab.replay_hit`).
  `findLatest` globs by the filename fields (newest wins via the lexically-sortable
  stamp) and guards field-count so a symbol-less request never matches a
  symbol-carrying file. This lets `pick`/`report`/etc. rerun the whole pipeline
  against recorded responses with zero live calls — the fundamentals-mapping bug
  below is now debuggable fully offline. **Secret-safe by construction**: Schwab OAuth
  token-exchange/refresh flows through `auth.go`'s `tokenURL` path, which never
  reaches `executeRequest`/`Capture`/`Replay`, so credentials are never archived or
  replayed; non-2xx (incl. 401) bodies are skipped. Implemented as env toggles
  (`MYCASE_CAPTURE` / `MYCASE_REPLAY`) rather than CLI flags — originally both off by
  default; capture flipped to **on by default** in R-store-2 (see below), with
  `MYCASE_CAPTURE=0` as the opt-out.
  **Follow-up (capture-as-default + retention) is designed below** — see
  *"Raw response store — capture-by-default + retention (design)"*.

#### Raw response store — capture-by-default + retention (design, 2026-09-15)

Design conclusion from a working-through of *why* we built capture/replay. The
motivating incident: Schwab returned HTTP **200** hundreds of times but the report
came out empty, and the bodies had been **thrown away** — so we could not tell a
**data issue** (Schwab genuinely returned zeros/thin fields) from a **code issue**
(`mapSchwabFundamentals` mis-parsed a full body). Transport was never in doubt (200s);
the lost evidence was. This reframes the feature.

**Decisions reached:**

1. **Three independent activities, not one "offline mode":** *capture* (record a
   response), *triage* (read/understand recorded responses — the primary need, and
   the current gap), *replay* (re-serve them). Replay is a distant third — it only
   pays off *after* triage has localized a bug to code; it delivered nothing for the
   "200-but-empty" incident. Capture + triage is the real value.

2. **Capture must be ON by default, not on-demand.** On-demand capture is
   after-the-fact: the run that surprises you is the run you didn't arm. The failure
   mode was evidence loss, so default-on. Sampling (save X%) is rejected — it is a
   throughput answer to a problem we don't have (quarterly, ~500 tickers, single-digit
   MB/run) and statistically drops exactly the one weird ticker triage needs.

3. **Bound growth by retention, not by not-capturing.** "Define *broken*, not *okay*."
   Correctness-gated pruning is a tar pit — subtle bugs *look okay*, so an "okay →
   prune" rule deletes precisely the interesting evidence. Instead: **keep everything
   recent + anything flagged broken; prune only the boring old middle.** Delete
   triggers are *age*, *total size*, and (optionally, later) *run known-not-broken*.
   False-keep is cheap (disk); false-delete is catastrophic (unreproducible bug).

4. **Layering: split `rawcapture` into a leaf hook + an injected high store.** The
   env-duplication smell (`rawcapture` re-reading `MYCASE_DATA_DIR` instead of calling
   `config`) is *not* "config is at the wrong layer" (config is correctly L0). It is a
   **non-leaf policy hiding inside a leaf**. `rawcapture` is called from `yfinance`(L1)
   and `schwab`(L2), so its *hook* half must stay below L1 (L0). But retention /
   run-grouping / config / verdicts want to be high. Resolve by **dependency
   inversion**, not by relaxing layering:
   - **Leaf (L0, stays `rawcapture`, `MustBeLeaf`, zero imports):** declares a `Sink`
     interface (`Write(source, endpoint, symbol, body)`, `Open(source, endpoint,
     symbol) (io.ReadCloser, bool)`), a settable package var + `SetSink`, and thin
     `Capture`/`Replay` delegators. **Holds no filenames, no directories, no config** —
     both write and replay route through the injected `Sink`. Inert (silent no-op)
     until a sink is wired, which is correct for tests/library use.
   - **`rawstore` (new, L4):** *implements* `Sink`; owns the data dir (via `config`),
     the filename convention (`sanitize`/`Filename`/glob move here — the writer/reader
     contract lives in one place, at the only layer that constructs names), run
     identity (reuse the existing `req_id` from `main`'s `Before` hook / `ctx`), and
     retention policy. May import `config`/`logging`/`marketcal` legally.
   - **`main` `Before`:** constructs `rawstore`, calls `rawcapture.SetSink(store)`,
     supplies req_id + config. Only the composition root names `rawstore`; nothing
     below L4 references it (arrows always point down; `check-deps` stays green).
   - This is the architecture-doc pattern (consumer-defined interface in a leaf;
     domains own their persistence) — same shape as `attribution.Store`/`tax.Store`.

5. **Extensible (not generic) triage.** A generic "understand any response" engine is a
   worse `jq`. Instead a stable, schema-blind spine (list/show by filename fields —
   works for *every* source, incl. future EDGAR, for free) plus a small **per-source
   field inspector** that lives *with its source* and registers in — Schwab's inspector
   imports Schwab's mapper, EDGAR's imports EDGAR's XBRL mapper. Adding a source's
   inspector touches only that source, never the spine or other inspectors. The
   inspector's value is showing **raw-wire value vs. what production mapped** (the diff
   that localizes a `0/N`-style mapping bug); it must reuse the production parser to
   stay truthful yet surface the raw field even when production drops it. Contract must
   be expressible for XBRL without contortion — validate against Schwab **and** EDGAR
   before trusting it (two implementations prove extensibility; one bakes in Schwab's
   shape).

**Task sequence** (dependency-ordered; each independently shippable):

- ✅ **DONE — R-store-1 — Split `rawcapture` into leaf + `rawstore`, wire in `main`.**
  `rawcapture` (L0, `MustBeLeaf`) now declares the `Sink` interface
  (`Write(source, endpoint, symbol, body []byte)`, `Open(source, endpoint, symbol)
  (io.ReadCloser, bool)`), a `sync.RWMutex`-guarded sink var + `SetSink`, and thin
  `Capture`/`Replay` delegators that hold no filenames/dirs/config and are inert
  no-ops until a sink is wired (correct for tests/library use). All dir/config
  resolution, the `<source>__<endpoint>__<symbol>__<stamp>.json` filename convention
  (`sanitize`/`Filename`/`findLatest`/glob), and run identity moved to a new L4
  `rawstore` (`New(base, reqID)` / `NewDefault(reqID)` over `config.DataDir()`;
  `reqID` reserved for later retention/run-grouping, not yet encoded in filenames).
  `main`'s `Before` hook constructs the store and calls
  `rawcapture.SetSink(rawstore.NewDefault(reqID))`. No behavior change: still
  env-gated (`MYCASE_CAPTURE` / `MYCASE_REPLAY`), still off by default. `rawstore`
  registered at L4 in `layers.go`; `make check-deps`, `make test`, `make build` green.
  *This is the enabling refactor; everything below builds on it.*
- ✅ **DONE — R-store-2 — Capture ON by default + clean opt-out.** Wiring the sink
  (in `main`'s `Before`) turns capture on: `rawcapture.Enabled()` is now default-on,
  suppressed only by an explicit falsy `MYCASE_CAPTURE` (`0`/`false`/`no`/`off`, via a
  new `falsy()` helper) or by replay mode (`Enabled = !falsy && !ReplayEnabled`, so
  replayed bytes are never re-archived). The inert-without-sink guarantee is
  preserved — `Capture` still no-ops when no sink is wired, so tests/library callers
  archive nothing despite the default-on stance, and `main` remains the sole
  `SetSink` caller. `rawstore.Write` emits a Debug `rawstore.captured` event
  (`source`/`endpoint`/`symbol`/`file`/`bytes` — never the body); the `req_id` is
  attached by the default logger (`main`'s `Before` hook), making every capture
  attributable to its run without changing the on-disk filename convention.
  `rawstore` retains the `req_id` (with a `ReqID()` accessor) for later
  run-grouping/retention. Verified end-to-end: a `backtest` run with default env
  archived the Yahoo chart responses to `data/raw/`; the same run with
  `MYCASE_CAPTURE=0` archived nothing. `make check-deps`/`test`/`build` + staticcheck
  green.
- ✅ **DONE — R-store-3 — Retention: age + size ceiling (flat, no run/verdict).**
  Bounds archive growth by two independent ceilings (a file is pruned if it
  violates *either*): **age** (`raw.retain_days`, default 14) and **total size**
  (`raw.max_size_mb`, default 512, oldest-first eviction until under the cap). Both
  live in `config/defaults.json`'s new `raw` block, env-overridable
  (`MYCASE_RAW_RETAIN_DAYS` / `MYCASE_RAW_MAX_SIZE_MB`) and flag-overridable, with
  the standard flag>env>config>default precedence resolved once in
  `rawstore.ResolveRetention`. `(*Store).Prune` is best-effort (unreadable entries
  and individual delete failures are skipped, never propagated), only touches files
  matching the archive naming convention (foreign files untouched), and emits a
  single `rawstore.pruned` Info summary (never bodies). Runs inline at process exit
  via `main`'s `After` hook (skipped in replay mode) **and** exposed as
  `mycase raw prune [--retain-days N] [--max-size-mb N]` (0 disables a dimension).
  Minimal complete growth-bound; no run/verdict concept. `make
  check-deps`/`test`/`build`/`cleanup` green.
- ✅ **DONE — R-store-4 (Tier-1 triage) — `mycase raw` command group.** Schema-blind
  triage over the archive, complementing R-store-3's prune. `raw ls` parses the
  filename convention into columns (source, endpoint, symbol, when, size), newest-first,
  with case-insensitive `--source`/`--endpoint`/`--symbol` substring filters and `-n`
  limit; `raw show [query]` pretty-prints the newest matching capture (JSON indented,
  non-JSON verbatim); `raw path [query]` prints just the path for piping
  (`jless "$(mycase raw path AAPL)"`). `query` matches the symbol first, then the whole
  filename, so both `raw show AAPL` and `raw show schwab__quotes` work; empty query =
  newest capture overall. Backed by exported `rawstore.ParseFilename` (inverse of
  `Filename`), `(*Store).List(ListFilter)`, and `(*Store).ResolvePath` — all
  schema-blind, so they work for every source (schwab, yahoo, future EDGAR) the day it
  captures. `make check-deps`/`test`/`build`/`cleanup` green.
- ✅ **DONE — R-store-5 (Tier-2 triage) — Schwab-fundamentals field inspector.**
  `mycase raw inspect [symbol]` parses the newest archived Schwab `/instruments`
  (`projection=fundamental`) body **exactly as production does** (`encoding/json` →
  `InstrumentResponse`), reruns `mapSchwabFundamentals`, and renders each raw wire
  field beside the `marketdata.Fundamentals` value it produced, plus diagnostics.
  Purpose-built for the `pick 0/N` bug: because the mapper never zeros a nonzero
  input, a mapped `MarketCap == 0` means the wire value didn't bind — the inspector
  disambiguates the two ways that happens: **(A)** the wire genuinely sends
  `marketCap: 0`/absent (a *data* issue), or **(B)** the value arrives under a key
  the `Fundamental` struct doesn't declare (e.g. `marketCapInMillions`), which
  `encoding/json` silently drops (a *code* issue, fixable by struct tag). It
  surfaces (B) via a `map[string]json.RawMessage` cross-check that names any
  unbound wire keys, and also reports the empty-instruments / null-fundamental
  envelopes that make `FetchFundamentals` skip a ticker. Lives in
  `pkg/broker/schwab/inspect.go` (with its source, importing the real mapper +
  wire structs so the raw-vs-mapped comparison is truthful); `cmd/raw.go` wires it
  (the composition root can import both the L4 store and the L2 source — no `pkg/`
  package can). Hermetic tests cover healthy / zero-marketCap / renamed-key /
  null-fundamental / empty-instruments / invalid-JSON. **Contract validation
  against a second (XBRL) source is deferred to whenever EDGAR misbehaves**, per the
  R-store-5 note. `make check-deps`/`test`/`build`/`cleanup` green.
- ⬜ **R-store-6 (optional) — Verdict-driven early deletion.** Only if clean captures
  start crowding out interesting ones. Needs a *run + outcome* primitive: keep-floor
  (recent + flagged-broken runs pinned) vs prune-ceiling (plausibly-fine old runs
  deleted early). Lean entirely on cheap "broken" signals already emitted (`0/N`, empty
  report, high fetch-failure rate) — never attempt to define "correct". Highest risk of
  deleting the wrong thing; defer until demonstrably needed.
- ⬜ **R-store-7 — Review the replay feature's design & usefulness.** The
  capture + triage half of the raw-store has now proven itself in anger: the
  2026-09-18 session root-caused **three** distinct data bugs (`pick` `0/N` Schwab
  mapping, the 118 EDGAR `FCF=0` eliminations, the XOM shell-CIK) *entirely offline*
  against captured/cached bodies, and verified the fixes by re-running the real
  mappers over the same evidence — zero live API calls. **Replay
  (`MYCASE_REPLAY`) delivered none of that** and was never invoked, exactly as the
  R-store design note predicted ("replay is a distant third; it only pays off after
  triage has localized a bug to *code*"). Before investing further, decide replay's
  fate:
  - **Is it earning its keep?** It adds a chokepoint branch + a synthetic-200 path in
    every client (`schwab.replay_hit`, yfinance) and a `findLatest` glob contract that
    must stay in lock-step with the capture filename convention — real surface area
    and a coupling risk for a feature with no demonstrated use.
  - **If kept**, define the concrete workflow it wins (e.g. deterministic regression
    of a whole `pick`/pipeline run against a frozen fixture set; hermetic repro of a
    reported bug) and add a test that exercises the end-to-end replayed run so it
    can't silently rot. Note the gap this session exposed: EDGAR bodies are **not**
    archived to `data/raw/` the way the Schwab/Yahoo chokepoints are (the incomplete
    479/501 pass surfaced as "missing" rather than replayable), so a
    replay-a-whole-pick story is currently only partial.
  - **If dropped**, remove the `Replay`/`findLatest` machinery and the per-client
    replay branches, keeping capture + triage (the proven value) — smaller, clearer,
    one fewer contract to maintain.
  Not urgent; schedule a deliberate keep-or-cut review rather than letting it drift as
  untested dead-ish code.


- **Fix `pick` `0 / N` — Schwab fundamentals mapping** ✅ **DONE (2026-09-18)**:
  root-caused via `mycase raw inspect` against the Sep-17 live-captured
  `/instruments` bodies (~1005 tickers in `data/raw/`). The `0/N` was **not** the
  hypothesized market-cap-gate zero — it was the **ADV liquidity gate** zeroing out.
  Three mapping bugs in `mapSchwabFundamentals` / the `Fundamental` struct
  (`pkg/broker/schwab/market.go` + `types.go`), all disproven by the raw wire:
  (1) **`MarketCap` was multiplied by `1_000_000`** on a wrong "reported in millions"
  assumption — Schwab sends absolute dollars (`marketCap:4.84e12` for AAPL), so the
  scale inflated it to `4.84e18`; (2) **`RegularPrice`** inherited the same ×1e6
  inflation ($331M/share instead of $331); (3) **`AverageVolume` bound the wrong wire
  key** — the struct read `vol3MonthAvg` (Schwab sends `0.0`) instead of
  `avg3MonthVolume` (the real 52.3M for AAPL). Bugs (1)+(3) together made
  `ADV = AverageVolume × RegularPrice = 0`, failing the `min_adv` $50M gate for
  **every** US constituent → `0/N`. Fix: dropped both ×1e6 scalings, added the
  `Avg3MonthVolume` struct field + bound it, corrected the misleading `// in millions`
  comments, and updated the `raw inspect` field map + known-keys set. Verified with
  the inspector on real data: AAPL now maps MarketCap $4.84T, ADV ≈ $17B (passes),
  RegularPrice $331.34. `RevenueTTM` remains 0 (Schwab genuinely omits it on the
  wire — a real data gap EDGAR fills, not a mapping bug). Tests updated
  (`market_test.go`, `inspect_test.go`, `datafetcher/router_test.go`);
  `make test`/`check-deps`/`cleanup`/`build` green. Remaining operator step is a
  fresh live `pick` run (see "Next up") to confirm a non-zero funnel end-to-end.
- **Fix EDGAR FCF binding — capex tag breadth + tag-shadow fall-through** ✅ **DONE (2026-09-18)**:
  the first EDGAR-enabled `pick` still eliminated **118** cash-rich names as
  `FCF $0M ≤ 0`. Root-caused offline against the cached companyfacts blobs: the
  XBRL concept mapper (`pkg/edgar/concepts.go`) tried a **single** capex tag
  (`PaymentsToAcquirePropertyPlantAndEquipment`), but many large filers report capex
  under alternatives — verified distribution across the 118: `PaymentsToAcquireProductiveAssets`
  (62 — Visa/Qualcomm/Verizon/Chevron/Home Depot), `PaymentsForCapitalImprovements`
  (10 — REITs), plus O&G-property and other-productive-asset variants. Two fixes:
  (1) expanded `tagsCapEx` to 7 candidates; (2) fixed a **tag-shadow bug** — the old
  `firstPresentTag` locked onto the first candidate that merely *existed* as a key,
  so a present-but-empty tag (FTNT/PANW/ISRG/SRE/HPQ declare the classic capex tag
  with zero FY facts) shadowed a later populated one. `annualSeries`/`latestValue`
  now iterate all candidates and skip empties. Verified via production `mapFacts`
  against real blobs: Visa FCF $21.58B, PANW $4.11B, ISRG $2.49B (all were 0). Tests
  added; `make test`/`cleanup`/`build` green.
- **Sector-aware FCF exemption (Financials + Real Estate)** ✅ **DONE (2026-09-18)**:
  banks/insurers/REITs (~23 of the FCF=0 eliminations — JPM, BAC, MS, WFC, PLD…) do
  not report capex in the industrial sense, so authoritative OCF−capex FCF is
  structurally 0/undefined for them; a positive-FCF gate wrongly eliminated the whole
  sleeve. Added `isFCFExemptSector` (`pkg/stockpicker/scoring_us.go`, covers
  `yfinance.IsFinancialSector` + "Real Estate") that skips **only** the FCF gate in
  `ApplyUSHardFilters`; exempt names still face the market-cap + ADV gates and full
  scoring (they earn 0 on the 20%-weight FCF-yield factor, competing on the other
  80%). Tests added.
- **Fix bad EDGAR CIK mapping (XOM)** ✅ **DONE (2026-09-18)**: SEC's own
  `company_tickers.json` (and the constituents CSV's CIK column) map `XOM` to a
  2024-registered "Exxon Mobil Corporation" shell (CIK 2115436, **0 FY facts**), not
  the real 42-year filer (CIK 34088, OCF $51.97B / capex $28.36B → FCF ≈ $23.6B), so
  the EDGAR overlay silently no-oped and XOM degraded to Schwab-only → FCF gate. Added
  a small, documented, evidence-based `cikOverrides` map in `pkg/edgar/cik.go`
  (`{"XOM": 34088}`), applied at the top of `CIK()` so it wins over the upstream file.
  The other 18 "missing-facts" tickers were **not** bugs: most (APO, BG, BLK, CEG,
  FERG, GEV, KVUE, RDDT, TKO, VLTO, SW, SNDK, SOLV) have correct CIKs and just weren't
  cached in the incomplete first pass (self-heal on re-run); the rest (HONA, FDXF, Q,
  GEHC) are genuinely new 2025–26 spinoffs/IPOs with no/thin filings. Test added.
- **Guard nonsensical ROE from negative book equity** ✅ **DONE (2026-09-18)**:
  heavy-buyback firms (MAS, MCK, BKNG) carry **negative book equity** (P/B < 0),
  making Schwab's reported ROE economically meaningless (MAS +5862%, MCK −490%). ROE
  feeds scoring only via `computeROIC`'s last-resort fallback, but a single such
  outlier would blow out the cross-sectional min-max ROIC normalization and squash
  every other stock's ROIC score. Fixes in `pkg/stockpicker/scoring_us.go`:
  `computeROIC` uses the ROE fallback only when `PBRatio > 0` (else returns 0, no
  signal); new `clampROIC` bounds all capital-efficiency ratios to ±100% on every
  path; the transparency driver string now prints `n/m (neg. book equity)` instead of
  an absurd percentage. Tests added.
- **`edgar_facts` blob bloat — store extracted facts, not raw companyfacts** ✅ **DONE (2026-09-18)**:
  the post-run DB had ballooned to **3.6 GB on disk** (~1.9 GB logical in `edgar_facts`
  alone — 479 raw SEC companyfacts JSON blobs, avg ~3.9 MB, max 9.2 MB — plus dead
  pages from upsert/delete churn). Each blob carried every XBRL concept a company ever
  filed, while `mapFacts` extracts only ~11. Fix: `edgar_facts` now stores the compact
  **extracted** `marketdata.Fundamentals` (`facts_json` + `entity_name` +
  `schema_version`), not the raw blob — `data/raw/` already owns full-body retention.
  `fetchFacts` parses+maps+stores compact internally and returns the mapped struct;
  `schema_version` (const `factsSchemaVersion`) makes a future mapper change treat old
  rows as a miss and re-derive. In-place migration `mycase db migrate-edgar-facts`
  (`pkg/edgar/migrate.go` `MigrateFactsBlobs`) re-derives compact rows from the existing
  blobs **with no EDGAR re-fetch** (idempotent), then `reclaimDBFile` rewrites the DB to
  a fresh file (`ATTACH` + `COPY FROM DATABASE`, `.bak` kept) to reclaim dead space.
  Verified on the live DB: **3599 MB → 18 MB**, all 479 rows + `entity_name` preserved,
  FCF re-derived correctly (Visa $21.58B, AAPL $98.8B, NVDA $102.6B; JPM 0 = bank).
  New reference doc **`docs/edgar-facts-reference.md`** catalogues the wider companyfacts
  universe (fields we extract today + candidate facts/tags for future factors:
  stockholders-equity→authoritative ROE, dividends+buybacks→shareholder yield,
  D&A→EBITDA, EPS/shares, R&D, balance-sheet depth) to guide roadmap evolution.
  Measured extracted-vs-raw compression: 437×–2115×. `make build`/`cleanup` green.
- **Fix `pick` report/CSV naming** ✅ **DONE (2026-09-20)**: report dir, index-picks
  CSV, proposal CSV, incubator CSV, and PIT-snapshot filenames now derive from the
  actual `--index`/`--method` (and `--name`) flags, not a `--file`-derived universe
  name. Root cause was a split identity: the writer (`stockpicker.RunWithResult`)
  keyed everything off `tickersSrc.Name` — which on the `--file` path is
  `csvloader.GetUniverseName(csv)` (filename-derived, strips method words, ignores
  `--index`), so `--index sp500 --method us_quality_momentum` run against a golden
  `data/microsmall.csv` filed under `us_microsmall_multibagger/`. Compounding it,
  four call sites sanitized the `<name>_<method>` token four different ways
  (`SaveReport` lowercased+space→_; `snapshot.go` comma/space/^ ; run.go CSV paths
  raw; the cached-run reader in `cmd/pick.go` its own), so writer and reader could
  disagree on the path. Fix: new `pkg/stockpicker/identity.go` with the single
  `SanitizeName` / `DisplayName(opts, loadedName)` / `PickIdentity(name, method)`
  source of truth. `DisplayName` precedence is `--name` > `--index` > loaded
  (file-derived) name, so an explicit index anchors the identity. `SaveReport` now
  takes the caller-built identity token (keeps `selectiontracker` L0 — no import of
  L3 `stockpicker`); `run.go`, `snapshot.go`, and `cmd/pick.go`'s cached reader all
  route through `PickIdentity`/`SanitizeName` so writer and reader compose identical
  paths. The "Strategy Preset:" label already rendered `opts.Method` verbatim and
  `--method` already won over the config default (an earlier fix), so that half was
  correct; this change fixes the index/display-name half. Tests added
  (`identity_test.go`: sanitize, flag precedence, the exact `sp500` regression);
  `make build`/`test`/`check-deps`/`cleanup` green. The `pipeline` path was already
  correct (it sets `IndexName`/`DisplayName` = `src.name` and controls the CSV via
  `OutputFile`) and stays consistent.
- **Flatten `data/` + `report/`** ⬜ **DEFERRED (revisit later)**: collapse both nested trees into one
  flat `data/` plus a single disposable `data/raw/`; `report/` goes away. Filename
  convention carries the identity the paths used to:
  `<domain>__<portfolio>__<method>__<YYYYMMDD-HHMMSS>__<kind>.<ext>` (double-underscore
  field separator; `domain` ∈ universe/pick/proposal/backup/report/sim/research;
  `method`/`stamp` = `na` when N/A). E.g.
  `report/sp500_multibagger/simulations/20260915_000347_monitoring.txt` →
  `sim__sp500__multibagger__20260915-000347__monitoring.txt`. Centralize name
  construction in one path helper (`pkg/config` `DataPath` family) so writers compose
  and readers glob identically. Writers to change: `selectiontracker.SaveReport`,
  proposal/basket/backup/monitoring/scuttlebutt writers, golden-copy CSV resolution.
  Destructive (deletes `report/` + old `data/**`) — needs sign-off; prefer clean
  cutover after new writers verified. **Deferred deliberately (2026-09-20)**: highest
  effort + only destructive item left, and it's organizational cleanup with no
  functional pressure. Groundwork already exists — `stockpicker.PickIdentity`/
  `SanitizeName` (the #2 naming fix) is the centralized primitive this will build on.
- **Retire stale `data/cache.db`** ✅ **DONE (2026-09-20)**: `cache.db` (Sep 8,
  `source=NULL`) was a pre-consolidation artifact. Confirmed safe to delete: (1) no
  live reader — `pkg/cache`, `pkg/pithistory`, and `pkg/themedb` all default to
  `data/mycase.db`, and the only code referencing `cache.db` is the on-demand
  `mycase db migrate` command (reads it read-only); (2) `mycase.db` is a strict
  superset of `cache.db`'s tables **except** `tax_lots`/`tax_transactions`/
  `realized_gains`, all of which were **empty** in `cache.db` and are recreated
  lazily in `mycase.db` by `pkg/tax.Store` (via `cache.Conn()`, "domains own their
  persistence"); (3) `cache.db`'s populated data (stale Sep-8 `prices`/`fundamentals`/
  `cache_meta`) is re-fetchable market cache already superseded by fresher rows in
  `mycase.db`. Backed up to `data/backups/cache_db_retired_20260920.tar.gz` (gitignored,
  untracked) then deleted. Post-delete verification: `mycase db stats` reports all 19
  tables ONLINE; `mycase tax status` ran clean and recreated the three tax tables in
  `mycase.db`. Single DB at `data/mycase.db`.
- **Docs consolidation** ✅ **DONE (2026-09-20)**: consolidated `docs/` from 29 md
  files with heavy overlap + no index down to **24 flat, indexed docs**. Added
  [`docs/README.md`](README.md) as the navigable index (start-here / design &
  subsystems / strategy specs / India-legacy subsystems / testing / diagrams).
  Consolidation actions:
  - **Merged** the two theme docs (`ThemeBasedReturn.md` + `ThemeDatabase.md`) into a
    single `themes.md` covering the lifecycle DB (`pkg/themedb`) and exact-return
    engine (`pkg/themereturn`).
  - **Deleted 5 point-in-time process artifacts** whose durable substance already
    lives in the feature docs + git history: `Bugs_EMB.md` (resolved bug tracker →
    `earlyMB.md` §27), `DataAudit.md` (one-off audit → `earlyMB.md` §26 +
    `multibagger.md`), `value_impl.md` (completed impl plan; `value.md` is the
    spec-of-record), `embPrompt.md` (one-off operator prompt), `git-sync-guide.md`
    (generic git how-to, no project code). Fixed the earlyMB/multibagger cross-links
    that had pointed at the deleted audit/bug docs (now inline "§26/§27" references).
  - **Kept every doc that documents running code**, incl. India-legacy: promoted
    `executorError.md` → `executor-retry.md` (`pkg/executor`/`cmd/retry`), and kept
    `staticip.md` (documents the staticip.in proxy `pkg/yfinance` actively bypasses).
  - Governing rule going forward (in the index): **one doc per feature/subsystem, not
    per implementation step** — plans/progress go in this roadmap; don't spawn
    per-feature bug trackers / impl plans / audit snapshots as standalone docs.
  - Did **not** introduce `strategies/`/`design/` subdirectories: several docs
    (`edgar-design`, `datasources`, `refactor`, `architecture`, `roadmap`,
    `edgar-facts-reference`) are hard-referenced by `docs/…md` path from Go source
    comments and `.kiro/steering/*`, so a flat tree avoids breaking those refs. The
    stale `docs/plans/docs-consolidation.md` reference was never created and is moot.

---

### Phase 6: Options Overlay (Post-Maturity)


**What**: Once the portfolio is stable and well-tracked (6+ months live), add an options overlay for income generation and tail-risk hedging.

**Why**: For a portfolio of 15-20 quality stocks, covered calls on over-weight positions generate 2-5% additional annual income. Protective puts on concentrated positions hedge black-swan events. This is an optimization on a working system — not a priority for a system that's still being built.

**Prerequisite**: Schwab options chain API (included in Market Data Production bundle) + 6 months of live portfolio tracking to understand position sizing and volatility profiles.

**Deliverables** (future):
- `pkg/options/overlay.go` — strike/expiry selection engine
- Covered call candidates: positions > 100 shares, IV rank > 30, overweight vs target
- Protective put candidates: concentrated positions (> 8% weight), earnings approaching
- `mycase options suggest` — weekly option overlay recommendations
- Integration with Schwab order API for option execution

**Effort**: ~4 weeks. Deferred because it requires a stable, tracked portfolio and options expertise.

---

### Timeline Summary

Active and planned phases only (completed/dropped phases removed):

| Phase | Target | Dependency | Core value delivered | Status |
|-------|--------|------------|---------------------|--------|
| 10. Data Source Resilience | Q4 2026 | Phase 2 (Schwab) | Authoritative US data (SEC EDGAR), Schwab everywhere, provenance | 🟧 10a+10b+10c done, 10d pending |
| 11. Data & observability hygiene | Q4 2026 | none | Raw-response capture/replay (done); raw-store split + capture-by-default + retention + triage; pick `0/N` fix | 🟧 in progress |
| 6. Options Overlay | H2 2027 | 6mo live data | Income optimization | ⬜ |

---

## 5. Success Metrics

### Primary metric: Net-of-cost, net-of-tax CAGR vs benchmark

**Benchmark**: SPY (S&P 500 ETF). This is what you'd get from one index fund with zero effort.

**Target**: Outperform SPY by 1–3% annualized over a rolling 3-year window.

**Measurement**: Begin tracking from the first fully-automated quarterly rebalance. Report monthly. Meaningful statistical significance requires 3+ years of data.

### Secondary metrics

| Metric | Target | Why it matters |
|--------|--------|---------------|
| Max Drawdown | < benchmark drawdown | If we take more risk for the same return, the system is broken |
| Sharpe Ratio | > benchmark Sharpe | Risk-adjusted return, not just absolute return |
| Turnover | < 30% annually | Low turnover = low costs + low tax drag |
| Time spent by investor | < 1 hour/quarter | The entire point is removing busy work |
| Rebalance discipline | 100% execution rate | Never skip a scheduled rebalance (emotional override = system failure) |
| Tax savings | Track $ saved via TLH annually | Concrete, measurable benefit |

### Failure conditions (when to simplify to pure index funds)

- 3 consecutive years of negative alpha after costs → the factor tilts aren't working in the current regime
- Max drawdown exceeds benchmark by > 10% → the concentration risk isn't worth it
- System requires > 2 hours/quarter of manual intervention → automation has failed
- Investor overrides the system more than once per year → behavioral discipline has broken down

If any failure condition triggers: simplify to VOO/VTI (passive S&P 500 / total market index) and stop trying to outperform. Knowing when to quit is part of the system.

---

## 6. Anti-Goals

These are things we explicitly will **not** build or pursue:

### Won't build

| Anti-goal | Why |
|-----------|-----|
| Day trading or intraday strategies | Negative-sum after costs; requires constant attention (opposite of our goal) |
| Crypto/NFT/speculative assets | Outside competence; no fundamental valuation framework applies |
| Leverage / margin trading | Amplifies behavioral errors; can blow up the portfolio |
| AI/ML black-box stock prediction | Violates transparency principle; overfits to noise; can't be trusted in drawdowns |
| Social/sentiment trading signals | Twitter tips, Reddit sentiment, etc. — noise, not signal, at our timescale |
| High-frequency or latency-sensitive execution | We rebalance quarterly; microsecond edge is irrelevant |
| Multi-user SaaS | This is a personal tool; no desire to manage other people's money |

### Won't pursue

| Anti-goal | Why |
|-----------|-----|
| > 5% annual alpha | Unrealistic target leads to over-trading, excessive risk, and eventual blowup |
| Perfect market timing | Impossible; system stays fully invested through all conditions |
| Zero drawdowns | Drawdowns are the price of equity returns; we accept them, we don't avoid them |
| Beating the market every quarter | Factor tilts underperform for years at a time; the edge is long-term |
| Complex derivatives strategies | Covered calls/puts (Phase 6) are the ceiling; no multi-leg spreads, no straddles |

### Design constraints (inherited from vision.md)

- **Transparency**: Every score, filter, and weight must be explainable from the output
- **Local-first**: No cloud service, no subscription, no external dependencies beyond broker APIs and market data
- **Investor-in-the-loop for execution**: System recommends; investor confirms; orders fire. Never auto-execute without explicit opt-in
- **No black boxes**: If the investor can't explain why a stock is in the portfolio by reading the report, the system has failed

---

## Appendix: How Each Phase Maps to Alpha Sources

| Alpha source | Which phase delivers it | Expected contribution |
|--------------|------------------------|----------------------|
| Behavioral discipline | Phase 1 (autopilot removes temptation to override) | 1–2% / year |
| Rebalancing premium | Phase 1 (quarterly rebalance) | 0.3–0.8% / year |
| Factor tilts (US) | Phase 3 (quality + momentum) | 0.5–1.5% / year |
| Pipeline reliability | Phase 7 (DuckDB migration — atomic writes, run history) | Operational quality (no lost data) |
| Data source integrity | Phase 10 (authoritative SEC EDGAR fundamentals, Schwab prices, provenance) | Operational quality (correct inputs, no silent Yahoo scrape drift) |
| Tax-loss harvesting | Phase 4 | 0.5–1.5% / year (tax savings) |
| Performance awareness | Phase 5 (know when to simplify) | Prevents compounding losses from a broken strategy |
| Options income | Phase 6 | 1–3% / year on mature portfolio |

**Net expected alpha (Phases 1–5, conservative)**: 1.5–3% annually over SPY, at comparable max drawdown.

**Net expected alpha (all phases, optimistic)**: 3–5% annually. This is the upper bound; don't plan around it.

---

## Appendix B: Future Explorations (Parked)

Ideas worth revisiting once the core system is stable and the ecosystem matures.

### Native macOS App (SwiftUI + DuckDB)

**Revisit when**: macOS 27, DuckDB 2.0, and the core system is stable with 6+ months of live tracking.

**Why it might be better**: The current web dashboard (`mycase serve` + browser) works but has friction — starting a server, opening a tab, no native notifications. More critically, the pipeline's "pause for human editing" step currently requires opening a raw CSV in a text editor — zero context, zero validation, easy to break.

A SwiftUI app solves both problems:

#### Core Value: Portfolio Editing UI

The pipeline currently does:
```
Pipeline runs → writes proposal CSV → prints "remove unwanted stocks"
→ user opens CSV in TextEdit/Excel → deletes rows → saves
→ user presses Enter in terminal → pipeline resumes
```

This is fragile: wrong column deleted, accidental formatting, no context while editing. A native editing view replaces this with:

- Table view with ticker, weight, sector, score, momentum — full context visible
- Checkbox/swipe to exclude stocks (not raw row deletion)
- Color-coded flags: "dropped from index", "failed hard filter", "new addition"
- Inline sparklines for price history
- Confirm button that writes back to the golden copy (or signals the pipeline via API callback)
- Validation: can't accidentally remove all stocks, can't break CSV structure

**This eliminates the golden copy's "must be a file" constraint** — once a proper editing UI exists, the golden copy can move to DuckDB too. The pipeline "pause" becomes: write proposal to DB → push SSE event → SwiftUI shows approval UI → user confirms → pipeline resumes via HTTP callback.

#### Additional Capabilities

- Menu bar presence showing portfolio value / drift status at a glance
- Native notifications for autopilot proposals with confirm/dismiss action buttons
- No server process needed — read DuckDB directly from Swift
- Single `.app` bundle distribution (drag to Applications)
- Swift Charts for equity curves, sector donut, weight comparison
- Shortcuts / Siri integration for hands-free status checks
- Rebalance proposals with approve/reject per trade
- Push notifications for drift alerts (daemon already dispatches them)
- Touch Bar widget for quick portfolio health

#### Architecture

```
┌─────────────┐         ┌──────────────┐         ┌─────────────┐
│  SwiftUI    │◄──JSON──►│  pkg/server  │◄────────►│  DuckDB     │
│  (macOS)    │   HTTP    │  (existing)  │          │  + golden   │
└─────────────┘         └──────────────┘         └─────────────┘
                              │
                         SSE quotes
```

- The Go server already serves JSON APIs for holdings, weights, drift, orders, monitor
- SwiftUI app is a native client to the **same API** the web dashboard uses — no new server work
- Pipeline "pause for human edit" becomes an API-driven approval flow
- Swift reads DuckDB directly for read-heavy display (portfolio value, historical charts)
- Shells out to `mycase` CLI for all mutations (pick, optimize, basket)
- Go binary remains the source of truth for logic; Swift is pure presentation + OS integration
- The web dashboard stays for headless/remote scenarios

#### Phased Build Plan

| Step | What | Effort |
|------|------|--------|
| 1. Proposal approval view | The one screen that replaces CSV editing. `URLSession` + `Codable` against existing API | 1 week |
| 2. Holdings dashboard | Table + donut chart. Read-only. Same data as web dashboard | 1 week |
| 3. Menu bar widget | Portfolio value + drift %. Background polling or SSE | 3 days |
| 4. Native notifications | Wire to daemon alerts. macOS `UserNotifications` framework | 2 days |
| 5. Performance charts | Swift Charts equity curve overlaid with SPY | 1 week |
| 6. Full migration | Replace web dashboard as primary UI. Server stays for API | 2 weeks |

#### Why Not Now

Data-source resilience (Phase 10) is higher priority — it improves the correctness of inputs the strategy depends on; the UI doesn't. The CSV workflow is ugly but works for quarterly rebalance (4×/year). Defer until:
- The system is stable enough that UX is the bottleneck, not the strategy
- The golden copy can move to DuckDB (the pipeline migration is done — see `docs/duckdb-migration.md`)
- Swift Charts and DuckDB Swift bindings are mature enough for production use
