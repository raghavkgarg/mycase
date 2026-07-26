package yfinance

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
)

// FetchScreenerEarningsDates fetches quarterly result dates from Screener.in for an Indian stock symbol.
func FetchScreenerEarningsDates(ctx context.Context, ticker string) ([]time.Time, error) {
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
