package datafetcher

import (
	"context"
	"encoding/json"

	"github.com/raghavkgarg/mycase/pkg/cache"
	"github.com/raghavkgarg/mycase/pkg/marketdata"
)

// Fundamentals caching for the US (Schwab+EDGAR) path lives here, in the Router,
// rather than in the Schwab client. The reason: Schwab fundamentals are only a
// base; the Router overlays SEC EDGAR statement facts (operating cash flow, net
// income, authoritative FCF, annual series) on top. Caching in the client would
// persist the pre-overlay blob (FCF=0 for US names), so a warm re-run would
// serve the unusable value and re-trip the FCF hard-filter. Caching the MERGED
// result here means a warm hit skips BOTH the Schwab and the EDGAR calls.
//
// The blob is the JSON-encoded marketdata.Fundamentals — the provider-agnostic
// leaf type the merger produces and consumes — identical in shape to what the
// Yahoo path caches, so both providers share one on-disk `fundamentals` table
// (distinguished by the `source` column and by the US:-prefixed ticker key).
// This file imports only the marketdata leaf, not yfinance: it needs the type,
// not the Yahoo provider (per the layering rule "import the leaf directly").
// Freshness is the cache's own 24h / market-EOD rule (marketcal.ClockForTicker
// → NYSE for US:).

// checkFundamentalsCache returns the cached merged fundamentals for a US ticker
// when a fresh row exists. A nil global cache (tests that never call SetCache)
// or any decode error is a miss, leaving the live fetch path unchanged.
func checkFundamentalsCache(ctx context.Context, ticker string) (marketdata.Fundamentals, bool) {
	db := cache.GetDB()
	if db == nil {
		return marketdata.Fundamentals{}, false
	}
	data, ok, err := db.GetFundamentalsJSON(ctx, ticker)
	if err != nil || !ok {
		return marketdata.Fundamentals{}, false
	}
	var f marketdata.Fundamentals
	if json.Unmarshal(data, &f) != nil {
		return marketdata.Fundamentals{}, false
	}
	return f, true
}

// storeFundamentalsCache persists a US ticker's merged fundamentals. source is
// the provenance tag from the merge ("schwab+edgar", "schwab", or "edgar").
func storeFundamentalsCache(ctx context.Context, ticker string, f marketdata.Fundamentals, source string) {
	db := cache.GetDB()
	if db == nil {
		return
	}
	data, err := json.Marshal(f)
	if err != nil {
		return
	}
	_ = db.StoreFundamentalsJSON(ctx, ticker, data, source)
}
