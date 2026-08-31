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
- Automatic terminal width detection — tabwriter handles elastic alignment naturally.

(Note: pipe-bordered tables — originally a non-goal — were added as `BorderPipe` when the dense
financial reports in `pkg/printer` were migrated onto `render`.)

## Architecture: interface-first

`render` is the **single reporting layer** for all terminal output. It is interface-first so the
concrete rendering engine (today `text/tabwriter` + ANSI) can be swapped for a specialized
table/TUI library later without touching any call site.

```go
// The swappable structural surface. New(w) returns the default implementation.
type Renderer interface {
    Section(title string)   // light header "═══ Title ═══"
    Banner(title string)    // heavy centered title block
    KV(pairs []KVPair)      // aligned key/value block
    Table(opts TableOpts)   // table with optional footer/border/alignment
    Writer() io.Writer      // for interleaving plain text
}

func New(w io.Writer) Renderer // default tabwriter+ANSI renderer
```

Package-level convenience wrappers (`Table`, `TableWithOpts`, `Section`, `Banner`, `KV`) delegate
to a default renderer over the given writer, keeping one-shot usage ergonomic. Pure value
formatters (`Pct`, `Currency`, `PnL`, ...) are not rendering strategy and stay package-level.

**Layering standard**: domain code renders through `render`. Domain-specific composite reports
(holdings snapshot, basket preview) live in `pkg/printer`, which composes `render` primitives —
`cmd`/`executor` → `printer` → `render`. No package hand-rolls padding or table strings.

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
    Footer  []string    // optional summary/total row (ruled off), same column count
    Align   []Alignment // per-column alignment (Left/Right), default all Left
    Border  BorderStyle // BorderNone (tabwriter whitespace) or BorderPipe (" | " + rules)
}

type Alignment int
const ( AlignLeft Alignment = iota; AlignRight )

type BorderStyle int
const ( BorderNone BorderStyle = iota; BorderPipe )
```

`BorderPipe` computes rune-based column widths (multibyte symbols like `₹`/`€` align correctly)
and is used for the dense financial reports (holdings, basket, weight comparisons).

### Formatters

```go
func Pct(v float64) string            // ratio → "+12.34%" (signed, 2dp)
func PctRaw(v float64) string         // already-% → "+12.34%"
func Currency(v float64, sym string) string  // "$1,234.56", "₹1,234.56", "Rs. 1,234.56"
func PnL(v float64, sym string) string        // signed currency: "+$1,234.50" / "-₹500.00" / "$0.00"
func PnLPct(v float64) string          // signed pct, zero unsigned: "+6.47%" / "-5.33%" / "0.00%"
func Change(v float64) string          // Green "+12.34%" / Red "-4.10%" (when TTY)
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

Prints a light section header:
```
═══ Title ═══
```
Uses `═` when TTY, `===` when piped.

### `render.Banner(w io.Writer, title string)`

Prints a heavy, full-width (68-char) centered title block — used to open a dense report:
```
════════════════════════════════════════════════════════════════════
                          Holdings Snapshot
════════════════════════════════════════════════════════════════════
```
Uses `═` when TTY, `=` when piped.

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

## Migration Status — ✅ COMPLETE (R12.5)

The full adoption is done. `render` is now imported and used by every command that produces
report output, and `pkg/printer` was rebuilt on top of it.

- **`cmd/*`** — pipeline show/history/diff, holdings, tax, performance, report, backtest,
  monitor, optimize, cache, daemon, auth, autopilot, basket, pipeline all render through
  `render` (Banner/Section/KV/Table). Progress/status chatter stays on `fmt` (that is logging
  territory, tracked under R14, not reporting).
- **`pkg/printer`** — holdings snapshot + basket preview rebuilt as `render` compositions; all
  hand-rolled `PadString`/pipe-table/`FormatPnL` code deleted.
- **`pkg/executor`** — IP-whitelist banner via `render.Banner`.
- **Removed** — every `strings.Repeat("-", N)` separator and `====` banner in `cmd/` and
  `pkg/executor/`.
- **`pkg/server`** — unchanged: it emits JSON/HTML/SSE to HTTP writers, not terminal reports.

## File Layout

```
pkg/render/
├── render.go       # Package doc, Renderer interface, New(), shared types (Alignment, BorderStyle, KVPair, TableOpts)
├── table.go        # textRenderer impl + wrappers: Table, TableWithOpts, Section, Banner, KV
├── format.go       # Pct, PctRaw, Currency, PnL, PnLPct, Change, Sparkline
├── color.go        # Green, Red, Bold, Dim, IsTTY, ForceColor
└── render_test.go  # Table-driven tests
```
