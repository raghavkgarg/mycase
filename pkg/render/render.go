// Package render is the single reporting layer for all mycase terminal output.
//
// It is interface-first: callers render through the Renderer interface (or the
// package-level convenience wrappers), never against a concrete rendering
// engine. The current implementation is built on text/tabwriter with optional
// ANSI color, but nothing in the public surface exposes that — a specialized
// table/TUI library can later be dropped in behind the same interface without
// touching any call site.
//
// Two kinds of things live here:
//   - Structural rendering (Section, Banner, KV, Table) — the swappable surface,
//     expressed by the Renderer interface.
//   - Value formatting (Pct, Currency, PnL, Change, Sparkline) — pure
//     string helpers that are not rendering strategy and stay package-level.
//
// Guarantees: output is never silently lost (Table falls back to raw
// tab-separated text on internal failure); color is emitted only to a TTY and
// respects NO_COLOR (https://no-color.org).
//
// This package is for user-facing *reports* — the product a user reads. It is
// NOT for diagnostic/operational logging, which belongs to log/slog.
package render

import "io"

// Alignment controls column alignment in a table column.
type Alignment int

const (
	AlignLeft Alignment = iota
	AlignRight
)

// BorderStyle selects how table cells are separated.
type BorderStyle int

const (
	// BorderNone uses tabwriter whitespace between columns (default, clean).
	BorderNone BorderStyle = iota
	// BorderPipe separates columns with " | " and rules the header/footer,
	// matching the dense financial-report style used for holdings/basket.
	BorderPipe
)

// TableOpts configures table rendering.
type TableOpts struct {
	Headers []string
	Rows    [][]string
	// Footer, when non-empty, is rendered as a final rule + summary row
	// (e.g. a "Total" line). It must have the same column count as Headers.
	Footer []string
	// Align holds per-column alignment; defaults to AlignLeft for any column
	// not covered.
	Align []Alignment
	// Border selects the column separator style.
	Border BorderStyle
}

// KVPair is a key-value entry for the KV renderer.
type KVPair struct {
	Key   string
	Value string
}

// Renderer is the structural rendering surface. All report structure is emitted
// through this interface so the underlying engine can be swapped wholesale.
type Renderer interface {
	// Section prints a light header ("═══ Title ═══").
	Section(title string)
	// Banner prints a heavy, full-width centered title block — used to open a
	// dense report.
	Banner(title string)
	// KV prints an aligned key/value block.
	KV(pairs []KVPair)
	// Table prints a table per opts (headers, rows, optional footer, alignment,
	// border style).
	Table(opts TableOpts)
	// Writer returns the underlying output target, for interleaving plain text
	// (e.g. fmt.Fprintln) between structured blocks.
	Writer() io.Writer
}

// New returns the default Renderer writing to w. Swap the implementation here
// (and only here) to adopt a different rendering engine later.
func New(w io.Writer) Renderer {
	return &textRenderer{w: w}
}
