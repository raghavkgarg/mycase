package stockpicker

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestIsOnCooldown(t *testing.T) {
	asOf := time.Date(2026, 9, 7, 10, 0, 0, 0, time.UTC)
	recentExits := map[string]time.Time{
		"NSE:ARVIND": time.Date(2026, 8, 26, 0, 0, 0, 0, time.UTC), // 12 days ago
		"NSE:OLD":    time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),  // > 60 days ago
	}

	// 1. ARVIND at Rank 19 within 30-day window -> On Cooldown
	onCd, reason := IsOnCooldown("NSE:ARVIND", recentExits, 19, 5, asOf, 30)
	if !onCd {
		t.Errorf("expected NSE:ARVIND to be on cooldown, got false")
	}
	if reason == "" {
		t.Errorf("expected reason for cooldown block")
	}

	// 2. ARVIND at Rank 3 (<= bypassRank 5) -> Bypasses cooldown
	onCd, _ = IsOnCooldown("NSE:ARVIND", recentExits, 3, 5, asOf, 30)
	if onCd {
		t.Errorf("expected NSE:ARVIND at rank 3 to bypass cooldown, got true")
	}

	// 3. OLD exited > 30 days ago -> Not on cooldown
	onCd, _ = IsOnCooldown("NSE:OLD", recentExits, 18, 5, asOf, 30)
	if onCd {
		t.Errorf("expected NSE:OLD to not be on cooldown (> 30 days ago)")
	}

	// 4. Unknown ticker -> Not on cooldown
	onCd, _ = IsOnCooldown("NSE:UNKNOWN", recentExits, 10, 5, asOf, 30)
	if onCd {
		t.Errorf("expected unknown ticker to not be on cooldown")
	}
}

func TestLoadRecentExitsMock(t *testing.T) {
	tempDir := t.TempDir()
	bkDir := filepath.Join(tempDir, "data", "backups", "testuniverse")
	if err := os.MkdirAll(bkDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Create backup 1 from 15 days ago with TICKER_A and TICKER_B
	bk1 := filepath.Join(bkDir, "bk_20260822_120000.csv")
	_ = os.WriteFile(bk1, []byte("ticker,weight\nNSE:TICKER_A,0.50\nNSE:TICKER_B,0.50\n"), 0644)

	// Current holdings only has TICKER_A
	existing := map[string]float64{
		"NSE:TICKER_A": 0.50,
	}

	asOf := time.Date(2026, 9, 7, 0, 0, 0, 0, time.UTC)
	// Temporarily switch working dir to tempDir to test LoadRecentExits
	origWd, _ := os.Getwd()
	_ = os.Chdir(tempDir)
	defer os.Chdir(origWd)

	exits := LoadRecentExits("testuniverse", existing, 30, asOf)
	if _, ok := exits["NSE:TICKER_B"]; !ok {
		t.Errorf("expected NSE:TICKER_B to be detected as an exit")
	}
	if _, ok := exits["NSE:TICKER_A"]; ok {
		t.Errorf("did not expect current holding NSE:TICKER_A to be detected as an exit")
	}
}

func TestLoadRecentExits_ArvindCooldown(t *testing.T) {
	// LoadRecentExits records a ticker as a recent exit if it appears in any
	// within-window backup and is NOT a current holding, dated to the most recent
	// backup it appears in. So place ARVIND in the newest snapshot (2026-08-26)
	// and pass nil holdings → exit dated 2026-08-26.
	tempDir := t.TempDir()
	bkDir := filepath.Join(tempDir, "data", "backups", "microsmall")
	if err := os.MkdirAll(bkDir, 0755); err != nil {
		t.Fatal(err)
	}
	older := filepath.Join(bkDir, "bk_20260820_120000.csv")
	_ = os.WriteFile(older, []byte("ticker,weight\nNSE:KEEP,1.00\n"), 0644)
	newer := filepath.Join(bkDir, "bk_20260826_120000.csv")
	_ = os.WriteFile(newer, []byte("ticker,weight\nNSE:ARVIND,0.30\nNSE:KEEP,0.70\n"), 0644)

	origWd, _ := os.Getwd()
	_ = os.Chdir(tempDir)
	defer os.Chdir(origWd)

	refTime := time.Date(2026, 8, 27, 0, 0, 0, 0, time.UTC)
	exits := LoadRecentExits("microsmall", nil, 30, refTime)
	exitDate, ok := exits["NSE:ARVIND"]
	if !ok {
		t.Fatalf("expected NSE:ARVIND to be identified in recent exits for microsmall")
	}
	expectedExitDate := time.Date(2026, 8, 26, 0, 0, 0, 0, time.UTC)
	if !exitDate.Equal(expectedExitDate) {
		t.Errorf("expected ARVIND exit date %v, got %v", expectedExitDate, exitDate)
	}

	onCd, reason := IsOnCooldown("NSE:ARVIND", exits, 19, 5, refTime, 30)
	if !onCd {
		t.Errorf("expected NSE:ARVIND at rank 19 to be on cooldown, got false")
	}
	if reason == "" {
		t.Errorf("expected non-empty cooldown reason")
	}
}
