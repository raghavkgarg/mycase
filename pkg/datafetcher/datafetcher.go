package datafetcher

import (
	"context"
	"fmt"
	"maps"

	"log/slog"
	"strings"

	"github.com/raghavkgarg/mycase/pkg/broker"
	"github.com/raghavkgarg/mycase/pkg/portfolio"
	"github.com/raghavkgarg/mycase/pkg/yfinance"
)

// FetchMarketData retrieves stock price quotes (from yfinance with fallback to broker), holdings quantities, and detailed holdings metadata.
func FetchMarketData(ctx context.Context, b broker.Broker, basketKeys []string) (map[string]float64, map[string]int, map[string]broker.Holding, error) {
	var quoteData map[string]float64

	if b.IsMock() {
		slog.DebugContext(ctx, "quotes.mock_fetch", "count", len(basketKeys))
		prices, _ := b.GetQuotes(basketKeys)
		quoteData = prices
		for _, inst := range basketKeys {
			slog.DebugContext(ctx, "quotes.mock_price", "ticker", inst, "price", quoteData[inst])
		}
	} else {
		slog.DebugContext(ctx, "quotes.fetch_start", "source", "yfinance", "count", len(basketKeys))
		yfQuotes, err := yfinance.FetchQuotes(ctx, basketKeys)
		if err != nil {
			slog.WarnContext(ctx, "quotes.fetch_partial_failure", "source", "yfinance", "err", err)
		}
		if yfQuotes != nil {
			quoteData = yfQuotes
		} else {
			quoteData = make(map[string]float64)
		}

		// Determine missing tickers
		var missingKeys []string
		for _, inst := range basketKeys {
			if _, ok := quoteData[inst]; !ok {
				missingKeys = append(missingKeys, inst)
			}
		}

		if len(missingKeys) > 0 {
			slog.WarnContext(ctx, "quotes.broker_fallback", "count", len(missingKeys), "tickers", strings.Join(missingKeys, ","))
			kiteQuotes, err := b.GetQuotes(missingKeys)
			if err != nil {
				slog.WarnContext(ctx, "quotes.broker_fallback_failure", "err", err)
			} else {
				maps.Copy(quoteData, kiteQuotes)
			}
		}
	}

	rawHoldings, err := b.GetHoldings()
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to fetch holdings: %w", err)
	}
	currentHoldings := make(map[string]int, len(rawHoldings))
	holdingDetails := make(map[string]broker.Holding, len(rawHoldings))
	for _, h := range rawHoldings {
		baseSym := portfolio.StripSeriesSuffix(h.TradingSymbol)
		qty := h.Quantity + h.T1Quantity + h.T2Quantity
		currentHoldings[h.TradingSymbol] = qty
		currentHoldings[baseSym] = qty
		holdingDetails[h.TradingSymbol] = h
		holdingDetails[baseSym] = h

		// Fallback for missing quote price using holding LastPrice
		if h.LastPrice > 0 {
			symKey := h.TradingSymbol
			if !strings.Contains(symKey, ":") {
				symKey = "NSE:" + symKey
			}
			baseKey := "NSE:" + baseSym
			if p, ok := quoteData[symKey]; !ok || p <= 0 {
				quoteData[symKey] = h.LastPrice
			}
			if p, ok := quoteData[baseKey]; !ok || p <= 0 {
				quoteData[baseKey] = h.LastPrice
			}
			if p, ok := quoteData[h.TradingSymbol]; !ok || p <= 0 {
				quoteData[h.TradingSymbol] = h.LastPrice
			}
			if p, ok := quoteData[baseSym]; !ok || p <= 0 {
				quoteData[baseSym] = h.LastPrice
			}
		}
	}

	// Print fetched prices and verify completeness
	var unpriced []string
	for _, inst := range basketKeys {
		if price, ok := quoteData[inst]; ok && price > 0 {
			slog.DebugContext(ctx, "quotes.priced", "ticker", inst, "price", price)
		} else {
			unpriced = append(unpriced, inst)
		}
	}

	if len(unpriced) > 0 {
		return nil, nil, nil, fmt.Errorf("failed to fetch prices for instrument(s): %s", strings.Join(unpriced, ", "))
	}

	return quoteData, currentHoldings, holdingDetails, nil
}
