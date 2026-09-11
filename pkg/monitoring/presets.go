package monitoring

import "strings"

// PresetParams returns the monitoring PolicyParams for a named style
// ("hyper-aggressive", "passive", or default "moderate/balanced"), optionally
// tuned for a strategy. When strategy is "value", the DSO deterioration
// threshold is loosened (value names tolerate more receivables drift than
// momentum/multibagger names). Pass an empty strategy for the strategy-agnostic
// defaults.
//
// This is the single source of truth for monitoring presets; cmd/monitor.go and
// pkg/server both delegate here.
func PresetParams(style, strategy string) PolicyParams {
	p := PolicyParams{Strategy: strategy}
	isVal := strings.EqualFold(strategy, "value")

	switch strings.ToLower(style) {
	case "hyper-aggressive":
		p.ConsecutiveQuartersExit = 1
		p.DSODeteriorationThreshold = 0.10
		p.SMADays = 5
		p.RebalanceMonths = 3
		p.MaxWeightDrift = 0.12
		if isVal {
			p.DSODeteriorationThreshold = 0.20
		}
	case "passive":
		p.ConsecutiveQuartersExit = 3
		p.DSODeteriorationThreshold = 0.25
		p.SMADays = 20
		p.RebalanceMonths = 12
		p.MaxWeightDrift = 0.20
		if isVal {
			p.DSODeteriorationThreshold = 0.35
		}
	default:
		p.ConsecutiveQuartersExit = 2
		p.DSODeteriorationThreshold = 0.15
		p.SMADays = 10
		p.RebalanceMonths = 6
		p.MaxWeightDrift = 0.15
		if isVal {
			p.DSODeteriorationThreshold = 0.30
		}
	}
	return p
}
