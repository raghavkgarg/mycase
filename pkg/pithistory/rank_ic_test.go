package pithistory

import (
	"context"
	"math"
	"testing"
)

func TestComputeRanksWithTies(t *testing.T) {
	vals := []float64{10.0, 20.0, 20.0, 30.0}
	expected := []float64{1.0, 2.5, 2.5, 4.0}

	ranks := computeRanks(vals)
	if len(ranks) != len(expected) {
		t.Fatalf("length mismatch: got %d, want %d", len(ranks), len(expected))
	}
	for i := range expected {
		if math.Abs(ranks[i]-expected[i]) > 1e-6 {
			t.Errorf("rank[%d]: got %f, want %f", i, ranks[i], expected[i])
		}
	}
}

func TestComputeSpearmanCorrelation(t *testing.T) {
	// Perfect monotonic relationship
	x := []float64{1, 2, 3, 4, 5}
	y := []float64{10, 20, 30, 40, 50}
	rho := ComputeSpearmanCorrelation(x, y)
	if math.Abs(rho-1.0) > 1e-6 {
		t.Fatalf("expected rho=1.0, got %f", rho)
	}

	// Perfect inverse monotonic relationship
	yInv := []float64{50, 40, 30, 20, 10}
	rhoInv := ComputeSpearmanCorrelation(x, yInv)
	if math.Abs(rhoInv-(-1.0)) > 1e-6 {
		t.Fatalf("expected rho=-1.0, got %f", rhoInv)
	}

	// Non-linear monotonic relationship
	yNonlinear := []float64{1, 8, 27, 64, 125}
	rhoNL := ComputeSpearmanCorrelation(x, yNonlinear)
	if math.Abs(rhoNL-1.0) > 1e-6 {
		t.Fatalf("expected rho=1.0 for non-linear monotonic, got %f", rhoNL)
	}
}

func TestHorizonRankICExecution(t *testing.T) {
	ctx := context.Background()
	db, err := Open("")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()

	sumT5, err := db.ComputeHorizonRankIC(ctx, "niftytotalmarket", "earlymb", 5, true)
	if err != nil {
		t.Fatalf("ComputeHorizonRankIC T+5: %v", err)
	}
	if sumT5.DatesEvaluated == 0 {
		t.Logf("T+5 has no evaluated dates: %s", sumT5.Status)
	} else {
		t.Logf("T+5 Rank IC: Dates=%d, MeanRho=%.4f, t-stat=%.2f, Status=%s",
			sumT5.DatesEvaluated, sumT5.MeanRho, sumT5.TStatistic, sumT5.Status)
	}

	// T+21 should be PENDING since 21 sessions haven't elapsed
	sumT21, err := db.ComputeHorizonRankIC(ctx, "niftytotalmarket", "earlymb", 21, true)
	if err != nil {
		t.Fatalf("ComputeHorizonRankIC T+21: %v", err)
	}
	if sumT21.DatesEvaluated != 0 {
		t.Errorf("expected 0 dates for T+21, got %d", sumT21.DatesEvaluated)
	}
	if sumT21.Status == "" {
		t.Errorf("expected status string for pending T+21")
	}
}
