package edgar

import (
	"context"
	"sort"

	"github.com/raghavkgarg/mycase/pkg/marketdata"
)

// Candidate us-gaap tags per concept, in preference order. Filers use custom
// taxonomy extensions and tags drift over time, so each concept tries an ordered
// list and takes the first tag that has usable facts. See docs/07-datasources.md §4.3.
var (
	tagsOperatingCashflow = []string{
		"NetCashProvidedByUsedInOperatingActivities",
		"NetCashProvidedByUsedInOperatingActivitiesContinuingOperations",
	}
	tagsNetIncome = []string{
		"NetIncomeLoss",
		"ProfitLoss",
	}
	tagsOperatingIncome    = []string{"OperatingIncomeLoss"}
	tagsTotalAssets        = []string{"Assets"}
	tagsCurrentLiabilities = []string{"LiabilitiesCurrent"}
	tagsRevenue            = []string{
		"RevenueFromContractWithCustomerExcludingAssessedTax",
		"Revenues",
		"SalesRevenueNet",
	}
	tagsGrossProfit        = []string{"GrossProfit"}
	tagsNetPPE             = []string{"PropertyPlantAndEquipmentNet"}
	tagsAccountsReceivable = []string{"AccountsReceivableNetCurrent"}
	// Capex tag varies widely by filer/industry. The classic tag is
	// PaymentsToAcquirePropertyPlantAndEquipment, but many large filers report
	// under alternatives (verified against the S&P 500 companyfacts): Visa/Qualcomm/
	// Verizon/Chevron use PaymentsToAcquireProductiveAssets; REITs use
	// PaymentsForCapitalImprovements; oil & gas use the O&G-property tags. Ordered
	// most-specific-usable first; the first present tag with FY facts wins. Without
	// this breadth, capex fails to bind → FCF=0 → the FCF hard filter wrongly
	// eliminates cash-rich quality names (see docs/03-roadmap.md Phase 10 follow-up).
	tagsCapEx = []string{
		"PaymentsToAcquirePropertyPlantAndEquipment",
		"PaymentsToAcquireProductiveAssets",
		"PaymentsForCapitalImprovements",
		"PaymentsToAcquirePropertyPlantAndEquipmentAndIntangibleAssets",
		"PaymentsToAcquireOtherProductiveAssets",
		"PaymentsToAcquireOilAndGasProperty",
		"PaymentsToExploreAndDevelopOilAndGasProperties",
	}
	tagsInterestExpense = []string{"InterestExpense", "InterestExpenseDebt"}
)

// FetchFundamentals returns the statement-level fundamentals EDGAR can supply
// for each ticker, as a *partial* marketdata.Fundamentals (ratios, sector, price
// are left zero — the datafetcher merger overlays Schwab/CSV for those). Tickers
// with no CIK or no facts are skipped (absent from the result), never fatal —
// per the API-rules "fail gracefully" contract. Only the map keys that were
// successfully populated appear in the result.
func (c *Client) FetchFundamentals(ctx context.Context, tickers []string) (map[string]marketdata.Fundamentals, error) {
	result := make(map[string]marketdata.Fundamentals, len(tickers))
	for _, ticker := range tickers {
		cik, ok, err := c.CIK(ctx, ticker)
		if err != nil {
			// A transient CIK-map failure shouldn't abort the whole batch.
			c.logger.WarnContext(ctx, "edgar.cik_lookup_failed", "ticker", ticker, "err", err)
			continue
		}
		if !ok {
			c.logger.DebugContext(ctx, "edgar.cik_unknown", "ticker", ticker)
			continue
		}
		facts, ok, err := c.fetchFacts(ctx, cik)
		if err != nil {
			c.logger.WarnContext(ctx, "edgar.facts_failed", "ticker", ticker, "cik", cik, "err", err)
			continue
		}
		if !ok {
			continue
		}
		result[ticker] = facts
	}
	return result, nil
}

// mapFacts extracts the fields EDGAR authoritatively supplies into a partial
// Fundamentals. Point values use the latest available fact; annual series use
// FY/10-K facts sorted ascending and deduped by period end. FreeCashflow is
// derived (operating cash flow − capex) only when both are present.
func mapFacts(cf *companyFacts) marketdata.Fundamentals {
	g := cf.Facts.USGAAP

	f := marketdata.Fundamentals{
		AnnualOperatingIncome:    annualSeries(g, tagsOperatingIncome),
		AnnualTotalAssets:        annualSeries(g, tagsTotalAssets),
		AnnualCurrentLiabilities: annualSeries(g, tagsCurrentLiabilities),
		AnnualRevenue:            annualSeries(g, tagsRevenue),
		AnnualGrossProfit:        annualSeries(g, tagsGrossProfit),
		AnnualNetPPE:             annualSeries(g, tagsNetPPE),
		AnnualAccountsReceivable: annualSeries(g, tagsAccountsReceivable),
		AnnualCapEx:              annualSeries(g, tagsCapEx),
		AnnualInterestExpense:    annualSeries(g, tagsInterestExpense),
	}

	// Point values: latest reported fact for cash flow / net income.
	if v, ok := latestValue(g, tagsOperatingCashflow); ok {
		f.OperatingCashflow = v
	}
	if v, ok := latestValue(g, tagsNetIncome); ok {
		f.NetIncome = v
	}

	// Authoritative FCF = latest annual operating cash flow − latest annual capex,
	// only when both are present on the same (annual) basis. Otherwise leave zero
	// and let the merger fall back to Schwab's TTM FCF.
	if ocf, ok1 := latestAnnual(g, tagsOperatingCashflow); ok1 {
		if capex, ok2 := latestAnnual(g, tagsCapEx); ok2 {
			f.FreeCashflow = ocf - capex
		}
	}

	return f
}

// usdFacts returns the USD-unit fact rows for a concept. EDGAR reports absolute
// currency concepts under the "USD" unit key; per-share units (e.g. "USD/shares")
// are intentionally ignored since we want absolute statement values.
func usdFacts(cd conceptData) []factValue {
	return cd.Units["USD"]
}

// latestValue returns the most recently filed fact value across candidate tags,
// regardless of period type. It tries each candidate in order and returns the
// first that carries USD rows — a tag that is present but empty does not shadow
// a later tag that has data.
func latestValue(g map[string]conceptData, tags []string) (float64, bool) {
	for _, t := range tags {
		cd, ok := g[t]
		if !ok {
			continue
		}
		rows := usdFacts(cd)
		if len(rows) == 0 {
			continue
		}
		best := rows[0]
		for _, r := range rows[1:] {
			if r.Filed > best.Filed || (r.Filed == best.Filed && r.End > best.End) {
				best = r
			}
		}
		return best.Val, true
	}
	return 0, false
}

// latestAnnual returns the value of the most recent full-year (FY / 10-K) fact
// across candidate tags.
func latestAnnual(g map[string]conceptData, tags []string) (float64, bool) {
	series := annualSeries(g, tags)
	if len(series) == 0 {
		return 0, false
	}
	return series[len(series)-1].Value, true // series is ascending by year end
}

// annualSeries builds a deduped, ascending-by-period-end annual series from the
// first candidate tag that yields usable annual facts. It keeps only full-year
// facts (fp == "FY", annual form), and when a period end appears in multiple
// filings keeps the most recently filed (restatements win). A candidate tag that
// is present but has no annual facts is skipped so it cannot shadow a later tag
// that does (verified: FTNT/PANW/ISRG/SRE/HPQ declare the classic capex tag with
// zero FY facts but report real capex under PaymentsToAcquireProductiveAssets).
// Returns nil when no candidate yields annual facts.
func annualSeries(g map[string]conceptData, tags []string) []marketdata.AnnualMetric {
	for _, t := range tags {
		cd, ok := g[t]
		if !ok {
			continue
		}
		rows := usdFacts(cd)
		if len(rows) == 0 {
			continue
		}

		// Keep the most recently filed fact per period-end date.
		byEnd := make(map[string]factValue)
		for _, r := range rows {
			if !isAnnual(r) || r.End == "" {
				continue
			}
			if prev, ok := byEnd[r.End]; !ok || r.Filed > prev.Filed {
				byEnd[r.End] = r
			}
		}
		if len(byEnd) == 0 {
			continue // present but no annual facts — try the next candidate
		}

		out := make([]marketdata.AnnualMetric, 0, len(byEnd))
		for end, r := range byEnd {
			out = append(out, marketdata.AnnualMetric{Date: end, Value: r.Val})
		}
		sort.Slice(out, func(i, j int) bool { return out[i].Date < out[j].Date })
		return out
	}
	return nil
}

// isAnnual reports whether a fact is a full-year figure suitable for an annual
// series: fiscal period "FY" and an annual form (10-K, including amendments).
func isAnnual(r factValue) bool {
	if r.FP != "FY" {
		return false
	}
	switch r.Form {
	case "10-K", "10-K/A", "10-KT", "20-F", "40-F":
		return true
	default:
		return false
	}
}
