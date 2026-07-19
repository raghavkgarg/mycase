package monitoring

// PolicyParams defines the parameters for the 4-pillar portfolio monitoring policy.
type PolicyParams struct {
	ConsecutiveQuartersExit    int     // Pillar 1: Consecutive quarters of TTM growth < 3Y CAGR to trigger exit
	DSODeteriorationThreshold  float64 // Pillar 2: DSO YoY deterioration (increase) threshold (e.g. 0.15 for 15%)
	SMADays                    int     // Pillar 4: Consecutive days below 200-day SMA to trigger watch list
	RebalanceMonths            int     // Pillar 3: Rebalance frequency in months (e.g., 6)
	MaxWeightDrift             float64 // Pillar 3: Single stock weight drift limit (e.g., 0.15 for 15%)
	MaxCapExYoYMultiplier      float64 // CapEx YoY growth multiplier threshold (e.g., 2.00)
	StartDate                  string  // Start date for simulation (YYYY-MM-DD)
}

// StockInfo represents the baseline input for a stock in the portfolio.
type StockInfo struct {
	Ticker string
	Weight float64
	IsMock bool
}

// StockVerdict contains the monitoring verdict and metrics for a stock.
type StockVerdict struct {
	Ticker           string
	Sector           string
	CAGR3Y           float64
	TTMGrowth        float64
	DSODelta         float64
	CapStallSeverity string
	Verdict          string // "✅ KEEP HOLD", "⚠️ AUTO EXIT", "👀 HIGH ALERT"
	DataSource       string // "Live" or "Mock"
}

// SimulationResult holds the summary metrics of a backtest run.
type SimulationResult struct {
	InitialValue    float64
	FinalValue      float64
	PortfolioReturn float64
	BenchmarkReturn float64
	ExcessReturn    float64
	ChurnRate       float64
	AlphaEfficiency float64
	Verdicts        []StockVerdict
}
