package optimizer

import (
	"math"
	"testing"
	"testing/quick"
)

const eps = 1e-9

func sumWeights(w map[string]float64) float64 {
	var total float64
	for _, v := range w {
		total += v
	}
	return total
}

func TestCapWeights_Empty(t *testing.T) {
	result := CapWeights(map[string]float64{}, 0.10)
	if len(result) != 0 {
		t.Errorf("expected empty map, got %v", result)
	}
}

func TestCapWeights_AllUnderCap(t *testing.T) {
	input := map[string]float64{"A": 0.50, "B": 0.30, "C": 0.20}
	result := CapWeights(input, 0.60)
	for k, want := range input {
		if math.Abs(result[k]-want) > eps {
			t.Errorf("ticker %s: want %.6f got %.6f", k, want, result[k])
		}
	}
	if math.Abs(sumWeights(result)-1.0) > eps {
		t.Errorf("weights sum to %.6f, want 1.0", sumWeights(result))
	}
}

func TestCapWeights_Basic(t *testing.T) {
	// A is over the cap; excess should flow to B and C proportionally.
	input := map[string]float64{"A": 0.70, "B": 0.20, "C": 0.10}
	cap := 0.40
	result := CapWeights(input, cap)

	for k, v := range result {
		if v > cap+eps {
			t.Errorf("ticker %s: weight %.6f exceeds cap %.6f", k, v, cap)
		}
	}
	if math.Abs(sumWeights(result)-1.0) > eps {
		t.Errorf("weights sum to %.6f, want 1.0", sumWeights(result))
	}
	// A was capped at 0.40; its weight must equal the cap exactly
	if math.Abs(result["A"]-cap) > eps {
		t.Errorf("over-cap ticker A: want %.6f, got %.6f", cap, result["A"])
	}
}

func TestCapWeights_MultipleOverCap(t *testing.T) {
	// Both A and B start over the cap; C is under.
	input := map[string]float64{"A": 0.50, "B": 0.40, "C": 0.10}
	cap := 0.35
	result := CapWeights(input, cap)

	for k, v := range result {
		if v > cap+eps {
			t.Errorf("ticker %s: weight %.6f exceeds cap %.6f", k, v, cap)
		}
	}
	if math.Abs(sumWeights(result)-1.0) > eps {
		t.Errorf("weights sum to %.6f, want 1.0", sumWeights(result))
	}
}

func TestCapWeights_CapTooTight(t *testing.T) {
	// 1/3 ≈ 0.333 > cap 0.30 → equal weight fallback
	input := map[string]float64{"A": 0.60, "B": 0.30, "C": 0.10}
	result := CapWeights(input, 0.30)

	want := 1.0 / 3.0
	for k, v := range result {
		if math.Abs(v-want) > eps {
			t.Errorf("ticker %s: want equal weight %.6f, got %.6f", k, want, v)
		}
	}
}

func TestCapWeights_SingleStock(t *testing.T) {
	// N=1: 1/1 = 1.0 >= any cap → equal weight fallback → weight stays 1.0
	input := map[string]float64{"A": 1.0}
	result := CapWeights(input, 0.05)
	if math.Abs(result["A"]-1.0) > eps {
		t.Errorf("single-stock weight should be 1.0, got %.6f", result["A"])
	}
}

func TestCapWeights_SumInvariant(t *testing.T) {
	// Property: output weights always sum to ~1.0.
	// When 1/n >= cap the function falls back to equal weights (1/n > cap is expected).
	// We only check the cap constraint for cases where cap > 1/n.
	cases := []struct {
		weights map[string]float64
		cap     float64
	}{
		{map[string]float64{"A": 0.90, "B": 0.05, "C": 0.05}, 0.40},        // cap > 1/3
		{map[string]float64{"A": 0.34, "B": 0.33, "C": 0.33}, 0.50},        // cap > 1/3
		{map[string]float64{"A": 0.50, "B": 0.30, "C": 0.20}, 0.60},        // none over cap
		{map[string]float64{"A": 0.50, "B": 0.30, "C": 0.10, "D": 0.10}, 0.40}, // cap > 1/4
	}
	for _, tc := range cases {
		n := len(tc.weights)
		equalWt := 1.0 / float64(n)
		result := CapWeights(tc.weights, tc.cap)
		if math.Abs(sumWeights(result)-1.0) > 1e-6 {
			t.Errorf("cap=%.2f, weights=%v: sum=%.6f, want 1.0", tc.cap, tc.weights, sumWeights(result))
		}
		// Only assert cap constraint when not in equal-weight fallback territory
		if equalWt < tc.cap {
			for k, v := range result {
				if v > tc.cap+1e-6 {
					t.Errorf("cap=%.2f: ticker %s weight %.6f exceeds cap", tc.cap, k, v)
				}
			}
		}
	}
}

func TestCapWeights_QuickCheck(t *testing.T) {
	// Property: output weights always sum to 1.0.
	// When cap >= 1/n (equal-weight fallback), the cap constraint may not hold — that's by design.
	f := func(a, b, c, cap8 uint8) bool {
		total := float64(a) + float64(b) + float64(c)
		if total == 0 {
			return true
		}
		weights := map[string]float64{
			"A": float64(a) / total,
			"B": float64(b) / total,
			"C": float64(c) / total,
		}
		cap := (float64(cap8)+1.0) / 256.0
		result := CapWeights(weights, cap)

		// Sum must always be 1.0
		if math.Abs(sumWeights(result)-1.0) > 1e-6 {
			return false
		}
		// Cap constraint only applies when not in the equal-weight fallback case (1/n < cap)
		equalWt := 1.0 / 3.0
		if equalWt < cap {
			for _, v := range result {
				if v > cap+1e-6 {
					return false
				}
			}
		}
		return true
	}
	if err := quick.Check(f, &quick.Config{MaxCount: 1000}); err != nil {
		t.Error(err)
	}
}
