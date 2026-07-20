package stockpicker

import (
	"context"
	"encoding/csv"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/raghavkgarg/mycase/pkg/config"
	"github.com/raghavkgarg/mycase/pkg/csvloader"
	"github.com/raghavkgarg/mycase/pkg/yfinance"
)

func loadLocalCSVConstituents(filePath string) ([]string, error) {
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

	var tickers []string
	for _, record := range records[1:] {
		if len(record) > tickerIdx {
			ticker := strings.TrimSpace(record[tickerIdx])
			if ticker != "" {
				if !strings.HasPrefix(ticker, "NSE:") && !strings.HasPrefix(ticker, "BSE:") {
					ticker = "NSE:" + ticker
				}
				tickers = append(tickers, ticker)
			}
		}
	}

	return tickers, nil
}

func downloadNSEConstituents(url string) ([]string, error) {
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
			if strings.ToLower(strings.TrimSpace(h)) == "symbol" {
				symbolIdx = i
				break
			}
		}
	}

	if symbolIdx == -1 {
		return nil, fmt.Errorf("could not find 'Symbol' column in the CSV")
	}

	var tickers []string
	for _, record := range records[1:] {
		if len(record) > symbolIdx {
			sym := strings.TrimSpace(record[symbolIdx])
			if sym != "" {
				tickers = append(tickers, "NSE:"+sym)
			}
		}
	}

	return tickers, nil
}

// LoadConstituents loads constituent tickers from local file path or downloads them from NSE.
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
		return nil, fmt.Errorf("unsupported index '%s'. Please check docs/stockpicker.md for the list of 21 supported indices", indexName)
	}

	fmt.Printf("\nDownloading index constituents from NSE...\n")
	tickers, err := downloadNSEConstituents(url)
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

// GetBenchmarkAndSlicedPrices fetches benchmark prices and aligns stock prices with benchmark range.
func GetBenchmarkAndSlicedPrices(ctx context.Context, activeKeys []string, fullHistory map[string]*yfinance.HistoricalData, rangeStr string) (map[string][]float64, []float64, error) {
	fmt.Printf("Fetching historical benchmark prices for ^NSEI (%s)...\n", rangeStr)
	benchmarkPrices, err := yfinance.FetchHistoricalPrices(ctx, "^NSEI", rangeStr)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to fetch benchmark ^NSEI: %w", err)
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
