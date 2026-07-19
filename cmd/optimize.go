package cmd

import (
	"context"
	"encoding/csv"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/urfave/cli/v3"

	"github.com/raghavkgarg/mycase/pkg/config"
	"github.com/raghavkgarg/mycase/pkg/csvloader"
	"github.com/raghavkgarg/mycase/pkg/optimizer"
	"github.com/raghavkgarg/mycase/pkg/yfinance"
)

var OptimizeCommand = &cli.Command{
	Name:  "optimize",
	Usage: "Optimize portfolio weights using volatility or multi-factor scoring",
	Flags: []cli.Flag{
		&cli.StringFlag{Name: "method", Aliases: []string{"m"}, Value: "multifactor", Usage: "Weighting method (volatility or multifactor)"},
		&cli.StringFlag{Name: "file", Aliases: []string{"f"}, Value: "data/basket.csv", Usage: "Path to basket CSV"},
		&cli.StringFlag{Name: "remove", Usage: "Comma-separated tickers to remove (e.g. NSE:FCL,NSE:PARACABLES)"},
		&cli.StringFlag{Name: "range", Value: "3mo", Usage: "Historical data range (3mo, 6mo, 1y)"},
		&cli.FloatFlag{Name: "cap", Value: 0.10, Usage: "Maximum weight per stock (e.g. 0.10 for 10%%)"},
		&cli.BoolFlag{Name: "promote", Usage: "Promote optimized weights to golden copy (source file) with auto-backup"},
		&cli.StringFlag{Name: "golden", Usage: "Path to golden copy CSV to identify exited stocks"},
		&cli.IntFlag{Name: "top", Value: 20, Usage: "Number of top stocks to retain (0 = retain all)"},
	},
	Action: runOptimize,
}

func runOptimize(ctx context.Context, c *cli.Command) error {
	rangeStr := strings.ToLower(strings.TrimSpace(c.String("range")))
	if rangeStr == "1yr" || rangeStr == "1year" {
		rangeStr = "1y"
	}
	if rangeStr != "3mo" && rangeStr != "6mo" && rangeStr != "1y" {
		return fmt.Errorf("unsupported range '%s'. Supported ranges: 3mo, 6mo, 1y", rangeStr)
	}
	return runOptimizeWithParams(ctx,
		c.String("method"),
		c.String("file"),
		c.String("remove"),
		rangeStr,
		c.Float("cap"),
		c.Bool("promote"),
		c.String("golden"),
		c.Int("top"),
	)
}

func runOptimizeWithParams(ctx context.Context, method, basketPath, removeTickers, rangeStr string, capFlag float64, promote bool, goldenPath string, topN int) error {
	basket, basketKeys, err := csvloader.LoadBasketCSV(basketPath)
	if err != nil {
		return fmt.Errorf("loading basket config: %w", err)
	}

	toRemove := make(map[string]bool)
	if removeTickers != "" {
		for _, t := range strings.Split(removeTickers, ",") {
			toRemove[strings.TrimSpace(t)] = true
		}
	}

	if goldenPath != "" {
		goldenBasket, _, err := csvloader.LoadBasketCSV(goldenPath)
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
			fmt.Printf("Warning: Could not load golden copy %s: %v\n", goldenPath, err)
		}
	}

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
		return nil
	}

	fmt.Printf("\nFetching historical prices (%s) for %d tickers...\n", rangeStr, len(activeKeys))
	priceHistory := make(map[string][]float64)
	var mu sync.Mutex
	var wg sync.WaitGroup

	for _, ticker := range activeKeys {
		wg.Add(1)
		go func(t string) {
			defer wg.Done()
			prices, err := yfinance.FetchHistoricalPrices(t, rangeStr)
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

	var benchmarkPrices []float64
	if method != "volatility" {
		fmt.Printf("Fetching historical benchmark prices for ^NSEI (%s)...\n", rangeStr)
		benchmarkPrices, err = yfinance.FetchHistoricalPrices("^NSEI", rangeStr)
		if err != nil {
			fmt.Printf("Warning: Failed to fetch benchmark ^NSEI: %v. Falling back to volatility method.\n", err)
			method = "volatility"
		}
	}

	var newWeights map[string]float64
	var fundamentals map[string]yfinance.Fundamentals
	if method != "volatility" {
		mfsCfg, err := config.LoadMFSConfig("config/mfs.json", method)
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
		fmt.Printf("Fetching fundamentals from Yahoo Finance...\n")
		fundamentals, err = yfinance.FetchFundamentals(activeKeys)
		if err != nil {
			fmt.Printf("Warning: Failed to fetch fundamentals: %v. Using fallbacks.\n", err)
		}
		newWeights = optimizer.OptimizeMultiFactor(activeKeys, priceHistory, benchmarkPrices, fundamentals, optWeights)
	} else {
		newWeights = optimizer.OptimizeInverseVolatility(activeKeys, priceHistory)
	}

	if topN > 0 && len(activeKeys) > topN {
		fmt.Printf("\nPruning selection from %d to top %d stocks based on optimized weights...\n", len(activeKeys), topN)
		sort.Slice(activeKeys, func(i, j int) bool {
			return newWeights[activeKeys[i]] > newWeights[activeKeys[j]]
		})
		prunedKeys := activeKeys[topN:]
		activeKeys = activeKeys[:topN]
		for _, k := range prunedKeys {
			toRemove[k] = true
			fmt.Printf("Pruned ticker: %s (initial weight: %.4f)\n", k, newWeights[k])
		}
		fmt.Printf("Re-running weight optimization for the top %d selected stocks...\n", topN)
		prunedPriceHistory := make(map[string][]float64)
		for _, t := range activeKeys {
			prunedPriceHistory[t] = priceHistory[t]
		}
		priceHistory = prunedPriceHistory
		if method != "volatility" {
			mfsCfg, _ := config.LoadMFSConfig("config/mfs.json", method)
			optWeights := optimizer.MFSWeights{
				Sharpe: mfsCfg.Sharpe, Sortino: mfsCfg.Sortino, Return: mfsCfg.Return,
				Alpha: mfsCfg.Alpha, Volatility: mfsCfg.Volatility, Beta: mfsCfg.Beta,
				Treynor: mfsCfg.Treynor, Ulcer: mfsCfg.Ulcer, PEGRatio: mfsCfg.PEGRatio,
				ROE: mfsCfg.ROE, ForwardPE: mfsCfg.ForwardPE, OperatingMargins: mfsCfg.OperatingMargins,
				PBRatio: mfsCfg.PBRatio, NetDebtEBITDA: mfsCfg.NetDebtEBITDA,
				MarketCap: mfsCfg.MarketCap, InsidersPercent: mfsCfg.InsidersPercent,
			}
			newWeights = optimizer.OptimizeMultiFactor(activeKeys, priceHistory, benchmarkPrices, fundamentals, optWeights)
		} else {
			newWeights = optimizer.OptimizeInverseVolatility(activeKeys, priceHistory)
		}
	}

	if capFlag > 0 {
		newWeights = optimizer.CapWeights(newWeights, capFlag)
	}

	displayKeys := append([]string{}, activeKeys...)
	sort.Slice(displayKeys, func(i, j int) bool {
		return newWeights[displayKeys[i]] > newWeights[displayKeys[j]]
	})

	fmt.Println("\n==========================================================")
	if method != "volatility" {
		fmt.Printf("             MULTI-FACTOR WEIGHTS COMPARISON (%s)        \n", strings.ToUpper(method))
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

	outPath := basketPath
	if !promote {
		dir := filepath.Dir(basketPath)
		base := filepath.Base(basketPath)
		ext := filepath.Ext(base)
		nameWithoutExt := strings.TrimSuffix(base, ext)
		if strings.HasPrefix(strings.ToLower(base), "stockpicker_") {
			outPath = filepath.Join(dir, "optimized_"+strings.TrimPrefix(strings.ToLower(base), "stockpicker_"))
		} else {
			outPath = filepath.Join(dir, nameWithoutExt+"_optim"+ext)
		}
		fmt.Printf("\nGolden copy protection: Output will be saved to %s (source %s remains unchanged)\n", outPath, basketPath)
	} else {
		dir := filepath.Dir(basketPath)
		base := filepath.Base(basketPath)
		backupDir := filepath.Join(dir, "backups")
		if err := os.MkdirAll(backupDir, 0755); err != nil {
			fmt.Printf("Warning: Failed to create backups directory: %v\n", err)
		}
		ts := time.Now().Format("060102_150405")
		backupPath := filepath.Join(backupDir, fmt.Sprintf("%s_backup_%s.csv", strings.TrimSuffix(base, filepath.Ext(base)), ts))
		if input, err := os.ReadFile(basketPath); err == nil {
			if writeErr := os.WriteFile(backupPath, input, 0644); writeErr == nil {
				fmt.Printf("\nBackup created successfully at %s\n", backupPath)
			}
		}
		fmt.Printf("\nPromoting weights: Overwriting golden copy %s\n", basketPath)
	}

	outFile, err := os.Create(outPath)
	if err != nil {
		return fmt.Errorf("creating output file: %w", err)
	}
	defer outFile.Close()

	writer := csv.NewWriter(outFile)
	if err := writer.Write([]string{"ticker", "weight"}); err != nil {
		return fmt.Errorf("writing header: %w", err)
	}
	for _, t := range displayKeys {
		if err := writer.Write([]string{t, fmt.Sprintf("%.4f", newWeights[t])}); err != nil {
			return fmt.Errorf("writing record: %w", err)
		}
	}
	for t := range toRemove {
		if err := writer.Write([]string{t, "0.0000"}); err != nil {
			return fmt.Errorf("writing zero weight record: %w", err)
		}
	}
	writer.Flush()
	fmt.Printf("\nSuccessfully optimized and saved new weights to %s (zero-weighted: %s)\n", outPath, removeTickers)
	return nil
}

