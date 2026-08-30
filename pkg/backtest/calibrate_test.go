package backtest

import (
	"math"
	"testing"
)

func TestRankValues(t *testing.T) {
	vals := []float64{10.0, 20.0, 20.0, 30.0}
	ranks := RankValues(vals)
	if len(ranks) != 4 {
		t.Fatalf("expected 4 ranks, got %d", len(ranks))
	}
	if ranks[0] != 1.0 {
		t.Errorf("expected rank 1.0 for 10.0, got %.1f", ranks[0])
	}
	if ranks[1] != 2.5 || ranks[2] != 2.5 {
		t.Errorf("expected tie rank 2.5 for 20.0, got %.1f and %.1f", ranks[1], ranks[2])
	}
	if ranks[3] != 4.0 {
		t.Errorf("expected rank 4.0 for 30.0, got %.1f", ranks[3])
	}
}

func TestSpearmanRankCorrelation(t *testing.T) {
	// Perfect monotonic relationship
	x := []float64{1.0, 2.0, 3.0, 4.0, 5.0}
	y := []float64{10.0, 25.0, 33.0, 40.0, 99.0}
	corr := SpearmanRankCorrelation(x, y)
	if math.Abs(corr-1.0) > 1e-4 {
		t.Errorf("expected perfect correlation 1.0, got %.4f", corr)
	}

	// Perfect inverse monotonic relationship
	yInv := []float64{99.0, 40.0, 33.0, 25.0, 10.0}
	corrInv := SpearmanRankCorrelation(x, yInv)
	if math.Abs(corrInv-(-1.0)) > 1e-4 {
		t.Errorf("expected perfect inverse correlation -1.0, got %.4f", corrInv)
	}
}

func TestPercentile(t *testing.T) {
	sorted := []float64{10.0, 20.0, 30.0, 40.0, 50.0, 60.0, 70.0, 80.0, 90.0, 100.0}
	p50 := Percentile(sorted, 0.50)
	if math.Abs(p50-55.0) > 1e-4 {
		t.Errorf("expected median 55.0, got %.2f", p50)
	}
	p0 := Percentile(sorted, 0.0)
	if p0 != 10.0 {
		t.Errorf("expected p0 10.0, got %.2f", p0)
	}
	p100 := Percentile(sorted, 1.0)
	if p100 != 100.0 {
		t.Errorf("expected p100 100.0, got %.2f", p100)
	}
}

func TestComputePillarStats(t *testing.T) {
	ics := []float64{0.10, 0.20, 0.30}
	stats := ComputePillarStats(ics)
	if math.Abs(stats.MeanIC-0.20) > 1e-4 {
		t.Errorf("expected mean IC 0.20, got %.4f", stats.MeanIC)
	}
	if stats.PositivePct != 100.0 {
		t.Errorf("expected positive pct 100%%, got %.1f%%", stats.PositivePct)
	}
}
