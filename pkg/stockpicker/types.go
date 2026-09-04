package stockpicker

import (
	"context"

	"github.com/raghavkgarg/mycase/pkg/config"
	"github.com/raghavkgarg/mycase/pkg/optimizer"
	"github.com/raghavkgarg/mycase/pkg/yfinance"
)

// DataFetcher abstracts the data-fetching layer so that the stockpicker does not
// call yfinance directly. Production callers pass a *datafetcher.Router (which
// satisfies this interface); tests can provide a stub.
type DataFetcher interface {
	FetchFundamentals(ctx context.Context, tickers []string) (map[string]yfinance.Fundamentals, error)
	FetchHistoricalDataWithTimestamps(ctx context.Context, ticker string, rangeStr string) (*yfinance.HistoricalData, error)
	FetchHistoricalPrices(ctx context.Context, ticker string, rangeStr string) ([]float64, error)
}

// Options holds command line configurations.
type Options struct {
	IndexName          string
	FilePath           string
	Tickers            []string // pre-built ticker list (bypasses file/index loading)
	Method             string
	TopN               int
	RangeStr           string
	SkipScuttlebutt    bool
	GoldenPath         string
	RebalanceTolerance float64
	HysteresisBuffer   int
	DisplayName        string
	OutputFile         string
	DataFetcher        DataFetcher // optional; if nil, falls back to direct yfinance calls
}

// TickersSource encapsulates tickers list source info.
type TickersSource struct {
	Name    string
	Tickers []string
	// Sectors maps ticker -> sector when the constituents CSV carries a
	// GICS Sector column (e.g. the S&P 500 dataset). Empty for sources that
	// don't. Used to backfill Fundamentals.Sector on the US/Schwab path,
	// where the fundamentals endpoint returns no sector (Phase 10a).
	Sectors map[string]string
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
	EliminatedProximity52W       int
	EliminatedBaseDuration       int
	EliminatedEarningsBlackout   int
}
