package themedb

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestSyncThemeFromProposals(t *testing.T) {
	tmpDir := t.TempDir()
	proposalsDir := filepath.Join(tmpDir, "proposals")
	if err := os.MkdirAll(proposalsDir, 0755); err != nil {
		t.Fatalf("failed to create proposals dir: %v", err)
	}

	// 1. Create fake proposal files for v1, v2, v3
	f1 := filepath.Join(proposalsDir, "20260722_testtheme_optim.csv")
	os.WriteFile(f1, []byte("ticker,weight\nNSE:STOCK1,0.60\nNSE:STOCK2,0.40\n"), 0644)

	f2 := filepath.Join(proposalsDir, "20260801_testtheme_optim.csv")
	os.WriteFile(f2, []byte("ticker,weight\nNSE:STOCK1,0.50\nNSE:STOCK3,0.50\n"), 0644) // STOCK2 exited, STOCK3 entered

	goldenPath := filepath.Join(tmpDir, "testtheme.csv")
	os.WriteFile(goldenPath, []byte("ticker,weight\nNSE:STOCK1,0.70\nNSE:STOCK4,0.30\nNSE:STOCK3,0.00\n"), 0644) // STOCK3 exited, STOCK4 entered

	dbPath := filepath.Join(tmpDir, "mycase.db")
	db, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer db.Close()

	ctx := context.Background()
	opts := SyncThemeOptions{
		ThemeName:     "testtheme",
		GoldenCSVPath: goldenPath,
		ProposalsDir:  proposalsDir,
		Keyword:       "testtheme",
	}

	if err := db.SyncThemeFromProposals(ctx, opts); err != nil {
		t.Fatalf("SyncThemeFromProposals failed: %v", err)
	}

	v, err := db.GetLatestVersion(ctx, "testtheme")
	if err != nil {
		t.Fatalf("GetLatestVersion failed: %v", err)
	}
	if v < 3 {
		t.Fatalf("expected at least 3 versions, got %d", v)
	}

	active, err := db.GetActiveHoldings(ctx, "testtheme")
	if err != nil {
		t.Fatalf("GetActiveHoldings failed: %v", err)
	}
	if len(active) != 2 { // STOCK1, STOCK4
		t.Fatalf("expected 2 active holdings, got %d", len(active))
	}

	exited, err := db.GetExitedHoldings(ctx, "testtheme")
	if err != nil {
		t.Fatalf("GetExitedHoldings failed: %v", err)
	}
	// STOCK2 and STOCK3 should be exited
	exitedSyms := make(map[string]bool)
	for _, it := range exited {
		exitedSyms[it.Symbol] = true
	}
	if !exitedSyms["STOCK2"] {
		t.Errorf("expected STOCK2 to be in exited list")
	}
	if !exitedSyms["STOCK3"] {
		t.Errorf("expected STOCK3 to be in exited list")
	}
}
