package rawstore

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/raghavkgarg/mycase/pkg/rawcapture"
)

// Store must satisfy the rawcapture.Sink contract it is injected as.
var _ rawcapture.Sink = (*Store)(nil)

func TestFilename(t *testing.T) {
	ts := time.Date(2026, 9, 15, 13, 4, 5, 0, time.UTC)
	tests := []struct {
		name     string
		source   string
		endpoint string
		symbol   string
		want     string
	}{
		{"with symbol", "schwab", "quotes", "AAPL", "schwab__quotes__AAPL__20260915-130405.json"},
		{"no symbol", "schwab", "accounts", "", "schwab__accounts__20260915-130405.json"},
		{"sanitizes slashes", "yahoo", "market/data", "US:AAPL", "yahoo__market_data__US_AAPL__20260915-130405.json"},
		{"empty endpoint falls back", "yahoo", "", "AAPL", "yahoo__endpoint__AAPL__20260915-130405.json"},
		{"empty source falls back", "", "quotes", "AAPL", "unknown__quotes__AAPL__20260915-130405.json"},
		{"multi-symbol query", "schwab", "quotes", "A,B,C", "schwab__quotes__A_B_C__20260915-130405.json"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := Filename(tc.source, tc.endpoint, tc.symbol, ts); got != tc.want {
				t.Errorf("Filename(%q,%q,%q) = %q, want %q", tc.source, tc.endpoint, tc.symbol, got, tc.want)
			}
		})
	}
}

func TestFilename_SymbolLengthBound(t *testing.T) {
	ts := time.Date(2026, 9, 15, 0, 0, 0, 0, time.UTC)
	long := strings.Repeat("A", 200)
	got := Filename("schwab", "quotes", long, ts)
	parts := strings.Split(strings.TrimSuffix(got, ".json"), "__")
	if len(parts) != 4 {
		t.Fatalf("unexpected filename shape: %q", got)
	}
	if len(parts[2]) != maxSymbolLen {
		t.Errorf("symbol segment len = %d, want %d", len(parts[2]), maxSymbolLen)
	}
}

func TestNew_EmptyBase(t *testing.T) {
	if s := New("", "req-1"); s != nil {
		t.Error("New with empty base should return nil")
	}
}

func TestWrite_And_Layout(t *testing.T) {
	base := t.TempDir()
	s := New(base, "req-1")

	const payload = `{"ok":true}`
	s.Write("schwab", "instruments", "AAPL", []byte(payload))

	rawDir := filepath.Join(base, rawSubdir)
	entries, err := os.ReadDir(rawDir)
	if err != nil {
		t.Fatalf("read archive dir: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 archived file, got %d", len(entries))
	}
	name := entries[0].Name()
	if !strings.HasPrefix(name, "schwab__instruments__AAPL__") || !strings.HasSuffix(name, ".json") {
		t.Errorf("unexpected archive filename: %q", name)
	}
	archived, err := os.ReadFile(filepath.Join(rawDir, name))
	if err != nil {
		t.Fatalf("read archived file: %v", err)
	}
	if string(archived) != payload {
		t.Errorf("archived = %q, want %q", archived, payload)
	}
}

func TestWrite_NilStore(t *testing.T) {
	var s *Store
	// Must not panic.
	s.Write("schwab", "quotes", "AAPL", []byte(`{}`))
}

// writeFixture drops a raw archive file with the given name and content under
// <base>/raw, creating the dir.
func writeFixture(t *testing.T, base, name, content string) {
	t.Helper()
	rawDir := filepath.Join(base, rawSubdir)
	if err := os.MkdirAll(rawDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(rawDir, name), []byte(content), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
}

func TestOpen_NilStore_Miss(t *testing.T) {
	var s *Store
	if _, ok := s.Open("schwab", "quotes", "AAPL"); ok {
		t.Error("nil store Open should miss")
	}
}

func TestOpen_Hit(t *testing.T) {
	base := t.TempDir()
	const want = `{"symbol":"AAPL"}`
	writeFixture(t, base, "schwab__quotes__AAPL__20260915-130405.json", want)

	s := New(base, "req-1")
	rc, ok := s.Open("schwab", "quotes", "AAPL")
	if !ok {
		t.Fatal("expected hit")
	}
	got, _ := io.ReadAll(rc)
	if string(got) != want {
		t.Errorf("body = %q, want %q", got, want)
	}
}

func TestOpen_NewestWins(t *testing.T) {
	base := t.TempDir()
	writeFixture(t, base, "yahoo__quoteSummary__AAPL__20260915-090000.json", `"old"`)
	writeFixture(t, base, "yahoo__quoteSummary__AAPL__20260915-170000.json", `"new"`)

	s := New(base, "req-1")
	rc, ok := s.Open("yahoo", "quoteSummary", "AAPL")
	if !ok {
		t.Fatal("expected hit")
	}
	got, _ := io.ReadAll(rc)
	if string(got) != `"new"` {
		t.Errorf("body = %q, want newest %q", got, `"new"`)
	}
}

func TestOpen_SymbolMiss(t *testing.T) {
	base := t.TempDir()
	writeFixture(t, base, "schwab__quotes__AAPL__20260915-130405.json", `{"x":1}`)

	s := New(base, "req-1")
	if _, ok := s.Open("schwab", "quotes", "MSFT"); ok {
		t.Error("expected miss for a non-archived symbol")
	}
}

// A symbol-less request must not match a symbol-carrying archive file (prefix
// collision guard), but must match a genuinely symbol-less file.
func TestOpen_SymbollessDoesNotMatchSymbolFile(t *testing.T) {
	base := t.TempDir()
	writeFixture(t, base, "schwab__quotes__AAPL__20260915-130405.json", `{"x":1}`)

	s := New(base, "req-1")
	if _, ok := s.Open("schwab", "quotes", ""); ok {
		t.Error("symbol-less replay must not match a symbol-carrying file")
	}

	writeFixture(t, base, "schwab__accounts__20260915-130405.json", `[{"h":"x"}]`)
	if _, ok := s.Open("schwab", "accounts", ""); !ok {
		t.Error("expected hit for symbol-less archive file")
	}
}
