// Package render provides a thin CLI rendering layer built on text/tabwriter.
// It produces aligned tables, formatted numbers, and optional ANSI color —
// with a guarantee that output is never silently lost. If the rendering layer
// fails for any reason, it falls back to raw tab-separated text.
//
// Color is emitted only when stdout is a TTY. Piped output is always plain text.
// The NO_COLOR environment variable (https://no-color.org) is respected.
package render

// Alignment controls column alignment in TableWithOpts.
type Alignment int

const (
	AlignLeft Alignment = iota
	AlignRight
)

// TableOpts configures extended table rendering.
type TableOpts struct {
	Headers []string
	Rows    [][]string
	Align   []Alignment // per-column; defaults to AlignLeft if shorter than headers
}

// KVPair is a key-value entry for the KV renderer.
type KVPair struct {
	Key   string
	Value string
}
