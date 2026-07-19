package market

import (
	"fmt"
	"math"
	"strings"
	"time"
)

// IsMarketOpen reports whether the given time falls within Indian market hours (Mon-Fri 9:15 AM to 3:30 PM IST).
func IsMarketOpen(t time.Time) bool {
	loc, err := time.LoadLocation("Asia/Kolkata")
	if err != nil {
		loc = time.FixedZone("IST", 5.5*60*60)
	}

	istNow := t.In(loc)
	weekday := istNow.Weekday()
	hour := istNow.Hour()
	minute := istNow.Minute()

	currentMinutes := hour*60 + minute
	// 9:15 AM = 555 minutes. 3:30 PM = 930 minutes.
	isWeekDay := weekday >= time.Monday && weekday <= time.Friday
	return isWeekDay && currentMinutes >= 555 && currentMinutes <= 930
}

// CheckMarketHours prints current market status to standard output and returns true if open.
func CheckMarketHours() bool {
	loc, err := time.LoadLocation("Asia/Kolkata")
	if err != nil {
		loc = time.FixedZone("IST", 5.5*60*60)
	}

	now := time.Now()
	isOpen := IsMarketOpen(now)

	istNow := now.In(loc)
	if isOpen {
		fmt.Printf("\nMarket is currently OPEN (IST: %s). Defaulting to Regular orders.\n", istNow.Format("15:04"))
	} else {
		fmt.Printf("\nMarket is currently CLOSED (IST: %s, weekday=%s).\n", istNow.Format("15:04"), istNow.Weekday())
	}

	return isOpen
}

// CalculateGTTParams returns trigger and limit price complying with Zerodha restrictions.
// Trigger: Must differ from LTP by > 0.25% (we use 0.3%).
// Rounded to nearest 0.10 tick size.
// Limit price: flat ₹2.00 buffer.
func CalculateGTTParams(ltp float64, txType string) (float64, float64) {
	var triggerPrice float64
	if strings.ToUpper(txType) == "BUY" {
		triggerPrice = ltp * 1.003
	} else {
		triggerPrice = ltp * 0.997
	}
	triggerRounded := math.Round(triggerPrice*10.0) / 10.0

	var limitPrice float64
	if strings.ToUpper(txType) == "BUY" {
		limitPrice = ltp + 2.0
	} else {
		limitPrice = ltp - 2.0
	}
	limitRounded := math.Round(limitPrice*10.0) / 10.0

	return triggerRounded, limitRounded
}

// CalculateBufferedLimitPrice returns the buffered limit price (3% above LTP) rounded to the nearest tick size of 0.1.
func CalculateBufferedLimitPrice(ltp float64) float64 {
	bufferPrice := ltp * 1.03
	return math.Round(bufferPrice*10.0) / 10.0
}
