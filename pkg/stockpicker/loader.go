package stockpicker

import (
	"context"
	"encoding/csv"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
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

func loadLocalCSVConstituents(filePath string) ([]string, error) {
	if excel.IsXLSXFile(filePath) {
		fmt.Printf("Detected Excel (.xlsx) file format in %s, auto-converting...\n", filePath)
		tmpCSV := filePath + ".converted.csv"
		defer os.Remove(tmpCSV)
		if _, err := excel.ConvertXLSXToCSV(filePath, tmpCSV); err != nil {
			return nil, fmt.Errorf("auto-converting excel file: %w", err)
		}
		filePath = tmpCSV
	}

	file, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	reader := csv.NewReader(file)
	records, err := reader.ReadAll()
	if err != nil {
		return nil, err
	}

	tickerIdx := -1
	if len(records) > 0 {
		for i, h := range records[0] {
			hClean := strings.ToLower(strings.TrimSpace(h))
			if hClean == "ticker" || hClean == "symbol" {
				tickerIdx = i
				break
			}
		}
	}

	if tickerIdx == -1 {
		return nil, fmt.Errorf("could not find 'ticker' or 'symbol' column in the CSV")
	}

	isUSFile := IsUSIndex(filePath)
	var tickers []string
	for _, record := range records[1:] {
		if len(record) > tickerIdx {
			ticker := strings.TrimSpace(record[tickerIdx])
			if ticker != "" {
				if !strings.HasPrefix(ticker, "NSE:") && !strings.HasPrefix(ticker, "BSE:") && !strings.HasPrefix(ticker, "US:") && !strings.HasPrefix(ticker, "NASDAQ:") && !strings.HasPrefix(ticker, "NYSE:") {
					if isUSFile {
						ticker = "US:" + ticker
					} else {
						ticker = "NSE:" + ticker
					}
				}
				tickers = append(tickers, ticker)
			}
		}
	}

	return tickers, nil
}

func downloadConstituents(indexName, url string) ([]string, error) {
	client := &http.Client{Timeout: 15 * time.Second}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("network error: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP status: %d", resp.StatusCode)
	}

	reader := csv.NewReader(resp.Body)
	records, err := reader.ReadAll()
	if err != nil {
		return nil, err
	}

	symbolIdx := -1
	if len(records) > 0 {
		for i, h := range records[0] {
			hClean := strings.ToLower(strings.TrimSpace(h))
			if hClean == "symbol" || hClean == "ticker" {
				symbolIdx = i
				break
			}
		}
	}

	if symbolIdx == -1 {
		return nil, fmt.Errorf("could not find 'Symbol' or 'Ticker' column in the CSV")
	}

	isUS := IsUSIndex(indexName) || IsUSIndex(url)
	var tickers []string
	for _, record := range records[1:] {
		if len(record) > symbolIdx {
			sym := strings.TrimSpace(record[symbolIdx])
			if sym != "" {
				if isUS {
					tickers = append(tickers, "US:"+sym)
				} else {
					tickers = append(tickers, "NSE:"+sym)
				}
			}
		}
	}

	return tickers, nil
}

// LoadConstituents loads constituent tickers from local file path or downloads them from web.
func LoadConstituents(filePath, indexName string) (*TickersSource, error) {
	if filePath != "" {
		fmt.Printf("\nLoading constituents from custom file %s...\n", filePath)
		tickers, err := loadLocalCSVConstituents(filePath)
		if err != nil {
			return nil, fmt.Errorf("failed to load custom file: %w", err)
		}
		fmt.Printf("Loaded %d constituents from file.\n", len(tickers))
		return &TickersSource{
			Name:    csvloader.GetUniverseName(filePath),
			Tickers: tickers,
		}, nil
	}

	csvLinks, err := config.LoadCSVLinks("config/csvlinks.json")
	if err != nil {
		return nil, fmt.Errorf("failed to load config/csvlinks.json: %w", err)
	}

	cleanIndex := strings.ToLower(strings.ReplaceAll(indexName, " ", ""))
	url, ok := csvLinks[cleanIndex]
	if !ok {
		return nil, fmt.Errorf("unsupported index '%s'. Please check docs/stockpicker.md for the list of supported indices", indexName)
	}

	fmt.Printf("\nDownloading index constituents for %s...\n", indexName)
	tickers, err := downloadConstituents(cleanIndex, url)
	if err != nil {
		return nil, fmt.Errorf("failed to download index: %w", err)
	}
	fmt.Printf("Loaded %d constituents from index.\n", len(tickers))

	return &TickersSource{
		Name:    indexName,
		Tickers: tickers,
	}, nil
}

// FetchHistoricalPrices concurrently retrieves historical price data for the tickers.
func FetchHistoricalPrices(ctx context.Context, rawTickers []string) (map[string]*yfinance.HistoricalData, []string) {
	fmt.Printf("\nFetching historical prices (1y) for constituents...\n")
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

	// Start workers
	workerCount := 15
	for range workerCount {
		wg.Go(func() {
			for job := range jobs {
				hist, err := yfinance.FetchHistoricalDataWithTimestamps(ctx, job.ticker, "1y")
				results <- fetchResult{ticker: job.ticker, hist: hist, err: err}
			}
		})
	}

	for _, t := range rawTickers {
		jobs <- fetchJob{ticker: t}
	}
	close(jobs)

	// Wait for workers to finish in background and close results channel
	go func() {
		wg.Wait()
		close(results)
	}()

	fullHistory := make(map[string]*yfinance.HistoricalData)
	var activeKeys []string
	for res := range results {
		if res.err == nil && res.hist != nil && len(res.hist.Closes) >= 2 {
			fullHistory[res.ticker] = res.hist
			activeKeys = append(activeKeys, res.ticker)
		}
	}

	fmt.Printf("Successfully fetched historical data for %d / %d active tickers.\n", len(activeKeys), len(rawTickers))
	return fullHistory, activeKeys
}

// fetchHistoricalPricesWithFetcher is like FetchHistoricalPrices but routes through a DataFetcher.
func fetchHistoricalPricesWithFetcher(ctx context.Context, fetcher DataFetcher, rawTickers []string) (map[string]*yfinance.HistoricalData, []string) {
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
	for res := range results {
		if res.err == nil && res.hist != nil && len(res.hist.Closes) >= 2 {
			fullHistory[res.ticker] = res.hist
			activeKeys = append(activeKeys, res.ticker)
		}
	}

	fmt.Printf("Successfully fetched historical data for %d / %d active tickers.\n", len(activeKeys), len(rawTickers))
	return fullHistory, activeKeys
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
