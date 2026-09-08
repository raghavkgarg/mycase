package render

import (
	"bytes"
	"strings"
	"testing"
)

func TestTable_Basic(t *testing.T) {
	var buf bytes.Buffer
	headers := []string{"Ticker", "Score", "Return"}
	rows := [][]string{
		{"AAPL", "85.2", "+12.3%"},
		{"MSFT", "79.1", "+8.7%"},
		{"GOOG", "72.4", "-2.1%"},
	}

	Table(&buf, headers, rows)
	out := buf.String()

	// Verify headers present.
	if !strings.Contains(out, "Ticker") {
		t.Error("expected header 'Ticker' in output")
	}
	if !strings.Contains(out, "Score") {
		t.Error("expected header 'Score' in output")
	}

	// Verify all rows present.
	for _, row := range rows {
		for _, cell := range row {
			if !strings.Contains(out, cell) {
				t.Errorf("expected cell %q in output", cell)
			}
		}
	}

	// Verify separator exists.
	if !strings.Contains(out, "---") {
		t.Error("expected separator line")
	}
}

func TestTable_NilRows(t *testing.T) {
	var buf bytes.Buffer
	Table(&buf, []string{"A", "B"}, nil)
	// Headers only, no rows — still prints headers.
	out := buf.String()
	if !strings.Contains(out, "A") {
		t.Error("expected header even with nil rows")
	}
}

func TestTable_NilEverything(t *testing.T) {
	var buf bytes.Buffer
	Table(&buf, nil, nil)
	if buf.Len() != 0 {
		t.Errorf("expected no output for nil headers+rows, got %q", buf.String())
	}
}

func TestTable_EmptyEverything(t *testing.T) {
	var buf bytes.Buffer
	Table(&buf, []string{}, [][]string{})
	if buf.Len() != 0 {
		t.Errorf("expected no output for empty headers+rows, got %q", buf.String())
	}
}

func TestTable_ShortRow(t *testing.T) {
	var buf bytes.Buffer
	headers := []string{"A", "B", "C"}
	rows := [][]string{
		{"x"}, // shorter than headers
	}

	Table(&buf, headers, rows)
	out := buf.String()

	// Should not panic; row should still appear.
	if !strings.Contains(out, "x") {
		t.Error("short row should still render")
	}
}

func TestTable_LongRow(t *testing.T) {
	var buf bytes.Buffer
	headers := []string{"A", "B"}
	rows := [][]string{
		{"x", "y", "z"}, // more columns than headers
	}

	Table(&buf, headers, rows)
	out := buf.String()

	// Extra column should still appear (no truncation).
	if !strings.Contains(out, "z") {
		t.Error("extra column should not be truncated")
	}
}

func TestTable_NoHeaders(t *testing.T) {
	var buf bytes.Buffer
	rows := [][]string{
		{"one", "two"},
		{"three", "four"},
	}

	Table(&buf, nil, rows)
	out := buf.String()

	if strings.Contains(out, "---") {
		t.Error("should have no separator when no headers")
	}
	if !strings.Contains(out, "one") || !strings.Contains(out, "four") {
		t.Error("rows should still render without headers")
	}
}

func TestSection(t *testing.T) {
	ForceColor(false)
	defer ForceColor(false)

	var buf bytes.Buffer
	Section(&buf, "Portfolio")
	out := buf.String()

	if !strings.Contains(out, "Portfolio") {
		t.Error("section should contain title")
	}
	if !strings.Contains(out, "===") {
		t.Error("section should use === when not TTY")
	}
}

func TestSection_TTY(t *testing.T) {
	ForceColor(true)
	defer ForceColor(false)

	var buf bytes.Buffer
	Section(&buf, "Holdings")
	out := buf.String()

	if !strings.Contains(out, "═") {
		t.Error("section should use ═ when TTY")
	}
}

func TestKV(t *testing.T) {
	var buf bytes.Buffer
	pairs := []KVPair{
		{"Portfolio", "us_quality_momentum"},
		{"Holdings", "20"},
		{"As-of", "2026-01-15"},
	}

	KV(&buf, pairs)
	out := buf.String()

	if !strings.Contains(out, "Portfolio:") {
		t.Error("expected 'Portfolio:' in output")
	}
	if !strings.Contains(out, "us_quality_momentum") {
		t.Error("expected value in output")
	}
	// Keys should be aligned — all values should start at the same column.
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines, got %d", len(lines))
	}
	// The format is "  key:  value" where key is padded to max key width.
	// Find the value start: after "  " + padded_label + "  ".
	// With max key "Portfolio:" (10 chars), all labels are padded to 10.
	// So value always starts at 2 (indent) + 10 (label) + 2 (gap) = 14.
	for i, line := range lines {
		// Find the value — it's the non-space content after the padded label area.
		// We know the format: "  %-10s  %s" for this test case.
		// Just verify the value content is present and lines have same structure.
		trimmed := strings.TrimLeft(line, " ")
		found := strings.Contains(trimmed, ":")
		if !found {
			t.Errorf("line %d missing colon: %q", i, line)
		}
	}
	// Verify values are at the same column by checking line lengths correlate with value lengths.
	// "Portfolio:" (10) + "us_quality_momentum" (19) vs "Holdings:" (9→10) + "20" (2)
	// Both should have value starting at same offset.
	valStart0 := strings.Index(lines[0], "us_quality_momentum")
	valStart1 := strings.Index(lines[1], "20")
	valStart2 := strings.Index(lines[2], "2026-01-15")
	if valStart0 != valStart1 || valStart0 != valStart2 {
		t.Errorf("values not aligned: starts at %d, %d, %d\nOutput:\n%s",
			valStart0, valStart1, valStart2, out)
	}
}

func TestKV_Empty(t *testing.T) {
	var buf bytes.Buffer
	KV(&buf, nil)
	if buf.Len() != 0 {
		t.Error("expected no output for nil pairs")
	}
}

// --- Formatter tests ---

func TestPctRaw(t *testing.T) {
	tests := []struct {
		in   float64
		want string
	}{
		{12.34, "+12.34%"},
		{-4.10, "-4.10%"},
		{0.0, "+0.00%"},
	}
	for _, tt := range tests {
		got := PctRaw(tt.in)
		if got != tt.want {
			t.Errorf("PctRaw(%v) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestCurrency(t *testing.T) {
	tests := []struct {
		v    float64
		sym  string
		want string
	}{
		{1234.5, "$", "$1,234.50"},
		{1234567.89, "$", "$1,234,567.89"},
		{0.99, "₹", "₹0.99"},
		{-500, "$", "-$500.00"},
		{0, "$", "$0.00"},
		{999.999, "$", "$1,000.00"},
		{100, "€", "€100.00"},
	}
	for _, tt := range tests {
		got := Currency(tt.v, tt.sym)
		if got != tt.want {
			t.Errorf("Currency(%v, %q) = %q, want %q", tt.v, tt.sym, got, tt.want)
		}
	}
}

// --- Thousands separator ---

func TestAddThousandsSep(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"0", "0"},
		{"123", "123"},
		{"1234", "1,234"},
		{"12345", "12,345"},
		{"123456", "123,456"},
		{"1234567", "1,234,567"},
	}
	for _, tt := range tests {
		got := addThousandsSep(tt.in)
		if got != tt.want {
			t.Errorf("addThousandsSep(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

// --- New capabilities: Banner, footer, pipe border, PnL ---

func TestBanner_NoColor(t *testing.T) {
	ForceColor(false)
	defer ForceColor(false)

	var buf bytes.Buffer
	Banner(&buf, "Holdings Snapshot")
	out := buf.String()

	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("banner should be 3 lines, got %d: %q", len(lines), out)
	}
	if !strings.Contains(lines[1], "Holdings Snapshot") {
		t.Errorf("middle line should contain title, got %q", lines[1])
	}
	// Top and bottom rules should be equal-length "=" bars (no-color).
	if lines[0] != lines[2] {
		t.Errorf("top/bottom rules differ:\n%q\n%q", lines[0], lines[2])
	}
	if !strings.HasPrefix(lines[0], "===") {
		t.Errorf("rule should be = chars when not TTY, got %q", lines[0])
	}
	// Title should be roughly centered: some leading space.
	if !strings.HasPrefix(lines[1], "  ") {
		t.Errorf("title should be centered (leading space), got %q", lines[1])
	}
}

func TestTable_Footer(t *testing.T) {
	var buf bytes.Buffer
	TableWithOpts(&buf, TableOpts{
		Headers: []string{"Ticker", "Weight"},
		Rows:    [][]string{{"AAPL", "0.5000"}, {"MSFT", "0.5000"}},
		Footer:  []string{"Total", "1.0000"},
	})
	out := buf.String()
	if !strings.Contains(out, "Total") {
		t.Error("footer row should render")
	}
	if !strings.Contains(out, "1.0000") {
		t.Error("footer value should render")
	}
}

func TestTable_PipeBorder(t *testing.T) {
	var buf bytes.Buffer
	TableWithOpts(&buf, TableOpts{
		Headers: []string{"Symbol", "Qty", "Value"},
		Rows: [][]string{
			{"AAPL", "10", "₹1,500.00"},
			{"MSFT", "5", "₹2,000.00"},
		},
		Footer: []string{"Total", "15", "₹3,500.00"},
		Align:  []Alignment{AlignLeft, AlignRight, AlignRight},
		Border: BorderPipe,
	})
	out := buf.String()

	if !strings.Contains(out, " | ") {
		t.Error("pipe border should separate columns with ' | '")
	}
	// Header, rule, 2 rows, rule, footer = 6 lines.
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 6 {
		t.Fatalf("expected 6 lines (header,rule,2 rows,rule,footer), got %d:\n%s", len(lines), out)
	}
	// Columns must align: every non-rule line has the same display width.
	want := len([]rune(lines[0]))
	for i, ln := range lines {
		if strings.Trim(ln, "-") == "" {
			continue // rule line
		}
		if got := len([]rune(ln)); got != want {
			t.Errorf("line %d width %d != header width %d:\n%q", i, got, want, ln)
		}
	}
	// ₹ (multibyte) should not corrupt alignment — value column right-aligned.
	if !strings.Contains(out, "₹1,500.00") {
		t.Error("multibyte currency cell should survive")
	}
}

func TestPnL(t *testing.T) {
	tests := []struct {
		v    float64
		sym  string
		want string
	}{
		{1234.5, "$", "+$1,234.50"},
		{-500, "₹", "-₹500.00"},
		{0, "$", "$0.00"},
		{1000000, "$", "+$1,000,000.00"},
	}
	for _, tt := range tests {
		if got := PnL(tt.v, tt.sym); got != tt.want {
			t.Errorf("PnL(%v, %q) = %q, want %q", tt.v, tt.sym, got, tt.want)
		}
	}
}

func TestPnLPct(t *testing.T) {
	tests := []struct {
		in   float64
		want string
	}{
		{12.34, "+12.34%"},
		{-4.1, "-4.10%"},
		{0, "0.00%"},
	}
	for _, tt := range tests {
		if got := PnLPct(tt.in); got != tt.want {
			t.Errorf("PnLPct(%v) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestRenderer_Interface(t *testing.T) {
	ForceColor(false)
	defer ForceColor(false)

	var buf bytes.Buffer
	r := New(&buf)
	r.Banner("Report")
	r.Section("Holdings")
	r.KV([]KVPair{{"Portfolio", "sp500"}})
	r.Table(TableOpts{Headers: []string{"A", "B"}, Rows: [][]string{{"1", "2"}}})
	if r.Writer() != &buf {
		t.Error("Writer() should return the underlying writer")
	}
	out := buf.String()
	for _, want := range []string{"Report", "Holdings", "Portfolio:", "sp500", "A", "B", "1", "2"} {
		if !strings.Contains(out, want) {
			t.Errorf("renderer output missing %q", want)
		}
	}
}
