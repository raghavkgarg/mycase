# Testing Guide

## Running Tests

| Command | What it runs |
|---------|-------------|
| `make test` | All unit tests (30s timeout) |
| `make test-race` | All unit tests with Go race detector (60s timeout) |
| `make test-verbose` | All unit tests with full output |
| `make test-coverage` | Unit tests + generates `coverage.out` |
| `make test-integration` | Network-dependent tests (`//go:build integration`, 120s timeout) |

View coverage report: `go tool cover -html=coverage.out`

## Coverage by Package

All 11 packages pass `go test -race ./...` clean.

| Package | Tests | What's covered |
|---------|------:|---------------|
| `cmd` | 9 | `parsePerfDate` (ISO/compact/empty/invalid); `cleanBasketArg` (leading dashes); `PipelineConfig.UnmarshalYAML` (defaults, explicit, negative tolerance) |
| `pkg/backtest` | 22 | CAGR, MaxDrawdown, Sharpe, Sortino, Calmar, Beta, Alpha (edge cases); `Run` (no-rebalance, slippage, quarterly rebalance, invalid date range, missing ticker) |
| `pkg/cache` | 21 | Schema idempotency; upsert/ON CONFLICT; int64/float64 round-trip; staleness (IST same-day, 24h fundamentals); range filtering; clear-ticker/clear-all |
| `pkg/config` | 14 | `LoadMFSConfig` (missing file, unknown strategy, malformed JSON, valid); themes; config round-trip; `LoadAlertConfig` (missing, valid, zero threshold, no section, malformed) |
| `pkg/costs` | 13 | STT/stamp/DP/SEBI components (buy/sell); zero qty/price guards; custom brokerage; cost ratio consistency; STCG/LTCG classification under Finance Act 2024 |
| `pkg/csvloader` | 8 | `GetUniverseName` (table + property test); `ParseBasket` (valid, case-insensitive header, missing header, empty body, duplicate ticker, invalid weight) |
| `pkg/daemon` | 10 | `CalculateDrift` (exact match, no holdings, partial drift, T+1/T+2 qty, single stock, bounded); `NextIST1545` (always future, correct time, within one day) |
| `pkg/monitoring` | 8 | `GetCapStallSeverity` (boundary, negative growth); `RunSimulation` (determinism, insufficient history, empty portfolio, no NaN) |
| `pkg/optimizer` | 26 | `CapWeights` (empty, all under, basic, multiple over, too-tight, single, sum invariant, quick-check property); math (mean, covariance, downside deviation, total return, daily returns, ulcer index); `OptimizeFreshBuy`; `FilterMicroTransactions`; `DetectExits` |
| `pkg/stockpicker` | 10 | `IsAbove200DaySMA`; `NormalizeValue` (table + out-of-range clamping); `LoadLocalCSVConstituents`; `IsEligible`; `ApplyHysteresisSelection`; `ApplyRebalancingBand` |
| `pkg/yfinance` | 18 | RSI (insufficient data, all-up, all-down, alternating, always-in-bounds); sales growth; DSO; volume breakout; `MapTickerToYahoo`; `CleanIntradayNoise` (nil, empty, old timestamp, today-after-close) |

## Test Conventions

- **No network calls in unit tests.** HTTP-dependent code uses `//go:build integration` — only runs via `make test-integration`.
- **No writes to `data/`, `report/`, or `config/`** from tests. Use `t.TempDir()` for all temporary file I/O.
- **Race detector must pass.** Run `make test-race` before merging. All packages currently pass clean.
- **Table-driven tests** for pure math functions; use `±ε` tolerances for floats (typically `1e-9`).
- **Property tests** via `testing/quick` for invariants that must hold for any input (weights sum to 1.0, RSI ∈ [0, 100]).

## Coverage Gaps

These packages have no tests yet. Highest-value targets:

| Package | Approach |
|---------|---------|
| `pkg/server` | `httptest.NewRecorder` for handler unit tests; `httptest.NewServer` for SSE |
| `pkg/alert` | Mock HTTP server via `httptest.NewServer` for Telegram and Discord alerters |
| `pkg/performance` | Use `broker.MockBroker` directly — no network needed |
| `pkg/report` | Table-driven unit tests on `BuildRationale` — pure text generation |
| `pkg/broker/zerodha` | Mock HTTP server with fixture JSON responses |

## Fuzz Targets (Planned)

| Target | Package | Invariant |
|--------|---------|-----------|
| `FuzzLoadBasketCSV` | `pkg/csvloader` | No panic; error OK on bad input |
| `FuzzGetUniverseName` | `pkg/csvloader` | Returns non-empty string |
| `FuzzParseFundamentalsJSON` | `pkg/yfinance` | No panic |
| `FuzzPipelineConfigYAML` | `pkg/config` | No panic |

Run a fuzz target: `go test -fuzz=FuzzLoadBasketCSV -fuzztime=60s ./pkg/csvloader/`

## Integration Tests

Integration tests require network access and Zerodha credentials. They are excluded from `make test` and only run via `make test-integration`. Set `MYCASE_SKIP_INTEGRATION=1` to skip individual tests when network is unavailable.

No integration tests are currently implemented. When added, they go in `*_integration_test.go` files with `//go:build integration` at the top.
