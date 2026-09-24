package stockpicker

import (
	"bytes"
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/raghavkgarg/mycase/pkg/cache"
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
		slog.Info("constituents.xlsx_autoconvert", "path", filePath)
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
	cleanIdx := strings.ToLower(strings.TrimSpace(indexName))
	cleanIdx = strings.NewReplacer(",", "_", " ", "_", "^", "").Replace(cleanIdx)
	cacheDir := filepath.Join("data", "cache", "constituents")
	cachePath := filepath.Join(cacheDir, cleanIdx+".csv")

	var records [][]string
	var fetchErr error

	client := &http.Client{Timeout: 15 * time.Second}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		fetchErr = err
	} else {
		req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")
		resp, err := client.Do(req)
		if err != nil {
			fetchErr = fmt.Errorf("network error: %w", err)
		} else {
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				fetchErr = fmt.Errorf("HTTP status: %d", resp.StatusCode)
			} else {
				bodyBytes, rErr := io.ReadAll(resp.Body)
				if rErr != nil {
					fetchErr = rErr
				} else {
					reader := csv.NewReader(bytes.NewReader(bodyBytes))
					rec, cErr := reader.ReadAll()
					if cErr != nil {
						fetchErr = cErr
					} else {
						records = rec
						if len(records) > 1 {
							if mErr := os.MkdirAll(cacheDir, 0755); mErr == nil {
								_ = os.WriteFile(cachePath, bodyBytes, 0644)
							}
						}
					}
				}
			}
		}
	}

	// Fallback to local air-gapped mirror if network fetch failed
	if fetchErr != nil {
		if cacheBytes, rErr := os.ReadFile(cachePath); rErr == nil && len(cacheBytes) > 0 {
			reader := csv.NewReader(bytes.NewReader(cacheBytes))
			if rec, cErr := reader.ReadAll(); cErr == nil && len(rec) > 1 {
				slog.Warn("constituents.network_fetch_failed; using cached offline mirror",
					"index", indexName, "path", cachePath, "err", fetchErr)
				records = rec
				fetchErr = nil
			}
		}
	}

	if fetchErr != nil {
		return nil, nil, fetchErr
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
		case "gics sector", "sector", "industry":
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
		slog.Info("constituents.load_file", "path", filePath)
		tickers, sectors, err := loadLocalCSVConstituents(filePath)
		if err != nil {
			return nil, fmt.Errorf("failed to load custom file: %w", err)
		}
		slog.Info("constituents.loaded", "source", "file", "count", len(tickers))
		return &TickersSource{
			Name:    csvloader.GetUniverseName(filePath),
			Tickers: tickers,
			Sectors: sectors,
		}, nil
	}

	csvLinks, err := config.LoadCSVLinks(config.Path("csvlinks.json"))
	if err != nil {
		return nil, fmt.Errorf("failed to load %s: %w", config.Path("csvlinks.json"), err)
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
		switch cleanRaw {
		case "microsmall", "microsmall250", "micro_small":
			subIndices = []string{"microcap250", "smallcap250"}
		case "midsmallmicro", "allcaps":
			subIndices = []string{"midcap150", "smallcap250", "microcap250"}
		default:
			subIndices = []string{rawIdx}
		}

		for _, subIdx := range subIndices {
			cleanIndex := strings.ToLower(strings.ReplaceAll(subIdx, " ", ""))
			url, ok := csvLinks[cleanIndex]
			if !ok {
				return nil, fmt.Errorf("unsupported index '%s'. Please check docs/stockpicker.md for the list of supported indices", subIdx)
			}

			slog.Info("constituents.download", "index", subIdx)
			tickers, sectors, err := downloadConstituents(cleanIndex, url)
			if err != nil {
				return nil, fmt.Errorf("failed to download index '%s': %w", subIdx, err)
			}
			slog.Info("constituents.loaded", "source", "download", "index", subIdx, "count", len(tickers))

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

	slog.Info("constituents.combined", "unique_count", len(allTickers))

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
	slog.InfoContext(ctx, "prices.fetch_start", "range", "1y", "count", len(rawTickers))
	type fetchJob struct {
		ticker string
	}
	type fetchResult struct {
		err    error
		hist   *yfinance.HistoricalData
		ticker string
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
		slog.WarnContext(ctx, "prices.retry", "pending", len(pendingRetries), "attempt", retry+1, "max", maxRetries, "backoff", backoffs[retry].String())
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

	slog.InfoContext(ctx, "prices.fetch_complete", "active", len(activeKeys), "total", len(rawTickers))

	if len(failedKeys) > 0 {
		failPct := float64(len(failedKeys)) * 100.0 / float64(len(rawTickers))
		sampleCount := min(len(failedKeys), 10)
		if failPct >= 5.0 {
			slog.ErrorContext(ctx, "prices.fetch_failure_critical",
				"fail_pct", failPct, "failed", len(failedKeys), "total", len(rawTickers),
				"sample", strings.Join(failedKeys[:sampleCount], ","))
		} else {
			slog.WarnContext(ctx, "prices.fetch_failure",
				"failed", len(failedKeys), "total", len(rawTickers), "fail_pct", failPct)
		}
	}

	return fullHistory, activeKeys, failedKeys
}

// fetchHistoricalPricesWithFetcher is like FetchHistoricalPrices but routes through a DataFetcher,
// with multi-pass exponential backoff retries for failed tickers.
func fetchHistoricalPricesWithFetcher(ctx context.Context, fetcher DataFetcher, rawTickers []string) (map[string]*yfinance.HistoricalData, []string, []string) {
	slog.InfoContext(ctx, "prices.fetch_start", "range", "1y", "count", len(rawTickers), "source", "router")
	type fetchJob struct {
		ticker string
	}
	type fetchResult struct {
		err    error
		hist   *yfinance.HistoricalData
		ticker string
	}

	runBatch := func(tickers []string, workerCount int) ([]fetchResult, []string) {
		jobs := make(chan fetchJob, len(tickers))
		results := make(chan fetchResult, len(tickers))
		var wg sync.WaitGroup

		for range workerCount {
			wg.Go(func() {
				for job := range jobs {
					hist, err := fetcher.FetchHistoricalDataWithTimestamps(ctx, job.ticker, "1y")
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
		slog.WarnContext(ctx, "prices.retry", "pending", len(pendingRetries), "attempt", retry+1, "max", maxRetries, "backoff", backoffs[retry].String(), "source", "router")
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

	slog.InfoContext(ctx, "prices.fetch_complete", "active", len(activeKeys), "total", len(rawTickers), "source", "router")
	return fullHistory, activeKeys, failedKeys
}

// FetchBenchmarkPricesResilient fetches benchmark prices with retry backoff and persistent database fallback.
func FetchBenchmarkPricesResilient(ctx context.Context, fetcher DataFetcher, benchSym, rangeStr string) ([]float64, error) {
	slog.InfoContext(ctx, "pick.benchmark_fetch", "symbol", benchSym, "range", rangeStr)
	var benchmarkPrices []float64
	var fetchErr error

	backoffs := []time.Duration{1000 * time.Millisecond, 2000 * time.Millisecond, 4000 * time.Millisecond}
	for attempt := range 3 {
		if fetcher != nil {
			benchmarkPrices, fetchErr = fetcher.FetchHistoricalPrices(ctx, benchSym, rangeStr)
		} else {
			benchmarkPrices, fetchErr = yfinance.FetchHistoricalPrices(ctx, benchSym, rangeStr)
		}
		if fetchErr == nil && len(benchmarkPrices) >= 2 {
			return benchmarkPrices, nil
		}
		if attempt < len(backoffs)-1 {
			slog.WarnContext(ctx, "pick.benchmark_fetch_retry", "symbol", benchSym, "attempt", attempt+1, "err", fetchErr)
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(backoffs[attempt]):
			}
		}
	}

	// Fallback to DuckDB prices table
	c := cache.GetDB()
	var cacheOpened *cache.Cache
	if c == nil {
		if opened, err := cache.Open(config.DataPath("mycase.db")); err == nil {
			c = opened
			cacheOpened = opened
		}
	}
	if cacheOpened != nil {
		defer cacheOpened.Close()
	}

	if c != nil {
		query := `SELECT close FROM prices WHERE ticker = ? ORDER BY date ASC`
		rows, qErr := c.Conn().QueryContext(ctx, query, benchSym)
		if qErr == nil {
			defer rows.Close()
			var closes []float64
			for rows.Next() {
				var cl float64
				if err := rows.Scan(&cl); err == nil {
					closes = append(closes, cl)
				}
			}
			if len(closes) >= 2 {
				slog.WarnContext(ctx, "pick.benchmark_fallback_db", "symbol", benchSym, "bars", len(closes), "network_err", fetchErr)
				return closes, nil
			}
		}
	}

	return nil, fmt.Errorf("failed to fetch benchmark %s: %w", benchSym, fetchErr)
}

// GetBenchmarkAndSlicedPrices fetches benchmark prices and aligns stock prices with benchmark range.
func GetBenchmarkAndSlicedPrices(ctx context.Context, indexName string, activeKeys []string, fullHistory map[string]*yfinance.HistoricalData, rangeStr string) (map[string][]float64, []float64, error) {
	benchSym := GetBenchmarkSymbolForIndex(indexName, activeKeys)
	benchmarkPrices, err := FetchBenchmarkPricesResilient(ctx, nil, benchSym, rangeStr)
	if err != nil {
		return nil, nil, err
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
