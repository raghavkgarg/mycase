# CLI Rendering Layer — `pkg/render/`

## Problem

All CLI commands use ad-hoc `fmt.Printf` with hand-crafted column widths and ASCII separators.
This is fragile, inconsistent across commands, and painful to maintain. However, the fix must
not introduce new failure modes — a 3rd-party rendering library that panics on edge cases or
silently produces empty output is worse than ugly-but-present text.

## Design Principles

1. **Data is never lost.** If the rendering layer fails for any reason, output falls back to
   raw tab-separated text. A `recover` guard ensures panics never propagate.
2. **Stdlib only for core rendering.** `text/tabwriter` has been in Go since 1.0 — 14 years
   of battle-testing. No version churn, no API breaks.
3. **Color is optional decoration.** ANSI escapes are emitted only when stdout is a TTY.
   Piped output (`| less`, `> file.txt`) is always plain text.
4. **Thin API, zero magic.** The package exposes a handful of functions, not a framework.
   Commands remain in control of what they print and when.
5. **Testable without a terminal.** All functions accept `io.Writer`, making them unit-testable
   with `bytes.Buffer`.

## Non-Goals

- Interactive TUI (scrolling, selection, live updates) — use `serve` for that.
- Box-drawing borders or fancy Unicode frames — visual noise for audit files.
- Automatic terminal width detection — tabwriter handles elastic alignment naturally.

## API Surface

### `render.Table(w io.Writer, headers []string, rows [][]string)`

Renders an aligned table via `text/tabwriter`. Behavior:
- Prints a header row, a separator line (`---`), then data rows.
- If `headers` is nil/empty, skips the header.
- If `rows` is nil/empty, prints nothing (no error).
- On internal panic: recovers and dumps rows as tab-separated lines via `fmt.Fprintf`.

### `render.TableWithOpts(w io.Writer, opts TableOpts)`

Extended version with options:

```go
type TableOpts struct {
    Headers []string
    Rows    [][]string
    Sep     string // column separator, default "\t"
    Align   []Alignment // per-column alignment (Left/Right), default all Left
}

type Alignment int
const (
    AlignLeft Alignment = iota
    AlignRight
)
```

### Formatters

```go
func Pct(v float64) string           // "+12.34%" or "-4.10%" (2 decimal places)
func Currency(v float64, sym string) string  // "$1,234.56" or "₹1,234.56"
func Change(v float64) string         // Green "+12.34%" or Red "-4.10%" (when TTY)
func Sparkline(values []float64) string // "▁▂▃▅▇" mini-chart (8 Unicode blocks)
```

### Color Helpers

```go
func Green(s string) string   // ANSI green if TTY, plain otherwise
func Red(s string) string     // ANSI red if TTY, plain otherwise
func Bold(s string) string    // ANSI bold if TTY, plain otherwise
func Dim(s string) string     // ANSI dim if TTY, plain otherwise
```

Color is determined once at package init via `IsTTY()` check on stdout.
Can be overridden: `render.ForceColor(true|false)` for testing or `--color` flags.

### `render.Section(w io.Writer, title string)`

Prints a section header:
```
═══ Title ═══
```
Uses `═` when TTY, `===` when piped.

### `render.KV(w io.Writer, pairs []KVPair)`

Key-value list:
```go
type KVPair struct { Key, Value string }
```
Output:
```
  Portfolio:  us_quality_momentum
  Holdings:   20
  As-of:      2026-01-15
```
Right-pads keys to align values.

## Guarantees

| Scenario | Behavior |
|----------|----------|
| `rows` is nil | No output, no error |
| A row has fewer columns than headers | Pads with empty strings |
| A row has more columns than headers | Extra columns rendered (no truncation) |
| tabwriter panics (hypothetical) | Recover → fallback to `fmt.Fprintf` tab-separated |
| stdout is not a TTY | No ANSI codes emitted |
| `TERM=dumb` | No ANSI codes emitted |
| `NO_COLOR` env set | No ANSI codes emitted (https://no-color.org) |
| `FORCE_COLOR` env set | ANSI codes emitted regardless of TTY |

## Dependencies

- `text/tabwriter` — stdlib
- `os` — for `Stat` on stdout (isatty check)
- `golang.org/x/sys/unix` — **only** if we want a robust `isatty` on all platforms.
  Alternative: use `os.Stdout.Stat()` mode check (covers 99% of cases without external dep).

Decision: Start with `os.Stdout.Stat()` approach (zero external deps). If we find edge cases
where it misdetects (e.g., mintty on Windows), add `x/sys` later.

## Migration Plan

### Phase 1: Build `pkg/render/` (this work)
- Implement core API
- Table-driven tests covering edge cases and fallback behavior

### Phase 2: Migrate one command as proof
- Convert `cmd/performance` (simplest table output) to use `render.Table`
- Verify output is identical (modulo alignment improvement)

### Phase 3: Roll out to remaining commands
- `cmd/monitor` — tables + color for health scores
- `cmd/report` — tables written to file (no color)
- `cmd/basket` — order proposals
- `cmd/backtest` — metrics + equity curve sparkline
- `cmd/stockpicker` — scored candidate tables

### Phase 4: Remove ad-hoc formatting
- Delete hand-crafted `fmt.Printf` column-width code
- Remove `strings.Repeat("-", N)` separators
- Single source of truth for table rendering

## File Layout

```
pkg/render/
├── render.go       # Package doc, shared types (Alignment, KVPair, TableOpts)
├── table.go        # Table, TableWithOpts, Section, KV
├── format.go       # Pct, Currency, Change, Sparkline
├── color.go        # Green, Red, Bold, Dim, IsTTY, ForceColor
└── render_test.go  # Table-driven tests
```
