# Mycase — Architecture Principles

**Status**: COMPLETE — durable principles fleshed out with concrete file references, §9 scored against evidence (branch `integration/main-ebm`, build + check-deps + test green as of this writing).
**Purpose**: Codify the durable architectural principles the system is built on, so future changes (and merges from divergent branches, like the EBM integration) can be evaluated against a stable rubric rather than ad-hoc judgment.
**Related**: `docs/architecture.md` (current design + D-decisions), `.kiro/steering/architecture.md` (enforced layering), `docs/datasources.md` (data-source design), `docs/refactor.md` (refactor history).

---

## 0. Why the Architecture Looks Like This (Domain Grounding)

The engineering principles below are not neutral taste — they fall out of what this system *is*: an automated engine that puts **real money** into US equities on a **quarterly rebalance** using a **quality + momentum factor tilt**, with a human investor in the loop. Four domain facts drive nearly every technical choice:

- **Real capital, low frequency, high consequence.** A rebalance runs a few times a year and moves the whole portfolio. There is no "just re-run it" — a bad basket sits for a quarter. So the system *proposes* and the investor *confirms* before orders fire (§6 investor-in-the-loop), state is atomic and run-tracked so a partial failure is recoverable (§6), and every checkpoint stays green and reversible (§7, §8). Latency is irrelevant; correctness and auditability dominate.
- **A strategy must be explainable, not a black box.** A discretionary investor has to trust *why* a stock was picked and why the roster changed. Hence every score, filter, and weight is reconstructable from output (§5 transparency), decisions are captured as point-in-time snapshots for run-to-run diffing (§6, §9), and the structured driver metrics are source-of-truth with the human-readable string derived from them (§5). The `rsi`/`momentum_1y`-persist-zero bug matters *because* it silently breaks explainability, not because a number is wrong.
- **Numbers must be sourced from an authority and be auditable.** Factor scores are only as trustworthy as the fundamentals and prices behind them. So each data type is sourced from the most authoritative provider (broker/exchange for prices, SEC for fundamentals), aggregators are fallback-not-truth, fallbacks are explicit and logged, and provenance should be recorded so one bad source can be invalidated selectively (§4). This is why the missing provenance column and the router-bypass paths are ranked the #1 gap (§10) — they are a *data-trust* problem, not just a plumbing one.
- **Two markets, one engine, over time.** The system carries a live US factor strategy and legacy India strategies, and it must keep loading old config and old runs as it evolves. Hence market-agnostic DTOs with prefix-based routing (§1, §4), strategies pluggable at a single dispatch point without touching IO/data (§5), additive zero-value-safe config (§3), and "keep legacy, mark it, don't delete" (§5, §8).

Read the sections below as the technical expression of these four domain pressures.

---

## 1. Layering & Dependencies

- Imports go strictly downward; a package imports only lower layers, never same/higher.
- Shared DTOs live in zero-import leaf packages (`marketdata`, `broker/types`) — define OR consume, not both low in the stack.
- Interfaces are defined by their consumer; the low-level implementer satisfies structurally, never imports the consumer.
- Domains own their persistence (take a `*sql.DB` via `cache.Conn()`, define their own tables); `cache` never imports a domain.
- New package → must be placed in `devtools/internal/layers` layer map deliberately; guard fails on unlisted packages.
- Designated leaves never acquire an internal import (`marketdata`, `broker/types`, `cache`, `config`, `costs`, `render`, `market`, `logging`, `alert`).

### When to introduce a new package vs. extend an existing one

The deciding question is **"does this define a widely-shared type, or does it wire behavior?"** — the R16 cycle-magnets all failed by doing both low in the stack. Rules encoded in the layer map (`devtools/internal/layers/layers.go`, `var Layers`):

- If the artifact is only a **shared DTO** used across package boundaries, it gets (or joins) a zero-import leaf — `marketdata` (price/fundamental DTOs), `broker/types` (Holding/Order/MarketConfig). Consumers then import the leaf directly; `broker` and `yfinance` additionally re-export those types via aliases so legacy call sites are unchanged.
- If the artifact **wires behavior or config**, it becomes a heavier package placed at the layer matching its dependencies (e.g. `datafetcher` at L3 because it composes `broker/schwab` + `yfinance`).
- Every `pkg/` package MUST appear in the `layers` map or `make check-deps` fails with *"package … is not listed … add it at the correct layer"*. The reverse also fails (*"…is in the layer map but was not found — remove it"*), keeping the map honest. This forces placement to be a deliberate decision, never a default.

### How to break a would-be cycle

Two concrete techniques, both now in the tree:

- **Consumer-defined interface, structural satisfaction (the datafetcher→stockpicker case).** `stockpicker` (L3) defines `DataFetcher` (`pkg/stockpicker/types.go`); the production implementer `*datafetcher.Router` (also L3) satisfies it *structurally*. Crucially there is **no** compile-time `var _ stockpicker.DataFetcher = (*Router)(nil)` assert in `datafetcher` — that would create a sideways L3→L3 import and thus a cycle. The satisfaction is instead compile-checked at the assignment site (`stockpicker.Options.DataFetcher`), reached from `autopilot`/`cmd`, which legitimately import both. See the explanatory comment in `pkg/datafetcher/router.go` (R16 "problem P2").
- **Domain owns its own persistence (the pithistory case).** `pkg/pithistory` (L4) needs storage but does *not* push tables down into `cache`. It opens its own `data/pit_history.db`, defines its own schema (`pit_runs`, `pit_candidate_scores`) and its own `SaveRunSnapshot`/`UpdateForwardReturns` methods (`pkg/pithistory/db.go`). `pkg/cache` exposes `cache.Conn()` for domains that share the main DB (`attribution.Store`, `tax.Store`) and never imports a domain (R16 "problem P4"). `cmd/pit.go` is only the thin CLI wrapper — persistence lives in the domain package, not at the composition root.

## 2. IO & Side Effects

- Two output channels, never conflated: user output (stdout via `pkg/render`/`fmt`) vs. logs (slog → stderr + JSON file).
- Command *results* are the product → stdout; everything diagnostic → slog.
- No side effects hidden in low-level packages; IO/orchestration lives at L4+ and the composition root (`cmd/`, `main.go`).
- No hardcoded machine/user-specific paths in committed code (cf. rejected `SyncAccessTokenToAllConfigs`).
- Golden-copy CSVs never overwritten programmatically except via explicit merge command.
- Secrets referenced by key name, never logged by value; credential files gitignored.

### The two channels, concretely

- **User output → stdout via `pkg/render`.** `render.New(os.Stdout)` returns a renderer whose `Table`/`Section`/`Writer` all write to the injected `io.Writer`; color auto-disables unless stdout is a TTY (`pkg/render/color.go` stats `os.Stdout`). A user piping `mycase holdings` into a script gets clean tabular stdout.
- **Logs → stderr + JSON file via `pkg/logging` (slog).** `main.go`'s `Before` hook calls `setupLogging`, `slog.SetDefault`, generates a `req_id` (`logging.GenerateReqID`/`WithReqID`), and re-binds the default logger `With("req_id", …)` so every context-aware call is traced end-to-end; the `After` hook closes the file. See `.kiro/steering/logging.md` for the enforced conventions.

### Filesystem layout conventions

- `config/` — human-authored, **read-only at runtime**; the system never writes back to its own config as a side effect.
- `data/` — machine-generated, mutable state: DuckDB databases (`data/cache.db`, `data/pit_history.db`), CSV proposals/backups, daemon state (`data/daemon_state.json`, `data/daemon.pid`), autopilot proposal JSON (`data/autopilot/pending_proposal.json`), and debug dumps (`data/debug/`). `pkg/pithistory` only ever `os.MkdirAll`s the `data/` dir for its DB.
- No hardcoded machine/user-specific absolute paths in committed code. The lone reference to `SyncAccessTokenToAllConfigs` lives in this doc as a **rejected** pattern — token material is referenced by key name and never synced across configs by side effect.

### Network IO: rate limits as a budget, cache-first, save-raw-for-debug

- **Rate limit is a ceiling, not a target.** `pkg/broker/schwab/client.go` enforces a client-side sliding window (`rateLimitPerMinute = 120`): `doRequest` calls `waitForRateLimit(ctx)`, which prunes entries older than a minute, proceeds if `< 120`, else sleeps until the oldest expires (respecting `ctx.Done()`); `recordRequest` timestamps each send. 401s trigger a single token refresh + retry.
- **Cache-first.** `pkg/cache/prices.go` `GetPrices` returns a miss unless `isFreshToday` (same IST calendar day); `GetPricesByDateRange` treats historical ranges (ending before today) as never-expiring. Fundamentals use a rolling 24h TTL (`isFreshFundamentals`, `pkg/cache/fundamentals.go`). Always check the cache before hitting the network.
- **Save raw for debugging.** During development, save the full JSON response to `data/debug/schwab_response_YYYYMMDD.json` and analyze offline rather than re-hitting the endpoint from different angles. This is a documented discipline (`.kiro/steering/api-rules.md`), not a code path.

### Golden-copy protection

Golden-copy CSVs are mutated only through the explicit `mycase merge golden` command (`cmd/merge.go` → `csvloader.MergeGoldenCopy(src, dst)`, which preserves exited tickers at `0.0000` weight). No implicit overwrite path exists; the golden copy deliberately stays a file (Tier 2 in `docs/duckdb-migration.md`), so the merge is file→file.

## 3. Configuration Management

- Precedence is explicit and one-directional: flag > env > config file > built-in default (cf. logging config wiring in `main.go` `setupLogging`).
- User preferences (`config/defaults.json`) provide convenience defaults; explicit flags always override — config never forces behavior.
- Config files are read-only at runtime; the system never writes back to its own config as a side effect.
- Config structs are additive/backward-compatible: new fields default to zero-value and don't break existing files (cf. EBM `HardFilters` fields, `AllowCashOnSectorCapExhaustion`; `LoggingConfig.File` is a `*bool` so absence ≠ false).
- Strategy/behavior config (`mfs.json`, `pipeline.yaml`) is version-controlled; secrets/tokens are separate, gitignored files referenced by key name.
- Method/name aliasing handled at the config-load boundary, not scattered through call sites (`earlymb`→`early_multibagger` normalized in `LoadHardFilters`, `pkg/config/config.go`).
- Config loading is a leaf concern (`pkg/config`, zero internal imports); it parses, it doesn't orchestrate.
- Absent/malformed optional config degrades to zero-value defaults, never panics (`LoadUserDefaults` returns `defaults` on any open/decode error).

### Config schema documentation & validation — current state

There is **no dedicated validation layer**. Loaders (`LoadUserDefaults`, `LoadHardFilters`, `LoadGovernance`, `LoadConfig` in `pkg/config/config.go`) parse JSON and either return zero-value defaults or a plain decode error; ranges and required fields are not checked. This is an accepted gap for now — the design leans on additive/zero-value-safe structs so a missing or partial file degrades gracefully rather than being rejected. If validation is added later it belongs at the load boundary in `pkg/config` (staying a leaf), returning descriptive errors, and must preserve the degrade-to-default behavior for genuinely optional blocks. Tracked as an improvement in §10.

### Per-market vs. per-strategy vs. per-user config boundaries — which file owns what

| File | Owner (loader/struct) | Scope | Content |
|------|-----------------------|-------|---------|
| `config/defaults.json` | `config.UserDefaults` / `LoggingConfig` | per-user | broker/market/index/method/topN/range convenience defaults + logging block |
| `config/mfs.json` | `LoadHardFilters` → `MFSStrategies` | per-strategy | `filters.<method>` hard-filter thresholds + factor `score_weight_*`; `strategies.<name>` MFS optimizer weights |
| `config/governance.json` | `LoadGovernance` | per-strategy input | promoter-pledge percentages |
| `config/pipeline.yaml`, `config/pipeline_us.yaml` | `UserDefaults.PipelineConfig` | per-market pipeline | pipeline stage config |
| `config/themes.json`, `csvlinks.json`, `sector_tam.json`, `customer_concentration.json`, `management_alerts.json` | strategy inputs | per-strategy | domain reference data |
| `config/schwab.json` (+ `.example`), Zerodha creds | `config.Config` / `LoadConfig` | per-user secret | credentials; **gitignored**, referenced by key name, never logged by value |

Rule of thumb: **authored/behavioral inputs and credentials → config files; derived/fetched/run-tracked state → DuckDB.**

### Config files vs. DuckDB-stored state

Config files are version-controlled, human-authored, and read-only at runtime (strategy behavior + credentials). DuckDB holds machine-generated, mutable state: cached prices/fundamentals with freshness metadata (`cache_meta`), pipeline runs (`pipeline_runs`), per-index candidate scores (`index_picks`), staged baskets (`proposals`), the finalized portfolio audit trail (`selections`), and — in the separate `data/pit_history.db` — point-in-time research snapshots. Nothing crosses the boundary: config never records run state, DuckDB never records authored preferences.

### Config schema migration/versioning over time

The current story is *additive-only*: new fields must be zero-value-safe so old files keep loading (`*bool` for tri-state, zero-value sentinels for "unset"). There is no explicit schema-version field or migration runner. If a breaking change becomes necessary, add a `schema_version` field read at the load boundary and migrate forward in `pkg/config`, keeping the zero-value-degrade guarantee for optional blocks. Tracked in §10.

## 4. Data Sources & Integration

- Source each data type from the most authoritative provider that can supply it (prices→broker/exchange, fundamentals→SEC EDGAR, sector→classification standard).
- Aggregators (Yahoo) are fallback, not source-of-record; demote, don't eliminate.
- Routing selects provider; fundamentals may be *composed/merged* across sources rather than single-sourced.
- Fallback chains are explicit and logged — a degraded run (Schwab→Yahoo) must be observable, never silent.
- Record provenance (which source produced a value) so numbers are auditable and one source can be invalidated selectively.
- Ticker-prefix convention (`US:`/`NSE:`/`BSE:`) drives routing; DTOs are market-agnostic.
- New source = new package satisfying a consumer-defined source interface.

### How routing works today, and where it is bypassed

Routing is centralized in `pkg/datafetcher/router.go`. `Router` holds one field, `schwabClient *schwab.Client`; every method routes on `schwab.IsUSTicker(ticker) && r.schwabClient != nil` → Schwab, else Yahoo. `IsUSTicker`/`StripUSPrefix` (`pkg/broker/schwab/market.go`) recognize `US:`/`NASDAQ:`/`NYSE:` prefixes; `NSE:`/`BSE:` (and `.NS`/`.BO`) are not special-cased in the router and fall through to Yahoo, with prefix stripping done inside `pkg/yfinance`. `FetchQuotes`/`FetchFundamentals` split the ticker list, send US to Schwab with a **Yahoo fallback on error**, and use Yahoo directly when `schwabClient == nil`.

**The interface is a single consumer-defined `stockpicker.DataFetcher`** (`pkg/stockpicker/types.go`: `FetchFundamentals`, `FetchHistoricalDataWithTimestamps`, `FetchHistoricalPrices`) — *not* the `PriceSource`/`FundamentalsSource`/`SectorSource` trio the earlier draft imagined. Sector data rides on `yfinance.Fundamentals.Sector`. This is the honest current state; the trio is an aspiration, not a fact.

**Router-bypass paths (the "~7 bypasses" — real and enumerated).** Several call sites hit `yfinance.*` directly instead of going through the `Router`, so Yahoo remains primary for many US paths:

1. `pkg/server/handlers.go` — benchmark + per-holding historical + fundamentals for the dashboard.
2. `pkg/stockpicker/scoring.go` — the **benchmark** leg (`FetchHistoricalPrices`/`FetchHistoricalDataWithTimestamps`) is fetched directly even when a `DataFetcher` is injected for the per-ticker legs.
3. `cmd/calibrate.go` — per-ticker + benchmark history + fundamentals.
4. `cmd/monitor.go` — history + fundamentals.
5. `cmd/report.go` — history + fundamentals.
6. `cmd/backtest.go` — per-holding + benchmark date-range history.
7. `pkg/stockpicker/run.go` fallbacks (`fetchFundamentalsVia`/`fetchHistoricalPricesVia`/`getBenchmarkAndSlicedPricesVia`) drop to direct `yfinance.*` when `opts.DataFetcher == nil`; EBM added more in `pkg/stockpicker/io.go` (`FetchNselibDeliveryDataDetails`, `FetchQualitativeNSEData`, `FetchCustomerConcentrationData`) and `pkg/datafetcher/datafetcher.go` `FetchMarketData` calls `yfinance.FetchQuotes` directly (India-legacy interactive path).

**Provenance: not implemented.** No cache table (`prices`, `fundamentals`, `cache_meta`, `selections`, `proposals`, `index_picks`) has a `source`/`provenance` column. A value's origin is not auditable today, and one source cannot be selectively invalidated. This is the single biggest gap versus the stated principle (§10).

### How to add a provider without touching consumers

Because the consumer (`stockpicker`) owns the `DataFetcher` interface, a new provider is a new L2 package that the `Router` composes — consumers keep depending only on the interface. The clean path is: implement the provider, teach `datafetcher.Router` to route to it by ticker prefix (with an explicit, logged fallback), and leave `stockpicker`/`cmd` untouched. The bypass paths above are the debt that makes this less clean than it should be; consolidating them behind the Router is prerequisite to honoring "add a provider without touching consumers."

### Cache freshness policy per data type

- **Prices** — same-IST-day freshness (`isFreshToday`); today-ending ranges expire at the IST day boundary, historical ranges never expire.
- **Fundamentals** — rolling 24-hour TTL (`isFreshFundamentals`).

This matches the cadence of the data: prices move intraday, filings update quarterly, so fundamentals tolerate a coarser TTL.

## 5. Algorithms & Strategies

- Strategy = scoring + hard filters + selection; each pluggable by `--method` dispatch in one place (`stockpicker.RunWithResult`, `pkg/stockpicker/run.go`).
- Hard filters exclude entirely (not low-score); scoring normalizes within the candidate set.
- Algorithms are pure/deterministic over their inputs; data-fetching is injected (`DataFetcher`), not called directly.
- Transparency / no black boxes: every score, filter, weight explainable from output (design constraint from vision).
- New strategy = new Score/SelectTopN/Normalize trio + a dispatch branch + config block; no changes to IO or data layers.
- Legacy strategies (India multibagger/value) coexist with active (US quality-momentum, EBM) — kept, not deleted, but clearly marked.

### The `--method` dispatch

`stockpicker.RunWithResult(ctx, *Options)` is the single dispatch point (an if/else ladder on `opts.Method`):

- `value` → `ScoreValue`/`SelectTopNValue`/`NormalizeValueWeights`
- `multibagger` → `ScoreMultibagger`/`SelectTopNMultibagger`/`NormalizeMultibaggerWeights`
- `earlymb`|`early_multibagger` → `ScoreEarlyMultibagger`/`SelectTopNEarlyMultibagger`/`NormalizeEarlyMultibaggerWeights`
- `us_quality_momentum` → `ScoreUSQualityMomentum`/`SelectTopNUSQM`/`NormalizeUSQMWeights`
- default (EBM/standard) → `SelectTopNStandard`/`NormalizeStandardWeights` using `cfg.Weights` (MFS) + benchmark prices.

Each `SelectTopN*` records raw ranks, applies per-sector caps (`maxPerSector`, `RecordSectorCapDrop`), then a hysteresis buffer (`ApplyHysteresisSelection`) so existing holdings inside the buffer are retained across rebalances. Hard filters are applied *before* scoring: `ApplyUSHardFilters` for US quality-momentum (market-cap / ADV / positive-FCF), `ApplySafetyFilters` otherwise, with elimination counts tracked in `FilterStats`.

### Factor-weight config conventions

Weights live in `config/mfs.json`, loaded by `LoadStrategyConfig(opts.Method)`, in two blocks: `filters.<method>` carries hard-filter thresholds *and* `score_weight_*` factor weights (e.g. US quality-momentum: ROIC 20, FCF-yield 20, 12m-momentum 15, earnings-quality 15, shareholder-yield 15, low-vol 15); `strategies.<name>` carries the MFS optimizer weights consumed as `cfg.Weights` by the standard/EBM path. A new strategy adds its own block here and its own Score/Select/Normalize trio — no IO or data-layer change.

### Where derived metrics live — and the RSI/momentum-persists-zero bug

Two parallel representations coexist by design: **structured** `selectiontracker.DriverMetrics` (numeric, authoritative, via `RecordDriverMetrics` → `Tracker.DriverValues`) and a **derived** human-readable string (`RecordAdditionDriver`, reconstructed from the numeric fields by `formatDriverStringFromMetrics` in `run.go` — Phase 8 replaced re-parsing the old text report). The structured path is source-of-truth; the string is derived from it.

**Known bug (Phase 8 follow-up): `rsi` and `momentum_1y` persist as zero.** The `selections` table has `rsi`/`momentum_1y` columns and they are wired end-to-end (`cache.Selection` → `InsertSelections` writes them positionally; `pickResultToSelections` in `autopilot.go` copies them). But none of the three `RecordDriverMetrics` call sites assign them:

- `scoring.go` (~L519, multibagger): sets TTMGrowth, RevenueCAGR, DSODelta, FCFYield, ROIC — no RSI/Momentum1Y.
- `scoring.go` (~L713, value): sets FCFYield, DSODelta, ROIC — no RSI/Momentum1Y.
- `scoring_us.go` (~L221, us_quality_momentum): sets FCFYield, ROIC — no RSI/Momentum1Y.

So both values stay at Go's zero value all the way to DuckDB, and `cmd/pipeline_show.go` (which only prints them when `!= 0`) silently hides them. This partially violates "every metric explainable from output." Fix point = the three `RecordDriverMetrics(...)` literals. Tracked in §10.

## 6. Pipelines & Orchestration

- Pipeline stages share one process (one DB conn, one market-data session, one broker client); no subprocess chaining.
- Stages communicate via DuckDB tables + structured results (`PickResult`), not by re-parsing text output.
- Intermediate state is atomic, queryable, run-tracked (`pipeline_runs`; `proposals` stages: draft/optimized/final).
- Investor-in-the-loop for execution: system proposes, investor confirms, orders fire; never auto-execute without opt-in.
- Non-interactive path (autopilot) shares inner functions with interactive path; no duplicated logic.
- Point-in-time snapshots capture *why* a decision was made, for run-to-run diffing and audit.

### One process, one connection

`autopilot.Run` (`pkg/autopilot/autopilot.go`) runs the whole quarterly rebalance in-process: one `*datafetcher.Router` (`newDataRouter`), one `broker.Broker`, one process-wide DuckDB handle (`cache.GetDB()`/`SetGlobal`, pinned to `SetMaxOpenConns(1)` because DuckDB is single-writer). Stages hand each other structured values (`stockpicker.PickResult`, `SelectedKeys`) and read DuckDB tables (`GetAllIndexPicks`) rather than re-parsing text output. Run lifecycle is tracked in `pipeline_runs` (`InsertRun`→`CompleteRun`/`FailRun`, with a deferred `FailRun` nilled on success), and `config_json` snapshots the resolved config for reproducibility.

### Proposal lifecycle (proposed → executed reconcile)

The file-based proposal (`pkg/autopilot/proposal.go`, `data/autopilot/pending_proposal.json`) moves `pending → confirmed/dismissed/expired` (TTL auto-expiry in `LoadProposal`). Two reconciles write the `final` `proposals` stage:

- **Submitted-intent** — `FinalStageProposals(p)` turns a confirmed proposal's successfully-placed BUY orders into `final` rows weighted by limit-price × qty (order placement returns only an id, so these are intent weights).
- **Realized-fill** — `RealizedStageProposals(txns, from, to)` aggregates actual broker fills (Σ qty×price, fees excluded). Wired in `cmd/pipeline_reconcile.go`: fetch Schwab transactions (chunked ≤1yr) → normalize → `DeleteProposalsStage(runID,"final")` **then** `InsertProposals(...,"final",...)`. The delete is required because UPSERT only touches supplied tickers, so submitted-but-unfilled names would otherwise linger with stale intent weights.

The interactive (`cmd/pipeline.go`) and non-interactive (`autopilot.Run`, `pkg/server` confirm handler) paths call the same inner engine (`stockpicker.RunWithResult`, `csvloader.MergeGoldenCopy`, `computeOrders`) — no duplicated logic.

### PIT snapshot vs. `selections` — complementary, not duplicate

This was an open question in the prior draft; the code answers it clearly. They are two distinct audit trails in two databases:

- **`selections`** (`data/cache.db`) — the *finalized held portfolio* for one pipeline run: the ~20 selected names, weights, driver metrics, and cross-run deltas (`action`, `prev_rank`, `prev_weight`) vs. `GetPreviousSelections`. Keyed `run_id,ticker`, **selected names only**. Consumer: investor-facing "why is this held / how did the roster change."
- **`pit_candidate_scores`** (`data/pit_history.db`, `pkg/pithistory`) — a *point-in-time research snapshot of the whole candidate universe* on an as-of date: **every** constituent including rejected ones (`rejection_reason`), raw vs. effective scores, regime multiplier, `delivery_delta`, and a `forward_return_21d` backfilled later (`UpdateForwardReturns`) for calibration. Keyed `as_of_date,index_name,method,ticker`.

Different grain (selected-only vs. all-candidates), different keys, different DBs, different consumers. **Verdict: keep both — they are complementary, not redundant.** (Resolves the §10 open item.)

### Scheduling boundary — OS owns long intervals

Two models coexist deliberately:

- **Long-interval (quarterly/monthly) → launchd.** `pkg/autopilot/schedule.go` emits macOS `launchd` `StartCalendarInterval` plists (`LaunchdQuarterlyIntervals`, `LaunchdMonthlyInterval`) plus date math (`NextRunDate`, `NextQuarterDates`). The OS owns the lifecycle; there is no in-process multi-month sleep loop.
- **Short-interval (daily drift) → in-process loop.** `daemon.RunLoop` (`pkg/daemon/daemon.go`) blocks on `time.After(nextMarketClose)` and runs `RunCheck` each close, persisting `data/daemon_state.json` + `data/daemon.pid`.

## 7. Testing & Verification

- Build + layering guard + tests must stay green on every change (`make build`, `make check-deps`, `make test`).
- Table-driven, stdlib `testing`, hand-written mocks; interfaces are the seams. No mocking framework.
- Pure-logic packages: high unit coverage; IO glue: covered by E2E, not chased for %.
- Integration tests (network/creds/external scripts) skip gracefully when the environment is absent — never hard-fail the suite.
- Verify against the actual success criteria, not just "exited 0".

### The green bar

- `make build` — `go build` with LDFLAGS injecting version/commit/date → `dist/mycase`.
- `make check-deps` — `go run ./devtools/checkdeps`, the R16 layering guard (part of `make cleanup`).
- `make deps-graph` — `go run ./devtools/depsgraph`, the visual companion to `check-deps`: emits a layer-colored Graphviz dependency graph of `pkg/` to `dist/deps.dot`, rendering `dist/deps.svg` if Graphviz (`dot`) is installed. Not part of `make cleanup` — run on demand to eyeball the graph during refactors. Shares its layer map with `check-deps` via `devtools/internal/layers`.
- `make test` — `go test -timeout 30s ./...`; `test-race` (`-race -timeout 60s`), `test-integration` (`-tags=integration -timeout 120s`), `test-coverage` (→ `coverage.html`).

All three are green on `integration/main-ebm` as of this writing.

### Graceful skip

The primary pattern is runtime `t.Skipf` when the environment is absent (e.g. `pkg/yfinance/screener_test.go` skips when nselib delivery data is unavailable), alongside the `-tags=integration` build-tag target. Network/creds/external-script tests never hard-fail the default suite.

### Test patterns & coverage posture

Table-driven, stdlib `testing`, interface-based hand-written mocks (`broker.MockBroker` with an `IsMock()` seam), no framework. Examples: `pkg/autopilot/proposal_test.go` (both reconciles), `pkg/cache/pipeline_test.go` (`openTestCache` helper), `pkg/pithistory/pithistory_test.go`. **Coverage targets by layer:** pure-logic packages (scoring, optimizer, tax, costs, cache ops) are chased for high unit coverage; IO glue (server, executor, daemon, cmd) is covered by E2E and not chased for a percentage. The historically-low `stockpicker` coverage was materially improved by the EBM merge (`stockpicker_test.go` +399 lines, plus `bounds_test.go` and yfinance metric tests). A formal per-package percentage target is not set; the policy is directional, not numeric.

## 8. Change & Merge Discipline

- Distinguish algorithmic changes (bring) from architectural changes (evaluate against principles; our architecture wins on conflict).
- Union where both sides added disjoint things; take-theirs where we never touched a file; hand-merge only true conflicts.
- Relocate incoming changes to the architecturally-correct home (e.g. logic our refactor moved out of `cmd/` into `pkg/`).
- Keep unused-but-wanted code rather than delete, when explicitly requested.
- Commit in safe checkpoints; keep a pushed restore point before risky integration.

### cherry-pick vs. merge vs. rebase given branch divergence

Guidance derived from the EBM integration (commits `8d4d43e` algorithmic import, `460f01c` command wiring):

- **Cherry-pick** a specific algorithmic improvement when the branches have diverged architecturally and you want *only* the algorithm, relocated to its correct home (this is effectively what the EBM integration did file-by-file — take the scoring/strategy code, drop it into the refactored `pkg/` layout).
- **Merge** when both sides share the same architecture and you want the union of history.
- **Rebase** only a short-lived local branch onto an updated base before it is shared; never rebase a branch others may have pulled.
- The invariant across all three: after integrating, the code must land in the *architecturally-correct home* (our layering wins), and `make build && make check-deps && make test` must be green before the checkpoint commit.

---

## 9. Self-Evaluation: Current Solution vs. Principles

Scored after the EBM integration (`integration/main-ebm`), grounded in the file evidence above. Build + check-deps + test verified green.

| Area | Status | Evidence |
|------|--------|----------|
| Layering | 🟢 Strong | `devtools/checkdeps` enforces the full layer map with three failure modes (unlisted / leaf-violation / downward-only); `check-deps` green. datafetcher→stockpicker cycle avoided via consumer-defined interface + structural satisfaction (`router.go`); pithistory owns its own DB (`pithistory/db.go`) instead of pushing into `cache`. |
| IO / side effects | 🟢 Good | render(stdout)/slog(stderr+file) split with `req_id` tracing (`main.go` Before hook); `config/` read-only; golden-copy mutated only via `cmd/merge.go`. Residual gap: several diagnostic `fmt` sites not yet migrated to slog (audit pending). |
| Config management | 🟢 Good | flag>env>file>default proven in `setupLogging`; additive zero-value-safe structs; `LoadUserDefaults` degrades on malformed input; aliasing at load boundary. Gap: no schema/range validation, no schema-version/migration story (§10). |
| Data sources | 🟡 Partial | Router prefix-routing with logged Schwab→Yahoo fallback exists (`datafetcher/router.go`), but **~7 bypass paths** hit yfinance directly (server, benchmark leg, calibrate/monitor/report/backtest, run.go fallbacks + EBM io.go), and **no provenance column** exists in any cache table. Only one `DataFetcher` interface, not the aspirational Price/Fundamentals/Sector trio. Biggest gap vs. principles. |
| Algorithms | 🟢 Good | Single `--method` dispatch in `RunWithResult`; injected `DataFetcher`; hard-filters-then-score-then-select with sector caps + hysteresis; EBM slotted in without touching IO/data layers. Gap: `rsi`/`momentum_1y` persist zero (three `RecordDriverMetrics` sites omit them) — partial "explainable from output" violation. |
| Pipelines | 🟢 Good | One process / one DB conn / one broker (`autopilot.Run`); run-tracked (`pipeline_runs`); proposal lifecycle closed with both submitted-intent and realized-fill reconciles (`proposal.go`, `cmd/pipeline_reconcile.go`); launchd owns long intervals, in-process loop only for daily drift. PIT vs selections confirmed complementary. |
| Testing | 🟢 Good | build + check-deps + test all green; graceful `t.Skipf` for env-absent integration tests; table-driven + hand-written mocks; EBM added significant stockpicker coverage. Gap: no numeric per-layer coverage target (directional policy only). |
| Merge discipline | 🟢 Applied | EBM integration executed the algorithm-in / architecture-preserved rule cleanly across `8d4d43e`/`460f01c`; incoming code relocated to refactored `pkg/` homes; green checkpoint maintained. |

## 10. Improvements Identified (this branch)

Ordered by severity against the principles.

1. **Data-source resilience (§4) — highest priority.** Consolidate the ~7 router-bypass paths (`server/handlers.go`, the benchmark leg in `scoring.go`, `cmd/{calibrate,monitor,report,backtest}.go`, `run.go` fallbacks + EBM `io.go`) behind `datafetcher.Router`, and add a **provenance/source column** to the cache tables so values are auditable and one source can be invalidated selectively. This is the largest gap vs. the stated principles and the prerequisite for "add a provider without touching consumers." → Roadmap Phase 10.
2. **`rsi`/`momentum_1y` persist zero (§5).** Assign both in the three `RecordDriverMetrics` call sites (`scoring.go` ×2, `scoring_us.go`); the columns and end-to-end wiring already exist. Small, well-scoped fix. Restores "explainable from output."
3. **No config schema validation / migration story (§3).** Add optional range/required-field validation at the `pkg/config` load boundary (preserving degrade-to-default for optional blocks) and a `schema_version` + forward-migration path if a breaking change ever lands.
4. **Remaining diagnostic `fmt` sites (§2).** Audit for `fmt.Print*` diagnostics that should be slog; keep only genuine user-facing results / interactive UX on stdout.
5. **Zerodha/Kite residual code (§8).** The named `kiteclient` was removed (R10.1); what survives is the live-but-India-legacy Zerodha broker (`pkg/broker/zerodha`). Decide explicitly: keep as a real fallback broker or mark for removal. Currently kept.
6. **Dropped delivery column (§8).** `pkg/printer/printer.go` no longer renders main's delivery-% column (dropped during merge). The data still exists (`yfinance.FetchNselibDeliveryData*`, PIT `delivery_delta`); revisit if the EBM UI wants it back on the render layer.

**Resolved this session:** the PIT-snapshot-vs-`selections` "two audit trails" question — confirmed complementary (different grain/keys/DBs/consumers), not duplicative; keep both (§6).

---

## Promote-to-steering decision

`.kiro/steering/architecture.md` already enforces §1 (layering) and `.kiro/steering/logging.md` enforces the §2 two-channel rule; `.kiro/steering/api-rules.md` enforces the §4 network discipline. Those are the principles that are (a) mechanically checkable or (b) violated easily by rote edits, so they belong in always-included steering.

The remaining sections (§3 config, §5 algorithms, §6 pipelines, §7 testing, §8 merge) are **judgment rubrics**, not line-level rules — they are better kept in this doc and consulted deliberately during design/review than injected into every prompt. Recommendation: **do not promote §3/§5–§8 to steering**; instead cross-link this doc from `docs/architecture.md` as the design-review rubric, and keep the three existing steering files as the enforced subset. Revisit only if a specific principle starts being violated repeatedly in practice — that is the signal to codify it mechanically (as R16 did for layering).
