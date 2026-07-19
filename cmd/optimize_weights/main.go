package main

import (
	"encoding/csv"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gkgarg24/mycase/pkg/config"
	"github.com/gkgarg24/mycase/pkg/csvloader"
	"github.com/gkgarg24/mycase/pkg/optimizer"
	"github.com/gkgarg24/mycase/pkg/yfinance"
)

func main() {
	method := flag.String("method", "multifactor", "Weighting method (volatility or multifactor)")
	basketPath := flag.String("file", "data/basket.csv", "Path to basket CSV")
	removeTickers := flag.String("remove", "", "Comma-separated list of tickers to remove (e.g. NSE:FCL,NSE:PARACABLES)")
	rangeStr := flag.String("range", "3mo", "Yahoo Finance range for volatility calculation (e.g. 3mo, 6mo, 1y)")
	capFlag := flag.Float64("cap", 0.10, "Maximum weight limit for any single stock (e.g. 0.10 for 10%)")
	promote := flag.Bool("promote", false, "Promote optimized weights directly to golden copy (source file) with auto-backup")
	goldenPath := flag.String("golden", "", "Path to the live golden copy CSV to identify and sell exited stocks")
	topN := flag.Int("top", 20, "Number of top stocks to retain (default 20, use 0 or negative to retain all)")
	flag.Parse()

	// Sanitize and validate range
	*rangeStr = strings.ToLower(strings.TrimSpace(*rangeStr))
	if *rangeStr == "1yr" || *rangeStr == "1year" {
		*rangeStr = "1y"
	}
	if *rangeStr != "3mo" && *rangeStr != "6mo" && *rangeStr != "1y" {
		fmt.Printf("Error: Unsupported range '%s'. Supported ranges: 3mo, 6mo, 1y\n", *rangeStr)
		return
	}

	// 1. Load target basket weights
	basket, basketKeys, err := csvloader.LoadBasketCSV(*basketPath)
	if err != nil {
		fmt.Printf("Error loading basket config: %v\n", err)
		return
	}

	// 2. Identify tickers to remove
	toRemove := make(map[string]bool)
	if *removeTickers != "" {
		for _, t := range strings.Split(*removeTickers, ",") {
			toRemove[strings.TrimSpace(t)] = true
		}
	}

	// Identify exited stocks from the live golden copy
	if *goldenPath != "" {
		goldenBasket, _, err := csvloader.LoadBasketCSV(*goldenPath)
		if err == nil {
			for k, w := range goldenBasket {
				if w > 0.00001 {
					found := false
					for _, bk := range basketKeys {
						if bk == k {
							found = true
							break
						}
					}
					if !found && !toRemove[k] {
						toRemove[k] = true
						fmt.Printf("Exit detected: Ticker %s was active in golden copy but is not in new selection. Setting weight to 0.0000 to trigger liquidation.\n", k)
					}
				}
			}
		} else {
			fmt.Printf("Warning: Could not load golden copy %s: %v\n", *goldenPath, err)
		}
	}

	// 3. Filter tickers
	var activeKeys []string
	for _, k := range basketKeys {
		if !toRemove[k] {
			activeKeys = append(activeKeys, k)
		} else {
			fmt.Printf("Filtering out ticker: %s\n", k)
		}
	}

	if len(activeKeys) == 0 {
		fmt.Println("No active tickers remaining to optimize!")
		return
	}

	// 4. Fetch historical prices concurrently
	fmt.Printf("\nFetching historical prices (%s) for %d tickers...\n", *rangeStr, len(activeKeys))
	priceHistory := make(map[string][]float64)
	var mu sync.Mutex
	var wg sync.WaitGroup

	for _, ticker := range activeKeys {
		wg.Add(1)
		go func(t string) {
			defer wg.Done()
			prices, err := yfinance.FetchHistoricalPrices(t, *rangeStr)
			if err != nil {
				fmt.Printf("Warning: Failed to fetch historical prices for %s: %v. Using fallback.\n", t, err)
				return
			}
			mu.Lock()
			priceHistory[t] = prices
			mu.Unlock()
		}(ticker)
	}
	wg.Wait()

	// Fetch benchmark prices for Nifty 50 if method is any strategy (not "volatility")
	var benchmarkPrices []float64
	if *method != "volatility" {
		fmt.Printf("Fetching historical benchmark prices for ^NSEI (%s)...\n", *rangeStr)
		var err error
		benchmarkPrices, err = yfinance.FetchHistoricalPrices("^NSEI", *rangeStr)
		if err != nil {
			fmt.Printf("Warning: Failed to fetch benchmark ^NSEI: %v. Falling back to volatility method.\n", err)
			*method = "volatility"
		}
	}

	// 5. Run optimizer
	var newWeights map[string]float64
	var fundamentals map[string]yfinance.Fundamentals
	if *method != "volatility" {
		mfsCfg, err := config.LoadMFSConfig("config/mfs.json", *method)
		if err != nil {
			fmt.Printf("Warning: Failed to load config/mfs.json: %v. Using defaults.\n", err)
		}
		optWeights := optimizer.MFSWeights{
			Sharpe:           mfsCfg.Sharpe,
			Sortino:          mfsCfg.Sortino,
			Return:           mfsCfg.Return,
			Alpha:            mfsCfg.Alpha,
			Volatility:       mfsCfg.Volatility,
			Beta:             mfsCfg.Beta,
			Treynor:          mfsCfg.Treynor,
			Ulcer:            mfsCfg.Ulcer,
			PEGRatio:         mfsCfg.PEGRatio,
			ROE:              mfsCfg.ROE,
			ForwardPE:        mfsCfg.ForwardPE,
			OperatingMargins: mfsCfg.OperatingMargins,
			PBRatio:          mfsCfg.PBRatio,
			NetDebtEBITDA:    mfsCfg.NetDebtEBITDA,
			MarketCap:        mfsCfg.MarketCap,
			InsidersPercent:  mfsCfg.InsidersPercent,
		}

		// Fetch fundamentals
		fmt.Printf("Fetching fundamentals from Yahoo Finance...\n")
		var err2 error
		fundamentals, err2 = yfinance.FetchFundamentals(activeKeys)
		if err2 != nil {
			fmt.Printf("Warning: Failed to fetch fundamentals: %v. Using fallbacks.\n", err2)
		}

		newWeights = optimizer.OptimizeMultiFactor(activeKeys, priceHistory, benchmarkPrices, fundamentals, optWeights)
	} else {
		newWeights = optimizer.OptimizeInverseVolatility(activeKeys, priceHistory)
	}

	// 5.5 Prune to top N if specified
	if *topN > 0 && len(activeKeys) > *topN {
		fmt.Printf("\nPruning selection from %d to top %d stocks based on optimized weights...\n", len(activeKeys), *topN)

		// Sort active keys by initial weights descending
		sort.Slice(activeKeys, func(i, j int) bool {
			return newWeights[activeKeys[i]] > newWeights[activeKeys[j]]
		})

		// Add pruned tickers to toRemove, and keep only the top N in activeKeys
		prunedKeys := activeKeys[*topN:]
		activeKeys = activeKeys[:*topN]

		for _, k := range prunedKeys {
			toRemove[k] = true
			fmt.Printf("Pruned ticker: %s (initial weight: %.4f)\n", k, newWeights[k])
		}

		// Re-run optimization on the pruned top N activeKeys
		fmt.Printf("Re-running weight optimization for the top %d selected stocks...\n", *topN)

		// Clean the priceHistory to only contain the top N
		prunedPriceHistory := make(map[string][]float64)
		for _, t := range activeKeys {
			prunedPriceHistory[t] = priceHistory[t]
		}
		priceHistory = prunedPriceHistory

		if *method != "volatility" {
			mfsCfg, _ := config.LoadMFSConfig("config/mfs.json", *method)
			optWeights := optimizer.MFSWeights{
				Sharpe:           mfsCfg.Sharpe,
				Sortino:          mfsCfg.Sortino,
				Return:           mfsCfg.Return,
				Alpha:            mfsCfg.Alpha,
				Volatility:       mfsCfg.Volatility,
				Beta:             mfsCfg.Beta,
				Treynor:          mfsCfg.Treynor,
				Ulcer:            mfsCfg.Ulcer,
				PEGRatio:         mfsCfg.PEGRatio,
				ROE:              mfsCfg.ROE,
				ForwardPE:        mfsCfg.ForwardPE,
				OperatingMargins: mfsCfg.OperatingMargins,
				PBRatio:          mfsCfg.PBRatio,
				NetDebtEBITDA:    mfsCfg.NetDebtEBITDA,
				MarketCap:        mfsCfg.MarketCap,
				InsidersPercent:  mfsCfg.InsidersPercent,
			}
			newWeights = optimizer.OptimizeMultiFactor(activeKeys, priceHistory, benchmarkPrices, fundamentals, optWeights)
		} else {
			newWeights = optimizer.OptimizeInverseVolatility(activeKeys, priceHistory)
		}
	}

	// Apply capping limit (e.g. 10%)
	if *capFlag > 0 {
		newWeights = CapWeights(newWeights, *capFlag)
	}

	// 6. Sort remaining active keys by new weights descending for display
	displayKeys := append([]string{}, activeKeys...)
	sort.Slice(displayKeys, func(i, j int) bool {
		return newWeights[displayKeys[i]] > newWeights[displayKeys[j]]
	})

	// 7. Render output comparison table
	fmt.Println("\n==========================================================")
	if *method != "volatility" {
		fmt.Printf("             MULTI-FACTOR WEIGHTS COMPARISON (%s)        \n", strings.ToUpper(*method))
	} else {
		fmt.Println("             INVERSE-VOLATILITY WEIGHTS COMPARISON        ")
	}
	fmt.Println("==========================================================")
	fmt.Printf("%-16s | %-12s | %-12s | %-10s\n", "Ticker", "Old Weight", "New Weight", "Change")
	fmt.Println("----------------------------------------------------------")

	var totalNewWeight float64
	for _, t := range displayKeys {
		oldWt := basket[t]
		newWt := newWeights[t]
		diff := newWt - oldWt
		totalNewWeight += newWt
		fmt.Printf("%-16s | %-12.4f | %-12.4f | %-+10.4f\n", t, oldWt, newWt, diff)
	}
	fmt.Println("----------------------------------------------------------")
	fmt.Printf("%-16s | %-12s | %-12.4f |\n", "Total Weight", "", totalNewWeight)
	fmt.Println("==========================================================")

	// 8. Write back to CSV
	outPath := *basketPath
	if !*promote {
		dir := filepath.Dir(*basketPath)
		base := filepath.Base(*basketPath)
		ext := filepath.Ext(base)
		nameWithoutExt := strings.TrimSuffix(base, ext)
		if strings.HasPrefix(strings.ToLower(base), "stockpicker_") {
			outPath = filepath.Join(dir, "optimized_"+strings.TrimPrefix(strings.ToLower(base), "stockpicker_"))
		} else {
			outPath = filepath.Join(dir, nameWithoutExt+"_optim"+ext)
		}
		fmt.Printf("\nGolden copy protection: Output will be saved to %s (source %s remains unchanged)\n", outPath, *basketPath)
	} else {
		// Create a backup of the source file before overwriting it
		dir := filepath.Dir(*basketPath)
		base := filepath.Base(*basketPath)
		backupDir := filepath.Join(dir, "backups")
		if err := os.MkdirAll(backupDir, 0755); err != nil {
			fmt.Printf("Warning: Failed to create backups directory: %v\n", err)
		}
		timestamp := time.Now().Format("060102_150405")
		backupPath := filepath.Join(backupDir, fmt.Sprintf("%s_backup_%s.csv", strings.TrimSuffix(base, filepath.Ext(base)), timestamp))
		
		input, err := os.ReadFile(*basketPath)
		if err == nil {
			err = os.WriteFile(backupPath, input, 0644)
			if err == nil {
				fmt.Printf("\nBackup created successfully at %s\n", backupPath)
			}
		}
		if err != nil {
			fmt.Printf("Warning: Failed to create backup: %v\n", err)
		}
		fmt.Printf("\nPromoting weights: Overwriting golden copy %s\n", *basketPath)
	}

	outFile, err := os.Create(outPath)
	if err != nil {
		fmt.Printf("Error creating output file: %v\n", err)
		return
	}
	defer outFile.Close()

	writer := csv.NewWriter(outFile)
	if err := writer.Write([]string{"ticker", "weight"}); err != nil {
		fmt.Printf("Error writing header: %v\n", err)
		return
	}

	// Write active tickers with optimized weights
	for _, t := range displayKeys {
		weightStr := fmt.Sprintf("%.4f", newWeights[t])
		if err := writer.Write([]string{t, weightStr}); err != nil {
			fmt.Printf("Error writing record: %v\n", err)
			return
		}
	}

	// Write zero-weight tickers so the rebalancer triggers sell orders
	for t := range toRemove {
		if err := writer.Write([]string{t, "0.0000"}); err != nil {
			fmt.Printf("Error writing zero weight record: %v\n", err)
			return
		}
	}
	writer.Flush()
	fmt.Printf("\nSuccessfully optimized and saved new weights to %s (zero-weighted: %s)\n", outPath, *removeTickers)
}

// CapWeights limits the maximum weight of any single asset and redistributes the excess
func CapWeights(weights map[string]float64, cap float64) map[string]float64 {
	n := len(weights)
	if n == 0 {
		return weights
	}

	// If the average weight is >= cap, each stock gets the average
	if 1.0/float64(n) >= cap {
		equalWt := 1.0 / float64(n)
		result := make(map[string]float64)
		for k := range weights {
			result[k] = equalWt
		}
		return result
	}

	result := make(map[string]float64)
	for k, v := range weights {
		result[k] = v
	}

	for {
		var excess float64
		var underCapSum float64
		underCapCount := 0

		for k, v := range result {
			if v > cap {
				excess += v - cap
				result[k] = cap
			} else if v < cap {
				underCapSum += v
				underCapCount++
			}
		}

		if excess < 0.00001 {
			break
		}

		// Redistribute excess to under-cap stocks proportionally
		if underCapSum > 0 {
			for k, v := range result {
				if v < cap {
					result[k] += (v / underCapSum) * excess
				}
			}
		} else if underCapCount > 0 {
			for k, v := range result {
				if v < cap {
					result[k] += excess / float64(underCapCount)
				}
			}
		} else {
			break
		}
	}

	return result
}

