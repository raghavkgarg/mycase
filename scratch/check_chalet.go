package main

import (
	"context"
	"fmt"

	"github.com/raghavkgarg/mycase/pkg/config"
	"github.com/raghavkgarg/mycase/pkg/selectiontracker"
	"github.com/raghavkgarg/mycase/pkg/stockpicker"
	"github.com/raghavkgarg/mycase/pkg/yfinance"
)

func main() {
	ctx := context.Background()
	ticker := "NSE:CHALET"

	cfg, err := config.LoadMFSConfig("config/mfs.json", "multibagger")
	if err != nil {
		fmt.Println("Err loading config:", err)
		return
	}
	hardFilters, _ := config.LoadHardFilters("config/mfs.json", "multibagger")

	fmt.Println("Fetching data for", ticker)
	fullHistory, activeKeys := stockpicker.FetchHistoricalPrices(ctx, []string{ticker})
	if len(activeKeys) == 0 {
		fmt.Println("Failed to fetch historical prices for", ticker)
		return
	}

	fundamentals, err := yfinance.FetchFundamentals(ctx, activeKeys)
	if err != nil {
		fmt.Println("Err fetching fundamentals:", err)
		return
	}

	tracker := selectiontracker.New()
	filtered := stockpicker.ApplySafetyFilters(ctx, activeKeys, "multibagger", hardFilters, fundamentals, fullHistory, tracker)

	fmt.Println("\nResult:")
	if len(filtered) > 0 {
		fmt.Println(ticker, "PASSED all safety filters!")
	} else {
		fmt.Println(ticker, "REJECTED by safety filters:")
		fmt.Println("Reason:", tracker.SafetyReasons[ticker])
	}

	_ = cfg
}
