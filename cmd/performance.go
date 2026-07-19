package cmd

import (
	"context"
	"encoding/csv"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/urfave/cli/v3"

	"github.com/gkgarg24/mycase/pkg/yfinance"
)

var PerformanceCommand = &cli.Command{
	Name:  "performance",
	Usage: "Simulate portfolio performance from a purchase date to latest close",
	Flags: []cli.Flag{
		&cli.StringFlag{Name: "file", Aliases: []string{"f"}, Required: true, Usage: "Path to the portfolio CSV file"},
		&cli.FloatFlag{Name: "capital", Value: 100000.0, Usage: "Total capital invested"},
		&cli.StringFlag{Name: "date", Usage: "Purchase date in YYYY-MM-DD or YYYYMMDD format (IST, default: today)"},
		&cli.StringFlag{Name: "time", Value: "09:30", Usage: "Purchase time in HH:MM format (IST)"},
	},
	Action: runPerformance,
}

func runPerformance(ctx context.Context, c *cli.Command) error {
	return runPerfWithParams(ctx, c.String("file"), c.Float("capital"), c.String("date"), c.String("time"))
}

func runPerfWithParams(ctx context.Context, filePath string, capital float64, targetDateStr, targetTimeStr string) error {
	if filePath == "" {
		return fmt.Errorf("--file parameter is required")
	}

	timeParts := strings.Split(targetTimeStr, ":")
	if len(timeParts) != 2 {
		return fmt.Errorf("invalid time format. Must be HH:MM")
	}
	targetHour, err := strconv.Atoi(timeParts[0])
	if err != nil {
		return fmt.Errorf("parsing hour: %w", err)
	}
	targetMin, err := strconv.Atoi(timeParts[1])
	if err != nil {
		return fmt.Errorf("parsing minute: %w", err)
	}

	istLoc := time.FixedZone("IST", 5*3600+30*60)
	nowIST := time.Now().In(istLoc)

	targetDate, err := parsePerfDate(targetDateStr, istLoc)
	if err != nil {
		return err
	}

	targetTime := time.Date(targetDate.Year(), targetDate.Month(), targetDate.Day(), targetHour, targetMin, 0, 0, istLoc)

	useDailyClose := false
	rangeStr := "1d"
	daysDiff := nowIST.Sub(targetTime).Hours() / 24.0

	if targetTime.Before(time.Date(nowIST.Year(), nowIST.Month(), nowIST.Day(), 0, 0, 0, 0, istLoc)) {
		rangeStr = "7d"
	}
	if daysDiff > 7.0 {
		useDailyClose = true
		switch {
		case daysDiff <= 30.0:
			rangeStr = "1mo"
		case daysDiff <= 90.0:
			rangeStr = "3mo"
		case daysDiff <= 180.0:
			rangeStr = "6mo"
		case daysDiff <= 365.0:
			rangeStr = "1y"
		case daysDiff <= 730.0:
			rangeStr = "2y"
		default:
			rangeStr = "5y"
		}
		fmt.Printf("Target purchase time is %.1f days ago (> 7 days). Switching to daily Close prices for %s (ignoring time flag).\n", daysDiff, targetTime.Format("2006-01-02"))
	}

	file, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("opening file %s: %w", filePath, err)
	}
	defer file.Close()

	csvReader := csv.NewReader(file)
	records, err := csvReader.ReadAll()
	if err != nil {
		return fmt.Errorf("reading CSV: %w", err)
	}
	if len(records) < 2 {
		return fmt.Errorf("CSV file contains no data rows")
	}

	tickerIdx, weightIdx := -1, -1
	for i, h := range records[0] {
		hClean := strings.ToLower(strings.TrimSpace(h))
		if hClean == "ticker" {
			tickerIdx = i
		} else if hClean == "weight" {
			weightIdx = i
		}
	}
	if tickerIdx == -1 || weightIdx == -1 {
		return fmt.Errorf("invalid CSV format. Must contain 'ticker' and 'weight' columns")
	}

	type perfStock struct {
		ticker string
		weight float64
	}
	type perfResult struct {
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

	var portfolio []perfStock
	for _, record := range records[1:] {
		if len(record) <= tickerIdx || len(record) <= weightIdx {
			continue
		}
		ticker := strings.TrimSpace(record[tickerIdx])
		weightVal, err := strconv.ParseFloat(strings.TrimSpace(record[weightIdx]), 64)
		if err != nil || ticker == "" {
			continue
		}
		portfolio = append(portfolio, perfStock{ticker: ticker, weight: weightVal})
	}
	if len(portfolio) == 0 {
		return fmt.Errorf("no valid stocks found in CSV")
	}

	if useDailyClose {
		fmt.Printf("Analyzing portfolio performance: Bought at Close on %s till latest Close...\n\n", targetTime.Format("2006-01-02"))
	} else {
		fmt.Printf("Analyzing portfolio performance: Bought on %s at %s IST till latest Close...\n\n", targetTime.Format("2006-01-02"), targetTime.Format("15:04"))
	}

	var wg sync.WaitGroup
	results := make([]perfResult, len(portfolio))

	for i, s := range portfolio {
		wg.Add(1)
		go func(idx int, info perfStock) {
			defer wg.Done()
			res := perfResult{
				ticker:    info.ticker,
				weight:    info.weight,
				allocated: capital * info.weight,
			}

			if useDailyClose {
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
				priceClose := priceAtBuy
				if len(data.Closes) > 0 {
					priceClose = data.Closes[len(data.Closes)-1]
				}
				shares := res.allocated / priceAtBuy
				res.buyPrice = priceAtBuy
				res.buyTime = actualBuyDate.Format("2006-01-02 (Close)")
				res.closePrice = priceClose
				res.finalVal = shares * priceClose
				res.pctReturn = ((priceClose - priceAtBuy) / priceAtBuy) * 100.0
			} else {
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
					if tLocal.Year() == targetTime.Year() && tLocal.YearDay() == targetTime.YearDay() {
						if diff < closestTimeDiff && diff <= 300 {
							closestTimeDiff = diff
							priceAtBuy = data.Opens[j]
							actualBuyTime = tLocal
						}
					}
				}
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
				priceClose := priceAtBuy
				if len(data.Closes) > 0 {
					priceClose = data.Closes[len(data.Closes)-1]
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

	fmt.Printf("%-15s %-8s %-12s %-12s %-22s %-12s %-12s %-10s\n", "Ticker", "Weight", "Allocated", "Buy Price", "Buy Time/Date (IST)", "Close Price", "Final Value", "Return")
	fmt.Println(strings.Repeat("-", 112))

	var totalInitial, totalFinal float64
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
	unallocated := capital - totalInitial

	fmt.Printf("\n--- Portfolio Performance ---\n")
	fmt.Printf("Total Allocated Capital:  Rs. %.2f\n", totalInitial)
	fmt.Printf("Unallocated Cash:         Rs. %.2f\n", unallocated)
	fmt.Printf("Total End of Day Value:   Rs. %.2f\n", totalFinal+unallocated)
	fmt.Printf("Net Profit/Loss:          Rs. %+.2f\n", netReturn)
	fmt.Printf("Percentage Return:        %+.2f%%\n", pctReturn)
	return nil
}

func parsePerfDate(dateStr string, loc *time.Location) (time.Time, error) {
	if dateStr == "" {
		return time.Now().In(loc), nil
	}
	if t, err := time.ParseInLocation("2006-01-02", dateStr, loc); err == nil {
		return t, nil
	}
	if t, err := time.ParseInLocation("20060102", dateStr, loc); err == nil {
		return t, nil
	}
	return time.Time{}, fmt.Errorf("invalid date format: %s. Use YYYY-MM-DD or YYYYMMDD", dateStr)
}
