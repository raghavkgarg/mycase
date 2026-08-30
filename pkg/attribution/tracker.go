package attribution

import (
	"context"
	"fmt"
	"math"
	"sort"
	"time"

	"log/slog"

	"github.com/raghavkgarg/mycase/pkg/backtest"
)

// annualizationDays is the trading-day count used to annualize daily stats.
const annualizationDays = 252

// Tracker builds daily NAV series and computes vs-benchmark attribution.
type Tracker struct {
	fetcher PriceFetcher
	log     *slog.Logger
}

// NewTracker constructs a Tracker. If log is nil, slog.Default() is used.
func NewTracker(fetcher PriceFetcher, log *slog.Logger) *Tracker {
	if log == nil {
		log = slog.Default()
	}
	return &Tracker{fetcher: fetcher, log: log}
}

// BuildNAVSeries computes the daily portfolio and benchmark NAV over [From, To].
//
// Prices are sourced through the PriceFetcher (US tickers → Schwab, else Yahoo).
// The series is valued only on the intersection of trading days for which the
// benchmark AND every held ticker have a close, avoiding fabricated returns from
// misaligned calendars. A ticker whose fetch fails is skipped with a warning and
// its weight is dropped (renormalization happens implicitly per-day via the
// share counts) rather than aborting the whole run (API discipline: fail one,
// continue).
func (t *Tracker) BuildNAVSeries(ctx context.Context, holdings []Holding, cfg Config) ([]NAVPoint, error) {
	cfg = cfg.withDefaults()
	if len(holdings) == 0 {
		return nil, fmt.Errorf("no holdings provided")
	}
	if !cfg.From.Before(cfg.To) {
		return nil, fmt.Errorf("invalid range: from %s not before to %s", cfg.From.Format("2006-01-02"), cfg.To.Format("2006-01-02"))
	}

	// Fetch benchmark first — it is required.
	benchClose, err := t.fetchCloseByDay(ctx, cfg.Benchmark, cfg.From, cfg.To, cfg.Location)
	if err != nil {
		return nil, fmt.Errorf("fetch benchmark %s: %w", cfg.Benchmark, err)
	}
	if len(benchClose) == 0 {
		return nil, fmt.Errorf("benchmark %s returned no prices", cfg.Benchmark)
	}

	// Fetch each holding; skip failures with a warning.
	type held struct {
		Holding
		close map[string]float64
	}
	var kept []held
	for _, h := range holdings {
		if h.Weight <= 0 {
			continue
		}
		byDay, ferr := t.fetchCloseByDay(ctx, h.Ticker, cfg.From, cfg.To, cfg.Location)
		if ferr != nil || len(byDay) == 0 {
			t.log.WarnContext(ctx, "nav.ticker_skipped",
				"ticker", h.Ticker, "reason", skipReason(ferr))
			continue
		}
		kept = append(kept, held{Holding: h, close: byDay})
	}
	if len(kept) == 0 {
		return nil, fmt.Errorf("no holdings had usable price data")
	}

	// Renormalize kept weights to sum to 1.
	var wsum float64
	for _, h := range kept {
		wsum += h.Weight
	}
	if wsum <= 0 {
		return nil, fmt.Errorf("kept holdings have non-positive total weight")
	}

	// Common trading days: intersection of benchmark and every kept ticker.
	days := make([]string, 0, len(benchClose))
	for day := range benchClose {
		ok := true
		for _, h := range kept {
			if _, has := h.close[day]; !has {
				ok = false
				break
			}
		}
		if ok {
			days = append(days, day)
		}
	}
	if len(days) < 2 {
		return nil, fmt.Errorf("fewer than 2 common trading days across holdings + benchmark")
	}
	sort.Strings(days) // YYYY-MM-DD sorts chronologically

	// Buy at first common day's close; hold (no rebalancing in the NAV series —
	// this reflects the *actual held* portfolio drift, which is what we compare
	// against the benchmark).
	first := days[0]
	shares := make([]float64, len(kept))
	for i, h := range kept {
		alloc := cfg.InitialCapital * (h.Weight / wsum)
		shares[i] = alloc / h.close[first]
	}
	benchUnits := cfg.InitialCapital / benchClose[first]

	points := make([]NAVPoint, 0, len(days))
	for _, day := range days {
		var pv float64
		for i, h := range kept {
			pv += shares[i] * h.close[day]
		}
		d, _ := time.ParseInLocation("2006-01-02", day, cfg.Location)
		points = append(points, NAVPoint{
			Date:           d,
			PortfolioValue: pv,
			BenchmarkValue: benchUnits * benchClose[day],
		})
	}

	t.log.InfoContext(ctx, "nav.built",
		"holdings", len(kept), "skipped", len(holdings)-len(kept),
		"days", len(points), "benchmark", cfg.Benchmark,
		"from", points[0].Date.Format("2006-01-02"),
		"to", points[len(points)-1].Date.Format("2006-01-02"))

	return points, nil
}

// Attribution derives vs-benchmark metrics from a NAV series.
func Attribution(points []NAVPoint, riskFree float64) Result {
	if riskFree == 0 {
		riskFree = DefaultRiskFree
	}
	res := Result{TradingDays: len(points)}
	if len(points) < 2 {
		return res
	}

	portVals := make([]float64, len(points))
	benchVals := make([]float64, len(points))
	for i, p := range points {
		portVals[i] = p.PortfolioValue
		benchVals[i] = p.BenchmarkValue
	}

	res.From = points[0].Date
	res.To = points[len(points)-1].Date
	res.InitialCapital = portVals[0]
	res.FinalValue = portVals[len(portVals)-1]
	res.BenchmarkFinal = benchVals[len(benchVals)-1]

	if portVals[0] > 0 {
		res.TotalReturn = (res.FinalValue - portVals[0]) / portVals[0]
	}
	if benchVals[0] > 0 {
		res.BenchmarkReturn = (res.BenchmarkFinal - benchVals[0]) / benchVals[0]
	}

	calDays := int(res.To.Sub(res.From).Hours()/24) + 1
	res.CAGR = backtest.CalcCAGR(portVals[0], res.FinalValue, calDays)
	res.BenchmarkCAGR = backtest.CalcCAGR(benchVals[0], res.BenchmarkFinal, calDays)
	res.Beta = backtest.CalcBeta(portVals, benchVals)
	res.Alpha = backtest.CalcAlphaRF(res.CAGR, res.BenchmarkCAGR, res.Beta, riskFree)
	res.MaxDrawdown = backtest.CalcMaxDrawdown(portVals)
	res.Sharpe = backtest.CalcSharpeRF(portVals, riskFree)

	// Information ratio: annualized mean active return / annualized tracking error.
	portRets := simpleReturns(portVals)
	benchRets := simpleReturns(benchVals)
	n := min(len(portRets), len(benchRets))
	if n >= 2 {
		active := make([]float64, n)
		for i := range n {
			active[i] = portRets[i] - benchRets[i]
		}
		m := meanOf(active)
		te := stddevOf(active, m)
		res.TrackingError = te * math.Sqrt(annualizationDays)
		if te > 0 {
			res.InformationRatio = (m * annualizationDays) / res.TrackingError
		}
	}
	return res
}

// fetchCloseByDay fetches a ticker's daily closes and returns a YYYY-MM-DD → close
// map keyed in the given location. The last data point is not trimmed; callers
// bound the range via [from, to].
func (t *Tracker) fetchCloseByDay(ctx context.Context, ticker string, from, to time.Time, loc *time.Location) (map[string]float64, error) {
	hist, err := t.fetcher.FetchHistoricalByDateRange(ctx, ticker, from, to)
	if err != nil {
		return nil, err
	}
	if hist == nil || len(hist.Closes) == 0 {
		return map[string]float64{}, nil
	}
	byDay := make(map[string]float64, len(hist.Closes))
	for i, ts := range hist.Timestamps {
		if i >= len(hist.Closes) {
			break
		}
		c := hist.Closes[i]
		if c <= 0 {
			continue
		}
		day := time.Unix(ts, 0).In(loc).Format("2006-01-02")
		byDay[day] = c // later value for a day wins (handles duplicate ts)
	}
	return byDay, nil
}

func skipReason(err error) string {
	if err != nil {
		return err.Error()
	}
	return "no price data"
}

// simpleReturns computes day-over-day fractional returns.
func simpleReturns(values []float64) []float64 {
	if len(values) < 2 {
		return nil
	}
	ret := make([]float64, len(values)-1)
	for i := 1; i < len(values); i++ {
		if values[i-1] > 0 {
			ret[i-1] = (values[i] - values[i-1]) / values[i-1]
		}
	}
	return ret
}

func meanOf(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	s := 0.0
	for _, x := range xs {
		s += x
	}
	return s / float64(len(xs))
}

func stddevOf(xs []float64, m float64) float64 {
	if len(xs) < 2 {
		return 0
	}
	s := 0.0
	for _, x := range xs {
		d := x - m
		s += d * d
	}
	return math.Sqrt(s / float64(len(xs)-1))
}
