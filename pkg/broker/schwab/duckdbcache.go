package schwab

import (
	"context"
	"time"

	"github.com/raghavkgarg/mycase/pkg/cache"
	"github.com/raghavkgarg/mycase/pkg/marketdata"
)

// globalCache is the DuckDB serving cache for Schwab-sourced US data. It is set
// once at startup via SetCache (from main / cmd/serve), mirroring how yfinance
// wires the same cache for the India/Yahoo path. When nil (e.g. in unit tests
// that never call SetCache), every helper degrades to a miss/no-op so the
// client behaves exactly as it did before caching existed.
var globalCache *cache.Cache

// sourceSchwab is the provenance tag stored for data this client caches.
const sourceSchwab = "schwab"

// SetCache wires a DuckDB cache into the Schwab client for transparent
// price/fundamentals caching, so US re-runs serve warm instead of re-hitting
// the Schwab API (the "cache is truth during a session" rule in docs/api-rules.md).
// Symmetric with yfinance.SetCache; both write into the same singleton so the
// two providers share one on-disk cache, distinguished by the `source` column
// and by ticker prefix (US:/NYSE:/NASDAQ: for Schwab, unprefixed for Yahoo).
func SetCache(c *cache.Cache) {
	globalCache = c
	cache.SetGlobal(c)
}

// cacheKey builds the ticker key used for cache rows. Schwab fetch methods take
// a bare symbol (the US: prefix is stripped before the API call), but the cache
// is keyed by the full prefixed ticker so US and India rows never collide and
// the market-aware freshness clock (marketcal.ClockForTicker) picks NYSE.
func cacheKey(symbol string) string { return "US:" + symbol }

func checkPriceCache(ctx context.Context, symbol, rangeKey string) (*marketdata.HistoricalData, bool) {
	if globalCache == nil {
		return nil, false
	}
	records, fresh, err := globalCache.GetPrices(ctx, cacheKey(symbol), rangeKey)
	if err != nil || !fresh || len(records) == 0 {
		return nil, false
	}
	return recordsToHist(records), true
}

func storePriceCache(ctx context.Context, symbol, rangeKey string, hist *marketdata.HistoricalData) {
	if globalCache == nil || hist == nil {
		return
	}
	_ = globalCache.StorePrices(ctx, cacheKey(symbol), rangeKey, histToRecords(hist))
}

func checkDateRangeCache(ctx context.Context, symbol string, from, to time.Time) (*marketdata.HistoricalData, bool) {
	if globalCache == nil {
		return nil, false
	}
	records, fresh, err := globalCache.GetPricesByDateRange(ctx, cacheKey(symbol), from, to)
	if err != nil || !fresh || len(records) == 0 {
		return nil, false
	}
	return recordsToHist(records), true
}

func storeDateRangeCache(ctx context.Context, symbol string, from, to time.Time, hist *marketdata.HistoricalData) {
	if globalCache == nil || hist == nil {
		return
	}
	_ = globalCache.StorePricesByDateRange(ctx, cacheKey(symbol), from, to, histToRecords(hist))
}

// NOTE: fundamentals are deliberately NOT cached here. Schwab fundamentals are
// only a base; the datafetcher.Router overlays SEC EDGAR statement facts
// (operating cash flow, net income, authoritative FCF, annual series) AFTER
// this client returns. Caching at this layer would persist the pre-overlay blob
// (FCF=0 for US names), so a warm re-run would serve the unusable value and
// re-trip the FCF hard-filter. The merged result is cached one layer up, in the
// router, keyed the same way (US:<symbol>) — see pkg/datafetcher.

func recordsToHist(records []cache.PriceRecord) *marketdata.HistoricalData {
	hist := &marketdata.HistoricalData{
		Timestamps: make([]int64, len(records)),
		Closes:     make([]float64, len(records)),
		Opens:      make([]float64, len(records)),
		Volumes:    make([]float64, len(records)),
	}
	for i, r := range records {
		hist.Timestamps[i] = r.Timestamp
		hist.Closes[i] = r.Close
		hist.Opens[i] = r.Open
		hist.Volumes[i] = r.Volume
	}
	return hist
}

func histToRecords(hist *marketdata.HistoricalData) []cache.PriceRecord {
	records := make([]cache.PriceRecord, len(hist.Timestamps))
	for i, ts := range hist.Timestamps {
		records[i] = cache.PriceRecord{
			Timestamp: ts,
			Close:     hist.Closes[i],
			Open:      hist.Opens[i],
			Volume:    hist.Volumes[i],
			Source:    sourceSchwab,
		}
	}
	return records
}
