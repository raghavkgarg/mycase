package stockpicker

import (
	"github.com/raghavkgarg/mycase/pkg/config"
	"github.com/raghavkgarg/mycase/pkg/optimizer"
)

// Options holds command line configurations.
type Options struct {
	IndexName          string
	FilePath           string
	Method             string
	TopN               int
	RangeStr           string
	SkipScuttlebutt    bool
	GoldenPath         string
	RebalanceTolerance float64
	HysteresisBuffer   int
	DisplayName        string
	OutputFile         string
}

// TickersSource encapsulates tickers list source info.
type TickersSource struct {
	Name    string
	Tickers []string
}

// StrategyConfig wraps optimization weights, safety filters, and governance traps.
type StrategyConfig struct {
	Weights     optimizer.MFSWeights
	HardFilters *config.HardFilters
	Governance  map[string]float64
}

// FilterStats holds metric elimination counts from applySafetyFilters.
type FilterStats struct {
	EliminatedSize               int
	EliminatedLiquidity          int
	EliminatedCashFlow           int
	EliminatedEarningsTrend      int
	EliminatedPromoter           int
	EliminatedSMATrend           int
	EliminatedPledge             int
	EliminatedROCE               int
	EliminatedLeverage           int
	EliminatedInterestCoverage   int
	EliminatedSalesAccelerator   int
	EliminatedAssetTurnoverCapEx int
	EliminatedWorkingCapital     int
	EliminatedVolumeBreakout     int
	EliminatedPEG                int
	EliminatedGrossMargin        int
	EliminatedRSPercentile       int
	EliminatedCROIC              int
}
