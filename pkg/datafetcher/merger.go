package datafetcher

import "github.com/raghavkgarg/mycase/pkg/marketdata"

// mergeFundamentals composes a US ticker's fundamentals from a Schwab base and
// an EDGAR statement overlay, per the Phase 10c source-of-record precedence
// (docs/08-edgar-design.md §4):
//
//  1. Schwab TTM ratios (ROE, ROA, margins, P/E, P/B, beta, market cap, div
//     yield, shares/volume) are the base — Schwab is fine for these.
//  2. EDGAR statement facts overlay operating cash flow, net income, and the
//     annual series (the filing itself; EDGAR wins).
//  3. FreeCashflow prefers EDGAR (authoritative OCF − capex) when present, else
//     keeps Schwab's TTM FCF.
//  4. NetIncome/RegularPrice fall back to the Schwab-derived floor if EDGAR
//     didn't supply them.
//
// The overlay is FIELD-LEVEL and NON-DESTRUCTIVE: an EDGAR field only overrides
// when it is non-zero (or non-empty for a series), so a concept EDGAR couldn't
// map never zeroes a good Schwab value. Sector is deliberately left untouched —
// it is backfilled downstream from the constituents CSV by stockpicker.InjectSectors
// (Phase 10a), which is a better (GICS) source than EDGAR's coarse SIC.
//
// It returns the merged fundamentals and a provenance tag describing which
// sources contributed ("schwab+edgar", "schwab", or "edgar").
func mergeFundamentals(base marketdata.Fundamentals, edgarPartial marketdata.Fundamentals, hasSchwab, hasEDGAR bool) (marketdata.Fundamentals, string) {
	merged := base

	// Per-field provenance for the EDGAR-overlayable subset. The base for these
	// fields is whichever provider produced `base` (Schwab on the US path, else
	// Yahoo); EDGAR marks the fields it authoritatively overrides. Left nil when
	// there is no EDGAR overlay — the record-level Source then covers every field.
	var fieldSources map[string]string

	if hasEDGAR {
		baseTag := marketdata.SourceSchwab
		if !hasSchwab {
			baseTag = marketdata.SourceYahoo
		}
		fieldSources = map[string]string{
			marketdata.FieldOperatingCashflow: baseTag,
			marketdata.FieldNetIncome:         baseTag,
			marketdata.FieldFreeCashflow:      baseTag,
		}

		// Point values — overlay only when EDGAR supplied a non-zero value.
		if edgarPartial.OperatingCashflow != 0 {
			merged.OperatingCashflow = edgarPartial.OperatingCashflow
			fieldSources[marketdata.FieldOperatingCashflow] = marketdata.SourceEDGAR
		}
		if edgarPartial.NetIncome != 0 {
			merged.NetIncome = edgarPartial.NetIncome
			fieldSources[marketdata.FieldNetIncome] = marketdata.SourceEDGAR
		}
		// FreeCashflow: EDGAR (OCF − capex) is authoritative; prefer it when set.
		if edgarPartial.FreeCashflow != 0 {
			merged.FreeCashflow = edgarPartial.FreeCashflow
			fieldSources[marketdata.FieldFreeCashflow] = marketdata.SourceEDGAR
		}

		// Annual series — overlay only when EDGAR produced a non-empty series.
		merged.AnnualOperatingIncome = pickSeries(edgarPartial.AnnualOperatingIncome, base.AnnualOperatingIncome)
		merged.AnnualTotalAssets = pickSeries(edgarPartial.AnnualTotalAssets, base.AnnualTotalAssets)
		merged.AnnualCurrentLiabilities = pickSeries(edgarPartial.AnnualCurrentLiabilities, base.AnnualCurrentLiabilities)
		merged.AnnualRevenue = pickSeries(edgarPartial.AnnualRevenue, base.AnnualRevenue)
		merged.AnnualGrossProfit = pickSeries(edgarPartial.AnnualGrossProfit, base.AnnualGrossProfit)
		merged.AnnualNetPPE = pickSeries(edgarPartial.AnnualNetPPE, base.AnnualNetPPE)
		merged.AnnualAccountsReceivable = pickSeries(edgarPartial.AnnualAccountsReceivable, base.AnnualAccountsReceivable)
		merged.AnnualCapEx = pickSeries(edgarPartial.AnnualCapEx, base.AnnualCapEx)
		merged.AnnualInterestExpense = pickSeries(edgarPartial.AnnualInterestExpense, base.AnnualInterestExpense)
	}

	prov := provenance(hasSchwab, hasEDGAR)
	merged.Source = prov
	merged.FieldSources = fieldSources
	return merged, prov
}

// pickSeries returns the overlay series when it has data, else the base series.
func pickSeries(overlay, base []marketdata.AnnualMetric) []marketdata.AnnualMetric {
	if len(overlay) > 0 {
		return overlay
	}
	return base
}

// provenance describes which sources contributed to a merged record.
func provenance(hasSchwab, hasEDGAR bool) string {
	switch {
	case hasSchwab && hasEDGAR:
		return "schwab+edgar"
	case hasEDGAR:
		return "edgar"
	default:
		return "schwab"
	}
}
