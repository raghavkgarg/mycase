package datafetcher

import (
	"fmt"

	"github.com/raghavkgarg/mycase/pkg/portfolio"
	"github.com/raghavkgarg/mycase/pkg/yfinance"
	kiteconnect "github.com/zerodha/gokiteconnect/v4"
)

// FetchMarketData retrieves stock price quotes (from yfinance with fallback to Kite) and holdings.
func FetchMarketData(isMock bool, client *kiteconnect.Client, basketKeys []string) (map[string]float64, map[string]int, error) {
	quoteData := make(map[string]float64)

	if isMock {
		fmt.Println("\n[MOCK] Fetching mock prices for basket instruments...")
		quoteData["NSE:RELIANCE"] = 2450.0
		quoteData["NSE:TCS"] = 3200.0
		quoteData["NSE:INFY"] = 1420.0

		for _, inst := range basketKeys {
			if _, ok := quoteData[inst]; !ok {
				quoteData[inst] = 500.0
			}
		}
	} else {
		fmt.Println("\nFetching real-time quotes via yfinance...")
		yfQuotes, err := yfinance.FetchQuotes(basketKeys)
		if err == nil {
			quoteData = yfQuotes
			for _, inst := range basketKeys {
				fmt.Printf("Fetched %s Price: ₹%.2f\n", inst, quoteData[inst])
			}
		} else {
			fmt.Printf("Failed to fetch quotes using yfinance. Error: %v\n", err)
			fmt.Println("Falling back to Zerodha Kite quote API...")
			kiteQuote, err := client.GetQuote(basketKeys...)
			if err != nil {
				return nil, nil, fmt.Errorf("failed to fetch quotes from Zerodha Kite: %w", err)
			}
			for _, inst := range basketKeys {
				quoteData[inst] = kiteQuote[inst].LastPrice
				fmt.Printf("Fetched %s Price: ₹%.2f\n", inst, quoteData[inst])
			}
		}
	}

	currentHoldings := make(map[string]int)
	if isMock {
		currentHoldings["LT"] = 2
		currentHoldings["SWSOLAR"] = 15
		currentHoldings["ADVAIT"] = 10
		currentHoldings["INOXINDIA"] = 5
	} else {
		// Use portfolio helper to fetch and merge live T1, T2, and CNC positions
		rawHoldings, err := portfolio.FetchAndMergeHoldings(client, false)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to fetch merged holdings: %w", err)
		}
		for _, h := range rawHoldings {
			currentHoldings[h.TradingSymbol] = h.Quantity + h.T1Quantity + h.T2Quantity
		}
	}

	return quoteData, currentHoldings, nil
}
