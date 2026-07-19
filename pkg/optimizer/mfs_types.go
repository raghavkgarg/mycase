package optimizer

// MFSWeights holds the weights for each of the quantitative factors
type MFSWeights struct {
	Sharpe           float64
	Sortino          float64
	Return           float64
	Alpha            float64
	Volatility       float64
	Beta             float64
	Treynor          float64
	Ulcer            float64
	PEGRatio         float64
	ROE              float64
	ForwardPE        float64
	OperatingMargins float64
	PBRatio          float64
	NetDebtEBITDA    float64
	MarketCap        float64
	InsidersPercent  float64
}
