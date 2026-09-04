package csvloader

import (
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// ParseBasket reads target weights from an io.Reader
func ParseBasket(r io.Reader) (map[string]float64, []string, error) {
	reader := csv.NewReader(r)

	header, err := reader.Read()
	if err != nil {
		return nil, nil, err
	}

	tickerIdx := -1
	weightIdx := -1
	for i, h := range header {
		cleanH := strings.ToLower(strings.TrimSpace(h))
		if cleanH == "ticker" {
			tickerIdx = i
		} else if cleanH == "weight" {
			weightIdx = i
		}
	}

	if tickerIdx == -1 || weightIdx == -1 {
		return nil, nil, fmt.Errorf("invalid csv format: ticker and weight columns are required")
	}

	basket := make(map[string]float64)
	var tickers []string

	for {
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, nil, err
		}

		if len(record) <= tickerIdx || len(record) <= weightIdx {
			continue
		}

		ticker := strings.TrimSpace(record[tickerIdx])
		ticker = strings.ReplaceAll(ticker, "::", ":")
		if ticker == "" {
			continue
		}

		weightVal, err := strconv.ParseFloat(strings.TrimSpace(record[weightIdx]), 64)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to parse weight for %s: %w", ticker, err)
		}

		if _, exists := basket[ticker]; !exists {
			tickers = append(tickers, ticker)
		}
		basket[ticker] = weightVal
	}

	return basket, tickers, nil
}

// LoadBasketCSV opens the CSV file and delegates parsing to ParseBasket
func LoadBasketCSV(filename string) (map[string]float64, []string, error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, nil, err
	}
	defer file.Close()
	return ParseBasket(file)
}

// ParseMyAll reads the self-managed ticker list from an io.Reader
func ParseMyAll(r io.Reader) (map[string]bool, error) {
	reader := csv.NewReader(r)

	header, err := reader.Read()
	if err != nil {
		return nil, err
	}

	tickerIdx := -1
	for i, h := range header {
		if strings.ToLower(strings.TrimSpace(h)) == "ticker" {
			tickerIdx = i
			break
		}
	}

	if tickerIdx == -1 {
		return nil, fmt.Errorf("invalid csv format: ticker column required")
	}

	tickers := make(map[string]bool)
	for {
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}

		if len(record) <= tickerIdx {
			continue
		}

		ticker := strings.TrimSpace(record[tickerIdx])
		if ticker != "" {
			tickers[ticker] = true
		}
	}

	return tickers, nil
}

// LoadMyAllCSV opens the CSV file and delegates parsing to ParseMyAll
func LoadMyAllCSV(filename string) (map[string]bool, error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	return ParseMyAll(file)
}

// GetUniverseName extracts the core index/universe name from a file path.
// It parses the file's base name, splits it by separators, and filters out common prefixes,
// suffixes, strategies, and timestamp/date patterns (e.g. YYYYMMDD).
func GetUniverseName(filePath string) string {
	base := filepath.Base(filePath)
	nameWithoutExt := strings.TrimSuffix(base, filepath.Ext(base))

	// Replace typical delimiters with underscore for splitting
	nameWithoutExt = strings.ReplaceAll(nameWithoutExt, "-", "_")
	nameWithoutExt = strings.ReplaceAll(nameWithoutExt, " ", "_")

	parts := strings.Split(nameWithoutExt, "_")

	// Ignored words
	ignored := map[string]bool{
		"stockpicker":      true,
		"optimize":         true,
		"optimized":        true,
		"combine":          true,
		"bk":               true,
		"temp":             true,
		"backup":           true,
		"report":           true,
		"portfolio":        true,
		"basket":           true,
		"balanced":         true,
		"aggressive":       true,
		"conservative":     true,
		"multibagger":      true,
		"early_multibagger":true,
		"earlymb":          true,
		"value":            true,
		"volatility":       true,
		"multifactor":      true,
		"moderate":         true,
		"passive":          true,
		"hyper-aggressive": true,
		"optim":            true,
	}

	var validParts []string
	for _, part := range parts {
		if part == "" {
			continue
		}
		partLower := strings.ToLower(part)
		if ignored[partLower] {
			continue
		}
		// Check if it's a date or numeric timestamp.
		// If the part is purely numeric and has length 6, 8, or 14 (YYYYMMDD_HHMMSS), skip it.
		isNumeric := true
		for _, r := range part {
			if r < '0' || r > '9' {
				isNumeric = false
				break
			}
		}
		if isNumeric && (len(part) == 6 || len(part) == 8 || len(part) == 14) {
			continue
		}

		validParts = append(validParts, part)
	}

	if len(validParts) > 0 {
		return strings.Join(validParts, "_")
	}
	return "portfolio"
}
