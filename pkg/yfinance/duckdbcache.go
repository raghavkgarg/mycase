package yfinance

import (
	"context"
	"encoding/json"
	"time"

	"github.com/raghavkgarg/mycase/pkg/cache"
)

var globalCache *cache.Cache

// SetCache wires a DuckDB cache into the yfinance package for transparent
// price/fundamentals caching. Also sets the global cache singleton.
func SetCache(c *cache.Cache) {
	globalCache = c
	cache.SetGlobal(c)
}

// GetCache returns the active cache (nil if not set).
// Deprecated: use cache.GetDB() for non-yfinance access.
func GetCache() *cache.Cache { return globalCache }

func checkPriceCache(ctx context.Context, ticker, rangeKey string) (*HistoricalData, bool) {
	if globalCache == nil {
		return nil, false
	}
	records, fresh, err := globalCache.GetPrices(ctx, ticker, rangeKey)
	if err != nil || !fresh || len(records) == 0 {
		return nil, false
	}
	hist := &HistoricalData{
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
	return hist, true
}

func storePriceCache(ctx context.Context, ticker, rangeKey string, hist *HistoricalData) {
	if globalCache == nil || hist == nil {
		return
	}
	records := make([]cache.PriceRecord, len(hist.Timestamps))
	for i, ts := range hist.Timestamps {
		records[i] = cache.PriceRecord{
			Timestamp: ts,
			Close:     hist.Closes[i],
			Open:      hist.Opens[i],
			Volume:    hist.Volumes[i],
		}
	}
	_ = globalCache.StorePrices(ctx, ticker, rangeKey, records)
}

func checkDateRangeCache(ctx context.Context, ticker string, from, to time.Time) (*HistoricalData, bool) {
	if globalCache == nil {
		return nil, false
	}
	records, fresh, err := globalCache.GetPricesByDateRange(ctx, ticker, from, to)
	if err != nil || !fresh || len(records) == 0 {
		return nil, false
	}
	hist := &HistoricalData{
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
	return hist, true
}

func storeDateRangeCache(ctx context.Context, ticker string, from, to time.Time, hist *HistoricalData) {
	if globalCache == nil || hist == nil {
		return
	}
	records := make([]cache.PriceRecord, len(hist.Timestamps))
	for i, ts := range hist.Timestamps {
		records[i] = cache.PriceRecord{
			Timestamp: ts,
			Close:     hist.Closes[i],
			Open:      hist.Opens[i],
			Volume:    hist.Volumes[i],
		}
	}
	_ = globalCache.StorePricesByDateRange(ctx, ticker, from, to, records)
}

func checkFundamentalsCache(ctx context.Context, ticker string) (*Fundamentals, bool) {
	if globalCache == nil {
		return nil, false
	}
	data, fresh, err := globalCache.GetFundamentalsJSON(ctx, ticker)
	if err != nil || !fresh {
		return nil, false
	}
	var f Fundamentals
	if json.Unmarshal(data, &f) != nil {
		return nil, false
	}
	return &f, true
}

func storeFundamentalsCache(ctx context.Context, ticker string, f *Fundamentals) {
	if globalCache == nil || f == nil {
		return
	}
	data, err := json.Marshal(f)
	if err != nil {
		return
	}
	_ = globalCache.StoreFundamentalsJSON(ctx, ticker, data)
}
