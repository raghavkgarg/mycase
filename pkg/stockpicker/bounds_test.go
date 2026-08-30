package stockpicker

import (
	"math"
	"testing"
)

func TestNormScore_WorkedExamples(t *testing.T) {
	// 1. Pillar 2: VCP Tightness (lower is better, [0.25, 0.75], 25 pts)
	if s := NormScore(0.25, VCPRatioBounds, 25.0, true); math.Abs(s-25.0) > 1e-4 {
		t.Errorf("VCP 0.25: expected 25.0 pts, got %.2f", s)
	}
	if s := NormScore(0.75, VCPRatioBounds, 25.0, true); math.Abs(s-0.0) > 1e-4 {
		t.Errorf("VCP 0.75: expected 0.0 pts, got %.2f", s)
	}
	if s := NormScore(0.50, VCPRatioBounds, 25.0, true); math.Abs(s-12.5) > 1e-4 {
		t.Errorf("VCP 0.50: expected 12.5 pts, got %.2f", s)
	}
	if s := NormScore(0.10, VCPRatioBounds, 25.0, true); math.Abs(s-25.0) > 1e-4 {
		t.Errorf("VCP 0.10 (clamped): expected 25.0 pts, got %.2f", s)
	}
	if s := NormScore(1.12, VCPRatioBounds, 25.0, true); math.Abs(s-0.0) > 1e-4 {
		t.Errorf("VCP 1.12 (clamped): expected 0.0 pts, got %.2f", s)
	}

	// 2. Pillar 1: Composite RS ([-0.30, 0.70], 25 pts)
	if s := NormScore(0.70, CompositeRSBounds, 25.0, false); math.Abs(s-25.0) > 1e-4 {
		t.Errorf("RS +0.70: expected 25.0 pts, got %.2f", s)
	}
	if s := NormScore(-0.30, CompositeRSBounds, 25.0, false); math.Abs(s-0.0) > 1e-4 {
		t.Errorf("RS -0.30: expected 0.0 pts, got %.2f", s)
	}
	if s := NormScore(0.20, CompositeRSBounds, 25.0, false); math.Abs(s-12.5) > 1e-4 {
		t.Errorf("RS +0.20: expected 12.5 pts, got %.2f", s)
	}

	// 3. Sub-Pillar 3A: RVOL Z ([0.0, 3.0], 12.5 pts)
	if s := NormScore(3.0, RVOLZBounds, 12.5, false); math.Abs(s-12.5) > 1e-4 {
		t.Errorf("RVOL Z 3.0: expected 12.5 pts, got %.2f", s)
	}
	if s := NormScore(1.5, RVOLZBounds, 12.5, false); math.Abs(s-6.25) > 1e-4 {
		t.Errorf("RVOL Z 1.5: expected 6.25 pts, got %.2f", s)
	}

	// 4. Sub-Pillar 3B: Decayed Pocket Pivot ([0.0, 12.0], 12.5 pts)
	if s := NormScore(12.0, PocketPivotBounds, 12.5, false); math.Abs(s-12.5) > 1e-4 {
		t.Errorf("PP 12.0: expected 12.5 pts, got %.2f", s)
	}
	if s := NormScore(6.0, PocketPivotBounds, 12.5, false); math.Abs(s-6.25) > 1e-4 {
		t.Errorf("PP 6.0: expected 6.25 pts, got %.2f", s)
	}

	// 5. Pillar 4: Delivery Delta ([-0.10, 0.30], 25 pts)
	if s := NormScore(0.30, DeliveryDeltaBounds, 25.0, false); math.Abs(s-25.0) > 1e-4 {
		t.Errorf("Deliv +0.30: expected 25.0 pts, got %.2f", s)
	}
	if s := NormScore(0.10, DeliveryDeltaBounds, 25.0, false); math.Abs(s-12.5) > 1e-4 {
		t.Errorf("Deliv +0.10: expected 12.5 pts, got %.2f", s)
	}
}
