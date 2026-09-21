# Testing

## The test pyramid

Tests fall into three tiers, distinguished by build tag so the default `go test` stays
fast and offline:

```
        ╱╲      E2E (few, tag: e2e)       — a full command via cli.Command.Run,
       ╱  ╲                                 mock broker, temp DuckDB, fixture CSVs.
      ╱────╲    Integration (tag:          — real network (Schwab/Yahoo/EDGAR),
     ╱      ╲    integration)                skippable when creds/network absent.
    ╱────────╲  Unit (many, default)       — pure logic, table-driven, no I/O.
   ╱__________╲
```

The system is a pipeline of commands with data flowing through DuckDB and CSV. Unit tests
cover the pure logic (scoring, filters, weights, metrics); integration tests exercise the
live API client paths against real endpoints; end-to-end tests drive whole command chains
in-process, since `main.go` builds a `*cli.Command` and the commands are exported
(`PickCommand`, `PipelineCommand`, …) — a test constructs the same tree and calls
`.Run(ctx, args)` with a mock broker and a `t.TempDir()` data dir.

## Running tests

| Command | What it runs |
|---------|-------------|
| `make test` | All unit tests (30s timeout), no network |
| `make test-race` | Unit tests with the Go race detector (60s timeout) |
| `make test-verbose` | Unit tests with full output |
| `make test-coverage` | Unit tests + `coverage.out` (+ HTML report) |
| `make test-integration` | Network-dependent tests (`//go:build integration`, 120s timeout) |

View the coverage report: `go tool cover -html=coverage.out`.

## Conventions

- **No network in unit tests.** HTTP-dependent code goes behind `//go:build integration`
  and runs only via `make test-integration`. Live tests skip cleanly when credentials or
  network are absent.
- **No writes to `data/`, `report/`, or `config/`** from tests — use `t.TempDir()` for all
  temporary file I/O.
- **Table-driven tests** for pure functions; `±ε` tolerances for floats (typically `1e-9`).
- **Property tests** via `testing/quick` for invariants that must hold for any input
  (weights sum to 1.0, RSI ∈ [0, 100]).
- **Hand-written mocks, no mocking framework** — interfaces are the seams
  (`broker.MockBroker` exists; a mock fetcher satisfies `datafetcher.Router`'s consumer
  interfaces). This matches the minimal-dependencies constraint.
- **Fixtures** live under `pkg/<pkg>/testdata/`.
- **The race detector must pass** before merging concurrency-touching changes (daemon, SSE
  broadcaster).

## Coverage by tier

Coverage is uneven by design — pure-logic packages are held to a high bar; I/O glue is
covered by integration/E2E rather than chased for unit percentage. Representative numbers:

| Tier | Packages (illustrative) |
|------|-------------------------|
| **Strong (≥85%)** | `portfolio`, `marketfmt`, `rawcapture`, `costs`, `rawstore`, `attribution`, `logging`, `printer`, `tax`, `render`, `marketcal` |
| **Moderate (45–85%)** | `cache`, `datafetcher`, `csvloader`, `edgar`, `monitoring`, `themedb`, `backtest`, `config`, `broker/schwab` |
| **Weak / priority targets** | `stockpicker`, `optimizer`, `yfinance`, `daemon`, `pithistory`, `marketdata`, `server`, `selectiontracker`, `autopilot` |
| **Untested glue / dormant** | `alert`, `broker` iface, `market`, `excel`, `universe`, and the India-legacy `zerodha`/`kiteclient`/`kiteauth`/`themereturn` |

`pkg/stockpicker` (the scoring engine, architecture Layer 3) is the highest-value gap:
its selection and scoring logic is pure and table-testable, and a regression there is a
strategy regression. `optimizer` (sector-cap redistribution, inverse-vol weights,
micro-transaction filter) is second.

## Integration tests

Integration tests hit real endpoints and are excluded from `make test`; they run only via
`make test-integration` (`//go:build integration`). They exist for the EDGAR client
(`pkg/edgar/edgar_integration_test.go`, live `data.sec.gov`) and the NSE delivery-data
fetch path (`pkg/stockpicker/delivery_invariant_integration_test.go`). Per the API
discipline rules, live tests are the exception — kept minimal, skippable when network or
credentials are unavailable, and preferring recorded responses (`data/raw/`) where a
fixture suffices.

## Fuzz targets (candidates)

Parsers are the natural fuzz targets — the invariant is "no panic, error is acceptable on
bad input": `LoadBasketCSV` and `GetUniverseName` (`pkg/csvloader`), fundamentals JSON
parsing (`pkg/yfinance`), and `PipelineConfig` YAML (`pkg/config`). Run one with, e.g.,
`go test -fuzz=FuzzLoadBasketCSV -fuzztime=60s ./pkg/csvloader/`.
