package yfinance

import (
	"context"

	"golang.org/x/time/rate"
)

// Yahoo Finance publishes no official rate limit; the community-observed ceiling
// is roughly 2000 requests/hour, and aggressive bursts get 429s. The package
// fans out through fixed-size worker pools (15 for fundamentals, 10 for quotes)
// which bound *concurrency* but not request *rate* — several pools can overlap
// and each worker issues multiple requests per ticker back-to-back. A single
// process-wide token bucket bounds the aggregate rate across all pools and all
// callers, smoothing bursts well under the 429 threshold while staying responsive
// for interactive use.
//
// Defaults: 10 req/s sustained with a burst of 20. That is far below the ~0.55/s
// hourly-average ceiling only if fully saturated for an hour; in practice runs
// are short and bursty, so a 10/s cap prevents the instantaneous bursts that
// trigger 429s without throttling normal quarterly-rebalance fetches.
const (
	yahooRatePerSecond = 10
	yahooRateBurst     = 20
)

// limiter is the process-wide Yahoo Finance rate limiter. It is package-level
// state (like globalCache) because yfinance exposes package-level functions with
// no shared client object. Overridable via SetRateLimiter for tests/tuning.
var limiter = rate.NewLimiter(rate.Limit(yahooRatePerSecond), yahooRateBurst)

// SetRateLimiter overrides the process-wide Yahoo Finance rate limiter. Intended
// for tests (e.g. an "infinite" limiter to disable pacing) and for callers that
// want to tune pacing at startup. A nil argument is ignored.
func SetRateLimiter(l *rate.Limiter) {
	if l != nil {
		limiter = l
	}
}

// waitRate blocks until the rate limiter admits one request or ctx is done.
// Every outbound Yahoo request funnels through here (via executeYFinanceRequest
// and the fundamentals timeseries call) so the aggregate request rate is bounded.
func waitRate(ctx context.Context) error {
	return limiter.Wait(ctx)
}
