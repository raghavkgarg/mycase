// Package attribution computes live-portfolio performance versus a passive
// benchmark: a daily NAV series and vs-benchmark metrics (alpha, beta,
// information ratio, tracking error).
//
// Design (see docs/refactor.md Phase 5):
//   - Sources price series through a PriceFetcher (satisfied by
//     *datafetcher.Router), so US tickers route to Schwab and others to Yahoo.
//   - Reuses pkg/backtest metric formulas (RF-parameterized variants) rather
//     than reimplementing them.
//   - Timezone- and risk-free-parameterized (US uses America/New_York and a US
//     risk-free rate), unlike the IST-hardcoded backtest engine.
//   - Owns its persistence via a *sql.DB handle (cache.Conn()); pkg/cache does
//     not import this package (see R16 problem P4).
package attribution

import (
	"context"
	"time"

	"github.com/raghavkgarg/mycase/pkg/yfinance"
)

// DefaultBenchmark is the passive baseline for US portfolios: the SPY ETF,
// routed through Schwab. It is the honest "you could have bought this" baseline
// (unlike the ^GSPC index the backtest engine uses).
const DefaultBenchmark = "US:SPY"

// DefaultRiskFree is the annual risk-free rate used for US risk-adjusted metrics
// (fraction). Approximate short-Treasury yield.
const DefaultRiskFree = 0.045

// DefaultInitialCapital is used when Config.InitialCapital is unset.
const DefaultInitialCapital = 100000.0

// PriceFetcher sources historical price series. *datafetcher.Router satisfies it.
// Kept minimal (and defined here) so pkg/attribution does not import
// pkg/datafetcher, avoiding an import cycle and keeping the tracker mockable.
type PriceFetcher interface {
	FetchHistoricalByDateRange(ctx context.Context, ticker string, from, to time.Time) (*yfinance.HistoricalData, error)
}

// Holding is a ticker + target weight pair (weights should sum to 1.0).
type Holding struct {
	Ticker string
	Weight float64
}

// NAVPoint records portfolio and benchmark NAV on a single trading day.
// This is the unit persisted to the nav_history cache table.
type NAVPoint struct {
	Date           time.Time
	PortfolioValue float64
	BenchmarkValue float64
}

// Config parameterizes a NAV build.
type Config struct {
	InitialCapital float64        // starting capital (default DefaultInitialCapital if <= 0)
	From           time.Time      // series start (inclusive)
	To             time.Time      // series end (inclusive)
	Benchmark      string         // benchmark ticker (default DefaultBenchmark)
	RiskFree       float64        // annual risk-free rate, fraction (default DefaultRiskFree)
	Location       *time.Location // timezone for keying trading days (default America/New_York)
}

// withDefaults returns a copy of cfg with unset fields filled in.
func (c Config) withDefaults() Config {
	if c.InitialCapital <= 0 {
		c.InitialCapital = DefaultInitialCapital
	}
	if c.Benchmark == "" {
		c.Benchmark = DefaultBenchmark
	}
	if c.RiskFree == 0 {
		c.RiskFree = DefaultRiskFree
	}
	if c.Location == nil {
		if loc, err := time.LoadLocation("America/New_York"); err == nil {
			c.Location = loc
		} else {
			c.Location = time.UTC
		}
	}
	return c
}

// Result holds vs-benchmark performance metrics derived from a NAV series.
type Result struct {
	TradingDays      int
	From             time.Time
	To               time.Time
	InitialCapital   float64
	FinalValue       float64
	BenchmarkFinal   float64
	TotalReturn      float64 // portfolio, fraction
	BenchmarkReturn  float64 // benchmark, fraction
	CAGR             float64
	BenchmarkCAGR    float64
	Alpha            float64 // Jensen's alpha (annualized, fraction)
	Beta             float64
	InformationRatio float64 // annualized active-return / tracking-error
	TrackingError    float64 // annualized stddev of active daily returns, fraction
	MaxDrawdown      float64 // portfolio, fraction (negative)
	Sharpe           float64
}
