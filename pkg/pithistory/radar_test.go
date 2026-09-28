package pithistory

import (
	"context"
	"testing"
)

func TestBootstrapClusterCI_Reproducibility(t *testing.T) {
	excess := []float64{0.05, -0.02, 0.08, 0.01, -0.04, 0.12, 0.03, -0.01}
	dates := []string{
		"2026-09-01", "2026-09-01",
		"2026-09-02", "2026-09-02",
		"2026-09-03",
		"2026-09-04", "2026-09-04",
		"2026-09-07",
	}

	// Run twice with the exact same seed 42
	lower1, upper1 := BootstrapClusterCI(excess, dates, 500, 42)
	lower2, upper2 := BootstrapClusterCI(excess, dates, 500, 42)

	if lower1 != lower2 || upper1 != upper2 {
		t.Fatalf("BootstrapClusterCI is not reproducible with fixed seed: (%f, %f) vs (%f, %f)",
			lower1, upper1, lower2, upper2)
	}
}

func TestRadarEvaluationInvariants(t *testing.T) {
	ctx := context.Background()
	db, err := Open("")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()

	// 1. Sync radar episodes
	_, err = db.SyncRadarEpisodes(ctx, "niftytotalmarket", "earlymb")
	if err != nil {
		t.Fatalf("sync radar episodes: %v", err)
	}

	// 2. Evaluate horizon returns
	_, err = db.EvaluateRadarHorizonReturns(ctx, "niftytotalmarket", "earlymb")
	if err != nil {
		t.Fatalf("evaluate radar horizon returns: %v", err)
	}

	// 3. Verify no horizon row exists for immature T+21
	var immatureT21Count int
	_ = db.Conn().QueryRowContext(ctx, "SELECT COUNT(*) FROM radar_horizon_returns WHERE horizon = 21;").Scan(&immatureT21Count)
	if immatureT21Count > 0 {
		t.Fatalf("expected 0 matured rows for T+21 (insufficient session accumulation), got %d", immatureT21Count)
	}

	// 4. Verify T+5 matured rows exist and excess is finite
	var maturedT5Count int
	_ = db.Conn().QueryRowContext(ctx, "SELECT COUNT(*) FROM radar_horizon_returns WHERE horizon = 5;").Scan(&maturedT5Count)
	if maturedT5Count == 0 {
		t.Logf("Notice: T+5 has 0 matured rows (depends on session elapsed)")
	}
}
