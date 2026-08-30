package stockpicker

type ScoreBounds struct {
	Min float64
	Max float64
}

var (
	CompositeRSBounds   = ScoreBounds{Min: -0.30, Max: 0.70}
	VCPRatioBounds      = ScoreBounds{Min: 0.25, Max: 0.75} // lower is better
	RVOLZBounds         = ScoreBounds{Min: 0.0, Max: 3.0}
	PocketPivotBounds   = ScoreBounds{Min: 0.0, Max: 12.0}
	DeliveryDeltaBounds = ScoreBounds{Min: -0.10, Max: 0.30}
)

func clamp(val, minV, maxV float64) float64 {
	if val < minV {
		return minV
	}
	if val > maxV {
		return maxV
	}
	return val
}

// NormScore maps raw metric x into [0.0, targetPts] using invariant reference bounds.
func NormScore(x float64, b ScoreBounds, targetPts float64, lowerIsBetter bool) float64 {
	if b.Max <= b.Min {
		return 0.0
	}
	var frac float64
	if lowerIsBetter {
		frac = (b.Max - x) / (b.Max - b.Min)
	} else {
		frac = (x - b.Min) / (b.Max - b.Min)
	}
	return targetPts * clamp(frac, 0.0, 1.0)
}
