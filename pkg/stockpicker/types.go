package stockpicker

import (
	"context"

	"github.com/raghavkgarg/mycase/pkg/config"
	"github.com/raghavkgarg/mycase/pkg/marketcal"
	"github.com/raghavkgarg/mycase/pkg/optimizer"
	"github.com/raghavkgarg/mycase/pkg/yfinance"
)

// The stockpicker consumes three market-data capabilities, each declared as its
// own interface so a caller (or test stub) can depend on only what it needs and
// so new data sources can satisfy one capability without implementing all three
// (Phase 10d capability split). Production callers pass a *datafetcher.Router,
// which satisfies all three (and thus the composed DataFetcher below).
type (
	// PriceSource supplies historical price series for a ticker.
	PriceSource interface {
		FetchHistoricalDataWithTimestamps(ctx context.Context, ticker string, rangeStr string) (*yfinance.HistoricalData, error)
		FetchHistoricalPrices(ctx context.Context, ticker string, rangeStr string) ([]float64, error)
	}

	// FundamentalsSource supplies fundamental metrics for a batch of tickers.
	FundamentalsSource interface {
		FetchFundamentals(ctx context.Context, tickers []string) (map[string]yfinance.Fundamentals, error)
	}
)

// DataFetcher is the composed capability the full pipeline needs: prices +
// fundamentals. It embeds the capability interfaces so existing callers
// (`Options.DataFetcher`, `eod.Config.Fetcher`) and the compile-time assert in
// pkg/autopilot are unchanged, while new code can depend on the narrower
// PriceSource / FundamentalsSource directly. A *datafetcher.Router satisfies it.
//
// SectorSource is intentionally NOT part of this set: sector is not fetched
// through the router — it is backfilled from the constituents CSV by
// InjectSectors (Phase 10a), a better (GICS) source than any provider endpoint.
type DataFetcher interface {
	PriceSource
	FundamentalsSource
}

// Options holds command line configurations.
type Options struct {
	DataFetcher                         DataFetcher // optional; if nil, falls back to direct yfinance calls
	IndexName                           string
	FilePath                            string
	Method                              string
	RangeStr                            string
	GoldenPath                          string
	RebalanceTolerance                  float64
	HysteresisBuffer                    int
	HysteresisMinScoreDelta             float64
	HysteresisRequireGrowthAcceleration bool
	DisplayName                         string
	OutputFile                          string
	Tickers                             []string // pre-built ticker list (bypasses file/index loading)
	TopN                                int
	SkipScuttlebutt                     bool
	CooldownDays                        int
	CooldownBypassRank                  int
	AsOfDate                            string         // Target EOD market date (YYYY-MM-DD); if empty, defaults to Clock.SettlementDate(now)
	BasedOn                             string         // Formatted based-on string e.g. "2026-09-11 EOD (Synced: 2026-09-11 21:15:00 IST)"
	Force                               bool           // Force execution even if snapshot exists
	DisableSentryGate                   bool           // if true, skips Sentry technical gate (price >= 0.95*SMA200 and DD <= 20%)
	SectorMaxStocks                     map[string]int // sector-specific stock count caps (e.g. "Consumer Defensive": 2)
	MinEntryScore                       float64        // minimum score hurdle for new additions (e.g. 40.0)
	MinHoldingScore                     float64        // minimum score floor for incumbents (e.g. 35.0)
	MaxStockWeightCap                   float64        // maximum single stock weight cap (e.g. 0.06 / 6%)
	AllowCashReserve                    bool           // allow excess/unallocated weight to spill into CASH_RESERVE

	// Clock is the market settlement/trading calendar used for the run's as-of /
	// based-on date decisions. It is the single source of truth for "which
	// trading day is this run for?" — the caller (cmd/eod) injects the active
	// market's holiday-aware clock (broker.TradingClock()). A zero value defaults
	// to the bare marketcal.NSE, preserving the historical India-only behavior for
	// callers that don't set it. stockpicker cannot import broker (layering), so
	// the aware clock arrives here as a value, never via a global.
	Clock marketcal.Clock
}

// clock returns the configured settlement clock, defaulting to the bare
// marketcal.NSE when unset (matches the pre-injection behavior).
func (o Options) clock() marketcal.Clock {
	if o.Clock.Loc == nil {
		return marketcal.NSE
	}
	return o.Clock
}

// TickersSource encapsulates tickers list source info.
type TickersSource struct {
	// Sectors maps ticker -> sector when the constituents CSV carries a
	// GICS Sector column (e.g. the S&P 500 dataset). Empty for sources that
	// don't. Used to backfill Fundamentals.Sector on the US/Schwab path,
	// where the fundamentals endpoint returns no sector (Phase 10a).
	Sectors map[string]string
	Name    string
	Tickers []string
}

// StrategyConfig wraps optimization weights, safety filters, and governance traps.
type StrategyConfig struct {
	HardFilters *config.HardFilters
	Governance  map[string]float64
	Weights     optimizer.MFSWeights
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
