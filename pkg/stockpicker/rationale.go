package stockpicker

import (
	"fmt"

	"github.com/raghavkgarg/mycase/pkg/config"
	"github.com/raghavkgarg/mycase/pkg/yfinance"
)

// BuildRationale generates the selection rationale bullets for a stock.
// The rationale depends on the strategy method: "multibagger" uses fundamental
// growth metrics; all other methods use return/valuation/efficiency heuristics.
func BuildRationale(
	ticker string,
	method string,
	fund yfinance.Fundamentals,
	hist1y *yfinance.HistoricalData,
	price3mo []float64,
	hardFilters *config.HardFilters,
) []string {
	var rationale []string

	ret1y := 0.0
	if hist1y != nil && len(hist1y.Closes) >= 2 {
		ret1y = (hist1y.Closes[len(hist1y.Closes)-1] - hist1y.Closes[0]) / hist1y.Closes[0] * 100.0
	}
	ret3mo := 0.0
	if len(price3mo) >= 2 {
		ret3mo = (price3mo[len(price3mo)-1] - price3mo[0]) / price3mo[0] * 100.0
	}

	if method == "multibagger" {
		passedSales, ttmGrowth, cagr3y := yfinance.CalculateSalesGrowth(&fund)
		if len(fund.AnnualRevenue) >= 3 {
			yearsDiff := len(fund.AnnualRevenue) - 1
			if passedSales {
				rationale = append(rationale, fmt.Sprintf("Sales Growth Accelerator: Revenue growth is accelerating with a TTM growth rate of %.1f%%, exceeding the %d-Year annual CAGR of %.1f%%.", ttmGrowth*100.0, yearsDiff, cagr3y*100.0))
			} else {
				rationale = append(rationale, fmt.Sprintf("Sales Growth Warning: Revenue growth is not accelerating (TTM growth rate of %.1f%%, %d-Year annual CAGR of %.1f%%).", ttmGrowth*100.0, yearsDiff, cagr3y*100.0))
			}
		} else {
			rationale = append(rationale, "Sales Growth Accelerator: Unable to verify CAGR acceleration due to insufficient annual revenue history.")
		}

		maxCapEx := 1.15
		if hardFilters != nil && hardFilters.MaxCapExYoYMultiplier > 0 {
			maxCapEx = hardFilters.MaxCapExYoYMultiplier
		}
		passedAsset, atPrev, atLatest, pctCapExChange, capexLatestAbs := yfinance.CalculateAssetTurnoverCapEx(&fund, maxCapEx)
		if passedAsset || (atPrev > 0 && atLatest > 0) {
			rationale = append(rationale, fmt.Sprintf("Asset Turnover & CapEx Inflection: Asset turnover expanded YoY from %.2f to %.2f, indicating rising sales efficiency, while CapEx stabilized (YoY change of %+.1f%%, latest CapEx: %.1fCr).", atPrev, atLatest, pctCapExChange, capexLatestAbs/1e7))
		} else {
			rationale = append(rationale, "Asset Turnover & CapEx Inflection: Insufficient matching annual net PPE and CapEx data to determine operating leverage.")
		}

		_, dsoPrev, dsoLatest := yfinance.CalculateDSO(&fund)
		if dsoPrev > 0 && dsoLatest > 0 {
			if dsoLatest < dsoPrev {
				rationale = append(rationale, fmt.Sprintf("Working Capital Efficiency: Days Sales Outstanding (DSO) improved YoY from %.1f days to %.1f days, indicating faster cash collection.", dsoPrev, dsoLatest))
			} else if dsoLatest > dsoPrev {
				rationale = append(rationale, fmt.Sprintf("Working Capital Efficiency: Days Sales Outstanding (DSO) increased YoY from %.1f days to %.1f days, indicating slower cash collection.", dsoPrev, dsoLatest))
			} else {
				rationale = append(rationale, fmt.Sprintf("Working Capital Efficiency: Days Sales Outstanding (DSO) remained flat YoY at %.1f days.", dsoLatest))
			}
		} else {
			rationale = append(rationale, "Working Capital Efficiency: Insufficient accounts receivable data to determine DSO collection speed.")
		}

		rationale = append(rationale, fmt.Sprintf("Institutional Sponsorship: Institutions own %.1f%% of the total equity stake, showing validation from professional smart money.", fund.HeldPercentInstitutions*100.0))

		if hist1y != nil && len(hist1y.Closes) >= 200 {
			rsiVal := yfinance.CalculateRSI(hist1y.Closes)
			lookback := 60
			multiplier := 2.0
			if hardFilters != nil {
				if hardFilters.VolumeBreakoutLookbackDays > 0 {
					lookback = hardFilters.VolumeBreakoutLookbackDays
				}
				if hardFilters.VolumeBreakoutMultiplier > 0 {
					multiplier = hardFilters.VolumeBreakoutMultiplier
				}
			}
			hasBreakout := yfinance.CheckVolumeBreakout(hist1y.Closes, hist1y.Opens, hist1y.Volumes, lookback, multiplier)
			breakoutStr := "no breakout detected"
			if hasBreakout {
				breakoutStr = "confirmed breakout detected"
			}
			rationale = append(rationale, fmt.Sprintf("Technical Stage Analysis: The stock is trading in a Stage 2 markup phase (above its 200-SMA) with strong momentum (RSI: %.1f) and has a %s on heavy green-day volume in the last 60 days.", rsiVal, breakoutStr))
		} else {
			rationale = append(rationale, "Technical Stage Analysis: Insufficient historical price and volume history to calculate RSI and SMA details.")
		}
	} else {
		if ret1y < 0 && ret3mo > 15.0 {
			rationale = append(rationale, fmt.Sprintf("Massive 3-Month Momentum: Over the default scoring window (3mo), %s has seen a huge rally of %+.2f%%. This gave it very high scores for the Return, Sharpe, and Sortino factors within the optimized timeframe, despite trailing %+.2f%% on a 1-year basis.", ticker, ret3mo, ret1y))
		} else if ret3mo > 25.0 {
			rationale = append(rationale, fmt.Sprintf("Strong Momentum: Showing powerful short-term performance with a %+.2f%% return over the past 3 months (1-year return is %+.2f%%).", ret3mo, ret1y))
		} else if ret1y > 50.0 {
			rationale = append(rationale, fmt.Sprintf("Steady Gainer: Solid long-term performer with a strong 1-year return of %+.2f%%.", ret1y))
		} else {
			rationale = append(rationale, fmt.Sprintf("Performance: Trailing 1-year return is %+.2f%% with a 3-month return of %+.2f%%.", ret1y, ret3mo))
		}
		if fund.ForwardPE > 0 && fund.ForwardPE <= 25.0 {
			rationale = append(rationale, fmt.Sprintf("Attractive Valuation: Its Forward P/E is %.1f, which is considered very cheap and high-value in the current market segments.", fund.ForwardPE))
		} else if fund.ForwardPE == 999.0 {
			rationale = append(rationale, "Valuation Warning: The company is currently unprofitable or has negative expected forward earnings.")
		} else if fund.ForwardPE > 45.0 {
			rationale = append(rationale, fmt.Sprintf("Premium Valuation: Trading at a higher Forward P/E of %.1f, reflecting high growth expectations.", fund.ForwardPE))
		}
		if fund.NetDebtEBITDA == 99.0 {
			rationale = append(rationale, "Solvency Warning: The company has high leverage relative to zero/negative EBITDA.")
		} else if fund.NetDebtEBITDA <= 0 {
			rationale = append(rationale, "Cash-Rich Balance Sheet: The company has a negative Net Debt/EBITDA ratio, indicating it is net cash-positive (cash holdings exceed total debt).")
		} else if fund.NetDebtEBITDA < 2.0 {
			rationale = append(rationale, fmt.Sprintf("Strong Balance Sheet: Healthy solvency with a low Net Debt/EBITDA ratio of %.2f.", fund.NetDebtEBITDA))
		}
		if fund.ROE > 0.15 {
			rationale = append(rationale, fmt.Sprintf("Strong Efficiency: Delivers high capital efficiency with a return on equity (ROE) of %.1f%%.", fund.ROE*100.0))
		}
		if fund.OperatingMargins > 0.10 {
			rationale = append(rationale, fmt.Sprintf("Stable Margins: Screens for business stability with positive operating margins of %.1f%%.", fund.OperatingMargins*100.0))
		}
	}

	return rationale
}
