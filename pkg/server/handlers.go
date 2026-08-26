package server

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/raghavkgarg/mycase/pkg/autopilot"
	"github.com/raghavkgarg/mycase/pkg/backtest"
	"github.com/raghavkgarg/mycase/pkg/broker"
	"github.com/raghavkgarg/mycase/pkg/cache"
	"github.com/raghavkgarg/mycase/pkg/costs"
	"github.com/raghavkgarg/mycase/pkg/csvloader"
	"github.com/raghavkgarg/mycase/pkg/daemon"
	"github.com/raghavkgarg/mycase/pkg/executor"
	"github.com/raghavkgarg/mycase/pkg/monitoring"
	"github.com/raghavkgarg/mycase/pkg/optimizer"
	"github.com/raghavkgarg/mycase/pkg/yfinance"
)

// ── helpers ──────────────────────────────────────────────────────────────────

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-cache")
	json.NewEncoder(w).Encode(v) //nolint:errcheck
}

func writeError(w http.ResponseWriter, code int, msg string) {
	http.Error(w, msg, code)
}

// ── /api/portfolios ───────────────────────────────────────────────────────────

func (s *Server) handlePortfolios(w http.ResponseWriter, r *http.Request) {
	entries, err := os.ReadDir("data")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "cannot read data dir: "+err.Error())
		return
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		if !strings.HasSuffix(name, ".csv") {
			continue
		}
		names = append(names, strings.TrimSuffix(filepath.Base(name), ".csv"))
	}
	writeJSON(w, names)
}

// ── /api/portfolio/{name}/weights ─────────────────────────────────────────────

func (s *Server) handleWeights(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	weights, keys, err := csvloader.LoadBasketCSV("data/" + name + ".csv")
	if err != nil {
		writeError(w, http.StatusNotFound, "portfolio not found: "+err.Error())
		return
	}
	writeJSON(w, map[string]any{
		"weights": weights,
		"keys":    keys,
	})
}

// ── /api/portfolio/{name}/holdings ───────────────────────────────────────────

type holdingRow struct {
	Ticker        string  `json:"ticker"`
	Exchange      string  `json:"exchange"`
	Qty           int     `json:"qty"`
	AvgCost       float64 `json:"avg_cost"`
	LTP           float64 `json:"ltp"`
	CurrentValue  float64 `json:"current_value"`
	UnrealizedPnL float64 `json:"unrealized_pnl"`
	PnLPct        float64 `json:"pnl_pct"`
	ActualWeight  float64 `json:"actual_weight"`
	TargetWeight  float64 `json:"target_weight"`
	Deviation     float64 `json:"deviation"`
}

type holdingsSummary struct {
	TotalValue float64 `json:"total_value"`
	TotalPnL   float64 `json:"total_pnl"`
	DriftIndex float64 `json:"drift_index"`
}

func (s *Server) handleHoldings(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	targetWeights, basketKeys, err := csvloader.LoadBasketCSV("data/" + name + ".csv")
	if err != nil {
		writeError(w, http.StatusNotFound, "portfolio not found: "+err.Error())
		return
	}

	holdings, err := s.broker.GetHoldings()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get holdings: "+err.Error())
		return
	}

	holdingMap := make(map[string]broker.Holding, len(holdings))
	for _, h := range holdings {
		key := strings.ToUpper(h.Exchange) + ":" + h.TradingSymbol
		holdingMap[key] = h
	}

	// Total value is sum of all basket keys that are held.
	var totalValue float64
	for _, key := range basketKeys {
		if h, ok := holdingMap[key]; ok {
			qty := h.Quantity + h.T1Quantity + h.T2Quantity
			totalValue += float64(qty) * h.LastPrice
		}
	}

	rows := make([]holdingRow, 0, len(basketKeys))
	var totalPnL float64
	for _, key := range basketKeys {
		tw := targetWeights[key]
		parts := strings.SplitN(key, ":", 2)
		exchange, symbol := "", key
		if len(parts) == 2 {
			exchange, symbol = parts[0], parts[1]
		}

		row := holdingRow{
			Ticker:       symbol,
			Exchange:     exchange,
			TargetWeight: tw,
		}

		if h, ok := holdingMap[key]; ok {
			qty := h.Quantity + h.T1Quantity + h.T2Quantity
			cv := float64(qty) * h.LastPrice
			var aw float64
			if totalValue > 0 {
				aw = cv / totalValue
			}
			totalPnL += h.PnL
			row.Qty = qty
			row.AvgCost = h.AveragePrice
			row.LTP = h.LastPrice
			row.CurrentValue = cv
			row.UnrealizedPnL = h.PnL
			row.PnLPct = h.PnLPct
			row.ActualWeight = aw
			row.Deviation = aw - tw
		}
		rows = append(rows, row)
	}

	// Drift index = ½ Σ|actualWeight_i − targetWeight_i|
	var driftSum float64
	for _, key := range basketKeys {
		var aw float64
		if h, ok := holdingMap[key]; ok {
			qty := h.Quantity + h.T1Quantity + h.T2Quantity
			if totalValue > 0 {
				aw = float64(qty) * h.LastPrice / totalValue
			}
		}
		driftSum += math.Abs(aw - targetWeights[key])
	}

	writeJSON(w, map[string]any{
		"holdings": rows,
		"summary": holdingsSummary{
			TotalValue: totalValue,
			TotalPnL:   totalPnL,
			DriftIndex: driftSum * 0.5,
		},
	})
}

// ── /api/portfolio/{name}/drift ──────────────────────────────────────────────

func (s *Server) handleDrift(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	targetWeights, basketKeys, err := csvloader.LoadBasketCSV("data/" + name + ".csv")
	if err != nil {
		writeError(w, http.StatusNotFound, "portfolio not found: "+err.Error())
		return
	}
	result, err := daemon.CalculateDrift(r.Context(), s.broker, targetWeights, basketKeys)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "drift calculation failed: "+err.Error())
		return
	}
	writeJSON(w, result)
}

// ── /api/portfolio/{name}/orders ─────────────────────────────────────────────

type orderRow struct {
	Ticker    string  `json:"ticker"`
	Exchange  string  `json:"exchange"`
	Action    string  `json:"action"`
	Qty       int     `json:"qty"`
	LTP       float64 `json:"ltp"`
	Price     float64 `json:"price"`
	Value     float64 `json:"value"`
	TotalCost float64 `json:"total_cost"`
	CostRatio float64 `json:"cost_ratio"`
}

type orderSummary struct {
	TotalBuyValue  float64 `json:"total_buy_value"`
	TotalSellValue float64 `json:"total_sell_value"`
	TotalCost      float64 `json:"total_cost"`
	CostPct        float64 `json:"cost_pct"`
}

func (s *Server) handleOrders(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	targetWeights, basketKeys, err := csvloader.LoadBasketCSV("data/" + name + ".csv")
	if err != nil {
		writeError(w, http.StatusNotFound, "portfolio not found: "+err.Error())
		return
	}

	freshCash := 0.0
	if fc := r.URL.Query().Get("fresh_cash"); fc != "" {
		if v, err := strconv.ParseFloat(fc, 64); err == nil {
			freshCash = v
		}
	}

	holdings, err := s.broker.GetHoldings()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get holdings: "+err.Error())
		return
	}
	// currentQty keyed by TradingSymbol (no exchange prefix)
	currentQty := make(map[string]int, len(holdings))
	avgCostMap := make(map[string]float64, len(holdings))
	for _, h := range holdings {
		currentQty[h.TradingSymbol] += h.Quantity + h.T1Quantity + h.T2Quantity
		avgCostMap[h.TradingSymbol] = h.AveragePrice
	}

	quotes, err := s.broker.GetQuotes(basketKeys)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get quotes: "+err.Error())
		return
	}

	// Total value of basket-held positions.
	var totalValue float64
	for _, key := range basketKeys {
		parts := strings.SplitN(key, ":", 2)
		sym := key
		if len(parts) == 2 {
			sym = parts[1]
		}
		totalValue += float64(currentQty[sym]) * quotes[key]
	}
	totalTarget := totalValue + freshCash

	// Build orders.
	var orders []broker.Order
	for _, key := range basketKeys {
		ltp := quotes[key]
		if ltp <= 0 {
			continue
		}
		w2 := targetWeights[key]
		parts := strings.SplitN(key, ":", 2)
		exchange, sym := "", key
		if len(parts) == 2 {
			exchange, sym = parts[0], parts[1]
		}
		targetQty := int(math.Round(totalTarget * w2 / ltp))
		diff := targetQty - currentQty[sym]
		if diff == 0 {
			continue
		}
		txType := "BUY"
		price := math.Round((ltp+2)*10) / 10
		if diff < 0 {
			txType = "SELL"
			price = math.Round((ltp-2)*10) / 10
			diff = -diff
		}
		orders = append(orders, broker.Order{
			TradingSymbol:   sym,
			Exchange:        exchange,
			TransactionType: txType,
			Quantity:        diff,
			OrderType:       "LIMIT",
			Product:         "CNC",
			Price:           price,
			Ltp:             ltp,
		})
	}

	kept, filtered := optimizer.FilterMicroTransactions(orders, quotes, costs.DefaultZerodha, 0.005)

	// Build kept order rows with cost breakdown and tax warnings.
	keptRows := make([]orderRow, 0, len(kept))
	var taxWarnings []costs.TaxWarning
	var sumBuy, sumSell, sumCost float64

	for _, o := range kept {
		bd := costs.DefaultZerodha.Calculate(o.TransactionType, o.Quantity, o.Price)
		keptRows = append(keptRows, orderRow{
			Ticker:    o.TradingSymbol,
			Exchange:  o.Exchange,
			Action:    o.TransactionType,
			Qty:       o.Quantity,
			LTP:       o.Ltp,
			Price:     o.Price,
			Value:     bd.TradeValue,
			TotalCost: bd.Total,
			CostRatio: bd.CostRatio,
		})
		if o.TransactionType == "BUY" {
			sumBuy += bd.TradeValue
		} else {
			sumSell += bd.TradeValue
			avg := avgCostMap[o.TradingSymbol]
			tw := costs.ClassifySell(o.TradingSymbol, o.Quantity, o.Price, avg, time.Time{})
			taxWarnings = append(taxWarnings, tw)
		}
		sumCost += bd.Total
	}

	filteredRows := make([]orderRow, 0, len(filtered))
	for _, o := range filtered {
		bd := costs.DefaultZerodha.Calculate(o.TransactionType, o.Quantity, o.Price)
		filteredRows = append(filteredRows, orderRow{
			Ticker:    o.TradingSymbol,
			Exchange:  o.Exchange,
			Action:    o.TransactionType,
			Qty:       o.Quantity,
			LTP:       o.Ltp,
			Price:     o.Price,
			Value:     bd.TradeValue,
			TotalCost: bd.Total,
			CostRatio: bd.CostRatio,
		})
	}

	totalTradeValue := sumBuy + sumSell
	costPct := 0.0
	if totalTradeValue > 0 {
		costPct = sumCost / totalTradeValue * 100
	}

	writeJSON(w, map[string]any{
		"orders":       keptRows,
		"filtered_out": filteredRows,
		"summary": orderSummary{
			TotalBuyValue:  sumBuy,
			TotalSellValue: sumSell,
			TotalCost:      sumCost,
			CostPct:        costPct,
		},
		"tax_warnings": taxWarnings,
	})
}

// ── /api/portfolio/{name}/monitor ────────────────────────────────────────────

func (s *Server) handleMonitor(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	style := r.URL.Query().Get("style")
	if style == "" {
		style = "moderate"
	}

	weights, keys, err := csvloader.LoadBasketCSV("data/" + name + ".csv")
	if err != nil {
		writeError(w, http.StatusNotFound, "portfolio not found: "+err.Error())
		return
	}

	portfolio := make([]monitoring.StockInfo, 0, len(keys))
	for _, k := range keys {
		portfolio = append(portfolio, monitoring.StockInfo{Ticker: k, Weight: weights[k]})
	}

	params := monitorPresetParams(style)
	params.MaxCapExYoYMultiplier = 2.0

	ctx := r.Context()
	var (
		mu        sync.Mutex
		wg        sync.WaitGroup
		liveHist  = make(map[string]*yfinance.HistoricalData)
		liveFunds = make(map[string]yfinance.Fundamentals)
		benchData *yfinance.HistoricalData
	)

	wg.Go(func() {
		benchTicker := broker.LoadMarketConfig().Benchmark
		b, ferr := yfinance.FetchHistoricalDataWithTimestamps(ctx, benchTicker, "2y")
		if ferr == nil && b != nil && len(b.Closes) >= 200 {
			mu.Lock()
			benchData = b
			mu.Unlock()
		}
	})

	for _, t := range keys {
		wg.Add(2)
		go func(ticker string) {
			defer wg.Done()
			h, ferr := yfinance.FetchHistoricalDataWithTimestamps(ctx, ticker, "2y")
			if ferr == nil && h != nil && len(h.Closes) >= 200 {
				mu.Lock()
				liveHist[ticker] = h
				mu.Unlock()
			}
		}(t)
		go func(ticker string) {
			defer wg.Done()
			funds, ferr := yfinance.FetchFundamentals(ctx, []string{ticker})
			if ferr == nil && len(funds) > 0 {
				mu.Lock()
				if val, ok := funds[ticker]; ok {
					liveFunds[ticker] = val
				}
				mu.Unlock()
			}
		}(t)
	}
	wg.Wait()

	histData, outBench, fundamentals, mockedTickers, _ := monitoring.FillWithMockData(keys, liveHist, liveFunds, benchData)

	for i := range portfolio {
		if mockedTickers[portfolio[i].Ticker] {
			portfolio[i].IsMock = true
		}
	}

	result, err := monitoring.RunSimulation(portfolio, params, histData, outBench, fundamentals, 100000)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "simulation failed: "+err.Error())
		return
	}
	writeJSON(w, result)
}

// monitorPresetParams returns PolicyParams for the given style name.
func monitorPresetParams(style string) monitoring.PolicyParams {
	switch strings.ToLower(style) {
	case "hyper-aggressive":
		return monitoring.PolicyParams{
			ConsecutiveQuartersExit:   1,
			DSODeteriorationThreshold: 0.10,
			SMADays:                   5,
			RebalanceMonths:           3,
			MaxWeightDrift:            0.12,
		}
	case "passive":
		return monitoring.PolicyParams{
			ConsecutiveQuartersExit:   3,
			DSODeteriorationThreshold: 0.25,
			SMADays:                   20,
			RebalanceMonths:           12,
			MaxWeightDrift:            0.20,
		}
	default:
		return monitoring.PolicyParams{
			ConsecutiveQuartersExit:   2,
			DSODeteriorationThreshold: 0.15,
			SMADays:                   10,
			RebalanceMonths:           6,
			MaxWeightDrift:            0.15,
		}
	}
}

// ── POST /api/portfolio/{name}/backtest ──────────────────────────────────────

type backtestRequest struct {
	From      string  `json:"from"`
	To        string  `json:"to"`
	Rebalance string  `json:"rebalance"`
	Capital   float64 `json:"capital"`
	Slippage  float64 `json:"slippage"` // percent, e.g. 0.1
	Benchmark string  `json:"benchmark"`
}

func (s *Server) handleBacktest(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")

	var params backtestRequest
	if err := json.NewDecoder(r.Body).Decode(&params); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	weights, keys, err := csvloader.LoadBasketCSV("data/" + name + ".csv")
	if err != nil {
		writeError(w, http.StatusNotFound, "portfolio not found: "+err.Error())
		return
	}

	holdings := make([]backtest.Holding, 0, len(keys))
	for _, k := range keys {
		if wt := weights[k]; wt > 0 {
			holdings = append(holdings, backtest.Holding{Ticker: k, Weight: wt})
		}
	}
	if len(holdings) == 0 {
		writeError(w, http.StatusBadRequest, "no active tickers in portfolio")
		return
	}

	ist := time.FixedZone("IST", 5*3600+30*60)
	fromTime, err := time.ParseInLocation("2006-01-02", params.From, ist)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid from date: "+err.Error())
		return
	}
	toTime := time.Now().In(ist)
	if params.To != "" {
		toTime, err = time.ParseInLocation("2006-01-02", params.To, ist)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid to date: "+err.Error())
			return
		}
	}

	benchmark := params.Benchmark
	if benchmark == "" {
		benchmark = broker.LoadMarketConfig().Benchmark
	}
	capital := params.Capital
	if capital <= 0 {
		capital = 100000.0
	}
	rebalFreq := backtest.RebalanceFreq(params.Rebalance)
	if rebalFreq == "" {
		rebalFreq = backtest.FreqQuarterly
	}
	slippage := params.Slippage / 100.0

	// Fetch price data concurrently.
	type fetchResult struct {
		ticker string
		hist   *yfinance.HistoricalData
		err    error
	}
	ctx := r.Context()
	resultCh := make(chan fetchResult, len(holdings)+1)

	for _, h := range holdings {
		go func(ticker string) {
			hist, ferr := yfinance.FetchHistoricalByDateRange(ctx, ticker, fromTime, toTime)
			resultCh <- fetchResult{ticker: ticker, hist: hist, err: ferr}
		}(h.Ticker)
	}
	go func() {
		hist, ferr := yfinance.FetchHistoricalByDateRange(ctx, benchmark, fromTime, toTime)
		resultCh <- fetchResult{ticker: benchmark, hist: hist, err: ferr}
	}()

	priceData := make(map[string]*yfinance.HistoricalData, len(holdings))
	var benchData *yfinance.HistoricalData

	for range len(holdings) + 1 {
		res := <-resultCh
		if res.err != nil {
			writeError(w, http.StatusInternalServerError,
				fmt.Sprintf("price fetch failed for %s: %v", res.ticker, res.err))
			return
		}
		if res.ticker == benchmark {
			benchData = res.hist
		} else {
			priceData[res.ticker] = res.hist
		}
	}

	cfg := backtest.SimConfig{
		InitialCapital:  capital,
		From:            fromTime,
		To:              toTime,
		Rebalance:       rebalFreq,
		SlippagePct:     slippage,
		BenchmarkTicker: benchmark,
	}

	simResult, err := backtest.Run(holdings, priceData, benchData, cfg)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "backtest failed: "+err.Error())
		return
	}

	// Stream snapshots as NDJSON.
	w.Header().Set("Content-Type", "application/x-ndjson")
	w.Header().Set("Cache-Control", "no-cache")

	flusher, canFlush := w.(http.Flusher)
	enc := json.NewEncoder(w)

	for _, snap := range simResult.Snapshots {
		select {
		case <-ctx.Done():
			return
		default:
		}
		line := map[string]any{
			"type":      "snapshot",
			"date":      snap.Date.Format("2006-01-02"),
			"portfolio": snap.PortfolioValue,
			"benchmark": snap.BenchmarkValue,
		}
		enc.Encode(line) //nolint:errcheck
		if canFlush {
			flusher.Flush()
		}
	}

	final := map[string]any{
		"type":             "result",
		"cagr":             simResult.CAGR,
		"benchmark_cagr":   simResult.BenchmarkCAGR,
		"total_return":     simResult.TotalReturn,
		"benchmark_return": simResult.BenchmarkReturn,
		"sharpe":           simResult.SharpeRatio,
		"sortino":          simResult.SortinoRatio,
		"calmar":           simResult.CalmarRatio,
		"max_drawdown":     simResult.MaxDrawdown,
		"alpha":            simResult.Alpha,
		"beta":             simResult.Beta,
		"trading_days":     simResult.TradingDays,
		"rebalance_count":  simResult.RebalanceCount,
	}
	enc.Encode(final) //nolint:errcheck
	if canFlush {
		flusher.Flush()
	}
}

// ── POST /api/portfolio/{name}/execute ───────────────────────────────────────

func (s *Server) handleExecute(w http.ResponseWriter, r *http.Request) {
	if s.broker.IsMock() {
		writeError(w, http.StatusForbidden, "live execution disabled in mock mode")
		return
	}

	name := r.PathValue("name")
	targetWeights, basketKeys, err := csvloader.LoadBasketCSV("data/" + name + ".csv")
	if err != nil {
		writeError(w, http.StatusNotFound, "portfolio not found: "+err.Error())
		return
	}

	holdings, err := s.broker.GetHoldings()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get holdings: "+err.Error())
		return
	}
	currentQty := make(map[string]int, len(holdings))
	for _, h := range holdings {
		currentQty[h.TradingSymbol] += h.Quantity + h.T1Quantity + h.T2Quantity
	}

	quotes, err := s.broker.GetQuotes(basketKeys)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get quotes: "+err.Error())
		return
	}

	var totalValue float64
	for _, key := range basketKeys {
		parts := strings.SplitN(key, ":", 2)
		sym := key
		if len(parts) == 2 {
			sym = parts[1]
		}
		totalValue += float64(currentQty[sym]) * quotes[key]
	}

	var orders []broker.Order
	for _, key := range basketKeys {
		ltp := quotes[key]
		if ltp <= 0 {
			continue
		}
		wt := targetWeights[key]
		parts := strings.SplitN(key, ":", 2)
		exchange, sym := "", key
		if len(parts) == 2 {
			exchange, sym = parts[0], parts[1]
		}
		targetQty := int(math.Round(totalValue * wt / ltp))
		diff := targetQty - currentQty[sym]
		if diff == 0 {
			continue
		}
		txType := "BUY"
		price := math.Round((ltp+2)*10) / 10
		if diff < 0 {
			txType = "SELL"
			price = math.Round((ltp-2)*10) / 10
			diff = -diff
		}
		orders = append(orders, broker.Order{
			TradingSymbol:   sym,
			Exchange:        exchange,
			TransactionType: txType,
			Quantity:        diff,
			OrderType:       "LIMIT",
			Product:         "CNC",
			Price:           price,
			Ltp:             ltp,
		})
	}

	kept, _ := optimizer.FilterMicroTransactions(orders, quotes, costs.DefaultZerodha, 0.005)

	type placeResult struct {
		Ticker    string  `json:"ticker"`
		Action    string  `json:"action"`
		Qty       int     `json:"qty"`
		Price     float64 `json:"price"`
		OrderID   string  `json:"order_id,omitempty"`
		TriggerID int     `json:"trigger_id,omitempty"`
		Error     string  `json:"error,omitempty"`
	}

	placed := make([]placeResult, 0, len(kept))
	var errs []string
	var successLines []string
	var failedLines []string
	var failedSpecs []executor.FailedOrderSpec

	nowStr := time.Now().Format("060102_150405")

	for i, o := range kept {
		if i > 0 {
			time.Sleep(200 * time.Millisecond)
		}
		res, perr := s.broker.PlaceOrder("regular", o)
		pr := placeResult{
			Ticker: o.TradingSymbol,
			Action: o.TransactionType,
			Qty:    o.Quantity,
			Price:  o.Price,
		}
		if perr != nil {
			pr.Error = perr.Error()
			errs = append(errs, fmt.Sprintf("%s: %v", o.TradingSymbol, perr))
			failedLines = append(failedLines, fmt.Sprintf("Error placing REGULAR %s order for %s: %v", strings.ToUpper(o.TransactionType), o.TradingSymbol, perr))
			failedSpecs = append(failedSpecs, executor.FailedOrderSpec{
				TradingSymbol:   o.TradingSymbol,
				Exchange:        o.Exchange,
				TransactionType: o.TransactionType,
				Quantity:        o.Quantity,
				Price:           o.Price,
				Product:         o.Product,
				ErrorReason:     perr.Error(),
				OrderVariety:    "regular",
			})
		} else {
			pr.OrderID = res.OrderID
			pr.TriggerID = res.TriggerID
			successLines = append(successLines, fmt.Sprintf("Placed REGULAR %s order %s for %d shares of %s @ ₹%.2f", strings.ToUpper(o.TransactionType), res.OrderID, o.Quantity, o.TradingSymbol, o.Price))
		}
		placed = append(placed, pr)
	}

	if len(successLines) > 0 {
		executor.SaveSuccessLog(fmt.Sprintf("API EXECUTION (%s)", name), strings.Join(successLines, "\n"), nowStr)
	}
	if len(failedSpecs) > 0 {
		executor.SaveErrorLog(fmt.Sprintf("API EXECUTION (%s)", name), strings.Join(failedLines, "\n"), failedSpecs, nowStr)
	}

	writeJSON(w, map[string]any{
		"placed": placed,
		"errors": errs,
	})
}

// ── /api/portfolio/{name}/retry ───────────────────────────────────────────────

func (s *Server) handleRetry(w http.ResponseWriter, r *http.Request) {
	jsonPath, err := executor.FindLatestErrorPayload()
	if err != nil {
		writeError(w, http.StatusNotFound, "No retry payload found: "+err.Error())
		return
	}

	go executor.ExecuteRetryPayload(jsonPath, s.broker, nil)

	writeJSON(w, map[string]any{
		"message": "Retry execution launched for " + jsonPath,
	})
}

// ── /api/cache/status ─────────────────────────────────────────────────────────

func (s *Server) handleCacheStatus(w http.ResponseWriter, r *http.Request) {
	if s.cache == nil {
		writeJSON(w, map[string]any{"entries": []any{}})
		return
	}
	entries, err := s.cache.Status(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "cache status error: "+err.Error())
		return
	}
	if entries == nil {
		entries = []cache.StatusEntry{}
	}
	writeJSON(w, map[string]any{"entries": entries})
}

// ── /api/daemon/history ───────────────────────────────────────────────────────

func (s *Server) handleDaemonHistory(w http.ResponseWriter, r *http.Request) {
	state, err := daemon.LoadState()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load daemon state: "+err.Error())
		return
	}
	writeJSON(w, state)
}

// ── /api/autopilot/* ──────────────────────────────────────────────────────────

func (s *Server) handleAutopilotProposal(w http.ResponseWriter, r *http.Request) {
	proposal, err := autopilot.LoadProposal()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load proposal: "+err.Error())
		return
	}
	if proposal == nil {
		writeJSON(w, map[string]any{"proposal": nil})
		return
	}
	writeJSON(w, map[string]any{"proposal": proposal})
}

func (s *Server) handleAutopilotConfirm(w http.ResponseWriter, r *http.Request) {
	proposal, err := autopilot.LoadProposal()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load proposal: "+err.Error())
		return
	}
	if proposal == nil {
		writeError(w, http.StatusNotFound, "no pending proposal")
		return
	}
	if proposal.IsExpired() {
		writeError(w, http.StatusGone, "proposal has expired")
		return
	}
	if proposal.Status != autopilot.StatusPending {
		writeError(w, http.StatusConflict, "proposal is not pending (status: "+proposal.Status+")")
		return
	}
	if s.broker.IsMock() {
		writeError(w, http.StatusForbidden, "cannot execute in mock mode — start server with --live")
		return
	}

	// Execute orders from the proposal
	var results []autopilot.OrderResult
	for _, o := range proposal.Orders {
		order := broker.Order{
			TradingSymbol:   o.Ticker,
			Exchange:        o.Exchange,
			TransactionType: o.Action,
			Quantity:        o.Quantity,
			OrderType:       "LIMIT",
			Product:         "CNC",
			Price:           o.LimitPrice,
		}
		result, err := s.broker.PlaceOrder("regular", order)
		if err != nil {
			results = append(results, autopilot.OrderResult{
				Ticker:  o.Ticker,
				Action:  o.Action,
				Success: false,
				Error:   err.Error(),
			})
		} else {
			results = append(results, autopilot.OrderResult{
				Ticker:  o.Ticker,
				Action:  o.Action,
				OrderID: result.OrderID,
				Success: true,
			})
		}
		time.Sleep(200 * time.Millisecond) // throttle
	}

	// Update proposal with execution results
	proposal.ExecutionLog = results
	if err := autopilot.ConfirmProposal(proposal); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to confirm proposal: "+err.Error())
		return
	}

	// Archive
	_ = autopilot.ArchiveProposal(proposal)

	// Count results
	successCount := 0
	for _, r := range results {
		if r.Success {
			successCount++
		}
	}

	writeJSON(w, map[string]any{
		"status":  "confirmed",
		"placed":  successCount,
		"failed":  len(results) - successCount,
		"results": results,
	})
}

func (s *Server) handleAutopilotDismiss(w http.ResponseWriter, r *http.Request) {
	proposal, err := autopilot.LoadProposal()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load proposal: "+err.Error())
		return
	}
	if proposal == nil {
		writeError(w, http.StatusNotFound, "no pending proposal")
		return
	}
	if proposal.Status != autopilot.StatusPending {
		writeError(w, http.StatusConflict, "proposal is not pending (status: "+proposal.Status+")")
		return
	}

	if err := autopilot.DismissProposal(proposal); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to dismiss: "+err.Error())
		return
	}
	_ = autopilot.ArchiveProposal(proposal)

	writeJSON(w, map[string]any{
		"status": "dismissed",
		"id":     proposal.ID,
	})
}
