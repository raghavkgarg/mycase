package rawcapture

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

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
	// Fields are separated by "__": source, endpoint, symbol, stamp.
	parts := strings.Split(strings.TrimSuffix(got, ".json"), "__")
	if len(parts) != 4 {
		t.Fatalf("unexpected filename shape: %q", got)
	}
	if len(parts[2]) != maxSymbolLen {
		t.Errorf("symbol segment len = %d, want %d", len(parts[2]), maxSymbolLen)
	}
}

func TestCapture_Disabled_PassThrough(t *testing.T) {
	t.Setenv(captureEnv, "")
	t.Setenv(dataDirEnv, t.TempDir())
	resetEnabled()

	orig := io.NopCloser(strings.NewReader("hello"))
	got := Capture("yahoo", "quotes", "AAPL", orig)

	// When disabled, the exact same ReadCloser is returned (no buffering).
	body, err := io.ReadAll(got)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(body) != "hello" {
		t.Errorf("body = %q, want %q", body, "hello")
	}

	// Nothing should have been written under the data dir.
	rawDir := filepath.Join(os.Getenv(dataDirEnv), rawSubdir)
	if _, err := os.Stat(rawDir); !os.IsNotExist(err) {
		t.Errorf("expected no raw dir when disabled, stat err = %v", err)
	}
}

func TestCapture_Enabled_WritesAndBodyReadable(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv(captureEnv, "1")
	t.Setenv(dataDirEnv, dataDir)
	resetEnabled()

	const payload = `{"ok":true}`
	got := Capture("schwab", "instruments", "AAPL", io.NopCloser(strings.NewReader(payload)))

	// Caller can still fully read the body.
	body, err := io.ReadAll(got)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(body) != payload {
		t.Errorf("body = %q, want %q", body, payload)
	}

	// A file was archived flat under data/raw/ with the right prefix.
	rawDir := filepath.Join(dataDir, rawSubdir)
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

	// Archived content matches the payload.
	archived, err := os.ReadFile(filepath.Join(rawDir, name))
	if err != nil {
		t.Fatalf("read archived file: %v", err)
	}
	if string(archived) != payload {
		t.Errorf("archived = %q, want %q", archived, payload)
	}
}

func TestCapture_NilBody(t *testing.T) {
	t.Setenv(captureEnv, "1")
	resetEnabled()
	if got := Capture("yahoo", "quotes", "AAPL", nil); got != nil {
		t.Errorf("Capture(nil) = %v, want nil", got)
	}
}

func TestTruthy(t *testing.T) {
	on := []string{"1", "true", "TRUE", "Yes", " on "}
	off := []string{"", "0", "false", "no", "off", "maybe"}
	for _, v := range on {
		if !truthy(v) {
			t.Errorf("truthy(%q) = false, want true", v)
		}
	}
	for _, v := range off {
		if truthy(v) {
			t.Errorf("truthy(%q) = true, want false", v)
		}
	}
}

// writeFixture drops a raw archive file with the given name and content under
// <dataDir>/raw, creating the dir. Returns the raw dir.
func writeFixture(t *testing.T, dataDir, name, content string) string {
	t.Helper()
	rawDir := filepath.Join(dataDir, rawSubdir)
	if err := os.MkdirAll(rawDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(rawDir, name), []byte(content), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	return rawDir
}

func TestReplay_Disabled_Miss(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv(replayEnv, "")
	t.Setenv(dataDirEnv, dataDir)
	resetEnabled()
	writeFixture(t, dataDir, "schwab__quotes__AAPL__20260915-130405.json", `{"x":1}`)

	if _, ok := Replay("schwab", "quotes", "AAPL"); ok {
		t.Error("Replay returned a hit when disabled")
	}
}

func TestReplay_Hit(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv(replayEnv, "1")
	t.Setenv(dataDirEnv, dataDir)
	resetEnabled()
	const want = `{"symbol":"AAPL"}`
	writeFixture(t, dataDir, "schwab__quotes__AAPL__20260915-130405.json", want)

	rc, ok := Replay("schwab", "quotes", "AAPL")
	if !ok {
		t.Fatal("expected replay hit")
	}
	got, _ := io.ReadAll(rc)
	if string(got) != want {
		t.Errorf("body = %q, want %q", got, want)
	}
}

func TestReplay_NewestWins(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv(replayEnv, "1")
	t.Setenv(dataDirEnv, dataDir)
	resetEnabled()
	writeFixture(t, dataDir, "yahoo__quoteSummary__AAPL__20260915-090000.json", `"old"`)
	writeFixture(t, dataDir, "yahoo__quoteSummary__AAPL__20260915-170000.json", `"new"`)

	rc, ok := Replay("yahoo", "quoteSummary", "AAPL")
	if !ok {
		t.Fatal("expected replay hit")
	}
	got, _ := io.ReadAll(rc)
	if string(got) != `"new"` {
		t.Errorf("body = %q, want newest %q", got, `"new"`)
	}
}

func TestReplay_SymbolMiss(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv(replayEnv, "1")
	t.Setenv(dataDirEnv, dataDir)
	resetEnabled()
	writeFixture(t, dataDir, "schwab__quotes__AAPL__20260915-130405.json", `{"x":1}`)

	if _, ok := Replay("schwab", "quotes", "MSFT"); ok {
		t.Error("expected miss for a non-archived symbol")
	}
}

// A symbol-less request must not match a symbol-carrying archive file (prefix
// collision guard).
func TestReplay_SymbollessDoesNotMatchSymbolFile(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv(replayEnv, "1")
	t.Setenv(dataDirEnv, dataDir)
	resetEnabled()
	writeFixture(t, dataDir, "schwab__quotes__AAPL__20260915-130405.json", `{"x":1}`)

	if _, ok := Replay("schwab", "quotes", ""); ok {
		t.Error("symbol-less replay must not match a symbol-carrying file")
	}

	// But it should match a genuinely symbol-less archive file.
	writeFixture(t, dataDir, "schwab__accounts__20260915-130405.json", `[{"h":"x"}]`)
	if _, ok := Replay("schwab", "accounts", ""); !ok {
		t.Error("expected hit for symbol-less archive file")
	}
}
