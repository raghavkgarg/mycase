package themereturn

import (
	"fmt"
	"math"
	"sort"
	"time"
)

// EvaluateThemeReturn computes the complete audited return intelligence for a theme.
func EvaluateThemeReturn(
	db *DB,
	theme *MatchedTheme,
	ltpMap map[string]float64,
	opts ThemeReturnOptions,
) (*ThemeReturnReport, error) {
	now := time.Now()
	accountID := opts.AccountID
	if accountID == "" {
		accountID = "CBR420"
	}

	report := &ThemeReturnReport{
		ThemeName:      theme.Name,
		Prefix:         theme.Prefix,
		AccountID:      accountID,
		EvaluationDate: now,
		BenchmarkName:  opts.Benchmark,
	}
	if report.BenchmarkName == "" {
		report.BenchmarkName = "NIFTY50_TRI"
	}

	// 1. Fetch all raw DB records for active + lifecycle symbols
	allSymbols := theme.LifecycleSymbols
	if len(allSymbols) == 0 {
		allSymbols = theme.ActiveSymbols
	}

	trades, err := db.QueryTrades(accountID, allSymbols)
	if err != nil {
		return nil, fmt.Errorf("fetching trades: %w", err)
	}

	closedLots, err := db.QueryClosedLots(accountID, allSymbols)
	if err != nil {
		return nil, fmt.Errorf("fetching closed lots: %w", err)
	}

	dividends, err := db.QueryDividends(accountID, allSymbols)
	if err != nil {
		return nil, fmt.Errorf("fetching dividends: %w", err)
	}

	// Group DB records by symbol
	tradesBySym := make(map[string][]*DBTrade)
	for _, t := range trades {
		tradesBySym[t.Symbol] = append(tradesBySym[t.Symbol], t)
	}

	closedBySym := make(map[string][]*DBClosedLot)
	for _, cl := range closedLots {
		closedBySym[cl.Symbol] = append(closedBySym[cl.Symbol], cl)
	}

	divsBySym := make(map[string][]*DBDividend)
	for _, d := range dividends {
		divsBySym[d.Symbol] = append(divsBySym[d.Symbol], d)
	}

	// 2. Compute metrics for Active Core Holdings
	var earliestActiveDate time.Time
	var activeCashFlows []DatedCashFlow

	for _, sym := range theme.ActiveSymbols {
		sTrades := tradesBySym[sym]
		sDivs := divsBySym[sym]

		totQty := 0
		totCost := 0.0
		var tranches []TrancheDetail
		var sFlows []DatedCashFlow
		var earliestSymDate time.Time
		var latestSymDate time.Time

		for _, t := range sTrades {
			if t.TradeType != "BUY" {
				continue
			}
			totQty += t.Quantity
			cost := float64(t.Quantity)*t.Price + t.TotalCharges
			totCost += cost

			if earliestSymDate.IsZero() || t.TradeDate.Before(earliestSymDate) {
				earliestSymDate = t.TradeDate
			}
			if latestSymDate.IsZero() || t.TradeDate.After(latestSymDate) {
				latestSymDate = t.TradeDate
			}

			tranches = append(tranches, TrancheDetail{
				TradeDate: t.TradeDate,
				TradeType: t.TradeType,
				Quantity:  t.Quantity,
				Price:     t.Price,
				Charges:   t.TotalCharges,
			})

			sFlows = append(sFlows, DatedCashFlow{
				Date:   t.TradeDate,
				Amount: -cost,
			})
			activeCashFlows = append(activeCashFlows, DatedCashFlow{
				Date:   t.TradeDate,
				Amount: -cost,
			})
		}

		if totQty == 0 {
			// Position not yet bought
			continue
		}

		if earliestActiveDate.IsZero() || earliestSymDate.Before(earliestActiveDate) {
			earliestActiveDate = earliestSymDate
		}

		avgBuy := totCost / float64(totQty)
		ltp, ok := ltpMap[sym]
		if !ok || ltp <= 0 {
			ltp = avgBuy
		}
		curVal := float64(totQty) * ltp
		unrealizedPnL := curVal - totCost
		unrealizedPct := (unrealizedPnL / totCost) * 100.0

		// Corporate Dividends
		divTotal := 0.0
		for _, d := range sDivs {
			divTotal += d.TotalAmount
			sFlows = append(sFlows, DatedCashFlow{
				Date:   d.RecordDate,
				Amount: d.TotalAmount,
			})
			activeCashFlows = append(activeCashFlows, DatedCashFlow{
				Date:   d.RecordDate,
				Amount: d.TotalAmount,
			})
		}

		// Terminal cash flow for single-symbol XIRR
		sFlows = append(sFlows, DatedCashFlow{
			Date:   now,
			Amount: curVal,
		})

		holdingDays := int(now.Sub(earliestSymDate).Hours() / 24.0)
		if holdingDays < 1 {
			holdingDays = 1
		}

		hpr := ((curVal + divTotal - totCost) / totCost) * 100.0

		// Single-stock XIRR (require at least 14 days to prevent runaway annualization)
		symXIRR, err := CalculateXIRR(sFlows)
		hasMWR := err == nil && holdingDays >= 14

		sr := &ScriptReturn{
			Symbol:           sym,
			Exchange:         "NSE",
			IsOpen:           true,
			Quantity:         totQty,
			AvgBuyPrice:      avgBuy,
			CurrentPrice:     ltp,
			InvestedValue:    totCost,
			CurrentValue:     curVal,
			UnrealizedPnL:    unrealizedPnL,
			UnrealizedPnLPct: unrealizedPct,
			Dividends:        divTotal,
			TotalWealth:      unrealizedPnL + divTotal,
			HoldingDays:      holdingDays,
			HPR:              hpr,
			MWR:              symXIRR * 100.0,
			HasMWR:           hasMWR,
			FirstBuyDate:     earliestSymDate,
			LastTradeDate:    latestSymDate,
			Tranches:         tranches,
		}

		report.ActivePositions = append(report.ActivePositions, sr)
		report.ActiveInvestedValue += totCost
		report.ActiveCurrentValue += curVal
		report.ActiveUnrealizedPnL += unrealizedPnL
		report.ActiveDividends += divTotal
	}

	report.EarliestPurchase = earliestActiveDate
	report.HoldingDays = int(now.Sub(earliestActiveDate).Hours() / 24.0)
	if report.HoldingDays < 1 {
		report.HoldingDays = 1
	}

	report.ActiveTotalWealth = report.ActiveUnrealizedPnL + report.ActiveDividends
	if report.ActiveInvestedValue > 0 {
		report.ActiveUnrealizedPct = (report.ActiveUnrealizedPnL / report.ActiveInvestedValue) * 100.0
		report.ActiveHPR = (report.ActiveTotalWealth / report.ActiveInvestedValue) * 100.0
		report.ActiveHPRAnn = (math.Pow(1.0+report.ActiveHPR/100.0, 365.25/float64(report.HoldingDays)) - 1.0) * 100.0
	}

	// Terminal valuation for Active Aggregate XIRR
	activeCashFlows = append(activeCashFlows, DatedCashFlow{
		Date:   now,
		Amount: report.ActiveCurrentValue,
	})

	if xirr, err := CalculateXIRR(activeCashFlows); err == nil {
		report.ActiveMWR = xirr * 100.0
	}

	// Active Modified Dietz TWR
	dietzCum, dietzAnn := computeModifiedDietz(activeCashFlows, report.ActiveCurrentValue+report.ActiveDividends, earliestActiveDate, now)
	report.ActiveTWR = dietzCum * 100.0
	report.ActiveTWRAnn = dietzAnn * 100.0

	// 3. Compute Full Lifecycle Metrics (Active + Exited Rebalancing Positions)
	var lifecycleCashFlows []DatedCashFlow
	lifecycleGrossBuys := report.ActiveInvestedValue
	lifecycleGrossSells := 0.0
	lifecycleRealizedGain := 0.0
	lifecycleDividends := report.ActiveDividends

	// Copy active buy flows
	for _, cf := range activeCashFlows {
		if cf.Date != now {
			lifecycleCashFlows = append(lifecycleCashFlows, cf)
		}
	}

	for _, sym := range theme.ExitedSymbols {
		sTrades := tradesBySym[sym]
		sClosed := closedBySym[sym]
		sDivs := divsBySym[sym]

		if len(sTrades) == 0 && len(sClosed) == 0 {
			continue
		}

		buyOutflow := 0.0
		sellInflow := 0.0
		qtySold := 0
		var firstBuy time.Time
		var lastSell time.Time

		var symFlows []DatedCashFlow
		for _, t := range sTrades {
			// Ignore historical trades that occurred prior to theme inception
			if !earliestActiveDate.IsZero() && t.TradeDate.Before(earliestActiveDate) {
				continue
			}

			amt := float64(t.Quantity)*t.Price + t.TotalCharges
			switch t.TradeType {
			case "BUY":
				buyOutflow += amt
				symFlows = append(symFlows, DatedCashFlow{
					Date:   t.TradeDate,
					Amount: -amt,
				})
				if firstBuy.IsZero() || t.TradeDate.Before(firstBuy) {
					firstBuy = t.TradeDate
				}
			case "SELL":
				proceeds := float64(t.Quantity)*t.Price - t.TotalCharges
				sellInflow += proceeds
				qtySold += t.Quantity
				symFlows = append(symFlows, DatedCashFlow{
					Date:   t.TradeDate,
					Amount: proceeds,
				})
				if lastSell.IsZero() || t.TradeDate.After(lastSell) {
					lastSell = t.TradeDate
				}
			}
		}

		if qtySold == 0 || buyOutflow == 0 {
			// No complete trades during theme active window
			continue
		}

		lifecycleGrossBuys += buyOutflow
		lifecycleGrossSells += sellInflow
		lifecycleCashFlows = append(lifecycleCashFlows, symFlows...)

		realizedGain := 0.0
		for _, cl := range sClosed {
			realizedGain += cl.RealizedGain
		}
		// If closed_lots empty but trades exist, compute proceeds - outflow
		if len(sClosed) == 0 && buyOutflow > 0 {
			realizedGain = sellInflow - buyOutflow
		}
		lifecycleRealizedGain += realizedGain

		exitedDivs := 0.0
		for _, d := range sDivs {
			exitedDivs += d.TotalAmount
			lifecycleDividends += d.TotalAmount
			lifecycleCashFlows = append(lifecycleCashFlows, DatedCashFlow{
				Date:   d.RecordDate,
				Amount: d.TotalAmount,
			})
		}

		holdingDays := int(lastSell.Sub(firstBuy).Hours() / 24.0)
		gainPct := 0.0
		if buyOutflow > 0 {
			gainPct = (realizedGain / buyOutflow) * 100.0
		}

		report.ExitedPositions = append(report.ExitedPositions, &ExitedPositionReturn{
			Symbol:       sym,
			QuantitySold: qtySold,
			BuyOutflow:   buyOutflow,
			SellInflow:   sellInflow,
			RealizedGain: realizedGain,
			GainPct:      gainPct,
			Dividends:    exitedDivs,
			TotalProfit:  realizedGain + exitedDivs,
			FirstBuyDate: firstBuy,
			ExitDate:     lastSell,
			HoldingDays:  holdingDays,
		})
	}

	report.LifecycleGrossBuys = lifecycleGrossBuys
	report.LifecycleGrossSells = lifecycleGrossSells
	report.LifecycleNetOutlay = lifecycleGrossBuys - lifecycleGrossSells
	report.LifecycleRealizedGain = lifecycleRealizedGain
	report.LifecycleDividends = lifecycleDividends
	report.LifecycleTotalWealth = report.ActiveUnrealizedPnL + lifecycleRealizedGain + lifecycleDividends

	if lifecycleGrossBuys > 0 {
		report.LifecycleHPR = ((report.ActiveCurrentValue + lifecycleGrossSells + lifecycleDividends - lifecycleGrossBuys) / lifecycleGrossBuys) * 100.0
		report.LifecycleHPRAnn = (math.Pow(1.0+report.LifecycleHPR/100.0, 365.25/float64(report.HoldingDays)) - 1.0) * 100.0
	}

	// Terminal valuation for Lifecycle Aggregate XIRR
	lifecycleCashFlows = append(lifecycleCashFlows, DatedCashFlow{
		Date:   now,
		Amount: report.ActiveCurrentValue,
	})

	if xirr, err := CalculateXIRR(lifecycleCashFlows); err == nil {
		report.LifecycleMWR = xirr * 100.0
	}

	lDietzCum, lDietzAnn := computeModifiedDietz(lifecycleCashFlows, report.ActiveCurrentValue+lifecycleDividends, earliestActiveDate, now)
	report.LifecycleTWR = lDietzCum * 100.0
	report.LifecycleTWRAnn = lDietzAnn * 100.0

	// 4. Benchmark Quotes & Alpha
	if report.BenchmarkName != "" && !earliestActiveDate.IsZero() {
		quotes, qErr := db.QueryBenchmarkQuotes(report.BenchmarkName, earliestActiveDate.AddDate(0, 0, -5), now)
		if qErr == nil && len(quotes) >= 2 {
			sort.Slice(quotes, func(i, j int) bool {
				return quotes[i].TradeDate.Before(quotes[j].TradeDate)
			})
			pStart := quotes[0].ClosePrice
			pEnd := quotes[len(quotes)-1].ClosePrice
			report.BenchmarkStart = pStart
			report.BenchmarkEnd = pEnd
			if pStart > 0 {
				benchTWR := ((pEnd - pStart) / pStart) * 100.0
				report.BenchmarkTWR = benchTWR
				// Alpha: Active HPR/TWR minus Benchmark TWR
				report.BenchmarkAlpha = report.ActiveHPR - benchTWR
			}
		}
	}

	// Sort active positions by Unrealized P&L descending
	sort.Slice(report.ActivePositions, func(i, j int) bool {
		return report.ActivePositions[i].UnrealizedPnL > report.ActivePositions[j].UnrealizedPnL
	})

	// Sort exited positions by TotalProfit descending
	sort.Slice(report.ExitedPositions, func(i, j int) bool {
		return report.ExitedPositions[i].TotalProfit > report.ExitedPositions[j].TotalProfit
	})

	return report, nil
}

// computeModifiedDietz calculates time-weighted return using Modified Dietz method.
func computeModifiedDietz(cashFlows []DatedCashFlow, finalValuation float64, startDate, endDate time.Time) (float64, float64) {
	T := endDate.Sub(startDate).Hours() / 24.0
	if T <= 0 {
		return 0.0, 0.0
	}

	flowsByDate := make(map[string]float64)
	dateMap := make(map[string]time.Time)

	for _, cf := range cashFlows {
		if cf.Date.Equal(endDate) || cf.Date.After(endDate) {
			continue
		}
		dStr := cf.Date.Format("2006-01-02")
		flowsByDate[dStr] += cf.Amount
		dateMap[dStr] = cf.Date
	}

	d0Str := startDate.Format("2006-01-02")
	V0 := -flowsByDate[d0Str]
	if V0 <= 0 {
		V0 = 1.0
	}

	sumF := 0.0
	sumWF := 0.0

	for dStr, amt := range flowsByDate {
		if dStr == d0Str {
			continue
		}
		inflow := -amt
		tDate := dateMap[dStr]
		t_i := tDate.Sub(startDate).Hours() / 24.0
		W_i := (T - t_i) / T
		if W_i < 0 {
			W_i = 0
		}
		sumF += inflow
		sumWF += W_i * inflow
	}

	gain := finalValuation - V0 - sumF
	denom := V0 + sumWF
	if denom <= 0 {
		return 0.0, 0.0
	}

	dietzCum := gain / denom
	dietzAnn := dietzCum
	if T >= 30.0 {
		dietzAnn = math.Pow(1.0+dietzCum, 365.25/T) - 1.0
	}

	return dietzCum, dietzAnn
}
