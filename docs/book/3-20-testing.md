# Testing Architecture & Coverage Audit

## 1. The Test Pyramid

Tests fall into three tiers, distinguished by build tag so the default `go test` stays fast, hermetic, and offline:

```
        ╱╲      E2E (few, tag: e2e)       — a full command via cli.Command.Run,
       ╱  ╲                                 mock broker, temp DuckDB, fixture CSVs.
      ╱────╲    Integration (tag:          — real network (Schwab/Yahoo/EDGAR),
     ╱      ╲    integration)                skippable when creds/network absent.
    ╱────────╲  Unit (many, default)       — pure logic, table-driven, no I/O.
   ╱__________╲
```

The system is a pipeline of commands with data flowing through DuckDB and CSV. Unit tests cover pure logic (scoring, filters, weights, metrics, calendars); integration tests exercise live API client paths against real endpoints; end-to-end tests drive whole command chains in-process, since `main.go` builds a `*cli.Command` and the commands are exported (`PickCommand`, `PipelineCommand`, etc.) — a test constructs the same tree and calls `.Run(ctx, args)` with a mock broker and a `t.TempDir()` data directory.

---

## 2. Running Tests

| Command | What it runs | Timeout / Flags |
| :--- | :--- | :--- |
| `make test` | All unit tests across all packages, no network | 30s timeout |
| `make test-race` | Unit tests with the Go race detector enabled | 60s timeout, `-race` |
| `make test-verbose` | Unit tests with full per-test stdout output | `-v` |
| `make test-cover` | Unit tests printing statement coverage % per package | `-cover` |
| `make test-coverage`| Unit tests + full coverage profiling | Generates `coverage.out` and `coverage.html` |
| `make test-integration` | Network-dependent live API tests | `//go:build integration`, 120s timeout |

View the coverage report: `go tool cover -html=coverage.out`.

---

## 3. Core Testing Conventions

- **No network in unit tests.** HTTP-dependent code goes behind `//go:build integration` and runs only via `make test-integration`. Live tests skip cleanly when credentials or network are absent.
- **No writes to `data/`, `report/`, or `config/`** from tests — use `t.TempDir()` for all temporary file and DuckDB I/O.
- **Table-driven tests** for pure functions; `±ε` tolerances for floating-point calculations (typically `1e-9`).
- **Property tests** via `testing/quick` for invariants that must hold for any input (weights sum to 1.0, RSI $\in [0, 100]$).
- **Hand-written mocks, no external mocking framework** — interfaces are the seams (`broker.MockBroker` exists; a mock fetcher satisfies `datafetcher.Router`'s consumer interfaces).
- **Fixtures** live under `pkg/<pkg>/testdata/`.
- **The race detector must pass** before merging concurrency-touching changes (daemon, SSE broadcaster).

---

## 4. Master Package Test Registry & Coverage Audit

Following the test expansion on **September 27, 2026**, **100% of all runtime application and library packages (45/45)** are covered by automated unit test suites and pass under `make test`:

| # | Package | Layer | Test File(s) | Status | Coverage | Capabilities Verified |
| :-: | :--- | :---: | :--- | :---: | :---: | :--- |
| 1 | `github.com/raghavkgarg/mycase` | CLI Root | `main_test.go` | `ok` | **4.4%** | CLI version string assembly, root & subcommand resolution |
| 2 | `github.com/raghavkgarg/mycase/cmd` | L6 | `pipeline_test.go`, `cmd_test.go`, etc. | `ok` | **0.5%** | CLI command routing, flag validation, pipeline args |
| 3 | `pkg/alert` | L0 Leaf | `alert_test.go` | `ok` | **87.9%** | Discord webhook HTTP mock (200/204), Telegram bot client, network errors |
| 4 | `pkg/attribution` | L1 | `attribution_test.go` | `ok` | **92.8%** | Brinson-Fachler asset allocation and selection attribution |
| 5 | `pkg/autopilot` | L5 | `autopilot_test.go`, `schedule_test.go` | `ok` | **8.6%** | Autonomous rebalance proposals, scheduled timer cadences |
| 6 | `pkg/backtest` | L4 | `calibrate_test.go`, `backtest_test.go` | `ok` | **48.7%** | Historical backtest simulator, rolling Spearman Rank IC |
| 7 | `pkg/broker` | L2 | `broker_test.go` | `ok` | **41.0%** | Broker interface abstractions, mock broker operations |
| 8 | `pkg/broker/schwab` | L1 | `schwab_test.go`, `client_test.go` | `ok` | **46.1%** | Schwab OAuth token refresh, order placement, quote fetching |
| 9 | `pkg/broker/types` | L-1 Leaf | `types_test.go` | `ok` | *Pure DTOs* | Broker-agnostic DTOs (`Holding`, `Order`, `OrderResult`, `MarketConfig`) |
| 10 | `pkg/broker/zerodha` | L1 | `zerodha_test.go` | `ok` | **20.4%** | Kite IP whitelisting error enrichment, mock fallback, credential checks |
| 11 | `pkg/cache` | L0 | `cache_test.go`, `prices_test.go` | `ok` | **78.7%** | DuckDB price/fundamental caching, freshness sentry, TTL buffers |
| 12 | `pkg/config` | L0 Leaf | `config_test.go`, `defaults_yaml_test.go` | `ok` | **59.7%** | YAML master configuration loader, defaults migration, validation |
| 13 | `pkg/costs` | L-1 Leaf | `costs_test.go` | `ok` | **95.2%** | STT, stamp duty, exchange turnover, broker commissions |
| 14 | `pkg/csvloader` | L0 Leaf | `csvloader_test.go` | `ok` | **60.6%** | Constituent CSV parsing, ticker normalization, golden file loading |
| 15 | `pkg/daemon` | L4 | `daemon_test.go` | `ok` | **29.9%** | Background drift monitoring daemon, alert throttling |
| 16 | `pkg/datafetcher` | L1 | `router_test.go` | `ok` | **65.4%** | Market-routed data fetching (US to Schwab, India to Yahoo/NSE) |
| 17 | `pkg/edgar` | L0 | `edgar_test.go`, `facts_test.go` | `ok` | **58.9%** | SEC EDGAR XBRL company facts parsing, financial ratios |
| 18 | `pkg/eod` | L5 | `eod_test.go` | `ok` | **21.6%** | Unified EOD update orchestrator, clock injection, stdout capture |
| 19 | `pkg/excel` | L0 Leaf | `excel_test.go` | `ok` | **90.4%** | Raw XLSX parsing, cell ticker extraction, portfolio CSV conversion |
| 20 | `pkg/executor` | L4 | `executor_test.go` | `ok` | **37.3%** | Batch order execution, order state verification, retry loops |
| 21 | `pkg/golden` | L1 | `golden_test.go` | `ok` | **9.0%** | Golden model portfolio reconciliation, drift baseline |
| 22 | `pkg/kiteauth` | L0 Leaf | `kiteauth_test.go` | `ok` | **26.0%** | Zerodha Kite Connect login and automated TOTP handling |
| 23 | `pkg/kiteclient` | L0 Leaf | `client_test.go` | `ok` | **100.0%** | Kite client factory, HTTP proxy routing, mock credential guards |
| 24 | `pkg/logging` | L-1 Leaf | `logging_test.go` | `ok` | **92.6%** | Structured JSONL logger, request ID tracing, log rotation |
| 25 | `pkg/market` | L0 Leaf | `market_test.go` | `ok` | **94.6%** | IST market hours check, GTT trigger/limit prices, buffer ticks |
| 26 | `pkg/marketcal` | L-1 Leaf | `marketcal_test.go` | `ok` | **87.5%** | Exchange settlement calendar, holiday aware EOD cutoff |
| 27 | `pkg/marketdata` | L0 | `marketdata_test.go` | `ok` | **11.8%** | Market data structures, Bhavcopy formatters, settlement dates |
| 28 | `pkg/marketfmt` | L-1 Leaf | `marketfmt_test.go` | `ok` | **98.4%** | Currency and numeric formatting for INR (₹ Lakh/Cr) and USD ($) |
| 29 | `pkg/monitoring` | L3 | `monitoring_test.go` | `ok` | **55.2%** | Portfolio drift index calculation, rebalance triggers |
| 30 | `pkg/optimizer` | L1 Leaf | `optimizer_test.go` | `ok` | **26.6%** | Inverse-volatility weighting, sector caps, transaction threshold |
| 31 | `pkg/pithistory` | L4 | `pithistory_test.go`, `health_test.go` | `ok` | **29.0%** | Point-in-Time DuckDB history, data integrity sentry, entropy checks |
| 32 | `pkg/portfolio` | L-1 Leaf | `portfolio_test.go` | `ok` | **100.0%** | Portfolio model structures, weight rebalancing normalization |
| 33 | `pkg/printer` | L-1 Leaf | `printer_test.go` | `ok` | **89.7%** | Terminal table rendering, factor score heatmaps |
| 34 | `pkg/rawcapture` | L-1 Leaf | `rawcapture_test.go` | `ok` | **97.3%** | Transparent HTTP response body capture hooks |
| 35 | `pkg/rawstore` | L0 Leaf | `rawstore_test.go` | `ok` | **93.2%** | Raw HTTP payload archive storage, size-bounded pruning |
| 36 | `pkg/render` | L-1 Leaf | `render_test.go` | `ok` | **89.0%** | KV pair rendering, terminal styling |
| 37 | `pkg/scheduler` | L6 | `scheduler_test.go`, `lock_test.go` | `ok` | **70.9%** | Autonomous daily one-shot scheduler, lock mutual exclusion |
| 38 | `pkg/selectiontracker` | L0 Leaf | `tracker_test.go` | `ok` | **10.7%** | Selection funnel tracking, attrition reason recording |
| 39 | `pkg/server` | L5 | `server_test.go`, `handlers_test.go` | `ok` | **11.4%** | Web dashboard HTTP server, SSE live updates, REST APIs |
| 40 | `pkg/stockpicker` | L3 | `stockpicker_test.go`, `scoring_test.go` | `ok` | **33.9%** | EarlyMB, Multibagger, Fair Price, Value, USQM scoring & gates |
| 41 | `pkg/tax` | L-1 Leaf | `tax_test.go` | `ok` | **89.3%** | FIFO lot matching, wash sale detection, tax-loss harvesting |
| 42 | `pkg/themedb` | L1 | `themedb_test.go` | `ok` | **52.2%** | Theme lifecycle tracking, rebalance versioning in DuckDB |
| 43 | `pkg/themereturn` | L2 | `themereturn_test.go` | `ok` | **61.3%** | Audited Triple Returns (HPR, MWR/XIRR, TWR), dividends |
| 44 | `pkg/universe` | L0 Leaf | `resolver_test.go` | `ok` | **86.9%** | Historical constituent snapshot persistence, point-in-time lookup |
| 45 | `pkg/yfinance` | L1 | `yfinance_test.go`, `metrics_test.go` | `ok` | **41.6%** | Yahoo Finance scraper, delivery delta, Composite RS, VCP, RVOL |

*(Note: Internal build & architecture linters in `devtools/checkdeps`, `devtools/depsgraph`, and `devtools/internal/layers` are developer tooling excluded from runtime binaries and unit test suites.)*

---

## 5. Detailed Breakdown of Newly Covered Packages

| Package | Test File | Statement Coverage | What Is Tested & Mocked |
| :--- | :--- | :---: | :--- |
| **`pkg/kiteclient`** | [`client_test.go`](file:///Users/raghavgarg/Projects/myGo/mycase/pkg/kiteclient/client_test.go) | **100.0%** | Mock mode guards, empty & placeholder credentials, live init, HTTP proxy configuration, file loading with fallback |
| **`pkg/market`** | [`market_test.go`](file:///Users/raghavgarg/Projects/myGo/mycase/pkg/market/market_test.go) | **94.6%** | IST market hours open/close boundaries (9:14, 9:15, 11:30, 15:30, 15:31), weekends, GTT params (±0.3% trigger, ±₹2 limit), 3% buffer tick rounding |
| **`pkg/excel`** | [`excel_test.go`](file:///Users/raghavgarg/Projects/myGo/mycase/pkg/excel/excel_test.go) | **90.4%** | Zip header validation (`PK\x03\x04`), cell ticker extraction across Column D, Column A, shifted next rows, reserved filters, end-to-end XLSX to CSV conversion |
| **`pkg/alert`** | [`alert_test.go`](file:///Users/raghavgarg/Projects/myGo/mycase/pkg/alert/alert_test.go) | **87.9%** | Discord webhook HTTP mock server (`200 OK`, `204 No Content`, `400 Bad Request`), JSON payload formatting (`**[level] Title**\nBody`), network errors, Telegram bot client |
| **`pkg/universe`** | [`resolver_test.go`](file:///Users/raghavgarg/Projects/myGo/mycase/pkg/universe/resolver_test.go) | **86.9%** | Historical constituent snapshot persistence, exact-date constituent lookup, rolling historical date resolution, nil fallback when no snapshot exists |
| **`pkg/eod`** | [`eod_test.go`](file:///Users/raghavgarg/Projects/myGo/mycase/pkg/eod/eod_test.go) | **21.6%** | Market calendar clock injection, default fallback to `marketcal.NSE`, multi-method dry-run preview generation, stdout pipe capture into slog |
| **`pkg/broker/zerodha`** | [`zerodha_test.go`](file:///Users/raghavgarg/Projects/myGo/mycase/pkg/broker/zerodha/zerodha_test.go) | **20.4%** | IP whitelisting error enrichment regex (`IP (x.x.x.x) is not allowed`), mock fallback when `liveMode=false` or credentials absent, live client initialization |
| **`mycase` (root)** | [`main_test.go`](file:///Users/raghavgarg/Projects/myGo/mycase/main_test.go) | **4.4%** | Version string assembly (`Version`, `GitCommit`, `BuildDate`, runtime, OS/arch), CLI root command and subcommand execution routing |
| **`pkg/broker/types`** | [`types_test.go`](file:///Users/raghavgarg/Projects/myGo/mycase/pkg/broker/types/types_test.go) | *(Pure DTOs)* | JSON serialization/deserialization for `Holding`, struct fields for `Order`, `OrderResult`, and `MarketConfig` for US and India markets |

### Detailed Test Specifications

#### 5.1 `pkg/kiteclient` (100.0% Coverage)
- **`TestInitKiteClient_ForceMock`**: Asserts that passing `forceMock = true` returns `(nil, true)` regardless of config values.
- **`TestInitKiteClient_EmptyCredentials` & `TestInitKiteClient_PlaceholderCredentials`**: Verifies that empty strings or defaults like `"your_api_key"` / `"your_access_token"` trigger mock mode safely.
- **`TestInitKiteClient_ValidCredentials`**: Validates client instantiation and proxy parsing when `HTTPProxy` is configured.
- **`TestLoadAndInitClient_MissingFile` & `TestLoadAndInitClient_ValidFile`**: Tests configuration loading from disk with mock fallback on missing files.

#### 5.2 `pkg/market` (94.6% Coverage)
- **`TestIsMarketOpen`**: Table-driven test across all trading hour boundaries in Indian Standard Time (`Asia/Kolkata`):
  - Weekday 11:30 AM IST (Midday trading: `true`)
  - Weekday 09:15 AM IST (Market open boundary: `true`)
  - Weekday 15:30 PM IST (Market close boundary: `true`)
  - Weekday 09:14 AM IST (Pre-open: `false`)
  - Weekday 15:31 PM IST (Post-close: `false`)
  - Weekend Saturday 11:00 AM IST (`false`)
  - Weekend Sunday 02:00 PM IST (`false`)
- **`TestCalculateGTTParams`**: Validates Zerodha GTT parameter calculations with ₹0.10 tick size:
  - BUY order: trigger at $+0.3\%$, limit at $+₹2.00$
  - SELL order: trigger at $-0.3\%$, limit at $-₹2.00$
- **`TestCalculateBufferedLimitPrice`**: Verifies $+3.0\%$ buffer limit price calculation rounded to nearest tick.

#### 5.3 `pkg/excel` (90.4% Coverage)
- **`TestIsXLSXFile_NonExistent` & `TestIsXLSXFile_PlainFile`**: Verifies that non-existent files and non-zip plain text files are rejected.
- **`TestFindTicker`**: Validates ticker resolution logic:
  - Column D identifier matching (e.g. `NVDA`)
  - Column A matching (e.g. `AAPL`)
  - Shifted next-row Column A matching for ETF sheets (e.g. `MSFT`)
  - Filtering out non-ticker metadata strings (`CASH`, `USD`, `IDENTIFIER`, etc.)
- **`TestConvertXLSXToCSV_EndToEnd`**: Constructs an in-memory zip archive with `xl/sharedStrings.xml` and `xl/worksheets/sheet1.xml` and tests `ConvertXLSXToCSV`, asserting that valid portfolio CSV output (`ticker,weight`) is generated.

#### 5.4 `pkg/alert` (87.9% Coverage)
- **`TestDiscordAlerter_Success`**: Spins up an in-memory `httptest.Server`, delivers an `Alert`, and asserts that the HTTP method is `POST`, `Content-Type` is `application/json`, payload matches `**[level] Title**\nBody`, and both `200 OK` and `204 No Content` are treated as successful.
- **`TestDiscordAlerter_HTTPError`**: Verifies error propagation when the webhook returns `400 Bad Request`.
- **`TestDiscordAlerter_InvalidURL`**: Tests connection failure handling.
- **`TestTelegramAlerter_ConnectionError`**: Verifies error return when using invalid bot credentials.

#### 5.5 `pkg/universe` (86.9% Coverage)
- **`TestGetConstituentsForDate_NoMatch`**: Verifies that querying an index with no historical snapshots returns `nil` tickers and an empty string without an error (prompting fallback to live constituents).
- **`TestSaveAndGetConstituents`**: Saves a dated constituent snapshot CSV, tests exact-date lookup, verifies prefix normalization (`NSE:`), and tests historical rolling resolution where a query for a later date resolves to the closest prior snapshot.

#### 5.6 `pkg/eod` (21.6% Coverage)
- **`TestConfig_Clock`**: Asserts that `Config.clock()` defaults to `marketcal.NSE` when unset, and honors injected custom clocks (e.g. NYSE).
- **`TestConfig_DryRunPlan`**: Tests preview generation across single and multi-method runs (`earlymb, multibagger`).
- **`TestWithCapturedStdout`**: Verifies that stdout redirection safely captures console output into slog debug events without leaking to terminal.
- **`TestResult_Fields`**: Validates `Result` structure properties.

#### 5.7 `pkg/broker/zerodha` (20.4% Coverage)
- **`TestEnrichIPError_WithIP`**: Verifies that Zerodha Kite API IP errors matching `IP (x.x.x.x) is not allowed` are rewritten to include `[ACTION REQUIRED] Please whitelist IP 'x.x.x.x' under App Settings at https://developers.kite.trade/profile`.
- **`TestNew_MockMode`**: Verifies that `liveMode = false` returns `*broker.MockBroker`.
- **`TestNew_MissingConfigFile`**: Verifies safe fallback to mock mode when the configuration file is missing.
- **`TestNew_ValidConfig`**: Verifies live client initialization with valid credentials.

#### 5.8 `mycase` (Root CLI) (4.4% Coverage)
- **`TestVersionString`**: Asserts that `versionString()` produces non-empty build metadata containing Version, Git commit, build timestamp, Go runtime, OS, and architecture.
- **`TestCommandName`**: Tests root vs subcommand identification via `cli.Command.Run()`.

#### 5.9 `pkg/broker/types` (Pure DTOs)
- **`TestHolding_JSONSerialization`**: Verifies roundtrip JSON marshaling and unmarshaling of `Holding`.
- **`TestOrder_Fields` & `TestOrderResult_Fields`**: Validates struct field assignments.
- **`TestMarketConfig_Fields`**: Validates US vs India market configurations.

---

## 6. Key Regression Shields & Invariants Enforced

1. **Bug-014 Delivery Settlement Date Invariant**:
   - `pkg/stockpicker/scoring.go`, `run.go`, `retry.go`, `incubator.go`, and `velocity.go` all pass `marketcal.NSE.SettlementDate(now)` with `lagDays = 0`.
   - Verified that running on Friday at 21:15, Saturday, Sunday, or Monday evaluates the exact same settlement date (`2026-09-25`) without date drift.

2. **Dynamic Theme Return Invariant**:
   - [`pkg/themereturn/themereturn_test.go`](file:///Users/raghavgarg/Projects/myGo/mycase/pkg/themereturn/themereturn_test.go): Validates structural correctness, non-negative capital values, and positive returns dynamically against the user's active `data/microsmall.csv`, preventing brittle failures when portfolio holdings change.

3. **Weekend Cache Sentry Invariant**:
   - [`pkg/cache/cache_test.go`](file:///Users/raghavgarg/Projects/myGo/mycase/pkg/cache/cache_test.go): Uses `-72h` backdating so staleness tests correctly identify stale records on both weekdays and weekends without tripping on Saturday/Sunday non-trading settlement rollbacks.

4. **1Y Historical Buffer Invariant**:
   - [`pkg/cache/cache_test.go`](file:///Users/raghavgarg/Projects/myGo/mycase/pkg/cache/cache_test.go): Synchronized `rangeKeyToStartDate("1y")` test to match the canonical `-1y -15d` buffer in `pkg/cache/prices.go`.

5. **Master YAML Configuration Invariant**:
   - [`pkg/config/defaults_yaml_test.go`](file:///Users/raghavgarg/Projects/myGo/mycase/pkg/config/defaults_yaml_test.go): Verifies that `config/defaults.yaml` loads and correctly populates all strategy parameters, market hours, TAM estimates, and governance thresholds.

---

## 7. Integration Tests

Integration tests hit real endpoints and are excluded from `make test`; they run only via `make test-integration` (`//go:build integration`):
- **EDGAR Client**: `pkg/edgar/edgar_integration_test.go` (live `data.sec.gov`).
- **NSE Delivery Scraper**: `pkg/stockpicker/delivery_invariant_integration_test.go` (live Bhavcopy delivery history).

Per API discipline rules, live tests are kept minimal, skippable when network or credentials are unavailable, and prefer recorded responses (`data/raw/`) where a fixture suffices.

---

## 8. Fuzz Targets

Parsers are natural fuzz targets ("no panic, error is acceptable on bad input"):
- `LoadBasketCSV` and `GetUniverseName` in `pkg/csvloader`.
- Fundamentals JSON parsing in `pkg/yfinance`.
- `PipelineConfig` YAML in `pkg/config`.

Run a fuzz test with:
```bash
go test -fuzz=FuzzLoadBasketCSV -fuzztime=60s ./pkg/csvloader/
```
