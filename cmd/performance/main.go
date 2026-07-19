package main

import (
	"encoding/csv"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gkgarg24/mycase/pkg/yfinance"
)

type stockInfo struct {
	ticker string
	weight float64
}

type stockResult struct {
	ticker     string
	weight     float64
	allocated  float64
	buyPrice   float64
	buyTime    string
	closePrice float64
	finalVal   float64
	pctReturn  float64
	err        error
}

func parseDate(dateStr string, loc *time.Location) (time.Time, error) {
	if dateStr == "" {
		return time.Now().In(loc), nil
	}
	// Try YYYY-MM-DD
	if t, err := time.ParseInLocation("2006-01-02", dateStr, loc); err == nil {
		return t, nil
	}
	// Try YYYYMMDD
	if t, err := time.ParseInLocation("20060102", dateStr, loc); err == nil {
		return t, nil
	}
	return time.Time{}, fmt.Errorf("invalid date format: %s. Use YYYY-MM-DD or YYYYMMDD", dateStr)
}

func main() {
	filePath := flag.String("file", "", "Path to the portfolio CSV file (required)")
	capital := flag.Float64("capital", 100000.0, "Total capital invested")
	targetDateStr := flag.String("date", "", "Purchase date in YYYY-MM-DD or YYYYMMDD format (IST, default: today)")
	targetTimeStr := flag.String("time", "09:30", "Purchase time in HH:MM format (IST)")
	flag.Parse()

	if *filePath == "" {
		fmt.Println("Error: -file parameter is required.")
		flag.Usage()
		return
	}

	// Parse buy time
	timeParts := strings.Split(*targetTimeStr, ":")
	if len(timeParts) != 2 {
		fmt.Println("Error: Invalid time format. Must be HH:MM.")
		return
	}
	targetHour, err := strconv.Atoi(timeParts[0])
	if err != nil {
		fmt.Printf("Error parsing hour: %v\n", err)
		return
	}
	targetMin, err := strconv.Atoi(timeParts[1])
	if err != nil {
		fmt.Printf("Error parsing minute: %v\n", err)
		return
	}

	// Set up IST Location
	istLoc := time.FixedZone("IST", 5*3600+30*60)
	nowIST := time.Now().In(istLoc)

	targetDate, err := parseDate(*targetDateStr, istLoc)
	if err != nil {
		fmt.Println(err)
		return
	}

	targetTime := time.Date(targetDate.Year(), targetDate.Month(), targetDate.Day(), targetHour, targetMin, 0, 0, istLoc)

	// Determine if date is beyond 7 days
	useDailyClose := false
	rangeStr := "1d"
	daysDiff := nowIST.Sub(targetTime).Hours() / 24.0

	// Check if targetTime is before the 7-day intraday limit
	if targetTime.Before(time.Date(nowIST.Year(), nowIST.Month(), nowIST.Day(), 0, 0, 0, 0, istLoc)) {
		rangeStr = "7d"
	}

	if daysDiff > 7.0 {
		useDailyClose = true
		// Determine appropriate range for daily historical prices
		if daysDiff <= 30.0 {
			rangeStr = "1mo"
		} else if daysDiff <= 90.0 {
			rangeStr = "3mo"
		} else if daysDiff <= 180.0 {
			rangeStr = "6mo"
		} else if daysDiff <= 365.0 {
			rangeStr = "1y"
		} else if daysDiff <= 730.0 {
			rangeStr = "2y"
		} else {
			rangeStr = "5y"
		}
		fmt.Printf("Target purchase time is %.1f days ago (> 7 days). Switching to daily Close prices for %s (ignoring time flag).\n", daysDiff, targetTime.Format("2006-01-02"))
	}

	// 1. Read portfolio CSV file
	file, err := os.Open(*filePath)
	if err != nil {
		fmt.Printf("Error opening file %s: %v\n", *filePath, err)
		return
	}
	defer file.Close()

	reader := csv.NewReader(file)
	records, err := reader.ReadAll()
	if err != nil {
		fmt.Printf("Error reading CSV: %v\n", err)
		return
	}

	if len(records) < 2 {
		fmt.Println("Error: CSV file contains no data rows.")
		return
	}

	tickerIdx := -1
	weightIdx := -1
	for i, h := range records[0] {
		hClean := strings.ToLower(strings.TrimSpace(h))
		if hClean == "ticker" {
			tickerIdx = i
		} else if hClean == "weight" {
			weightIdx = i
		}
	}

	if tickerIdx == -1 || weightIdx == -1 {
		fmt.Println("Error: Invalid CSV format. Must contain 'ticker' and 'weight' columns.")
		return
	}

	var portfolio []stockInfo
	for _, record := range records[1:] {
		if len(record) <= tickerIdx || len(record) <= weightIdx {
			continue
		}
		ticker := strings.TrimSpace(record[tickerIdx])
		weightVal, err := strconv.ParseFloat(strings.TrimSpace(record[weightIdx]), 64)
		if err != nil || ticker == "" {
			continue
		}
		portfolio = append(portfolio, stockInfo{ticker: ticker, weight: weightVal})
	}

	if len(portfolio) == 0 {
		fmt.Println("Error: No valid stocks found in CSV.")
		return
	}

	if useDailyClose {
		fmt.Printf("Analyzing portfolio performance: Bought at Close on %s till latest Close...\n\n",
			targetTime.Format("2006-01-02"),
		)
	} else {
		fmt.Printf("Analyzing portfolio performance: Bought on %s at %s IST till latest Close...\n\n",
			targetTime.Format("2006-01-02"), targetTime.Format("15:04"),
		)
	}

	var wg sync.WaitGroup
	results := make([]stockResult, len(portfolio))

	for i, s := range portfolio {
		wg.Add(1)
		go func(idx int, info stockInfo) {
			defer wg.Done()
			res := stockResult{
				ticker:    info.ticker,
				weight:    info.weight,
				allocated: *capital * info.weight,
			}

			if useDailyClose {
				// Fetch daily data
				data, err := yfinance.FetchHistoricalDataWithTimestamps(info.ticker, rangeStr)
				if err != nil {
					res.err = err
					results[idx] = res
					return
				}

				var priceAtBuy float64
				var actualBuyDate time.Time

				for j, ts := range data.Timestamps {
					tLocal := time.Unix(ts, 0).In(istLoc)
					if tLocal.Year() == targetTime.Year() && tLocal.YearDay() == targetTime.YearDay() {
						priceAtBuy = data.Closes[j]
						actualBuyDate = tLocal
						break
					}
				}

				// If date is not found (holiday/weekend), fall back to closest previous trading day's close
				if priceAtBuy == 0 {
					var minDiff int64 = 99999999
					for j, ts := range data.Timestamps {
						tLocal := time.Unix(ts, 0).In(istLoc)
						diff := targetTime.Unix() - tLocal.Unix()
						if diff >= 0 && diff < minDiff {
							minDiff = diff
							priceAtBuy = data.Closes[j]
							actualBuyDate = tLocal
						}
					}
				}

				if priceAtBuy == 0 {
					res.err = fmt.Errorf("no daily price data available around %s", targetTime.Format("2006-01-02"))
					results[idx] = res
					return
				}

				var priceClose float64
				if len(data.Closes) > 0 {
					priceClose = data.Closes[len(data.Closes)-1]
				} else {
					priceClose = priceAtBuy
				}

				shares := res.allocated / priceAtBuy
				res.buyPrice = priceAtBuy
				res.buyTime = actualBuyDate.Format("2006-01-02 (Close)")
				res.closePrice = priceClose
				res.finalVal = shares * priceClose
				res.pctReturn = ((priceClose - priceAtBuy) / priceAtBuy) * 100.0
			} else {
				// Fetch 1m intraday data
				data, err := yfinance.FetchIntradayData(info.ticker, rangeStr)
				if err != nil {
					res.err = err
					results[idx] = res
					return
				}

				var priceAtBuy float64
				var closestTimeDiff int64 = 99999999
				var actualBuyTime time.Time

				for j, ts := range data.Timestamps {
					tLocal := time.Unix(ts, 0).In(istLoc)
					diff := targetTime.Unix() - tLocal.Unix()
					if diff < 0 {
						diff = -diff
					}
					// Look for closest match within 5 minutes (300 seconds) on the target day
					if tLocal.Year() == targetTime.Year() && tLocal.YearDay() == targetTime.YearDay() {
						if diff < closestTimeDiff && diff <= 300 {
							closestTimeDiff = diff
							priceAtBuy = data.Opens[j]
							actualBuyTime = tLocal
						}
					}
				}

				// Fallback: if we can't find exactly the requested time on that day, search for first available price of that day
				if priceAtBuy == 0 {
					for j, ts := range data.Timestamps {
						tLocal := time.Unix(ts, 0).In(istLoc)
						if tLocal.Year() == targetTime.Year() && tLocal.YearDay() == targetTime.YearDay() {
							priceAtBuy = data.Opens[j]
							actualBuyTime = tLocal
							break
						}
					}
				}

				if priceAtBuy == 0 {
					res.err = fmt.Errorf("no price data available for target date %s", targetTime.Format("2006-01-02"))
					results[idx] = res
					return
				}

				// Get the last valid closing price of the dataset (e.g. today's close)
				var priceClose float64
				if len(data.Closes) > 0 {
					priceClose = data.Closes[len(data.Closes)-1]
				} else {
					priceClose = priceAtBuy
				}

				shares := res.allocated / priceAtBuy
				res.buyPrice = priceAtBuy
				res.buyTime = actualBuyTime.Format("2006-01-02 15:04:05")
				res.closePrice = priceClose
				res.finalVal = shares * priceClose
				res.pctReturn = ((priceClose - priceAtBuy) / priceAtBuy) * 100.0
			}

			results[idx] = res
		}(i, s)
	}

	wg.Wait()

	// Print results table
	fmt.Printf("%-15s %-8s %-12s %-12s %-22s %-12s %-12s %-10s\n", "Ticker", "Weight", "Allocated", "Buy Price", "Buy Time/Date (IST)", "Close Price", "Final Value", "Return")
	fmt.Println(strings.Repeat("-", 112))

	var totalInitial float64
	var totalFinal float64

	for _, res := range results {
		if res.err != nil {
			fmt.Printf("%-15s ERROR: %v\n", res.ticker, res.err)
			continue
		}

		fmt.Printf("%-15s %-8.4f Rs. %-8.2f Rs. %-8.2f %-22s Rs. %-8.2f Rs. %-8.2f %+.2f%%\n",
			res.ticker, res.weight, res.allocated, res.buyPrice, res.buyTime, res.closePrice, res.finalVal, res.pctReturn)

		totalInitial += res.allocated
		totalFinal += res.finalVal
	}

	fmt.Println(strings.Repeat("-", 112))
	netReturn := totalFinal - totalInitial
	pctReturn := (netReturn / totalInitial) * 100.0
	unallocated := *capital - totalInitial

	fmt.Printf("\n--- Portfolio Performance ---\n")
	fmt.Printf("Total Allocated Capital:  Rs. %.2f\n", totalInitial)
	fmt.Printf("Unallocated Cash:         Rs. %.2f\n", unallocated)
	fmt.Printf("Total End of Day Value:   Rs. %.2f\n", totalFinal+unallocated)
	fmt.Printf("Net Profit/Loss:          Rs. %+.2f\n", netReturn)
	fmt.Printf("Percentage Return:        %+.2f%%\n", pctReturn)
}
