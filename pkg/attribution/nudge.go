package attribution

// DefaultAlphaNudgeThreshold is the trailing (annualized) alpha, as a negative
// fraction, below which the system suggests reviewing the strategy. -0.02 means
// "underperforming the benchmark by more than 2% annualized after adjusting for
// beta". This feeds the roadmap's "when to simplify to index funds" question.
const DefaultAlphaNudgeThreshold = -0.02

// NudgeAssessment is the outcome of evaluating whether trailing performance
// warrants a "review your strategy" nudge.
type NudgeAssessment struct {
	Nudge       bool    // true if the strategy is materially underperforming
	Alpha       float64 // trailing annualized alpha (fraction) that was evaluated
	Threshold   float64 // the threshold applied
	TradingDays int     // days of data behind the assessment
	Reason      string  // human-readable explanation
}

// AssessNudge decides whether trailing performance is poor enough to suggest
// simplifying to an index. It is a pure function of an already-computed Result
// so it is trivially testable and carries no I/O.
//
// A nudge fires only when there is enough data to be meaningful (at least
// minDays trading days) AND alpha is at or below the threshold. Threshold <= 0
// falls back to DefaultAlphaNudgeThreshold; a caller wanting a custom (still
// negative) threshold passes it explicitly.
func AssessNudge(r Result, threshold float64) NudgeAssessment {
	const minDays = 60 // ~3 months of trading days; below this, alpha is noise
	if threshold >= 0 {
		threshold = DefaultAlphaNudgeThreshold
	}
	a := NudgeAssessment{
		Alpha:       r.Alpha,
		Threshold:   threshold,
		TradingDays: r.TradingDays,
	}
	if r.TradingDays < minDays {
		a.Reason = "insufficient history for a reliable trailing-alpha assessment"
		return a
	}
	if r.Alpha <= threshold {
		a.Nudge = true
		a.Reason = "trailing alpha is materially negative — the active strategy is lagging the benchmark after adjusting for risk"
		return a
	}
	a.Reason = "trailing alpha is within acceptable bounds"
	return a
}
