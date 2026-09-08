package stockpicker

import (
	"context"
	"encoding/csv"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/raghavkgarg/mycase/pkg/config"
	"github.com/raghavkgarg/mycase/pkg/csvloader"
	"github.com/raghavkgarg/mycase/pkg/excel"
	"github.com/raghavkgarg/mycase/pkg/yfinance"
)

// IsUSIndex returns true if the index name or path refers to a US index or US market portfolio.
func IsUSIndex(name string) bool {
	if strings.TrimSpace(name) == "" {
		return false
	}
	clean := strings.ToLower(name)
	clean = strings.ReplaceAll(clean, "&", "")
	clean = strings.ReplaceAll(clean, " ", "")
	clean = strings.ReplaceAll(clean, "-", "_")

	keywords := []string{"sp500", "nasdaq", "nyse", "us_", "qtum", "dow", "dji", "russell", "rut", "mag7", "fang", "spx", "qqq"}
	for _, kw := range keywords {
		if strings.Contains(clean, kw) {
			return true
		}
	}
	if strings.HasPrefix(clean, "us") {
		return true
	}

	baseName := filepath.Base(name)
	baseClean := strings.ToLower(strings.TrimSuffix(baseName, filepath.Ext(baseName)))
	for _, kw := range keywords {
		if strings.Contains(baseClean, kw) {
			return true
		}
	}

	targetPaths := []string{
		name,
		filepath.Join("data", name),
	}
	if !strings.HasSuffix(name, ".csv") && !strings.HasSuffix(name, ".xlsx") {
		targetPaths = append(targetPaths, name+".csv", filepath.Join("data", name+".csv"))
	}

	for _, p := range targetPaths {
		if info, err := os.Stat(p); err == nil && !info.IsDir() {
			if checkFileContainsUSTickers(p) {
				return true
			}
		}
	}

	return false
}

func checkFileContainsUSTickers(filePath string) bool {
	if excel.IsXLSXFile(filePath) {
		tmpCSV := filePath + ".converted_check.csv"
		defer os.Remove(tmpCSV)
		if _, err := excel.ConvertXLSXToCSV(filePath, tmpCSV); err != nil {
			return false
		}
		filePath = tmpCSV
	}

	file, err := os.Open(filePath)
	if err != nil {
		return false
	}
	defer file.Close()

	reader := csv.NewReader(file)
	records, err := reader.ReadAll()
	if err != nil || len(records) < 2 {
		return false
	}

	tickerIdx := -1
	for i, h := range records[0] {
		hClean := strings.ToLower(strings.TrimSpace(h))
		if hClean == "ticker" || hClean == "symbol" {
			tickerIdx = i
			break
		}
	}

	if tickerIdx == -1 {
		return false
	}

	for _, record := range records[1:] {
		if len(record) > tickerIdx {
			ticker := strings.TrimSpace(record[tickerIdx])
			if ticker != "" {
				if strings.HasPrefix(ticker, "US:") || strings.HasPrefix(ticker, "NASDAQ:") || strings.HasPrefix(ticker, "NYSE:") || strings.HasSuffix(ticker, ".US") {
					return true
				}
			}
		}
	}

	return false
}

// GetBenchmarkSymbolForIndex determines the appropriate benchmark ticker for an index or active tickers.
func GetBenchmarkSymbolForIndex(indexName string, tickers []string) string {
	cleanIndex := strings.ToLower(indexName)
	if strings.Contains(cleanIndex, "nasdaq") {
		return "^IXIC"
	}
	if IsUSIndex(indexName) {
		return "^GSPC"
	}
	return yfinance.GetBenchmarkSymbol(tickers)
}

func loadLocalCSVConstituents(filePath string) ([]string, map[string]string, error) {
	if excel.IsXLSXFile(filePath) {
		fmt.Printf("Detected Excel (.xlsx) file format in %s, auto-converting...\n", filePath)
		tmpCSV := filePath + ".converted.csv"
		defer os.Remove(tmpCSV)
		if _, err := excel.ConvertXLSXToCSV(filePath, tmpCSV); err != nil {
			return nil, nil, fmt.Errorf("auto-converting excel file: %w", err)
		}
		filePath = tmpCSV
	}

	file, err := os.Open(filePath)
	if err != nil {
		return nil, nil, err
	}
	defer file.Close()

	reader := csv.NewReader(file)
	records, err := reader.ReadAll()
	if err != nil {
		return nil, nil, err
	}

	tickerIdx := -1
	sectorIdx := -1
	if len(records) > 0 {
		tickerIdx, sectorIdx = findTickerAndSectorColumns(records[0])
	}

	if tickerIdx == -1 {
		return nil, nil, fmt.Errorf("could not find 'ticker' or 'symbol' column in the CSV")
	}

	isUSFile := IsUSIndex(filePath)
	var tickers []string
	sectors := make(map[string]string)
	for _, record := range records[1:] {
		if len(record) > tickerIdx {
			ticker := strings.TrimSpace(record[tickerIdx])
			if ticker != "" {
				if IsDummyTicker(ticker) {
					continue
				}
				if !strings.HasPrefix(ticker, "NSE:") && !strings.HasPrefix(ticker, "BSE:") && !strings.HasPrefix(ticker, "US:") && !strings.HasPrefix(ticker, "NASDAQ:") && !strings.HasPrefix(ticker, "NYSE:") {
					if isUSFile {
						ticker = "US:" + ticker
					} else {
						ticker = "NSE:" + ticker
					}
				}
				tickers = append(tickers, ticker)
				if sectorIdx != -1 && len(record) > sectorIdx {
					if sec := strings.TrimSpace(record[sectorIdx]); sec != "" {
						sectors[ticker] = sec
					}
				}
			}
		}
	}

	return tickers, sectors, nil
}

func downloadConstituents(indexName, url string) ([]string, map[string]string, error) {
	client := &http.Client{Timeout: 15 * time.Second}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")

	resp, err := client.Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("network error: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, nil, fmt.Errorf("HTTP status: %d", resp.StatusCode)
	}

	reader := csv.NewReader(resp.Body)
	records, err := reader.ReadAll()
	if err != nil {
		return nil, nil, err
	}

	symbolIdx := -1
	sectorIdx := -1
	if len(records) > 0 {
		symbolIdx, sectorIdx = findTickerAndSectorColumns(records[0])
	}

	if symbolIdx == -1 {
		return nil, nil, fmt.Errorf("could not find 'Symbol' or 'Ticker' column in the CSV")
	}

	isUS := IsUSIndex(indexName) || IsUSIndex(url)
	var tickers []string
	sectors := make(map[string]string)
	for _, record := range records[1:] {
		if len(record) > symbolIdx {
			sym := strings.TrimSpace(record[symbolIdx])
			if sym != "" {
				if IsDummyTicker(sym) {
					continue
				}
				var ticker string
				if isUS {
					ticker = "US:" + sym
				} else {
					ticker = "NSE:" + sym
				}
				tickers = append(tickers, ticker)
				if sectorIdx != -1 && len(record) > sectorIdx {
					if sec := strings.TrimSpace(record[sectorIdx]); sec != "" {
						sectors[ticker] = sec
					}
				}
			}
		}
	}

	return tickers, sectors, nil
}

// findTickerAndSectorColumns locates the ticker/symbol column and an optional
// sector column ("GICS Sector" or "Sector") in a CSV header row, case-insensitively.
// Returns -1 for a column that is absent.
func findTickerAndSectorColumns(header []string) (tickerIdx, sectorIdx int) {
	tickerIdx, sectorIdx = -1, -1
	for i, h := range header {
		hClean := strings.ToLower(strings.TrimSpace(h))
		switch hClean {
		case "ticker", "symbol":
			if tickerIdx == -1 {
				tickerIdx = i
			}
		case "gics sector", "sector":
			if sectorIdx == -1 {
				sectorIdx = i
			}
		}
	}
	return tickerIdx, sectorIdx
}

// IsDummyTicker returns true if a ticker is an artificial corporate action or demerger placeholder.
func IsDummyTicker(ticker string) bool {
	upper := strings.ToUpper(ticker)
	return strings.Contains(upper, "DUMMY")
}

// LoadConstituents loads constituent tickers from local file path or downloads them from web.
func LoadConstituents(filePath, indexName string) (*TickersSource, error) {
	if filePath != "" {
		fmt.Printf("\nLoading constituents from custom file %s...\n", filePath)
		tickers, sectors, err := loadLocalCSVConstituents(filePath)
		if err != nil {
			return nil, fmt.Errorf("failed to load custom file: %w", err)
		}
		fmt.Printf("Loaded %d constituents from file.\n", len(tickers))
		return &TickersSource{
			Name:    csvloader.GetUniverseName(filePath),
			Tickers: tickers,
			Sectors: sectors,
		}, nil
	}

	csvLinks, err := config.LoadCSVLinks("config/csvlinks.json")
	if err != nil {
		return nil, fmt.Errorf("failed to load config/csvlinks.json: %w", err)
	}

	rawNames := strings.FieldsFunc(indexName, func(r rune) bool {
		return r == ',' || r == '+'
	})
	if len(rawNames) == 0 {
		return nil, fmt.Errorf("no index specified")
	}

	var allTickers []string
	allSectors := make(map[string]string)
	seen := make(map[string]bool)

	for _, rawIdx := range rawNames {
		rawIdx = strings.TrimSpace(rawIdx)
		if rawIdx == "" {
			continue
		}

		// Alias check: "microsmall" -> expand to microcap250 and smallcap250
		var subIndices []string
		cleanRaw := strings.ToLower(strings.ReplaceAll(rawIdx, " ", ""))
		if cleanRaw == "microsmall" || cleanRaw == "microsmall250" || cleanRaw == "micro_small" {
			subIndices = []string{"microcap250", "smallcap250"}
		} else if cleanRaw == "midsmallmicro" || cleanRaw == "allcaps" {
			subIndices = []string{"midcap150", "smallcap250", "microcap250"}
		} else {
			subIndices = []string{rawIdx}
		}

		for _, subIdx := range subIndices {
			cleanIndex := strings.ToLower(strings.ReplaceAll(subIdx, " ", ""))
			url, ok := csvLinks[cleanIndex]
			if !ok {
				return nil, fmt.Errorf("unsupported index '%s'. Please check docs/stockpicker.md for the list of supported indices", subIdx)
			}

			fmt.Printf("\nDownloading index constituents for %s...\n", subIdx)
			tickers, sectors, err := downloadConstituents(cleanIndex, url)
			if err != nil {
				return nil, fmt.Errorf("failed to download index '%s': %w", subIdx, err)
			}
			fmt.Printf("Loaded %d constituents from %s.\n", len(tickers), subIdx)

			for _, t := range tickers {
				if !seen[t] {
					seen[t] = true
					allTickers = append(allTickers, t)
				}
			}
			for t, sec := range sectors {
				if _, ok := allSectors[t]; !ok {
					allSectors[t] = sec
				}
			}
		}
	}

	fmt.Printf("Total combined unique constituents: %d.\n", len(allTickers))

	displayName := indexName
	if len(rawNames) > 1 {
		displayName = strings.Join(rawNames, "_")
	}

	return &TickersSource{
		Name:    displayName,
		Tickers: allTickers,
		Sectors: allSectors,
	}, nil
}

// FetchHistoricalPrices concurrently retrieves historical price data for the tickers,
// with automatic retry passes and backoff for failed fetches.
// Returns: (fullHistory, activeKeys, failedKeys)
func FetchHistoricalPrices(ctx context.Context, rawTickers []string) (map[string]*yfinance.HistoricalData, []string, []string) {
	fmt.Printf("\nFetching historical prices (1y) for constituents...\n")
	type fetchJob struct {
		ticker string
	}
	type fetchResult struct {
		ticker string
		hist   *yfinance.HistoricalData
		err    error
	}

	runBatch := func(tickers []string, workerCount int) ([]fetchResult, []string) {
		jobs := make(chan fetchJob, len(tickers))
		results := make(chan fetchResult, len(tickers))
		var wg sync.WaitGroup

		for range workerCount {
			wg.Go(func() {
				for job := range jobs {
					hist, err := yfinance.FetchHistoricalDataWithTimestamps(ctx, job.ticker, "1y")
					results <- fetchResult{ticker: job.ticker, hist: hist, err: err}
				}
			})
		}

		for _, t := range tickers {
			jobs <- fetchJob{ticker: t}
		}
		close(jobs)

		go func() {
			wg.Wait()
			close(results)
		}()

		var succeeded []fetchResult
		var failed []string
		for res := range results {
			if res.err == nil && res.hist != nil && len(res.hist.Closes) >= 2 {
				succeeded = append(succeeded, res)
			} else {
				failed = append(failed, res.ticker)
			}
		}
		return succeeded, failed
	}

	// Initial concurrent pass (15 workers)
	fullHistory := make(map[string]*yfinance.HistoricalData)
	var activeKeys []string
	succeeded, pendingRetries := runBatch(rawTickers, 15)
	for _, res := range succeeded {
		fullHistory[res.ticker] = res.hist
		activeKeys = append(activeKeys, res.ticker)
	}

	// Retry passes with exponential backoff (up to 2 passes with smaller worker pool)
	maxRetries := 2
	backoffs := []time.Duration{1500 * time.Millisecond, 3000 * time.Millisecond}
retryLoop:
	for retry := 0; retry < maxRetries && len(pendingRetries) > 0; retry++ {
		fmt.Printf("Retrying %d failed ticker fetches (attempt %d/%d after %v backoff)...\n",
			len(pendingRetries), retry+1, maxRetries, backoffs[retry])
		select {
		case <-ctx.Done():
			break retryLoop
		case <-time.After(backoffs[retry]):
		}

		retriedSucceeded, stillFailed := runBatch(pendingRetries, 5)
		for _, res := range retriedSucceeded {
			fullHistory[res.ticker] = res.hist
			activeKeys = append(activeKeys, res.ticker)
		}
		pendingRetries = stillFailed
	}

	failedKeys := pendingRetries
	sort.Strings(activeKeys)
	sort.Strings(failedKeys)

	fmt.Printf("Successfully fetched historical data for %d / %d active tickers.\n", len(activeKeys), len(rawTickers))

	if len(failedKeys) > 0 {
		failPct := float64(len(failedKeys)) * 100.0 / float64(len(rawTickers))
		if failPct >= 5.0 {
			fmt.Printf("\n========================================================================================\n")
			fmt.Printf("🚨 [HARD WARNING: CRITICAL DATA FETCH FAILURE RATE: %.1f%% (%d / %d tickers)] 🚨\n",
				failPct, len(failedKeys), len(rawTickers))
			fmt.Printf("Over 5%% of index constituents failed historical price retrieval. Stage-1 candidate pool is incomplete!\n")
			sampleCount := min(len(failedKeys), 10)
			fmt.Printf("Failed sample (first 10): %s\n", strings.Join(failedKeys[:sampleCount], ", "))
			fmt.Printf("========================================================================================\n\n")
		} else {
			fmt.Printf("Notice: %d / %d tickers (%.1f%%) failed price fetch and will be tagged with DATA_FETCH_FAILED.\n",
				len(failedKeys), len(rawTickers), failPct)
		}
	}

	return fullHistory, activeKeys, failedKeys
}

// fetchHistoricalPricesWithFetcher is like FetchHistoricalPrices but routes through a DataFetcher.
func fetchHistoricalPricesWithFetcher(ctx context.Context, fetcher DataFetcher, rawTickers []string) (map[string]*yfinance.HistoricalData, []string, []string) {
	fmt.Printf("\nFetching historical prices (1y) for constituents via router...\n")
	type fetchJob struct {
		ticker string
	}
	type fetchResult struct {
		ticker string
		hist   *yfinance.HistoricalData
		err    error
	}

	jobs := make(chan fetchJob, len(rawTickers))
	results := make(chan fetchResult, len(rawTickers))
	var wg sync.WaitGroup

	workerCount := 15
	for range workerCount {
		wg.Go(func() {
			for job := range jobs {
				hist, err := fetcher.FetchHistoricalDataWithTimestamps(ctx, job.ticker, "1y")
				results <- fetchResult{ticker: job.ticker, hist: hist, err: err}
			}
		})
	}

	for _, t := range rawTickers {
		jobs <- fetchJob{ticker: t}
	}
	close(jobs)

	go func() {
		wg.Wait()
		close(results)
	}()

	fullHistory := make(map[string]*yfinance.HistoricalData)
	var activeKeys []string
	var failedKeys []string
	for res := range results {
		if res.err == nil && res.hist != nil && len(res.hist.Closes) >= 2 {
			fullHistory[res.ticker] = res.hist
			activeKeys = append(activeKeys, res.ticker)
		} else {
			failedKeys = append(failedKeys, res.ticker)
		}
	}

	fmt.Printf("Successfully fetched historical data for %d / %d active tickers.\n", len(activeKeys), len(rawTickers))
	return fullHistory, activeKeys, failedKeys
}

// GetBenchmarkAndSlicedPrices fetches benchmark prices and aligns stock prices with benchmark range.
func GetBenchmarkAndSlicedPrices(ctx context.Context, indexName string, activeKeys []string, fullHistory map[string]*yfinance.HistoricalData, rangeStr string) (map[string][]float64, []float64, error) {
	benchSym := GetBenchmarkSymbolForIndex(indexName, activeKeys)
	fmt.Printf("Fetching historical benchmark prices for %s (%s)...\n", benchSym, rangeStr)
	benchmarkPrices, err := yfinance.FetchHistoricalPrices(ctx, benchSym, rangeStr)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to fetch benchmark %s: %w", benchSym, err)
	}

	slicedPriceHistory := make(map[string][]float64)
	for _, t := range activeKeys {
		prices := fullHistory[t].Closes
		if len(prices) > len(benchmarkPrices) {
			slicedPriceHistory[t] = prices[len(prices)-len(benchmarkPrices):]
		} else {
			slicedPriceHistory[t] = prices
		}
	}

	return slicedPriceHistory, benchmarkPrices, nil
}
