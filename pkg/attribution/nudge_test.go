package attribution

import "testing"

func TestAssessNudge(t *testing.T) {
	tests := []struct {
		name      string
		alpha     float64
		days      int
		threshold float64
		wantNudge bool
	}{
		{"clear underperformance", -0.05, 250, 0, true},
		{"exactly at default threshold", DefaultAlphaNudgeThreshold, 250, 0, true},
		{"just above threshold", -0.01, 250, 0, false},
		{"positive alpha", 0.03, 250, 0, false},
		{"underperforming but too little data", -0.10, 30, 0, false},
		{"custom stricter threshold triggers", -0.015, 250, -0.01, true},
		{"custom threshold not met", -0.005, 250, -0.01, false},
		{"positive threshold falls back to default", -0.03, 250, 0.5, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := Result{Alpha: tt.alpha, TradingDays: tt.days}
			got := AssessNudge(r, tt.threshold)
			if got.Nudge != tt.wantNudge {
				t.Errorf("Nudge = %v, want %v (alpha=%.3f days=%d thr=%.3f)",
					got.Nudge, tt.wantNudge, tt.alpha, tt.days, tt.threshold)
			}
			if got.Reason == "" {
				t.Error("expected a non-empty Reason")
			}
		})
	}
}

func TestAssessNudge_ThresholdNormalization(t *testing.T) {
	// A positive threshold should be replaced by the default negative one.
	r := Result{Alpha: -0.5, TradingDays: 100}
	got := AssessNudge(r, 0.1)
	if got.Threshold != DefaultAlphaNudgeThreshold {
		t.Errorf("Threshold = %v, want default %v", got.Threshold, DefaultAlphaNudgeThreshold)
	}
}
