package performance

import (
	"fmt"
	"sync"
	"time"

	"github.com/raghavkgarg/mycase/pkg/yfinance"
)

// StockEntry holds a ticker and its portfolio weight.
type StockEntry struct {
	Ticker string
	Weight float64
}

// StockResult holds the per-stock P&L calculation result.
type StockResult struct {
	Ticker     string
	Weight     float64
	Allocated  float64
	BuyPrice   float64
	BuyTime    string
	ClosePrice float64
	FinalValue float64
	PctReturn  float64
	Err        error
}

// ValuatePortfolio computes per-stock P&L from the target purchase time to the
// latest available close. Daily-close mode is used when the purchase was more than
// 7 days ago; intraday mode is used otherwise.
func ValuatePortfolio(
	portfolio []StockEntry,
	capital float64,
	targetTime time.Time,
	useDailyClose bool,
	rangeStr string,
	istLoc *time.Location,
) []StockResult {
	results := make([]StockResult, len(portfolio))
	var wg sync.WaitGroup

	for i, s := range portfolio {
		wg.Add(1)
		go func(idx int, info StockEntry) {
			defer wg.Done()
			res := StockResult{
				Ticker:    info.Ticker,
				Weight:    info.Weight,
				Allocated: capital * info.Weight,
			}

			if useDailyClose {
				data, err := yfinance.FetchHistoricalDataWithTimestamps(info.Ticker, rangeStr)
				if err != nil {
					res.Err = err
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
					res.Err = fmt.Errorf("no daily price data available around %s", targetTime.Format("2006-01-02"))
					results[idx] = res
					return
				}
				priceClose := priceAtBuy
				if len(data.Closes) > 0 {
					priceClose = data.Closes[len(data.Closes)-1]
				}
				shares := res.Allocated / priceAtBuy
				res.BuyPrice = priceAtBuy
				res.BuyTime = actualBuyDate.Format("2006-01-02 (Close)")
				res.ClosePrice = priceClose
				res.FinalValue = shares * priceClose
				res.PctReturn = ((priceClose - priceAtBuy) / priceAtBuy) * 100.0
			} else {
				data, err := yfinance.FetchIntradayData(info.Ticker, rangeStr)
				if err != nil {
					res.Err = err
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
					res.Err = fmt.Errorf("no price data available for target date %s", targetTime.Format("2006-01-02"))
					results[idx] = res
					return
				}
				priceClose := priceAtBuy
				if len(data.Closes) > 0 {
					priceClose = data.Closes[len(data.Closes)-1]
				}
				shares := res.Allocated / priceAtBuy
				res.BuyPrice = priceAtBuy
				res.BuyTime = actualBuyTime.Format("2006-01-02 15:04:05")
				res.ClosePrice = priceClose
				res.FinalValue = shares * priceClose
				res.PctReturn = ((priceClose - priceAtBuy) / priceAtBuy) * 100.0
			}
			results[idx] = res
		}(i, s)
	}
	wg.Wait()
	return results
}
