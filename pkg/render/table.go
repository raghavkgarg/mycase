package render

import (
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
)

// textRenderer is the default Renderer: text/tabwriter + ANSI color +
// box-drawing rules. It is the only place that knows about the concrete
// rendering engine.
type textRenderer struct {
	w io.Writer
}

func (r *textRenderer) Writer() io.Writer { return r.w }

func (r *textRenderer) Section(title string) {
	fmt.Fprintln(r.w, sectionLine(title))
}

func (r *textRenderer) Banner(title string) {
	writeBanner(r.w, title)
}

func (r *textRenderer) KV(pairs []KVPair) {
	writeKV(r.w, pairs)
}

func (r *textRenderer) Table(opts TableOpts) {
	writeTable(r.w, opts)
}

// --- Package-level convenience wrappers ---
//
// These delegate to the default renderer over the given writer. They keep the
// common one-shot usage ergonomic (no explicit New) and preserve the original
// function-style API.

// Table renders an aligned table to w with the given headers and rows.
// If headers is empty, no header or separator is printed.
// If rows is empty, nothing is printed.
func Table(w io.Writer, headers []string, rows [][]string) {
	writeTable(w, TableOpts{Headers: headers, Rows: rows})
}

// TableWithOpts renders a table with extended options.
// Guarantees: never panics outward, never silently drops rows.
func TableWithOpts(w io.Writer, opts TableOpts) {
	writeTable(w, opts)
}

// Section prints a light section header to w ("═══ Title ═══").
func Section(w io.Writer, title string) {
	fmt.Fprintln(w, sectionLine(title))
}

// Banner prints a heavy, full-width centered title block to w.
func Banner(w io.Writer, title string) {
	writeBanner(w, title)
}

// KV prints a key-value list with right-padded keys for alignment.
func KV(w io.Writer, pairs []KVPair) {
	writeKV(w, pairs)
}

// --- Implementation ---

// bannerWidth is the fixed width of a heavy banner block.
const bannerWidth = 68

func writeBanner(w io.Writer, title string) {
	ch := sectionChar() // "═" on TTY, "=" otherwise
	bar := strings.Repeat(ch, bannerWidth)
	// Center the title within bannerWidth.
	t := strings.TrimSpace(title)
	if len(t) > bannerWidth {
		t = t[:bannerWidth]
	}
	pad := (bannerWidth - len(t)) / 2
	centered := strings.Repeat(" ", max(pad, 0)) + t
	centered += strings.Repeat(" ", max(0, bannerWidth-len(centered)))
	fmt.Fprintln(w, bar)
	fmt.Fprintln(w, centered)
	fmt.Fprintln(w, bar)
}

func writeKV(w io.Writer, pairs []KVPair) {
	if len(pairs) == 0 {
		return
	}
	maxKey := 0
	for _, p := range pairs {
		if len(p.Key)+1 > maxKey {
			maxKey = len(p.Key) + 1 // +1 for ":"
		}
	}
	for _, p := range pairs {
		label := p.Key + ":"
		fmt.Fprintf(w, "  %-*s  %s\n", maxKey, label, p.Value)
	}
}

// writeTable renders per opts. Guarantees: never panics outward, never silently
// drops rows (falls back to raw tab-separated text on internal failure).
func writeTable(w io.Writer, opts TableOpts) {
	if len(opts.Rows) == 0 && len(opts.Headers) == 0 {
		return
	}

	defer func() {
		if r := recover(); r != nil {
			fallbackDump(w, opts.Headers, opts.Rows)
		}
	}()

	if opts.Border == BorderPipe {
		writePipeTable(w, opts)
		return
	}
	writePlainTable(w, opts)
}

// writePlainTable uses tabwriter whitespace columns (the clean default style).
func writePlainTable(w io.Writer, opts TableOpts) {
	tw := tabwriter.NewWriter(w, 2, 4, 2, ' ', 0)
	numCols := len(opts.Headers)

	if len(opts.Headers) > 0 {
		fmt.Fprintln(tw, strings.Join(opts.Headers, "\t"))
		seps := make([]string, len(opts.Headers))
		for i, h := range opts.Headers {
			seps[i] = strings.Repeat("-", max(len(h), 3))
		}
		fmt.Fprintln(tw, strings.Join(seps, "\t"))
	}

	for _, row := range opts.Rows {
		fmt.Fprintln(tw, strings.Join(padRow(row, numCols), "\t"))
	}

	if len(opts.Footer) > 0 {
		seps := make([]string, max(numCols, len(opts.Footer)))
		for i := range seps {
			seps[i] = strings.Repeat("-", 3)
		}
		fmt.Fprintln(tw, strings.Join(seps, "\t"))
		fmt.Fprintln(tw, strings.Join(padRow(opts.Footer, numCols), "\t"))
	}

	tw.Flush()
}

// writePipeTable renders the dense " | "-separated style with ruled
// header/footer, matching the financial-report look.
func writePipeTable(w io.Writer, opts TableOpts) {
	numCols := len(opts.Headers)
	if numCols == 0 {
		for _, row := range opts.Rows {
			numCols = max(numCols, len(row))
		}
	}

	// Compute column widths across headers, rows, and footer.
	widths := make([]int, numCols)
	consider := func(cells []string) {
		for i := 0; i < numCols && i < len(cells); i++ {
			if l := displayWidth(cells[i]); l > widths[i] {
				widths[i] = l
			}
		}
	}
	consider(opts.Headers)
	for _, row := range opts.Rows {
		consider(row)
	}
	consider(opts.Footer)

	line := func(cells []string) string {
		padded := padRow(cells, numCols)
		parts := make([]string, numCols)
		for i := 0; i < numCols; i++ {
			parts[i] = padCell(padded[i], widths[i], alignOf(opts.Align, i))
		}
		return strings.Join(parts, " | ")
	}

	rule := func() string {
		total := 0
		for _, wd := range widths {
			total += wd
		}
		// account for " | " separators (3 chars each)
		total += 3 * max(0, numCols-1)
		return strings.Repeat("-", total)
	}

	if len(opts.Headers) > 0 {
		fmt.Fprintln(w, line(opts.Headers))
		fmt.Fprintln(w, rule())
	}
	for _, row := range opts.Rows {
		fmt.Fprintln(w, line(row))
	}
	if len(opts.Footer) > 0 {
		fmt.Fprintln(w, rule())
		fmt.Fprintln(w, line(opts.Footer))
	}
}

func alignOf(align []Alignment, i int) Alignment {
	if i < len(align) {
		return align[i]
	}
	return AlignLeft
}

// padCell pads s to width per alignment, measured in display runes.
func padCell(s string, width int, a Alignment) string {
	gap := width - displayWidth(s)
	if gap <= 0 {
		return s
	}
	if a == AlignRight {
		return strings.Repeat(" ", gap) + s
	}
	return s + strings.Repeat(" ", gap)
}

// displayWidth counts runes (not bytes) so multibyte symbols like ₹ and € align.
func displayWidth(s string) int {
	return len([]rune(s))
}

// padRow ensures the row has at least n columns, padding with empty strings.
func padRow(row []string, n int) []string {
	if n <= 0 || len(row) >= n {
		return row
	}
	padded := make([]string, n)
	copy(padded, row)
	return padded
}

// fallbackDump writes headers and rows as plain tab-separated lines.
func fallbackDump(w io.Writer, headers []string, rows [][]string) {
	if len(headers) > 0 {
		fmt.Fprintln(w, strings.Join(headers, "\t"))
	}
	for _, row := range rows {
		fmt.Fprintln(w, strings.Join(row, "\t"))
	}
}
