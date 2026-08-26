package render

import (
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
)

// Table renders an aligned table to w with the given headers and rows.
// If headers is empty, no header or separator is printed.
// If rows is empty, nothing is printed.
// On any internal panic, falls back to raw tab-separated output.
func Table(w io.Writer, headers []string, rows [][]string) {
	TableWithOpts(w, TableOpts{
		Headers: headers,
		Rows:    rows,
	})
}

// TableWithOpts renders a table with extended options.
// Guarantees: never panics outward, never silently drops rows.
func TableWithOpts(w io.Writer, opts TableOpts) {
	if len(opts.Rows) == 0 && len(opts.Headers) == 0 {
		return
	}

	defer func() {
		if r := recover(); r != nil {
			// Fallback: dump everything as tab-separated text.
			fallbackDump(w, opts.Headers, opts.Rows)
		}
	}()

	tw := tabwriter.NewWriter(w, 2, 4, 2, ' ', 0)

	// Header
	if len(opts.Headers) > 0 {
		fmt.Fprintln(tw, strings.Join(opts.Headers, "\t"))
		// Separator: dashes under each header
		seps := make([]string, len(opts.Headers))
		for i, h := range opts.Headers {
			seps[i] = strings.Repeat("-", max(len(h), 3))
		}
		fmt.Fprintln(tw, strings.Join(seps, "\t"))
	}

	// Rows
	numCols := len(opts.Headers)
	for _, row := range opts.Rows {
		padded := padRow(row, numCols)
		fmt.Fprintln(tw, strings.Join(padded, "\t"))
	}

	tw.Flush()
}

// Section prints a section header to w.
func Section(w io.Writer, title string) {
	fmt.Fprintln(w, sectionLine(title))
}

// KV prints a key-value list with right-padded keys for alignment.
func KV(w io.Writer, pairs []KVPair) {
	if len(pairs) == 0 {
		return
	}

	// Find max key width (including the colon we'll append).
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
// This is the last-resort output when the tabwriter path fails.
func fallbackDump(w io.Writer, headers []string, rows [][]string) {
	if len(headers) > 0 {
		fmt.Fprintln(w, strings.Join(headers, "\t"))
	}
	for _, row := range rows {
		fmt.Fprintln(w, strings.Join(row, "\t"))
	}
}
