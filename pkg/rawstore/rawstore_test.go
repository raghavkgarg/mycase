package rawstore

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/raghavkgarg/mycase/pkg/config"
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

func TestReqID(t *testing.T) {
	if got := New(t.TempDir(), "req-42").ReqID(); got != "req-42" {
		t.Errorf("ReqID() = %q, want %q", got, "req-42")
	}
	var nilStore *Store
	if got := nilStore.ReqID(); got != "" {
		t.Errorf("nil ReqID() = %q, want empty", got)
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

// setModTime backdates a fixture's modification time so age-based pruning can be
// exercised deterministically.
func setModTime(t *testing.T, base, name string, mod time.Time) {
	t.Helper()
	p := filepath.Join(base, rawSubdir, name)
	if err := os.Chtimes(p, mod, mod); err != nil {
		t.Fatalf("chtimes %s: %v", name, err)
	}
}

func TestPrune_NilStore(t *testing.T) {
	var s *Store
	res, err := s.Prune(Retention{MaxAge: time.Hour})
	if err != nil {
		t.Fatalf("nil Prune err: %v", err)
	}
	if res.Removed() != 0 {
		t.Errorf("nil Prune removed %d, want 0", res.Removed())
	}
}

func TestPrune_MissingDir(t *testing.T) {
	// Store pointed at a base with no raw/ dir yet — nothing captured.
	s := New(t.TempDir(), "req-1")
	res, err := s.Prune(Retention{MaxAge: time.Hour, MaxBytes: 1})
	if err != nil {
		t.Fatalf("missing-dir Prune err: %v", err)
	}
	if res.Removed() != 0 {
		t.Errorf("missing-dir removed %d, want 0", res.Removed())
	}
}

func TestPrune_ByAge(t *testing.T) {
	base := t.TempDir()
	now := time.Now()
	writeFixture(t, base, "schwab__quotes__OLD__20260101-000000.json", `{"a":1}`)
	writeFixture(t, base, "schwab__quotes__NEW__20260901-000000.json", `{"b":2}`)
	setModTime(t, base, "schwab__quotes__OLD__20260101-000000.json", now.Add(-48*time.Hour))
	setModTime(t, base, "schwab__quotes__NEW__20260901-000000.json", now.Add(-1*time.Hour))

	s := New(base, "req-1")
	res, err := s.Prune(Retention{MaxAge: 24 * time.Hour}) // size disabled
	if err != nil {
		t.Fatalf("Prune err: %v", err)
	}
	if res.RemovedByAge != 1 || res.RemovedBySize != 0 {
		t.Errorf("removed byAge=%d bySize=%d, want 1/0", res.RemovedByAge, res.RemovedBySize)
	}
	if res.Remaining != 1 {
		t.Errorf("remaining=%d, want 1", res.Remaining)
	}
	if _, err := os.Stat(filepath.Join(base, rawSubdir, "schwab__quotes__NEW__20260901-000000.json")); err != nil {
		t.Errorf("recent file should survive: %v", err)
	}
	if _, err := os.Stat(filepath.Join(base, rawSubdir, "schwab__quotes__OLD__20260101-000000.json")); !os.IsNotExist(err) {
		t.Errorf("old file should be pruned, stat err=%v", err)
	}
}

func TestPrune_BySize_OldestFirst(t *testing.T) {
	base := t.TempDir()
	now := time.Now()
	// Three 100-byte files; cap at 250 bytes => oldest one must go.
	body := strings.Repeat("x", 100)
	writeFixture(t, base, "yahoo__chart__A__20260901-000000.json", body)
	writeFixture(t, base, "yahoo__chart__B__20260901-000001.json", body)
	writeFixture(t, base, "yahoo__chart__C__20260901-000002.json", body)
	setModTime(t, base, "yahoo__chart__A__20260901-000000.json", now.Add(-3*time.Hour))
	setModTime(t, base, "yahoo__chart__B__20260901-000001.json", now.Add(-2*time.Hour))
	setModTime(t, base, "yahoo__chart__C__20260901-000002.json", now.Add(-1*time.Hour))

	s := New(base, "req-1")
	res, err := s.Prune(Retention{MaxBytes: 250}) // age disabled
	if err != nil {
		t.Fatalf("Prune err: %v", err)
	}
	if res.RemovedBySize != 1 || res.RemovedByAge != 0 {
		t.Errorf("removed byAge=%d bySize=%d, want 0/1", res.RemovedByAge, res.RemovedBySize)
	}
	if res.Remaining != 2 || res.RemainingSize != 200 {
		t.Errorf("remaining=%d size=%d, want 2/200", res.Remaining, res.RemainingSize)
	}
	// The oldest (A) must be the one deleted.
	if _, err := os.Stat(filepath.Join(base, rawSubdir, "yahoo__chart__A__20260901-000000.json")); !os.IsNotExist(err) {
		t.Errorf("oldest file A should be pruned, stat err=%v", err)
	}
}

func TestPrune_IgnoresForeignFiles(t *testing.T) {
	base := t.TempDir()
	writeFixture(t, base, "schwab__quotes__A__20260101-000000.json", `{"a":1}`)
	writeFixture(t, base, "notes.txt", "keep me") // no __, wrong ext
	writeFixture(t, base, "README.md", "keep me") // foreign
	setModTime(t, base, "schwab__quotes__A__20260101-000000.json", time.Now().Add(-100*time.Hour))

	s := New(base, "req-1")
	res, err := s.Prune(Retention{MaxAge: time.Hour})
	if err != nil {
		t.Fatalf("Prune err: %v", err)
	}
	if res.RemovedByAge != 1 {
		t.Errorf("removedByAge=%d, want 1", res.RemovedByAge)
	}
	for _, f := range []string{"notes.txt", "README.md"} {
		if _, err := os.Stat(filepath.Join(base, rawSubdir, f)); err != nil {
			t.Errorf("foreign file %s should be untouched: %v", f, err)
		}
	}
}

func TestPrune_DisabledCeilings(t *testing.T) {
	base := t.TempDir()
	writeFixture(t, base, "schwab__quotes__A__20260101-000000.json", strings.Repeat("x", 1000))
	setModTime(t, base, "schwab__quotes__A__20260101-000000.json", time.Now().Add(-1000*time.Hour))

	s := New(base, "req-1")
	res, err := s.Prune(Retention{MaxAge: 0, MaxBytes: 0}) // both disabled
	if err != nil {
		t.Fatalf("Prune err: %v", err)
	}
	if res.Removed() != 0 {
		t.Errorf("disabled ceilings removed %d, want 0", res.Removed())
	}
}

func TestResolveRetention_Precedence(t *testing.T) {
	// Config layer only.
	cfg := config.RawConfig{RetainDays: 30, MaxSizeMB: 100}
	ret := ResolveRetention(cfg, -1, -1)
	if ret.MaxAge != 30*24*time.Hour {
		t.Errorf("config age = %v, want 30d", ret.MaxAge)
	}
	if ret.MaxBytes != 100*1024*1024 {
		t.Errorf("config size = %d, want 100MB", ret.MaxBytes)
	}

	// Env overrides config.
	t.Setenv(retainDaysEnv, "7")
	t.Setenv(maxSizeMBEnv, "50")
	ret = ResolveRetention(cfg, -1, -1)
	if ret.MaxAge != 7*24*time.Hour {
		t.Errorf("env age = %v, want 7d", ret.MaxAge)
	}
	if ret.MaxBytes != 50*1024*1024 {
		t.Errorf("env size = %d, want 50MB", ret.MaxBytes)
	}

	// Explicit override (flag) beats env, including 0 (disable).
	ret = ResolveRetention(cfg, 0, 200)
	if ret.MaxAge != 0 {
		t.Errorf("override age = %v, want 0 (disabled)", ret.MaxAge)
	}
	if ret.MaxBytes != 200*1024*1024 {
		t.Errorf("override size = %d, want 200MB", ret.MaxBytes)
	}
}

func TestResolveRetention_Defaults(t *testing.T) {
	// Empty config, no env, no overrides → built-in defaults.
	ret := ResolveRetention(config.RawConfig{}, -1, -1)
	if ret.MaxAge != time.Duration(config.DefaultRawRetainDays)*24*time.Hour {
		t.Errorf("default age = %v", ret.MaxAge)
	}
	if ret.MaxBytes != int64(config.DefaultRawMaxSizeMB)*1024*1024 {
		t.Errorf("default size = %d", ret.MaxBytes)
	}
}
