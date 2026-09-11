package themedb

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestThemeDB_Lifecycle(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "mycase.db")

	db, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer db.Close()

	ctx := context.Background()

	// 1. Initial state: HasTheme should be false, version 0
	has, err := db.HasTheme(ctx, "microsmall")
	if err != nil {
		t.Fatalf("HasTheme failed: %v", err)
	}
	if has {
		t.Fatalf("expected HasTheme to be false initially")
	}

	v, err := db.GetLatestVersion(ctx, "microsmall")
	if err != nil {
		t.Fatalf("GetLatestVersion failed: %v", err)
	}
	if v != 0 {
		t.Fatalf("expected version 0, got %d", v)
	}

	// 2. Inception v1: 3 stocks
	t1 := time.Date(2026, 7, 22, 10, 0, 0, 0, time.UTC)
	v1Rebalance := ThemeRebalance{
		ThemeName:     "microsmall",
		Version:       1,
		EffectiveDate: t1,
		CreatedAt:     t1,
		Status:        "COMMITTED",
		Notes:         "Initial basket launch",
	}

	v1Items := []ThemeHistoryItem{
		{
			Symbol:       "STOCKA",
			Action:       "NEW_ENTRY",
			TargetWeight: 0.50,
			PrevWeight:   0.0,
		},
		{
			Symbol:       "STOCKB",
			Action:       "NEW_ENTRY",
			TargetWeight: 0.30,
			PrevWeight:   0.0,
		},
		{
			Symbol:       "STOCKC",
			Action:       "NEW_ENTRY",
			TargetWeight: 0.20,
			PrevWeight:   0.0,
		},
	}

	if err := db.RecordRebalance(ctx, v1Rebalance, v1Items); err != nil {
		t.Fatalf("RecordRebalance v1 failed: %v", err)
	}

	v, err = db.GetLatestVersion(ctx, "microsmall")
	if err != nil || v != 1 {
		t.Fatalf("expected version 1, got %d, err: %v", v, err)
	}

	active, err := db.GetActiveHoldings(ctx, "microsmall")
	if err != nil {
		t.Fatalf("GetActiveHoldings failed: %v", err)
	}
	if len(active) != 3 {
		t.Fatalf("expected 3 active holdings, got %d", len(active))
	}

	exited, err := db.GetExitedHoldings(ctx, "microsmall")
	if err != nil {
		t.Fatalf("GetExitedHoldings failed: %v", err)
	}
	if len(exited) != 0 {
		t.Fatalf("expected 0 exited holdings, got %d", len(exited))
	}

	// 3. Rebalance v2:
	// - STOCKA reweighted to 0.40
	// - STOCKB exited (target 0.0)
	// - STOCKD new entry (target 0.40)
	// - STOCKC unchanged / reweighted (0.20)
	t2 := time.Date(2026, 8, 26, 10, 0, 0, 0, time.UTC)
	v2Rebalance := ThemeRebalance{
		ThemeName:     "microsmall",
		Version:       2,
		EffectiveDate: t2,
		CreatedAt:     t2,
		Status:        "COMMITTED",
		Notes:         "August churn",
	}

	v2Items := []ThemeHistoryItem{
		{
			Symbol:       "STOCKA",
			Action:       "REWEIGHT",
			TargetWeight: 0.40,
			PrevWeight:   0.50,
		},
		{
			Symbol:       "STOCKB",
			Action:       "EXITED",
			TargetWeight: 0.0,
			PrevWeight:   0.30,
			ExitCategory: "RANK_DECAY",
			ExitReason:   "Fell to rank 35",
		},
		{
			Symbol:       "STOCKC",
			Action:       "UNCHANGED",
			TargetWeight: 0.20,
			PrevWeight:   0.20,
		},
		{
			Symbol:       "STOCKD",
			Action:       "NEW_ENTRY",
			TargetWeight: 0.40,
			PrevWeight:   0.0,
		},
	}

	if err := db.RecordRebalance(ctx, v2Rebalance, v2Items); err != nil {
		t.Fatalf("RecordRebalance v2 failed: %v", err)
	}

	v, err = db.GetLatestVersion(ctx, "microsmall")
	if err != nil || v != 2 {
		t.Fatalf("expected version 2, got %d, err: %v", v, err)
	}

	active, err = db.GetActiveHoldings(ctx, "microsmall")
	if err != nil {
		t.Fatalf("GetActiveHoldings failed: %v", err)
	}
	if len(active) != 3 { // STOCKA, STOCKC, STOCKD
		t.Fatalf("expected 3 active holdings in v2, got %d", len(active))
	}

	exited, err = db.GetExitedHoldings(ctx, "microsmall")
	if err != nil {
		t.Fatalf("GetExitedHoldings failed: %v", err)
	}
	if len(exited) != 1 {
		t.Fatalf("expected 1 exited holding, got %d", len(exited))
	}
	if exited[0].Symbol != "STOCKB" {
		t.Fatalf("expected exited symbol STOCKB, got %s", exited[0].Symbol)
	}
	if exited[0].ExitCategory != "RANK_DECAY" {
		t.Fatalf("expected exit category RANK_DECAY, got %s", exited[0].ExitCategory)
	}

	// 4. Test idempotency: re-running RecordRebalance should update without duplicating
	if err := db.RecordRebalance(ctx, v2Rebalance, v2Items); err != nil {
		t.Fatalf("Re-recording v2 failed: %v", err)
	}
	active, _ = db.GetActiveHoldings(ctx, "microsmall")
	if len(active) != 3 {
		t.Fatalf("expected 3 active holdings after idempotent update, got %d", len(active))
	}
}
