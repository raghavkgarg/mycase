package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadHolidays(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "holidays.json")
	content := `{
		"_comment": "ignored",
		"exchanges": {
			"NYSE": ["2026-01-01", "2026-12-25"],
			"NSE": ["2026-01-26"]
		}
	}`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	h := LoadHolidays(path)
	if got := h.For("NYSE"); len(got) != 2 || got[0] != "2026-01-01" || got[1] != "2026-12-25" {
		t.Errorf("NYSE holidays = %v, want [2026-01-01 2026-12-25]", got)
	}
	if got := h.For("NSE"); len(got) != 1 || got[0] != "2026-01-26" {
		t.Errorf("NSE holidays = %v, want [2026-01-26]", got)
	}
	if got := h.For("UNKNOWN"); got != nil {
		t.Errorf("unknown exchange = %v, want nil", got)
	}
}

// A missing or malformed file must degrade to an empty set, never an error.
func TestLoadHolidays_MissingFile(t *testing.T) {
	h := LoadHolidays(filepath.Join(t.TempDir(), "does-not-exist.json"))
	if h.Exchanges == nil {
		t.Fatal("Exchanges map must be non-nil even for a missing file")
	}
	if got := h.For("NYSE"); got != nil {
		t.Errorf("missing file NYSE = %v, want nil", got)
	}
}

// The committed config/holidays.json must parse and carry both exchanges.
func TestLoadHolidays_CommittedFile(t *testing.T) {
	// Locate repo-root config/ from this package dir (pkg/config).
	path := filepath.Join("..", "..", "config", "holidays.json")
	if _, err := os.Stat(path); err != nil {
		t.Skipf("committed holidays.json not found: %v", err)
	}
	h := LoadHolidays(path)
	if len(h.For("NYSE")) == 0 {
		t.Error("committed holidays.json should list NYSE holidays")
	}
	if len(h.For("NSE")) == 0 {
		t.Error("committed holidays.json should list NSE holidays")
	}
}
