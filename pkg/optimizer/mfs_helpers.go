package optimizer

import (
	"strings"

	"github.com/raghavkgarg/mycase/pkg/yfinance"
)

type fundamentalAverages struct {
	peg             float64
	roe             float64
	fwdPE           float64
	opMargins       float64
	pb              float64
	netDebtEbitda   float64
	marketCap       float64
	insidersPercent float64
}

// computeAverages calculates fallback average values for fundamentals imputation
func computeAverages(tickers []string, fundamentals map[string]yfinance.Fundamentals) fundamentalAverages {
	var sumPEG, sumROE, sumFwdPE, sumOpMargins, sumPB, sumNetDebtEbitda, sumMarketCap, sumInsidersPercent float64
	var countPEG, countROE, countFwdPE, countOpMargins, countPB, countNetDebtEbitda, countMarketCap, countInsidersPercent int

	for _, t := range tickers {
		f, ok := fundamentals[t]
		if !ok {
			continue
		}
		if f.PEGRatio != 0 {
			sumPEG += f.PEGRatio
			countPEG++
		}
		if f.ROE != 0 {
			sumROE += f.ROE
			countROE++
		}
		if f.ForwardPE != 0 {
			sumFwdPE += f.ForwardPE
			countFwdPE++
		}
		if f.OperatingMargins != 0 {
			sumOpMargins += f.OperatingMargins
			countOpMargins++
		}
		if f.PBRatio != 0 {
			sumPB += f.PBRatio
			countPB++
		}
		if f.NetDebtEBITDA != 0 {
			sumNetDebtEbitda += f.NetDebtEBITDA
			countNetDebtEbitda++
		}
		if f.MarketCap != 0 {
			sumMarketCap += f.MarketCap
			countMarketCap++
		}
		if f.InsidersPercent != 0 {
			sumInsidersPercent += f.InsidersPercent
			countInsidersPercent++
		}
	}

	avg := fundamentalAverages{
		peg:             1.5,
		roe:             0.12,
		fwdPE:           25.0,
		opMargins:       0.15,
		pb:              2.5,
		netDebtEbitda:   2.0,
		marketCap:       1.5e10,
		insidersPercent: 0.50,
	}

	if countPEG > 0 {
		avg.peg = sumPEG / float64(countPEG)
	}
	if countROE > 0 {
		avg.roe = sumROE / float64(countROE)
	}
	if countFwdPE > 0 {
		avg.fwdPE = sumFwdPE / float64(countFwdPE)
	}
	if countOpMargins > 0 {
		avg.opMargins = sumOpMargins / float64(countOpMargins)
	}
	if countPB > 0 {
		avg.pb = sumPB / float64(countPB)
	}
	if countNetDebtEbitda > 0 {
		avg.netDebtEbitda = sumNetDebtEbitda / float64(countNetDebtEbitda)
	}
	if countMarketCap > 0 {
		avg.marketCap = sumMarketCap / float64(countMarketCap)
	}
	if countInsidersPercent > 0 {
		avg.insidersPercent = sumInsidersPercent / float64(countInsidersPercent)
	}

	return avg
}

// EnforceSectorCaps enforces a sector weight cap limit iteratively
func EnforceSectorCaps(tickers []string, weights map[string]float64, fundamentals map[string]yfinance.Fundamentals, sectorCap float64) {
	if sectorCap <= 0 {
		sectorCap = 0.25
	}
	for range 10 {
		sectorWeights := make(map[string]float64)
		for _, ticker := range tickers {
			sec := strings.TrimSpace(fundamentals[ticker].Sector)
			if sec == "" {
				sec = "Unknown"
			}
			sectorWeights[sec] += weights[ticker]
		}

		excessSum := 0.0
		nonExceededSum := 0.0
		exceededSectors := make(map[string]bool)

		for sec, wt := range sectorWeights {
			if wt > sectorCap {
				excessSum += (wt - sectorCap)
				exceededSectors[sec] = true
			} else {
				nonExceededSum += wt
			}
		}

		if excessSum == 0 {
			break
		}

		for _, ticker := range tickers {
			sec := strings.TrimSpace(fundamentals[ticker].Sector)
			if sec == "" {
				sec = "Unknown"
			}

			if exceededSectors[sec] {
				oldSectorWeight := sectorWeights[sec]
				weights[ticker] = (weights[ticker] / oldSectorWeight) * sectorCap
			} else {
				if nonExceededSum > 0 {
					redist := (weights[ticker] / nonExceededSum) * excessSum
					weights[ticker] += redist
				}
			}
		}
	}
}
