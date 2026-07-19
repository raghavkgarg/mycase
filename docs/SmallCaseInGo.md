# Mycase Basket & Rebalancing Engine in Go

This document outlines the design, module structure, and implementation plan for porting the Mojo Mycase system to Go (Golang) using the official Zerodha Go SDK (`gokiteconnect/v4`).

---

## 1. Directory Structure

We will adopt standard Go project layout practices:

```
mycase-go/
├── cmd/
│   ├── mycase/
│   │   └── main.go          # Main CLI (Fresh Buy / Rebalance menu)
│   └── holdings/
│       └── main.go          # Portfolio holdings snapshot printer
├── pkg/
│   ├── config/
│   │   └── config.go        # JSON credentials mapping (config.json)
│   ├── csvloader/
│   │   └── loader.go        # Data parser (basket.csv, myall.csv)
│   ├── kiteclient/
│   │   └── client.go        # Wrapper for gokiteconnect initialization
│   ├── market/
│   │   └── market.go        # Market open checks, AMO/GTT/Regular placement
│   ├── optimizer/
│   │   └── optimizer.go     # Greedy budget-allocation optimizer
│   ├── printer/
│   │   └── printer.go       # Unicode-aligned table layouts & terminal outputs
│   └── yfinance/
│       └── yfinance.go      # HTTP quote parser fetching from Yahoo Finance REST API
├── config/
│   └── config.json          # Credentials store
├── data/
│   ├── basket.csv           # Target weights csv
│   └── myall.csv            # Self-managed holdings checklist
├── go.mod
└── go.sum
```

---

## 2. Dependency Selection

Instead of relying on Python wrappers or yfinance CLI utilities, Go enables native, high-performance network clients.

* **Kite Connect:** Official package [github.com/zerodha/gokiteconnect/v4](https://github.com/zerodha/gokiteconnect).
* **Real-time Quotes (yfinance):** Instead of importing python, we can make direct HTTP requests to Yahoo Finance's query v7 endpoint:
  `https://query1.finance.yahoo.com/v7/finance/quote?symbols=SWSOLAR.NS,ADVAIT.NS,INOXINDIA.NS`
  This returns standard JSON, which Go's `encoding/json` can parse natively into structs without external dependencies.
* **Date & Time timezone handling:** Standard library `"time"` with `time.LoadLocation("Asia/Kolkata")`.

---

## 3. Core Engine Implementations in Go

### A. Real-Time Quotes (`pkg/yfinance/yfinance.go`)
Directly fetch Yahoo Finance quotes with standard HTTP headers:
```go
package yfinance

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

type QuoteResponse struct {
	QuoteResponse struct {
		Result []struct {
			Symbol             string  `json:"symbol"`
			RegularMarketPrice float64 `json:"regularMarketPrice"`
		} `json:"result"`
	} `json:"quoteResponse"`
}

func FetchQuotes(tickers []string) (map[string]float64, error) {
	var yahooSymbols []string
	for _, t := range tickers {
		// Convert "NSE:SYMBOL" -> "SYMBOL.NS"
		parts := strings.Split(t, ":")
		if len(parts) == 2 {
			yahooSymbols = append(yahooSymbols, parts[1]+".NS")
		}
	}

	url := fmt.Sprintf("https://query1.finance.yahoo.com/v7/finance/quote?symbols=%s", strings.Join(yahooSymbols, ","))
	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("User-Agent", "Mozilla/5.0") // Required to avoid rate-limiting

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var quoteRes QuoteResponse
	if err := json.NewDecoder(resp.Body).Decode(&quoteRes); err != nil {
		return nil, err
	}

	prices := make(map[string]float64)
	for _, res := range quoteRes.QuoteResponse.Result {
		// Map back: "SYMBOL.NS" -> "NSE:SYMBOL"
		cleanSym := strings.TrimSuffix(res.Symbol, ".NS")
		prices["NSE:"+cleanSym] = res.RegularMarketPrice
	}
	return prices, nil
}
```

### B. Greedy Optimizer (`pkg/optimizer/optimizer.go`)
Implementing the local-minima-free budget optimizer cleanly using strict typing:
```go
package optimizer

import (
	"math"
)

type AssetInfo struct {
	Ticker       string
	LTP          float64
	TargetWeight float64
	CurrentQty   int
}

func OptimizeFreshBuy(assets []AssetInfo, totalInvestment float64) []int {
	n := len(assets)
	rawQuantities := make([]int, n)
	
	// Calculate baseline cost (initial 1 share for items not owned)
	baselineCost := 0.0
	for i, asset := range assets {
		limitPrice := math.Round((asset.LTP+2.0)*10.0) / 10.0 // +₹2 buffer
		if asset.CurrentQty == 0 {
			rawQuantities[i] = 1
			baselineCost += limitPrice
		} else {
			rawQuantities[i] = asset.CurrentQty
		}
	}

	// Fallback if baseline cost overshoots budget
	if baselineCost > totalInvestment {
		for i, asset := range assets {
			rawQuantities[i] = asset.CurrentQty
		}
	}

	// Greedy allocation loop
	for {
		bestIdx := -1
		maxEfficiency := -999.0

		// Current deviation
		currentTotalCost := 0.0
		for i, asset := range assets {
			currentTotalCost += float64(rawQuantities[i]) * asset.LTP
		}

		currentDev := 0.0
		if currentTotalCost > 0 {
			for i, asset := range assets {
				actWt := (float64(rawQuantities[i]) * asset.LTP) / currentTotalCost
				currentDev += math.Abs(actWt - asset.TargetWeight)
			}
		} else {
			currentDev = 999.0
		}

		// Evaluate candidates
		for i, asset := range assets {
			candidateQtys := make([]int, n)
			copy(candidateQtys, rawQuantities)
			candidateQtys[i]++

			// Evaluate candidates cost & deviation
			newSharesCost := 0.0
			newTotalLTPCost := 0.0
			limitPrice := math.Round((asset.LTP+2.0)*10.0) / 10.0

			for j, a := range assets {
				added := candidateQtys[j] - a.CurrentQty
				if added > 0 {
					lPrice := math.Round((a.LTP+2.0)*10.0) / 10.0
					newSharesCost += float64(added) * lPrice
				}
				newTotalLTPCost += float64(candidateQtys[j]) * a.LTP
			}

			if newSharesCost <= totalInvestment {
				dev := 0.0
				for j, a := range assets {
					actWt := (float64(candidateQtys[j]) * a.LTP) / newTotalLTPCost
					dev += math.Abs(actWt - a.TargetWeight)
				}
				devRed := currentDev - dev
				efficiency := devRed / limitPrice
				if efficiency > maxEfficiency {
					maxEfficiency = efficiency
					bestIdx = i
				}
			}
		}

		if bestIdx != -1 {
			rawQuantities[bestIdx]++
		} else {
			break
		}
	}

	return rawQuantities
}
```

### C. GTT Trigger & Limit Calculation (`pkg/market/market.go`)
Handle trigger price validation constraints (+0.3% buffer from LTP) and NSE tick size rounding (nearest 0.10):
```go
package market

import (
	"math"
	"strings"
)

// CalculateGTTParams returns the trigger values and limit price complying with Zerodha restrictions
func CalculateGTTParams(ltp float64, txType string) (float64, float64) {
	// Trigger: Must differ from LTP by > 0.25% (we use 0.30%)
	var triggerPrice float64
	if strings.ToUpper(txType) == "BUY" {
		triggerPrice = ltp * 1.003
	} else {
		triggerPrice = ltp * 0.997
	}
	
	// Tick Size Rounding (nearest 0.10)
	triggerRounded := math.Round(triggerPrice*10.0) / 10.0

	// Limit price: flat ₹2.00 buffer
	var limitPrice float64
	if strings.ToUpper(txType) == "BUY" {
		limitPrice = ltp + 2.0
	} else {
		limitPrice = ltp - 2.0
	}
	limitRounded := math.Round(limitPrice*10.0) / 10.0

	return triggerRounded, limitRounded
}
```

### D. Holdings Segmentation & Sorting (`cmd/holdings/main.go`)
We will segment holdings based on `myall.csv` and sort them on PnL% ascending:
```go
package main

import (
	"fmt"
	"math"
	"sort"
)

type Holding struct {
	TradingSymbol string
	Exchange      string
	Quantity      int
	AveragePrice  float64
	LastPrice     float64
	PnL           float64
	PnLPct        float64
	PendingT1     bool
}

// Implement sort.Interface for []Holding
type ByPnLPct []Holding
func (a ByPnLPct) Len() int           { return len(a) }
func (a ByPnLPct) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }
func (a ByPnLPct) Less(i, j int) bool { return a[i].PnLPct < a[j].PnLPct }

func FormatCurrency(val float64) string {
	sign := ""
	if val < 0 {
		sign = "-"
	} else if val > 0 {
		sign = "+"
	}
	return fmt.Sprintf("%s₹%.2f", sign, math.Abs(val))
}
```

---

## 4. Key Benefits of Porting to Go

1. **Standalone Binary:** Compile the whole system into simple static binaries (`holdings` and `mycase`) that don't need Python libraries, `.venv` setups, or shebang wrappers.
2. **True Multithreading:** Go's channels and goroutines can fetch quotes for multiple instruments in parallel instantly.
3. **No Dynamic Type Casting Overhead:** Mojo's `PythonObject` boundary checks are replaced by strict, native struct mappings at compilation, making logic much less prone to runtime crashes.
4. **Clean UTF-8 Handling:** Go handles UTF-8 strings natively (`unicode/utf8` package), making column alignment with multi-byte symbols (`₹`) simpler to calculate correctly.
