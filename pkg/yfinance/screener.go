package yfinance

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"
)

// NSEResponse represents the JSON output from scripts/fetch_nse_data.py
type NSEResponse struct {
	Symbol    string   `json:"symbol"`
	DatesOnly []string `json:"dates_only"`
	Error     string   `json:"error,omitempty"`
}

// FetchNselibEarningsDates fetches earnings & board meeting dates using the Python nselib CLI script.
func FetchNselibEarningsDates(ctx context.Context, ticker string) ([]time.Time, error) {
	cleanSym := strings.TrimSpace(ticker)
	cleanSym = strings.TrimPrefix(cleanSym, "NSE:")
	cleanSym = strings.TrimPrefix(cleanSym, "BSE:")
	cleanSym = strings.TrimSuffix(cleanSym, ".NS")
	cleanSym = strings.TrimSuffix(cleanSym, ".BO")

	if cleanSym == "" || strings.HasPrefix(cleanSym, "^") {
		return nil, nil
	}

	pyCandidates := []string{
		".venv/bin/python3",
		"./.venv/bin/python3",
		"/Users/raghavgarg/Projects/myGo/mycase/.venv/bin/python3",
		"python3",
	}

	scriptCandidates := []string{
		"scripts/fetch_nse_data.py",
		"./scripts/fetch_nse_data.py",
		"/Users/raghavgarg/Projects/myGo/mycase/scripts/fetch_nse_data.py",
	}

	var chosenScript string
	for _, sPath := range scriptCandidates {
		if _, err := os.Stat(sPath); err == nil {
			chosenScript = sPath
			break
		}
	}
	if chosenScript == "" {
		return nil, fmt.Errorf("fetch_nse_data.py script not found")
	}

	var chosenPy string
	for _, pPath := range pyCandidates {
		if strings.Contains(pPath, "/") {
			if _, err := os.Stat(pPath); err == nil {
				chosenPy = pPath
				break
			}
		} else {
			if p, err := exec.LookPath(pPath); err == nil {
				chosenPy = p
				break
			}
		}
	}
	if chosenPy == "" {
		return nil, fmt.Errorf("python interpreter not found")
	}

	subCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	cmd := exec.CommandContext(subCtx, chosenPy, chosenScript, "--symbol", cleanSym, "--mode", "earnings_dates")
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("nselib execution failed: %w", err)
	}

	var resp NSEResponse
	if err := json.Unmarshal(out, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse nselib JSON output: %w", err)
	}

	if resp.Error != "" {
		return nil, fmt.Errorf("nselib script returned error: %s", resp.Error)
	}

	var dates []time.Time
	for _, dStr := range resp.DatesOnly {
		if tVal, err := time.Parse("2006-01-02", dStr); err == nil {
			dates = append(dates, tVal)
		}
	}

	return dates, nil
}

// FetchScreenerEarningsDates fetches quarterly result dates for an Indian stock symbol.
// It first attempts fetching via Python nselib, falling back to direct Screener.in scraping.
func FetchScreenerEarningsDates(ctx context.Context, ticker string) ([]time.Time, error) {
	// Try nselib first
	if dates, err := FetchNselibEarningsDates(ctx, ticker); err == nil && len(dates) > 0 {
		return dates, nil
	}

	cleanSym := strings.TrimSpace(ticker)
	cleanSym = strings.TrimPrefix(cleanSym, "NSE:")
	cleanSym = strings.TrimPrefix(cleanSym, "BSE:")
	cleanSym = strings.TrimSuffix(cleanSym, ".NS")
	cleanSym = strings.TrimSuffix(cleanSym, ".BO")

	// Skip non-Indian symbols or benchmark symbols starting with ^
	if cleanSym == "" || strings.HasPrefix(cleanSym, "^") {
		return nil, nil
	}

	url := fmt.Sprintf("https://www.screener.in/company/%s/", cleanSym)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/115.0.0.0 Safari/537.36")

	client := &http.Client{Timeout: 8 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("screener status %d", resp.StatusCode)
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	html := string(bodyBytes)

	var dates []time.Time

	// 1. Extract data-date-key="YYYY-MM-DD" from quarters table headers
	reKey := regexp.MustCompile(`data-date-key=["'](\d{4}-\d{2}-\d{2})["']`)
	matches := reKey.FindAllStringSubmatch(html, -1)
	for _, m := range matches {
		if len(m) > 1 {
			if tVal, err := time.Parse("2006-01-02", m[1]); err == nil {
				dates = append(dates, tVal)
			}
		}
	}

	// 2. Extract board meeting intimation dates from corporate announcements section
	reBoard := regexp.MustCompile(`Board meeting on (\d{1,2}\s+[A-Za-z]+\s+\d{4})`)
	boardMatches := reBoard.FindAllStringSubmatch(html, -1)
	for _, m := range boardMatches {
		if len(m) > 1 {
			if tVal, err := time.Parse("2 January 2006", m[1]); err == nil {
				dates = append(dates, tVal)
			}
		}
	}

	return dates, nil
}

// NSEDeliveryRecord represents a single day's deliverable position record from nselib
type NSEDeliveryRecord struct {
	Date           string  `json:"date"`
	ClosePrice     float64 `json:"close_price"`
	DeliverableQty float64 `json:"deliverable_qty"`
	DeliveryPct    float64 `json:"delivery_pct"`
}

// NSEDeliverySymbolResult holds the delivery data payload for a single symbol
type NSEDeliverySymbolResult struct {
	Symbol       string              `json:"symbol"`
	RecordsCount int                 `json:"records_count"`
	Records      []NSEDeliveryRecord `json:"records"`
	Error        string              `json:"error,omitempty"`
}

// FetchNselibDeliveryDataDetails fetches full delivery records (delivery %, deliverable qty, date) for tickers.
func FetchNselibDeliveryDataDetails(ctx context.Context, tickers []string) (map[string]NSEDeliveryRecord, error) {
	if len(tickers) == 0 {
		return make(map[string]NSEDeliveryRecord), nil
	}

	var cleanSyms []string
	symToOriginal := make(map[string]string)
	for _, t := range tickers {
		s := strings.TrimSpace(t)
		s = strings.TrimPrefix(s, "NSE:")
		s = strings.TrimPrefix(s, "BSE:")
		s = strings.TrimSuffix(s, ".NS")
		s = strings.TrimSuffix(s, ".BO")
		if s != "" && !strings.HasPrefix(s, "^") {
			cleanSyms = append(cleanSyms, s)
			symToOriginal[s] = t
		}
	}

	if len(cleanSyms) == 0 {
		return make(map[string]NSEDeliveryRecord), nil
	}

	pyCandidates := []string{
		".venv/bin/python3",
		"./.venv/bin/python3",
		"/Users/raghavgarg/Projects/myGo/mycase/.venv/bin/python3",
		"python3",
	}

	scriptCandidates := []string{
		"scripts/fetch_nse_data.py",
		"./scripts/fetch_nse_data.py",
		"/Users/raghavgarg/Projects/myGo/mycase/scripts/fetch_nse_data.py",
	}

	var chosenScript string
	for _, sPath := range scriptCandidates {
		if _, err := os.Stat(sPath); err == nil {
			chosenScript = sPath
			break
		}
	}
	if chosenScript == "" {
		return nil, fmt.Errorf("fetch_nse_data.py script not found")
	}

	var chosenPy string
	for _, pPath := range pyCandidates {
		if strings.Contains(pPath, "/") {
			if _, err := os.Stat(pPath); err == nil {
				chosenPy = pPath
				break
			}
		} else {
			if p, err := exec.LookPath(pPath); err == nil {
				chosenPy = p
				break
			}
		}
	}
	if chosenPy == "" {
		return nil, fmt.Errorf("python interpreter not found")
	}

	subCtx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()

	symArg := strings.Join(cleanSyms, ",")
	cmd := exec.CommandContext(subCtx, chosenPy, chosenScript, "--symbol", symArg, "--mode", "delivery_data", "--period", "1W")
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("nselib delivery execution failed: %w", err)
	}

	resultMap := make(map[string]NSEDeliveryRecord)

	if len(cleanSyms) == 1 {
		var singleRes NSEDeliverySymbolResult
		if err := json.Unmarshal(out, &singleRes); err == nil && len(singleRes.Records) > 0 {
			rec := singleRes.Records[0]
			orig := symToOriginal[cleanSyms[0]]
			resultMap[orig] = rec
			resultMap[cleanSyms[0]] = rec
		}
	} else {
		var multiRes map[string]NSEDeliverySymbolResult
		if err := json.Unmarshal(out, &multiRes); err == nil {
			for cSym, item := range multiRes {
				if len(item.Records) > 0 {
					rec := item.Records[0]
					orig, ok := symToOriginal[cSym]
					if !ok {
						orig = cSym
					}
					resultMap[orig] = rec
					resultMap[cSym] = rec
				}
			}
		}
	}

	return resultMap, nil
}

// FetchNselibDeliveryData fetches delivery statistics (delivery %) for a batch of tickers using scripts/fetch_nse_data.py
func FetchNselibDeliveryData(ctx context.Context, tickers []string) (map[string]float64, error) {
	details, err := FetchNselibDeliveryDataDetails(ctx, tickers)
	if err != nil {
		return nil, err
	}
	resultMap := make(map[string]float64)
	for k, v := range details {
		resultMap[k] = v.DeliveryPct
	}
	return resultMap, nil
}

type QualitativeNSEData struct {
	Symbol              string `json:"symbol"`
	AuditorStatus       string `json:"auditor_status"`
	TranscriptSummary   string `json:"transcript_summary"`
	ManagementStability string `json:"management_stability"`
	RPTStatus           string `json:"rpt_status"`
}

// FetchQualitativeNSEData fetches auditor opinion status and transcript summaries for a batch of tickers.
func FetchQualitativeNSEData(ctx context.Context, tickers []string) (map[string]QualitativeNSEData, error) {
	if len(tickers) == 0 {
		return make(map[string]QualitativeNSEData), nil
	}

	var cleanSyms []string
	symToOriginal := make(map[string]string)
	for _, t := range tickers {
		s := strings.TrimSpace(t)
		s = strings.TrimPrefix(s, "NSE:")
		s = strings.TrimPrefix(s, "BSE:")
		s = strings.TrimSuffix(s, ".NS")
		s = strings.TrimSuffix(s, ".BO")
		if s != "" && !strings.HasPrefix(s, "^") {
			cleanSyms = append(cleanSyms, s)
			symToOriginal[s] = t
		}
	}

	if len(cleanSyms) == 0 {
		return make(map[string]QualitativeNSEData), nil
	}

	pyCandidates := []string{
		".venv/bin/python3",
		"./.venv/bin/python3",
		"/Users/raghavgarg/Projects/myGo/mycase/.venv/bin/python3",
		"python3",
	}

	scriptCandidates := []string{
		"scripts/fetch_nse_data.py",
		"./scripts/fetch_nse_data.py",
		"/Users/raghavgarg/Projects/myGo/mycase/scripts/fetch_nse_data.py",
	}

	var chosenScript string
	for _, sPath := range scriptCandidates {
		if _, err := os.Stat(sPath); err == nil {
			chosenScript = sPath
			break
		}
	}
	if chosenScript == "" {
		return nil, fmt.Errorf("fetch_nse_data.py script not found")
	}

	var chosenPy string
	for _, pPath := range pyCandidates {
		if strings.Contains(pPath, "/") {
			if _, err := os.Stat(pPath); err == nil {
				chosenPy = pPath
				break
			}
		} else {
			if p, err := exec.LookPath(pPath); err == nil {
				chosenPy = p
				break
			}
		}
	}
	if chosenPy == "" {
		return nil, fmt.Errorf("python interpreter not found")
	}

	subCtx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()

	symArg := strings.Join(cleanSyms, ",")
	cmd := exec.CommandContext(subCtx, chosenPy, chosenScript, "--symbol", symArg, "--mode", "qualitative_data")
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("nselib qualitative execution failed: %w", err)
	}

	resultMap := make(map[string]QualitativeNSEData)

	if len(cleanSyms) == 1 {
		var singleRes QualitativeNSEData
		if err := json.Unmarshal(out, &singleRes); err == nil && singleRes.Symbol != "" {
			orig := symToOriginal[cleanSyms[0]]
			resultMap[orig] = singleRes
			resultMap[cleanSyms[0]] = singleRes
		}
	} else {
		var multiRes map[string]QualitativeNSEData
		if err := json.Unmarshal(out, &multiRes); err == nil {
			for cSym, item := range multiRes {
				orig, ok := symToOriginal[cSym]
				if !ok {
					orig = cSym
				}
				resultMap[orig] = item
				resultMap[cSym] = item
			}
		}
	}

	return resultMap, nil
}

// FetchCustomerConcentrationData fetches customer concentration metrics for a batch of tickers.
func FetchCustomerConcentrationData(ctx context.Context, tickers []string) (map[string]string, error) {
	if len(tickers) == 0 {
		return make(map[string]string), nil
	}

	resultMap := make(map[string]string)

	pyCandidates := []string{
		".venv/bin/python3",
		"./.venv/bin/python3",
		"/Users/raghavgarg/Projects/myGo/mycase/.venv/bin/python3",
		"python3",
	}

	var chosenPy string
	for _, pPath := range pyCandidates {
		if strings.Contains(pPath, "/") {
			if _, err := os.Stat(pPath); err == nil {
				chosenPy = pPath
				break
			}
		} else {
			if p, err := exec.LookPath(pPath); err == nil {
				chosenPy = p
				break
			}
		}
	}
	if chosenPy == "" {
		return nil, fmt.Errorf("python interpreter not found")
	}

	scriptPath := "scripts/check_customer_concentration.py"
	if _, err := os.Stat(scriptPath); err != nil {
		if _, err2 := os.Stat("../" + scriptPath); err2 == nil {
			scriptPath = "../" + scriptPath
		} else if _, err3 := os.Stat("../../" + scriptPath); err3 == nil {
			scriptPath = "../../" + scriptPath
		}
	}

	var wg sync.WaitGroup
	var mu sync.Mutex
	sem := make(chan struct{}, 3) // Max 3 concurrent Python tasks
	for _, t := range tickers {
		s := strings.TrimSpace(t)
		s = strings.TrimPrefix(s, "NSE:")
		s = strings.TrimPrefix(s, "BSE:")
		s = strings.TrimSuffix(s, ".NS")
		s = strings.TrimSuffix(s, ".BO")
		if s == "" || strings.HasPrefix(s, "^") {
			continue
		}

		wg.Add(1)
		go func(ticker, cleanSym string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			subCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
			defer cancel()
			cmd := exec.CommandContext(subCtx, chosenPy, scriptPath, cleanSym)
			cmd.Env = os.Environ()
			out, err := cmd.Output()

			mu.Lock()
			defer mu.Unlock()
			if err == nil {
				resultMap[ticker] = strings.TrimSpace(string(out))
				resultMap[cleanSym] = strings.TrimSpace(string(out))
			} else {
				var stderrStr string
				if exitErr, ok := err.(*exec.ExitError); ok {
					stderrStr = string(exitErr.Stderr)
				}
				fmt.Printf("[DEBUG] Command failed for %s: %v. Stderr: %s. Output: %s\n", cleanSym, err, stderrStr, string(out))
				resultMap[ticker] = "Metric Coverage Pending"
				resultMap[cleanSym] = "Metric Coverage Pending"
			}
		}(t, s)
	}
	wg.Wait()

	return resultMap, nil
}

