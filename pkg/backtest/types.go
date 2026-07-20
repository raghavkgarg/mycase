package backtest

import "time"

// RebalanceFreq controls how often the portfolio is rebalanced during simulation.
type RebalanceFreq string

const (
	FreqMonthly   RebalanceFreq = "monthly"
	FreqQuarterly RebalanceFreq = "quarterly"
	FreqDrift     RebalanceFreq = "drift-triggered"
)

// SimConfig holds all parameters for a backtest run.
type SimConfig struct {
	InitialCapital  float64
	From            time.Time
	To              time.Time
	Rebalance       RebalanceFreq
	SlippagePct     float64 // fraction, e.g. 0.001 = 0.1%
	BenchmarkTicker string  // e.g. "^NSEI"
	DriftThreshold  float64 // for FreqDrift; fraction, e.g. 0.05 = 5%
}

// Holding is a ticker + target weight pair (weights should sum to 1.0).
type Holding struct {
	Ticker string
	Weight float64
}

// DailySnapshot records portfolio and benchmark NAV on a single trading day.
type DailySnapshot struct {
	Date           time.Time
	PortfolioValue float64
	BenchmarkValue float64
}

// SimResult is the full output of a backtest run.
type SimResult struct {
	Snapshots       []DailySnapshot
	TotalReturn     float64 // total portfolio return, fraction
	BenchmarkReturn float64 // total benchmark return, fraction
	CAGR            float64 // annualized portfolio return, fraction
	BenchmarkCAGR   float64
	MaxDrawdown     float64 // peak-to-trough, fraction (negative)
	SharpeRatio     float64
	SortinoRatio    float64
	CalmarRatio     float64
	Alpha           float64 // Jensen's alpha (annualized, fraction)
	Beta            float64
	TradingDays     int
	RebalanceCount  int
}
