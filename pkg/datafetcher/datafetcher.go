package datafetcher

import (
	"context"
	"fmt"
	"strings"

	"github.com/raghavkgarg/mycase/pkg/broker"
	"github.com/raghavkgarg/mycase/pkg/yfinance"
)

// FetchMarketData retrieves stock price quotes (from yfinance with fallback to broker), holdings quantities, and detailed holdings metadata.
func FetchMarketData(ctx context.Context, b broker.Broker, basketKeys []string) (map[string]float64, map[string]int, map[string]broker.Holding, error) {
	var quoteData map[string]float64

	if b.IsMock() {
		fmt.Println("\n[MOCK] Fetching mock prices for basket instruments...")
		prices, _ := b.GetQuotes(basketKeys)
		quoteData = prices
		for _, inst := range basketKeys {
			fmt.Printf("[MOCK] %s Price: ₹%.2f\n", inst, quoteData[inst])
		}
	} else {
		fmt.Println("\nFetching real-time quotes via yfinance...")
		yfQuotes, err := yfinance.FetchQuotes(ctx, basketKeys)
		if err != nil {
			fmt.Printf("Failed to fetch some/all quotes using yfinance. Error: %v\n", err)
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
			fmt.Printf("Falling back to broker GetQuotes for %d missing key(s): %v...\n", len(missingKeys), missingKeys)
			kiteQuotes, err := b.GetQuotes(missingKeys)
			if err != nil {
				fmt.Printf("Broker GetQuotes fallback warning: %v\n", err)
			} else {
				for k, v := range kiteQuotes {
					quoteData[k] = v
				}
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
		currentHoldings[h.TradingSymbol] = h.Quantity + h.T1Quantity + h.T2Quantity
		holdingDetails[h.TradingSymbol] = h

		// Fallback for missing quote price using holding LastPrice
		if h.LastPrice > 0 {
			symKey := h.TradingSymbol
			if !strings.Contains(symKey, ":") {
				symKey = "NSE:" + symKey
			}
			if p, ok := quoteData[symKey]; !ok || p <= 0 {
				quoteData[symKey] = h.LastPrice
			}
			if p, ok := quoteData[h.TradingSymbol]; !ok || p <= 0 {
				quoteData[h.TradingSymbol] = h.LastPrice
			}
		}
	}

	// Print fetched prices and verify completeness
	var unpriced []string
	for _, inst := range basketKeys {
		if price, ok := quoteData[inst]; ok && price > 0 {
			fmt.Printf("Fetched %s Price: ₹%.2f\n", inst, price)
		} else {
			unpriced = append(unpriced, inst)
		}
	}

	if len(unpriced) > 0 {
		return nil, nil, nil, fmt.Errorf("failed to fetch prices for instrument(s): %s", strings.Join(unpriced, ", "))
	}

	return quoteData, currentHoldings, holdingDetails, nil
}
