# Mycase Refactor Plan

**Branch**: `feature/mycase-changes`  
**Go version**: 1.26.3  
See `docs/architecture.md` for design details, CLI structure, directory layout, and decisions.

---

## Progress Summary (Completed)

| Phase | What was done | Commit(s) |
|-------|--------------|-----------|
| **R1** — CLI Unification | 9 separate `cmd/*/main.go` binaries replaced with single `mycase` binary (10 subcommands) using `urfave/cli/v3`. `pipeline.go` calls all steps as direct Go function calls — no `os/exec`. Old `cmd/*/main.go` subdirectories and `scripts/merge.go` deleted. | `aef5489`, `bd3c4c0`, `3c51409` |
| **D1** — Module rename | `module mycase` → `module github.com/raghavkgarg/mycase`. All 29 source files, Makefile, and tests updated. | `aef5489`, `782a663` |
| **R3 infra** — Go + deps | `go 1.24.4` → `go 1.26.3`; `gocsv` updated from 2018 pin to latest. | `bd3c4c0` |
| **R3 language** — Go 1.26 idioms | `math/rand/v2` replaces `math/rand` in monitor mock generator; `slices.SortFunc`/`slices.Sort` replace `sort.Interface` boilerplate (`ByPnLPct`) and `sort.Strings`; `max()` builtin replaces `math.Max` cast in printer; `go mod tidy` promotes `urfave/cli/v3` to direct dep. | `d019c9a` |
| **Tests (partial)** + **R2.4 partial** | `CapWeights` extracted from `cmd/optimize.go` → `pkg/optimizer/cap_weights.go` (exported). 28 new tests across `pkg/optimizer` (capWeights + math edge cases + quick-check property test), `pkg/monitoring` (simulator determinism, empty portfolio, insufficient history, NaN guard, boundary cases), `pkg/config` (MFSConfig, Themes, Config round-trip — all graceful fallback paths). | _current_ |
| **Makefile** | Targets: build, install, cross-compile (linux/darwin arm64/amd64), run, test, test-verbose, test-race, test-integration, test-coverage, cleanup, clean, help. LDFLAGS inject Version/GitCommit/BuildDate. | `0ad3a25`, `782a663` |

---

## Effort & Priority — Remaining

| Phase | Goal | Effort | Priority | Status |
|-------|------|--------|----------|--------|
| **Tests** | Coverage baseline before R2 changes logic | Medium (2d) | P1 | ⏳ Next |
| **R2** | Code cleanup / logic extraction | Medium (2–3d) | P1 | ⏳ |
| **R3.5** | `context.Context` in HTTP calls | Small | P1 | ⏳ |
| **R-cache** | DuckDB price/fundamentals cache | Medium (2d) | P1 | ⏳ |
| **R4** | Broker abstraction layer | Medium (2d) | P2 | ⏳ |
| **R5** | Drift monitoring daemon | Large (4–6d) | P2 | ⏳ |
| **R6** | Tax & transaction cost awareness | Medium (2–3d) | P3 | ⏳ |
| **R7** | Historical backtesting engine | XL (8–12d) | P3 | ⏳ |
| **R8** | Web dashboard | XL (10–15d) | P4 | ⏳ |

---

## Implementation Guard Rails

- All existing `pkg/` packages have tests. Run `go test ./...` before and after each phase — zero regressions.
- `mfs.json` and `pipeline.yaml` config file formats must stay backward-compatible through R2–R3.
- `data/*.csv` golden copy files are user data — never touch them programmatically except through the guarded backup → overwrite flow already in place.

---

## Phase: Tests (pre-R2 gate)

**Goal**: Establish coverage baseline before R2 changes any business logic. No network calls — all tests use `httptest` mocks or file fixtures.

**Strategy**:
1. Pure logic correctness — math functions (RSI, Sharpe, Sortino, normalizeValue, capWeights) must be exact. Table-driven unit tests with ±ε tolerances.
2. Behavioral correctness — command flag parsing, file I/O flows, error paths. Integration tests drive the real `cmd.Run(ctx, args)` without network.
3. Invariant correctness — financial constraints that must hold for any input (weights sum to 1.0, no weight exceeds cap, RSI ∈ [0,100]). Property-based and fuzz tests.

**Core rules**:
- No test may make real HTTP calls. All network-dependent tests use `//go:build integration`.
- `cmd` tests must not write to `data/`, `report/`, or `config/`. Use `t.TempDir()`.
- `go test -race ./...` must pass clean.

**Test tooling**:
- `testing` + `testing/quick` — unit and basic property tests
- `pgregory.net/rapid` — property tests with automatic shrinking (add: `go get -t pgregory.net/rapid`)
- `net/http/httptest` — mock HTTP server for yfinance tests
- `go test -fuzz` — native fuzzing

**Test dependency matrix** (current → target):

| Package | Unit | Table | Property | Fuzz | Integration | Benchmark |
|---------|------|-------|----------|------|-------------|-----------|
| `pkg/csvloader` | ✅ partial | ✅ | ❌ | ❌ | ❌ | ❌ |
| `pkg/stockpicker` | ✅ partial | ✅ | ❌ | ❌ | ❌ | ❌ |
| `pkg/optimizer` | ✅ partial | ✅ | ❌ | ❌ | ❌ | ❌ |
| `pkg/monitoring` | ✅ partial | ✅ | ❌ | ❌ | ❌ | ❌ |
| `pkg/yfinance` | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| `pkg/config` | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| `cmd` | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |

---

### pkg/csvloader — Tests to Add

Existing: `TestGetUniverseName` (9 table cases).

Add to `loader_test.go`:
```go
// TestLoadBasketCSV_Valid — header variants, NSE: prefix normalization
// TestLoadBasketCSV_MissingHeader — returns error, not panic
// TestLoadBasketCSV_EmptyFile — returns empty map, empty keys, no error
// TestLoadBasketCSV_DuplicateTicker — last row wins (or error — define behavior)
// TestLoadBasketCSV_WeightParsing — scientific notation, commas, negative weights

// TestCombineMultipleCSVs_Basic — 3 files, tickers deduplicated, weights summed/replaced
// TestCombineMultipleCSVs_SingleFile — passthrough case
// TestCombineMultipleCSVs_EmptyFile — skips silently
// TestCombineMultipleCSVs_MissingFile — returns error

// TestMergeGoldenCopy_NewTickers — tickers not in golden get added
// TestMergeGoldenCopy_ExitedTickers — tickers in golden but not in src get weight 0.0000
// TestMergeGoldenCopy_WeightUpdate — existing tickers get updated weight
// TestMergeGoldenCopy_NoChangeNeeded — idempotent merge returns no error
```

Fuzz target — `FuzzLoadBasketCSV`:
- Prerequisite: extract `loadBasketCSVFromReader(r io.Reader)` from `LoadBasketCSV(path string)` — standard io.Reader refactor for testability.
- Invariant: must not panic; error is acceptable on bad input.

Fuzz target — `FuzzGetUniverseName`:
- Invariant: `GetUniverseName` must never return empty string for any input.

---

### pkg/stockpicker — Tests to Add

Existing: `TestIsAbove200DaySMA`, `TestNormalizeValue` (7 table cases), `TestLoadLocalCSVConstituents`, `TestIsEligible`.

Add:
```go
// TestNormalizeValue_Boundary — val == minVal, val == maxVal, val outside range
// TestNormalizeValue_ZeroRange — minVal == maxVal (verify no div/0)
// TestCalculateSharpe — known returns series → expected Sharpe ≈ expected ±0.001
// TestCalculateSortino — downside-only, all-positive, mixed series
// TestCalculateBeta — perfectly correlated with benchmark → beta = 1.0
// TestCalculateAlpha — same as benchmark → alpha ≈ 0.0
// TestCalculateUlcer — constant prices → ulcer = 0.0
// TestScoreStock_Balanced — known inputs → deterministic score
// TestScoreStock_Multibagger — validates each of the 11 safety filter outcomes
// TestApplyRebalanceTolerance — within-tolerance stocks retained in same rank order
// TestApplyHysteresisBuffer — top-21 request with buffer=5 returns at most top-25 candidates
```

Property test (`rapid`) — `TestNormalizeValue_Invariants`: for any valid (val, lo, hi, max), result ∈ [0, max].

Property test (`rapid`) — `TestScoreOrdering_StableSort`: same input → same ranking every time (determinism invariant — prevents oscillating golden copies on repeated pipeline runs).

---

### pkg/optimizer — Tests to Add

Existing: `TestOptimizeFreshBuy`, `TestVolatility`, `TestOptimizeInverseVolatility`.

Gaps: `capWeights` has zero tests despite being financial-critical. `OptimizeMultiFactor` has no tests.

Add:
```go
// TestCapWeights_Basic — single stock over cap → weight clamped, sum remains 1.0
// TestCapWeights_AllUnderCap — no capping → output equals input
// TestCapWeights_CapTooTight — cap < 1/N → equal weight fallback triggered
// TestCapWeights_SingleStock — N=1, any cap → weight = 1.0
// TestCapWeights_ZeroWeights — stocks with weight 0.0 excluded from redistribution
// TestCapWeights_NegativeWeight — error or treat as 0 (define behavior)
// TestOptimizeFreshBuy_ExactBudget — budget exactly covers N shares of each
// TestOptimizeFreshBuy_InsufficientBudget — returns all zeros, no partial allocations
// TestOptimizeFreshBuy_ZeroPrice — stock with price=0.0 → skipped, no div/0 panic
// TestCalculateVolatility_ConstantReturns — all same → 0.0
// TestCalculateVolatility_Empty — empty → 0.0 (no NaN/Inf)
```

Property test (`rapid`) — `TestCapWeights_Invariants`:
- For any n ∈ [1,30] stocks and cap ∈ [0.01, 1.0]: every output weight ≤ cap+ε, and Σweights ≈ 1.0.

Benchmark — `BenchmarkOptimizeMultiFactor`: 25-ticker portfolio, 3mo price history. Target < 50ms per run.

---

### pkg/monitoring — Tests to Add

Existing: `TestGetCapStallSeverity` (4 table cases), `TestRunSimulation` (minimal).

Add:
```go
// TestRunSimulation_SingleStock — portfolio with one stock at weight=1.0
// TestRunSimulation_ZeroCapital — capital=0 → error or zero returns (define behavior)
// TestRunSimulation_FlatPrices — all prices constant → return=0, drawdown=0
// TestRunSimulation_AllDecline — all stocks fall to 0 → drawdown ≈ 100%, no panic
// TestRunSimulation_ShortHistory — fewer days than SMADays → handles gracefully
// TestRunSimulation_Determinism — same inputs, same seed → identical SimulationResult (bit-for-bit)
// TestGetCapStallSeverity_Boundary — ttmGrowth == cagr3y exactly
// TestGetCapStallSeverity_NegativeGrowth — both negative → classifies correctly
```

Mock determinism test:
```go
func TestMockDataDeterminism(t *testing.T) {
    // GenerateMockPortfolioData with fixed seed must return bit-for-bit identical results
    // Canary for rand/v2 migration: if seed behavior breaks, this catches it
}
```

---

### pkg/yfinance — Tests to Add (Zero Today)

Largest gap — every command ultimately calls yfinance.

**Prerequisite refactor**: extract `var yfinanceBaseURL = "https://query1.finance.yahoo.com"` and expose `SetBaseURLForTesting(url string)` via `export_test.go`. Functions currently hardcode the URL.

Pure math tests (no HTTP):
```go
// TestCalculateRSI_AllUp — 15 sessions all up 1% → RSI near 100
// TestCalculateRSI_AllDown — 15 sessions all down 1% → RSI near 0
// TestCalculateRSI_Alternating — up/down alternating → RSI near 50
// TestCalculateRSI_Insufficient — fewer than 14 prices → returns 50.0 (neutral)
// TestCalculateSalesGrowth_Accelerating — TTM > CAGR → passed=true
// TestCalculateSalesGrowth_Decelerating — TTM < CAGR → passed=false
// TestCalculateSalesGrowth_InsufficientHistory — < 3 annual revenue points → passed=false
// TestCalculateDSO_ZeroRevenue — division-by-zero guard
// TestCheckVolumeBreakout_Clear — strong green day on 3× average volume → true
// TestCheckVolumeBreakout_Insufficient — lookback > available data → false (no panic)
// TestMapTickerToYahoo — NSE:TCS → TCS.NS, BSE:500112 → 500112.BO, ^NSEI → ^NSEI
// TestCleanIntradayNoise — today's after-15:30 IST data is stripped
```

HTTP mock tests (use `httptest.NewServer`):
```go
// TestFetchHistoricalPrices_Success — fixture JSON → known price series
// TestFetchHistoricalPrices_RateLimit — 429 response → wrapped error, not panic
// TestFetchHistoricalPrices_MalformedJSON — garbage body → returns error
// TestFetchFundamentals_Success — fixture JSON matching Fundamentals struct
// TestFetchFundamentals_PartialData — missing fields → zero values, no panic
// TestFetchQuotes_MultiTicker — batch quote response for 5 tickers
// TestFetchQuotes_UnknownTicker — ticker not in response → value=0.0, no key error
```

Fixture JSON files in `pkg/yfinance/testdata/`:
- `historical_1y_TCS.json`, `fundamentals_TCS.json`, `quotes_batch.json`, `historical_empty.json`

Fuzz — `FuzzParseFundamentalsJSON`: invariant = no panic; zero-value Fundamentals acceptable on bad input.

Property test (`rapid`) — `TestRSI_AlwaysInBounds`: for any price series of length 5–500, RSI ∈ [0, 100].

---

### pkg/config — Tests to Add (Zero Today)

```go
// TestLoadMFSConfig_Balanced — loads config/mfs.json "balanced", spot-checks weights
// TestLoadMFSConfig_UnknownMethod — falls back to defaults, no error
// TestLoadMFSConfig_MissingFile — returns defaults, no error
// TestLoadMFSConfig_MalformedJSON — returns error
// TestLoadMFSConfig_NegativeWeights — loaded weights are non-negative
// TestPipelineConfig_UnmarshalYAML_Defaults — minimal YAML → all defaults populated
// TestPipelineConfig_UnmarshalYAML_NegativeTolerance — clamped to 0.10
// TestPipelineConfig_UnmarshalYAML_ZeroTopN — clamped to default (20)
```

Fuzz — `FuzzPipelineConfigYAML`: invariant = no panic on any YAML input.

---

### cmd/ — Integration Tests (Zero Today)

File: `cmd/cmd_integration_test.go` (package `cmd_test`).

Tier 1 — Flag parsing (no I/O, no network):
```go
// TestPickCommand_Help — exits 0, output contains "pick"
// TestPickCommand_MissingArgs — no --index, no --file → error
// TestPickCommand_UnknownFlag → error
// TestOptimizeCommand_CapBelowZero — --cap -0.1 → error or clamp to 0
// TestReportCommand_MissingFile — --file required but missing → error
// TestPerformanceCommand_InvalidDate — --date notadate → error
// TestMergeCommand_Golden_MissingArgs — no args → error
```

Tier 2 — File I/O (fixture files, `t.TempDir()`):
```go
// TestOptimizeCommand_WithFixture — --file testdata/basket.csv → output CSV written
// TestReportCommand_WithFixture — --file testdata/golden.csv → report file written
// TestMergeCommand_Combine_TwoFiles — two CSVs → output contains all tickers
// TestParsePerfDate_Formats — "2026-01-15", "20260115", "" (today), "baddate"
// TestCleanBasketArg_LeadingDashes — "--MICROSMALL" → "MICROSMALL"
```

Fixture files in `cmd/testdata/`: `basket.csv` (5 tickers with weights), `golden.csv` (10 tickers), `pipeline.yaml` (minimal valid config).

Note: Commands that open `data/` or `report/` relative to CWD need `t.Chdir()` (Go 1.24+).

---

### Fuzz Targets Summary

| Target | File | Invariant |
|--------|------|-----------|
| `FuzzLoadBasketCSV` | `pkg/csvloader/csvloader_fuzz_test.go` | No panic; error OK |
| `FuzzGetUniverseName` | `pkg/csvloader/csvloader_fuzz_test.go` | Returns non-empty string |
| `FuzzParseFundamentalsJSON` | `pkg/yfinance/yfinance_fuzz_test.go` | No panic |
| `FuzzPipelineConfigYAML` | `pkg/config/config_fuzz_test.go` | No panic |
| `FuzzParsePerfDate` | `cmd/cmd_fuzz_test.go` | No panic; valid time or error |
| `FuzzCleanBasketArg` | `cmd/cmd_fuzz_test.go` | No panic; result has no leading dashes |

### Property-Based Invariants to Verify (`rapid`)

| Invariant | Package | Test name |
|-----------|---------|-----------|
| `capWeights(w,c)[k] ≤ c+ε` and `Σw ≈ 1.0` | `optimizer` | `TestCapWeights_Invariants` |
| `OptimizeInverseVolatility`: `Σw ≈ 1.0`, all `w > 0` | `optimizer` | `TestInverseVol_SumsToOne` |
| `CalculateRSI(prices) ∈ [0, 100]` | `yfinance` | `TestRSI_AlwaysInBounds` |
| `normalizeValue(v, lo, hi, max) ∈ [0, max]` | `stockpicker` | `TestNormalize_Bounded` |
| `MergeGoldenCopy`: exited tickers have `weight == 0.0000` | `csvloader` | `TestMergeGolden_ExitedWeight` |
| `CombineMultipleCSVs`: no ticker appears twice | `csvloader` | `TestCombine_NoDuplicates` |
| `OptimizeFreshBuy`: total spend ≤ budget | `optimizer` | `TestFreshBuy_BudgetConstraint` |
| `ScoreStocks` is deterministic: same input → same order | `stockpicker` | `TestScore_Deterministic` |

### Benchmark Targets

| Benchmark | File | Target |
|-----------|------|--------|
| `BenchmarkOptimizeInverseVolatility_25` | `pkg/optimizer/` | < 100 µs |
| `BenchmarkOptimizeMultiFactor_25` | `pkg/optimizer/` | < 50ms |
| `BenchmarkCapWeights_25` | `pkg/optimizer/` | < 1 µs |
| `BenchmarkCalculateRSI` | `pkg/yfinance/` | — |
| `BenchmarkScoreStocks_25` | `pkg/stockpicker/` | < 500 µs |

### Integration Tests (Network, `//go:build integration`)

Run via `make test-integration` → `go test -tags=integration -timeout=120s ./...`.

```go
//go:build integration

// TestFetchHistoricalPrices_Live_TCS — TCS.NS 3mo data, ≥ 60 prices
// TestFetchFundamentals_Live_Reliance — RELIANCE.NS, spot-checks ForwardPE > 0
// TestPickCommand_Live_Nifty50 — pick --index nifty50 --top 5, 5 tickers returned
```

Integration tests must use `t.TempDir()` for all output and respect `MYCASE_SKIP_INTEGRATION=1`.

### Error Path Coverage Checklist

**`pick`**
- [ ] Index name not in lookup table → `"unknown index: X"`
- [ ] `--file` path does not exist → OS error with path in message
- [ ] `--file` CSV has no `ticker` column → format error
- [ ] `--range` is unsupported → `"unsupported range 'X'"`

**`optimize`**
- [ ] `--file` does not exist → error
- [ ] `--method` is unknown → warning + fallback to `volatility`
- [ ] `--cap` > 1.0 → clamp to 1.0 with warning
- [ ] All tickers removed via `--remove` → `"no active tickers remaining"`

**`report`**
- [ ] `--file` required but missing → urfave/cli required-flag error
- [ ] CSV exists but has < 2 rows → `"CSV file contains no data rows"`

**`performance`**
- [ ] `--date` in wrong format → `"invalid date format: X"`
- [ ] `--capital` = 0 → all allocations = 0, no div/0

**`pipeline`**
- [ ] `--config` file not found → error with path
- [ ] YAML config has no `indices` → `"no indices configured"`
- [ ] Individual step failure propagates with step number: `"step 2 (pick nifty50): ..."`

---

## Phase R2 — Code Cleanup & Logic Extraction

**Goal**: Move business logic out of `cmd/` mains and into `pkg/`. Reduce cmd files to thin flag-parsing + orchestration.  
**Effort**: Medium (2–3d) | **Risk**: Low (covered by tests)

**R2.1 — Extract mock data generator from `cmd/monitor.go`**  
The `generateMonitorReport` / mock-data logic (plus the synchronized `rand.New(rand.NewPCG(42, 0))`) belongs in `pkg/monitoring/mock.go`. The cmd should just call `monitoring.GenerateMockPortfolio(...)`.

**R2.2 — Extract portfolio valuation from `cmd/performance.go`**  
The date-matching, price lookup, and P&L calculation logic belongs in `pkg/performance/` (new package). The cmd becomes: parse flags → call `performance.RunBacktest(...)` → print result.

**R2.3 — Extract heuristic text from `cmd/report.go`**  
The `generateHeuristics()` / narrative text generation belongs in `pkg/report/` (new package). The cmd becomes: parse flags → call `report.Generate(...)` → write file.

**R2.4 — Complete rebalancing band diff logic move from `cmd/optimize.go`**  
The golden copy diffing and exit-weight injection logic belongs in `pkg/optimizer/rebalance.go`. Already partially there — complete the move.

**R2.5 — Consolidate `pkg/config/config.go`**  
Move `pipeline.yaml` loading (currently in `cmd/pipeline.go` as `PipelineConfig` + `rawPipelineConfig` + `resolveFirst[T any]()` generics helper) into `pkg/config/pipeline.go`. The `resolveFirst` generics hack is a workaround for YAML ambiguity — replace with a proper strict YAML struct and explicit defaults.

---

## Phase R3.5 — context.Context in HTTP calls

**Goal**: Add `ctx context.Context` as the first parameter to all `pkg/yfinance` fetch functions so callers can set deadlines.

- `pkg/yfinance/prices.go` — `FetchHistoricalDataWithTimestamps`, `FetchQuotes`
- `pkg/yfinance/metrics.go` — `FetchFundamentals`
- Use `http.NewRequestWithContext(ctx, ...)` in place of `http.NewRequest`
- Update all callers (cmd files + pipeline)

---

## Phase R-cache — Persistent Price & Fundamentals Cache

**Goal**: Add DuckDB-backed persistent cache under `pkg/cache/` so Yahoo Finance API is only called when data is genuinely stale.  
**Effort**: Medium (2d) | **Risk**: Low (additive; yfinance fetch functions remain as the cache-miss path)

**R-cache.1 — Add `github.com/duckdb/duckdb-go/v2`**
```bash
go get github.com/duckdb/duckdb-go/v2
```

**R-cache.2 — Implement `pkg/cache/`**  
Pattern mirrors sanvasify's `pkg/db/` — `database/sql` interface, `InitSchema(ctx)`, transactions for bulk inserts:
- `db.go`: `New(path string) (*Cache, error)` — `sql.Open("duckdb", path)`, `InitSchema`, `Close`
- `prices.go`: `GetPrices(ticker, rangeKey string)` / `StorePrices(...)` with `ON CONFLICT DO UPDATE`
- `fundamentals.go`: `GetFundamentals(ticker string)` / `StoreFundamentals(...)`

Schema (`data/cache.db`):
```sql
CREATE TABLE IF NOT EXISTS prices (
    ticker  VARCHAR NOT NULL,
    date    DATE    NOT NULL,
    close   DOUBLE  NOT NULL,
    open    DOUBLE,
    volume  DOUBLE,
    PRIMARY KEY (ticker, date)
);
CREATE TABLE IF NOT EXISTS fundamentals (
    ticker       VARCHAR PRIMARY KEY,
    fetched_at   TIMESTAMP NOT NULL,
    -- scalar fields: sector, market_cap, roe, forward_pe, peg_ratio, pb_ratio, ...
    -- annual time-series stored as JSON arrays
    annual_revenue JSON, annual_capex JSON, annual_net_ppe JSON,
    annual_ar JSON, annual_op_income JSON,
    raw_json JSON  -- full Fundamentals struct as escape hatch
);
CREATE TABLE IF NOT EXISTS cache_meta (
    ticker     VARCHAR NOT NULL,
    range_key  VARCHAR NOT NULL,
    fetched_at TIMESTAMP NOT NULL,
    PRIMARY KEY (ticker, range_key)
);
```

Staleness policy:
- Past trading day prices: permanent
- Current-day prices: stale before 15:30 IST
- Fundamentals: stale after 24 hours

**R-cache.3 — Wire cache into yfinance fetch functions**  
Wrap `FetchHistoricalDataWithTimestamps` and `FetchFundamentals` with cache-check-first. Caller API is unchanged.

**R-cache.4 — `mycase cache` subcommand**
```
mycase cache status          # Show row counts, last fetch timestamps per ticker
mycase cache clear --ticker  # Evict a specific ticker
mycase cache clear --all     # Wipe entire cache
```

---

## Phase R4 — Broker Abstraction Layer

**Goal**: Decouple order execution from Zerodha. Enables Phase 5 (multi-broker) without touching `cmd/basket` or `cmd/holdings`.  
**Effort**: Medium (2d) | **Risk**: Low (additive)

**R4.1 — Define `pkg/broker/broker.go` interface**
```go
type Broker interface {
    GetHoldings() ([]Holding, error)
    PlaceOrders(orders []Order) ([]string, error)
    GetPositions() ([]Holding, error)
    IsAuthenticated() bool
}
```

**R4.2 — Implement `pkg/broker/zerodha/`**  
Move Kite-specific logic from `pkg/kiteclient/`, `pkg/executor/`, and `pkg/portfolio/` into `pkg/broker/zerodha/zerodha.go` which implements the `Broker` interface.

**R4.3 — Wire `cmd/basket.go` and `cmd/holdings.go` to `broker.Broker`**  
Commands receive a `Broker` via the app's `Metadata` map or a package-level factory. Broker-agnostic.

**R4.4 — Research: Fyers & AngelOne APIs**  
- Fyers REST API, Go community wrapper (`gofyers`)
- AngelOne SmartAPI — REST + WebSocket, no official Go SDK, custom HTTP client needed
- Upstox REST API, Go SDK available (`upstox-go`)
- Effort per broker implementation: ~1–2d once interface is defined

---

## Phase R5 — Drift Monitoring Daemon

**Goal**: Real background drift alerting service, distinct from the interactive backtest simulator. Sends notifications when portfolio drift exceeds threshold.  
**Effort**: Large (4–6d) | **Risk**: Medium (new infrastructure)

**R5.1 — Define `pkg/alert/alert.go` Alerter interface**
```go
type Alert struct { Title, Body, Level string } // Level: "info", "warn", "critical"
type Alerter interface { Send(a Alert) error }
```

**R5.2 — Telegram bot alerter (`pkg/alert/telegram.go`)**  
Requires `TELEGRAM_BOT_TOKEN` and `TELEGRAM_CHAT_ID` in config. Plain HTTP POST to `https://api.telegram.org/bot{token}/sendMessage` — no SDK needed.

**R5.3 — Discord webhook alerter (`pkg/alert/discord.go`)**  
Single HTTP POST with JSON body to Discord Incoming Webhook URL.

**R5.4 — Drift calculation engine (`pkg/daemon/drift.go`)**  
Implements: `Drift = ½ Σ|w_actual_i - w_target_i|`. Fetches live quotes, computes actual vs. target weights, returns drift index.

**R5.5 — Daemon runner (`pkg/daemon/daemon.go`)**  
- Runs drift check at configurable interval (default: daily at 15:45 IST)
- Persists last-check state to `data/daemon_state.json` (survives restarts)
- On drift > threshold: calls all configured Alerters

**R5.6 — Add `mycase daemon` subcommand**
```
mycase daemon start    # Start background daemon (writes PID file)
mycase daemon stop     # Stop running daemon
mycase daemon status   # Show last drift check results
mycase daemon check    # One-shot drift check (no loop)
mycase daemon install  # Write launchd plist (macOS) or print systemd unit (Linux)
mycase daemon uninstall
```

**R5.7 — Config additions to `config/pipeline.yaml`**
```yaml
alerts:
  drift_threshold: 0.05
  channels: [telegram, discord]
  telegram_bot_token: ""   # Override via MYCASE_TELEGRAM_TOKEN env
  telegram_chat_id: ""
  discord_webhook_url: ""
```

---

## Phase R6 — Tax & Transaction Awareness

**Goal**: Make optimizer and basket engine aware of real-world friction costs before suggesting orders.  
**Effort**: Medium (2–3d) | **Risk**: Low (additive filter layer)

**R6.1 — Transaction cost model (`pkg/costs/costs.go`)**  
Indian market charges:
- STT: 0.1% on buy, 0.1% on sell (equity delivery)
- Stamp duty: 0.015% on buy
- DP charge: flat ₹15.93 per ISIN per day of sell
- Brokerage: configurable (Zerodha = ₹0 for equity delivery)
- SEBI charges: 0.0001%

**R6.2 — Micro-transaction filter**  
Skip trade if `transaction_cost / trade_value > configurable_threshold` (e.g. 0.5%). Add to `pkg/optimizer/rebalance.go`.

**R6.3 — STCG/LTCG warning layer**  
Cross-reference sell orders against purchase date. Flag orders where `holding_period < 365 days` as STCG (15%) vs. LTCG (10% above ₹1L). Show warning banner in basket output.  
*Research needed*: verify current STCG/LTCG rates under Finance Act 2024 amendments.

---

## Phase R7 — Historical Backtesting Engine

**Goal**: Full portfolio simulator with configurable rebalancing frequency and comprehensive performance analytics.  
**Effort**: XL (8–12d) | **Risk**: Medium

**R7.1** — Extend `pkg/yfinance/prices.go` to fetch and cache daily adjusted close prices for multi-year windows.

**R7.2 — Simulation engine (`pkg/backtest/`)**
```
pkg/backtest/
├── types.go    # SimConfig, SimResult, DailySnapshot
├── engine.go   # Core simulation loop
└── metrics.go  # CAGR, Max Drawdown, Sharpe, Sortino, Calmar
```
Parameters: initial capital, start/end date, rebalancing frequency (`monthly`, `quarterly`, `drift-triggered`), slippage %.

**R7.3** — Benchmark comparison: download `^NSEI`, `^CNXSC`. Calculate portfolio Alpha and Beta.

**R7.4 — Add `mycase backtest` subcommand**
```
mycase backtest --file data/microsmall.csv --capital 100000 \
    --from 2023-01-01 --to 2026-07-01 --rebalance quarterly --slippage 0.1
```

Reference: existing `pkg/monitoring/simulator.go` has partial Sharpe/Sortino implementations to leverage.

---

## Phase R8 — Web Dashboard

**Goal**: Local web UI to visualize portfolio, adjust weights interactively, trigger rebalance.  
**Effort**: XL (10–15d) | **Risk**: High (frontend stack decision, new tech surface)

**R8.1 — Go HTTP server (`pkg/server/`)**
```
GET  /api/portfolio/:name     → Current holdings + target weights
GET  /api/quotes/:name        → Live prices
POST /api/rebalance/:name     → Trigger basket order (requires auth)
GET  /api/performance/:name   → Backtest results
```
Lightweight `net/http` stdlib — no Gin needed for this scope.

**R8.2 — Frontend: Plain HTML/CSS/JS + Web Components + Apache ECharts**  
No framework, no build pipeline. ECharts for: weight donut, drift timeline, backtest equity curve. Web Components for: `<portfolio-summary>`, `<weight-slider>`, `<holdings-table>`, `<drift-alert>`. SSE for live quote streaming (not polling).

```
static/
├── index.html
├── css/app.css
├── js/app.js
├── js/components/portfolio-summary.js, weight-slider.js, holdings-table.js
└── vendor/echarts.min.js   # vendored, no CDN
```

**R8.3 — Embed static assets**  
`//go:embed static/*` embeds entire `static/` tree into the binary. `mycase serve --port 8080` starts the dashboard.
