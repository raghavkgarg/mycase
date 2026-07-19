package optimizer

// CapWeights enforces a per-stock weight cap, redistributing excess to under-cap stocks
// proportionally to their current weights.
// If 1/n >= cap (cap too tight for n stocks), falls back to equal weights.
func CapWeights(weights map[string]float64, cap float64) map[string]float64 {
	n := len(weights)
	if n == 0 {
		return weights
	}
	if 1.0/float64(n) >= cap {
		equalWt := 1.0 / float64(n)
		result := make(map[string]float64)
		for k := range weights {
			result[k] = equalWt
		}
		return result
	}
	result := make(map[string]float64)
	for k, v := range weights {
		result[k] = v
	}
	for {
		var excess float64
		var underCapSum float64
		underCapCount := 0
		for k, v := range result {
			if v > cap {
				excess += v - cap
				result[k] = cap
			} else if v < cap {
				underCapSum += v
				underCapCount++
			}
		}
		if excess < 0.00001 {
			break
		}
		if underCapSum > 0 {
			for k, v := range result {
				if v < cap {
					result[k] += (v / underCapSum) * excess
				}
			}
		} else if underCapCount > 0 {
			for k, v := range result {
				if v < cap {
					result[k] += excess / float64(underCapCount)
				}
			}
		} else {
			break
		}
	}
	return result
}
