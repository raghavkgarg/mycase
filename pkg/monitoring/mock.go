package monitoring

import (
	"math"
	"math/rand/v2"
	"time"

	"github.com/raghavkgarg/mycase/pkg/yfinance"
)

// mockStockMeta provides hardcoded fundamentals and price trend for known tickers.
var mockStockMeta = map[string]struct {
	sector    string
	cagr      float64
	ttm       float64
	dsoPrev   float64
	dsoLatest float64
	retTrend  float64
}{
	"NSE:ATLANTAELE": {sector: "Industrials", cagr: 0.287, ttm: 0.488, dsoPrev: 103.2, dsoLatest: 83.6, retTrend: 0.45},
	"NSE:THYROCARE":  {sector: "Healthcare", cagr: 0.167, ttm: 0.218, dsoPrev: 39.1, dsoLatest: 32.8, retTrend: 0.20},
	"NSE:NETWEB":     {sector: "Technology", cagr: 0.704, ttm: 0.900, dsoPrev: 115.2, dsoLatest: 112.0, retTrend: 0.75},
	"NSE:AEGISLOG":   {sector: "Energy", cagr: -0.011, ttm: 0.232, dsoPrev: 37.4, dsoLatest: 21.1, retTrend: 0.18},
	"NSE:NH":         {sector: "Healthcare", cagr: 0.207, ttm: 0.440, dsoPrev: 37.0, dsoLatest: 30.3, retTrend: 0.35},
	"NSE:MINDACORP":  {sector: "Industrials", cagr: 0.135, ttm: 0.223, dsoPrev: 59.7, dsoLatest: 58.7, retTrend: 0.22},
	"NSE:CCAVENUE":   {sector: "Technology", cagr: 0.605, ttm: 1.033, dsoPrev: 8.2, dsoLatest: 5.1, retTrend: 0.90},
	"NSE:APLLTD":     {sector: "Healthcare", cagr: 0.095, ttm: 0.123, dsoPrev: 78.1, dsoLatest: 74.2, retTrend: 0.10},
	"NSE:CHALET":     {sector: "Consumer Cyclical", cagr: 0.349, ttm: 0.612, dsoPrev: 13.9, dsoLatest: 9.1, retTrend: 0.55},
	"NSE:BELRISE":    {sector: "Industrials", cagr: 0.142, ttm: 0.147, dsoPrev: 70.0, dsoLatest: 67.0, retTrend: 0.14},
	"NSE:ETHOSLTD":   {sector: "Consumer Cyclical", cagr: 0.269, ttm: 0.288, dsoPrev: 5.0, dsoLatest: 4.0, retTrend: 0.27},
	"NSE:SMLMAH":     {sector: "Industrials", cagr: 0.159, ttm: 0.192, dsoPrev: 41.0, dsoLatest: 35.0, retTrend: 0.18},
}

// FillWithMockData merges live data (liveHist/liveFunds) with deterministic mock data
// for any tickers missing live prices or fundamentals. If benchData is nil, a mock
// benchmark series is generated first using the same seeded RNG to preserve the
// identical random sequence for all subsequent stock mocks.
func FillWithMockData(
	tickers []string,
	liveHist map[string]*yfinance.HistoricalData,
	liveFunds map[string]yfinance.Fundamentals,
	benchData *yfinance.HistoricalData,
) (
	histData map[string]*yfinance.HistoricalData,
	outBenchData *yfinance.HistoricalData,
	fundamentals map[string]yfinance.Fundamentals,
	mockedTickers map[string]bool,
	mockUsed bool,
) {
	localRand := rand.New(rand.NewPCG(42, 0))
	nDays := 504

	histData = make(map[string]*yfinance.HistoricalData)
	fundamentals = make(map[string]yfinance.Fundamentals)
	mockedTickers = make(map[string]bool)
	outBenchData = benchData

	if outBenchData == nil || len(outBenchData.Closes) <= 200 {
		outBenchData = generateMockBench(nDays, localRand)
	}

	if outBenchData != nil {
		nDays = len(outBenchData.Closes)
	}

	for _, t := range tickers {
		h, hasLiveHist := liveHist[t]
		f, hasLiveFund := liveFunds[t]
		if hasLiveHist && hasLiveFund && len(h.Closes) > 200 {
			if len(h.Closes) < nDays {
				padLen := nDays - len(h.Closes)
				paddedCloses := make([]float64, nDays)
				paddedOpens := make([]float64, nDays)
				paddedVolumes := make([]float64, nDays)
				paddedTimestamps := make([]int64, nDays)

				firstClose := h.Closes[0]
				firstOpen := h.Opens[0]
				firstVol := h.Volumes[0]

				for i := range padLen {
					paddedCloses[i] = firstClose
					paddedOpens[i] = firstOpen
					paddedVolumes[i] = firstVol
					if i < len(outBenchData.Timestamps) {
						paddedTimestamps[i] = outBenchData.Timestamps[i]
					}
				}
				copy(paddedCloses[padLen:], h.Closes)
				copy(paddedOpens[padLen:], h.Opens)
				copy(paddedVolumes[padLen:], h.Volumes)
				copy(paddedTimestamps[padLen:], h.Timestamps)

				h = &yfinance.HistoricalData{
					Closes:     paddedCloses,
					Opens:      paddedOpens,
					Volumes:    paddedVolumes,
					Timestamps: paddedTimestamps,
				}
			}
			histData[t] = h
			fundamentals[t] = f
			continue
		}
		mockUsed = true
		mockedTickers[t] = true

		meta, exists := mockStockMeta[t]
		if !exists {
			meta = struct {
				sector    string
				cagr      float64
				ttm       float64
				dsoPrev   float64
				dsoLatest float64
				retTrend  float64
			}{sector: "General", cagr: 0.10, ttm: 0.12, dsoPrev: 50.0, dsoLatest: 45.0, retTrend: 0.12}
		}

		fundamentals[t] = yfinance.Fundamentals{
			Sector:                  meta.sector,
			HeldPercentInstitutions: 0.15,
			TTMRevenue:              100.0 * (1.0 + meta.ttm),
			AnnualRevenue: []yfinance.AnnualMetric{
				{Date: "2021-03-31", Value: 100.0 / math.Pow(1.0+meta.cagr, 3.0)},
				{Date: "2022-03-31", Value: 100.0 / math.Pow(1.0+meta.cagr, 2.0)},
				{Date: "2023-03-31", Value: 100.0 / (1.0 + meta.cagr)},
				{Date: "2024-03-31", Value: 100.0},
			},
			AnnualAccountsReceivable: []yfinance.AnnualMetric{
				{Date: "2023-03-31", Value: (meta.dsoPrev / 365.0) * (100.0 / (1.0 + meta.cagr))},
				{Date: "2024-03-31", Value: (meta.dsoLatest / 365.0) * 100.0},
			},
		}

		closes := make([]float64, nDays)
		opens := make([]float64, nDays)
		volumes := make([]float64, nDays)
		timestamps := make([]int64, nDays)
		currPrice := 500.0 + localRand.Float64()*1000.0
		for i := range nDays {
			currPrice += currPrice * (meta.retTrend/252.0 + (localRand.Float64()-0.5)*0.02)
			closes[i] = currPrice
			opens[i] = currPrice * (1.0 + (localRand.Float64()-0.5)*0.008)
			volumes[i] = 10000.0 + localRand.Float64()*50000.0
			timestamps[i] = outBenchData.Timestamps[i]
		}
		histData[t] = &yfinance.HistoricalData{
			Timestamps: timestamps, Closes: closes, Opens: opens, Volumes: volumes,
		}
	}
	return
}

func generateMockBench(nDays int, localRand *rand.Rand) *yfinance.HistoricalData {
	closes := make([]float64, nDays)
	opens := make([]float64, nDays)
	volumes := make([]float64, nDays)
	timestamps := make([]int64, nDays)
	curr := 18500.0
	for i := range nDays {
		curr += curr * (0.15/252.0 + (localRand.Float64()-0.5)*0.015)
		closes[i] = curr
		opens[i] = curr * (1.0 + (localRand.Float64()-0.5)*0.005)
		volumes[i] = 1000000.0 + localRand.Float64()*500000.0
		timestamps[i] = time.Now().AddDate(0, 0, -(nDays - 1 - i)).Unix()
	}
	return &yfinance.HistoricalData{
		Timestamps: timestamps, Closes: closes, Opens: opens, Volumes: volumes,
	}
}
