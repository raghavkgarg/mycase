package yfinance

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

func getCachePath(prefix, key string) string {
	today := time.Now().Format("2006-01-02")
	cleanKey := strings.NewReplacer("^", "_", ":", "_", "/", "_").Replace(key)
	return filepath.Join("data", ".cache", fmt.Sprintf("%s_%s_%s.json", prefix, cleanKey, today))
}

func loadFromCache(prefix, key string, target interface{}) bool {
	path := getCachePath(prefix, key)
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	return json.Unmarshal(data, target) == nil
}

func saveToCache(prefix, key string, source interface{}) {
	path := getCachePath(prefix, key)
	_ = os.MkdirAll(filepath.Dir(path), 0755)
	data, err := json.Marshal(source)
	if err == nil {
		_ = os.WriteFile(path, data, 0644)
	}
}

// MapTickerToYahoo converts an exchange-specific ticker like "BSE:LT" or "NSE:RELIANCE" to a Yahoo suffix symbol.
func MapTickerToYahoo(ticker string) string {
	if strings.HasPrefix(ticker, "^") {
		return ticker
	}
	parts := strings.Split(ticker, ":")
	if len(parts) == 2 {
		exchange := strings.ToUpper(strings.TrimSpace(parts[0]))
		symbol := strings.TrimSpace(parts[1])
		if exchange == "BSE" {
			return symbol + ".BO"
		}
		return symbol + ".NS"
	}
	// Default fallback
	return strings.TrimSpace(ticker) + ".NS"
}

// FetchQuotes fetches the latest LTP for the tickers natively in Go.
func FetchQuotes(tickers []string) (map[string]float64, error) {
	if len(tickers) == 0 {
		return make(map[string]float64), nil
	}

	prices := make(map[string]float64)
	var mu sync.Mutex
	var wg sync.WaitGroup
	errChan := make(chan error, len(tickers))

	client := &http.Client{
		Timeout: 8 * time.Second,
	}

	for _, t := range tickers {
		wg.Add(1)
		go func(ticker string) {
			defer wg.Done()

			ySym := MapTickerToYahoo(ticker)

			url := fmt.Sprintf("https://query1.finance.yahoo.com/v8/finance/chart/%s?range=1d&interval=1d", ySym)
			req, err := http.NewRequest("GET", url, nil)
			if err != nil {
				errChan <- fmt.Errorf("failed to create request for %s: %w", ticker, err)
				return
			}

			req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/115.0.0.0 Safari/537.36")
			req.Header.Set("Accept", "application/json")

			resp, err := client.Do(req)
			if err != nil {
				errChan <- fmt.Errorf("network error for %s: %w", ticker, err)
				return
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				errChan <- fmt.Errorf("http status %d for %s", resp.StatusCode, ticker)
				return
			}

			var chartRes ChartResponse
			if err := json.NewDecoder(resp.Body).Decode(&chartRes); err != nil {
				errChan <- fmt.Errorf("failed to parse json for %s: %w", ticker, err)
				return
			}

			if chartRes.Chart.Error != nil {
				errChan <- fmt.Errorf("yahoo error for %s: %v", ticker, chartRes.Chart.Error)
				return
			}

			if len(chartRes.Chart.Result) == 0 {
				errChan <- fmt.Errorf("no quote data returned for %s", ticker)
				return
			}

			price := chartRes.Chart.Result[0].Meta.RegularMarketPrice
			if price <= 0 {
				errChan <- fmt.Errorf("invalid price %f fetched for %s", price, ticker)
				return
			}

			mu.Lock()
			prices[ticker] = price
			mu.Unlock()
		}(t)
	}

	wg.Wait()
	close(errChan)

	if len(errChan) > 0 {
		return nil, <-errChan
	}

	return prices, nil
}

// FetchHistoricalPrices fetches daily close prices for a single ticker over a range (e.g. "3mo")
func FetchHistoricalPrices(ticker string, rangeStr string) ([]float64, error) {
	data, err := FetchHistoricalDataWithTimestamps(ticker, rangeStr)
	if err != nil {
		return nil, err
	}
	return data.Closes, nil
}

// FetchHistoricalDataWithTimestamps fetches daily close prices and timestamps for a ticker over a range
func FetchHistoricalDataWithTimestamps(ticker string, rangeStr string) (*HistoricalData, error) {
	var cached HistoricalData
	cacheKey := fmt.Sprintf("%s_%s", ticker, rangeStr)
	if loadFromCache("prices", cacheKey, &cached) {
		return &cached, nil
	}

	ySym := MapTickerToYahoo(ticker)
	url := fmt.Sprintf("https://query1.finance.yahoo.com/v8/finance/chart/%s?range=%s&interval=1d", ySym, rangeStr)

	client := &http.Client{Timeout: 8 * time.Second}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/115.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("network error: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("http status %d", resp.StatusCode)
	}

	var histRes HistoricalChartResponse
	if err := json.NewDecoder(resp.Body).Decode(&histRes); err != nil {
		return nil, fmt.Errorf("failed to parse json: %w", err)
	}

	if histRes.Chart.Error != nil {
		return nil, fmt.Errorf("yahoo error: %v", histRes.Chart.Error)
	}

	if len(histRes.Chart.Result) == 0 {
		return nil, fmt.Errorf("no chart results returned")
	}

	result := histRes.Chart.Result[0]
	if len(result.Indicators.Quote) == 0 {
		return nil, fmt.Errorf("no quote indicators returned")
	}

	rawClose := result.Indicators.Quote[0].Close
	rawVolume := result.Indicators.Quote[0].Volume
	rawOpen := result.Indicators.Quote[0].Open

	var validClose []float64
	var validOpen []float64
	var validVolume []float64
	var validTimestamps []int64

	for i, p := range rawClose {
		if p > 0 && i < len(result.Timestamp) {
			validClose = append(validClose, p)
			validTimestamps = append(validTimestamps, result.Timestamp[i])

			oVal := 0.0
			if i < len(rawOpen) {
				oVal = rawOpen[i]
			}
			validOpen = append(validOpen, oVal)

			vVal := 0.0
			if i < len(rawVolume) {
				vVal = rawVolume[i]
			}
			validVolume = append(validVolume, vVal)
		}
	}

	if len(validClose) == 0 {
		return nil, fmt.Errorf("no valid historical closing prices found for %s", ticker)
	}

	res := &HistoricalData{
		Timestamps: validTimestamps,
		Closes:     validClose,
		Opens:      validOpen,
		Volumes:    validVolume,
	}
	res.CleanIntradayNoise()

	saveToCache("prices", cacheKey, res)

	return res, nil
}

// FetchIntradayData fetches intraday price data for a ticker for the specified range (e.g. 1d, 5d, 7d) with 1m interval
func FetchIntradayData(ticker string, rangeStr string) (*IntradayData, error) {
	ySym := MapTickerToYahoo(ticker)
	url := fmt.Sprintf("https://query1.finance.yahoo.com/v8/finance/chart/%s?range=%s&interval=1m", ySym, rangeStr)

	client := &http.Client{Timeout: 8 * time.Second}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/115.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("network error: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("http status %d", resp.StatusCode)
	}

	var chartRes IntradayChartResponse
	if err := json.NewDecoder(resp.Body).Decode(&chartRes); err != nil {
		return nil, fmt.Errorf("failed to parse json: %w", err)
	}

	if chartRes.Chart.Error != nil {
		return nil, fmt.Errorf("yahoo error: %v", chartRes.Chart.Error)
	}

	if len(chartRes.Chart.Result) == 0 {
		return nil, fmt.Errorf("no chart results returned")
	}

	res := chartRes.Chart.Result[0]
	if len(res.Indicators.Quote) == 0 {
		return nil, fmt.Errorf("no quote indicators returned")
	}

	quote := res.Indicators.Quote[0]

	var timestamps []int64
	var opens []float64
	var closes []float64

	for i, ts := range res.Timestamp {
		if i >= len(quote.Open) || i >= len(quote.Close) {
			break
		}
		if quote.Open[i] != nil && quote.Close[i] != nil {
			timestamps = append(timestamps, ts)
			opens = append(opens, *quote.Open[i])
			closes = append(closes, *quote.Close[i])
		}
	}

	if len(timestamps) == 0 {
		return nil, fmt.Errorf("no valid intraday prices found for %s", ticker)
	}

	return &IntradayData{
		Timestamps: timestamps,
		Opens:      opens,
		Closes:     closes,
	}, nil
}
