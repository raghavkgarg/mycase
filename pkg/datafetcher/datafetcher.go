package datafetcher

import (
	"context"
	"fmt"

	"github.com/raghavkgarg/mycase/pkg/broker"
	"github.com/raghavkgarg/mycase/pkg/yfinance"
)

// FetchMarketData retrieves stock price quotes (from yfinance with fallback to broker) and holdings.
func FetchMarketData(ctx context.Context, b broker.Broker, basketKeys []string) (map[string]float64, map[string]int, error) {
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
		if err == nil {
			quoteData = yfQuotes
			for _, inst := range basketKeys {
				fmt.Printf("Fetched %s Price: ₹%.2f\n", inst, quoteData[inst])
			}
		} else {
			fmt.Printf("Failed to fetch quotes using yfinance. Error: %v\n", err)
			fmt.Println("Falling back to Zerodha Kite quote API...")
			kiteQuotes, err := b.GetQuotes(basketKeys)
			if err != nil {
				return nil, nil, fmt.Errorf("failed to fetch quotes: %w", err)
			}
			quoteData = kiteQuotes
			for _, inst := range basketKeys {
				fmt.Printf("Fetched %s Price: ₹%.2f\n", inst, quoteData[inst])
			}
		}
	}

	rawHoldings, err := b.GetHoldings()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to fetch holdings: %w", err)
	}
	currentHoldings := make(map[string]int, len(rawHoldings))
	for _, h := range rawHoldings {
		currentHoldings[h.TradingSymbol] = h.Quantity + h.T1Quantity + h.T2Quantity
	}

	return quoteData, currentHoldings, nil
}
