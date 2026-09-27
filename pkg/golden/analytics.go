package golden

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"time"
)

// ComputeValuationScore maps upside percentage to a normalized 0-100 score.
// It uses a piece-wise linear function so that negative upside is heavily penalized
// and positive upside is rewarded without allowing low-quality deep value to distort ranks.
func ComputeValuationScore(upside *float64) float64 {
	if upside == nil {
		return 20.0
	}
	u := *upside
	if u >= 50.0 {
		return 100.0
	} else if u >= -20.0 {
		return 50.0 + u // e.g., -20% -> 30.0, 0% -> 50.0, +30% -> 80.0
	} else if u >= -50.0 {
		return 30.0 + (u+20.0)*0.6 // e.g., -35% -> 21.0
	} else {
		score := 12.0 + (u+50.0)*0.3 // e.g., -60% -> 9.0, -80% -> 3.0
		if score < 0 {
			return 0.0
		}
		return score
	}
}

// ComputeCompositeScore calculates the Golden Composite Score (0-100).
func ComputeCompositeScore(mbScore, embScore *float64, valScore float64) float64 {
	mbVal := 0.0
	if mbScore != nil {
		mbVal = *mbScore
	}
	embVal := 0.0
	if embScore != nil {
		embVal = *embScore
	}
	return 0.35*mbVal + 0.40*embVal + 0.25*valScore
}

// ClassifyRegime assigns the constituent to one of the 4 Triangle Regimes.
func ClassifyRegime(c *GoldenConstituent) {
	mbScore := 0.0
	if c.MBScore != nil {
		mbScore = *c.MBScore
	}
	upside := -100.0
	if c.UpsidePct != nil {
		upside = *c.UpsidePct
	}

	if c.EMBPassedStage1 && (mbScore >= 50.0 || c.MBPassedStage1) && upside >= -20.0 {
		c.Regime = RegimeGoldenCore
		if upside > 20.0 {
			c.DiagnosticAction = "TRINITY: High Quality + Coiling Base + Deep Discount"
		} else {
			c.DiagnosticAction = "TRINITY: High Quality + Coiling Base + Valuation Cushion"
		}
	} else if c.EMBPassedStage1 && upside < -20.0 {
		c.Regime = RegimeHighVelocity
		if upside <= -60.0 {
			c.DiagnosticAction = "⚠ MULTIPLE BUBBLE: Strict trailing stop (-60%+ Overhang)"
		} else {
			c.DiagnosticAction = "GROWTH LEADER: Active base; moderate multiple overhang"
		}
	} else if c.EMBPassedStage1 && upside >= 20.0 && mbScore < 50.0 {
		c.Regime = RegimeValueBreakout
		c.DiagnosticAction = "ASYMMETRIC VALUE: Deep value with early volume accumulation"
	} else if !c.EMBPassedStage1 && mbScore >= 60.0 && upside >= 10.0 {
		c.Regime = RegimeCompounderSale
		c.DiagnosticAction = "QUALITY ON SALE: Elite business at deep discount; await base"
	} else {
		c.Regime = RegimeMonitor
		c.DiagnosticAction = "Cohort constituent under observation"
	}
}

// RunAnalysis executes the Golden Triangle cross-strategy audit against DuckDB.
func RunAnalysis(ctx context.Context, db *sql.DB, indexName string) (*GoldenReport, error) {
	if indexName == "" {
		indexName = "niftytotalmarket"
	}

	query := `
WITH latest_dates AS (
    SELECT 
        COALESCE((SELECT MAX(as_of_date) FROM pit_candidate_scores WHERE method = 'earlymb'), CURRENT_DATE) AS latest_emb,
        COALESCE((SELECT MAX(as_of_date) FROM pit_candidate_scores WHERE method = 'multibagger'), CURRENT_DATE) AS latest_mb,
        COALESCE((SELECT MAX(as_of_date) FROM pit_fairprice_scores), CURRENT_DATE) AS latest_fp
),
latest_prices AS (
    SELECT ticker, close AS latest_close
    FROM prices
    QUALIFY ROW_NUMBER() OVER (PARTITION BY ticker ORDER BY date DESC) = 1
),
emb AS (
    SELECT ticker, sector, 
           MAX(effective_score) AS emb_score, 
           BOOL_OR(passed_stage1) AS emb_stage1, 
           AVG(vcp_ratio) AS vcp_ratio, 
           AVG(delivery_delta) AS delivery_delta, 
           AVG(rvol_z_score) AS rvol_z_score
    FROM pit_candidate_scores
    WHERE method = 'earlymb' AND as_of_date = (SELECT latest_emb FROM latest_dates)
    GROUP BY ticker, sector
),
mb AS (
    SELECT ticker, sector, 
           MAX(effective_score) AS mb_score, 
           BOOL_OR(passed_stage1) AS mb_stage1
    FROM pit_candidate_scores
    WHERE method = 'multibagger' AND as_of_date = (SELECT latest_mb FROM latest_dates)
    GROUP BY ticker, sector
),
fp AS (
    SELECT ticker, 
           ARG_MAX(cmp, created_at) AS raw_cmp,
           ARG_MAX(fair_price_ensemble, created_at) AS fair_price,
           ARG_MAX(upside_pct, created_at) AS upside_pct,
           ARG_MAX(verdict, created_at) AS verdict,
           ARG_MAX(valid_model_count, created_at) AS valid_model_count
    FROM pit_fairprice_scores
    WHERE as_of_date = (SELECT latest_fp FROM latest_dates)
    GROUP BY ticker
)
SELECT 
    COALESCE(emb.ticker, mb.ticker, fp.ticker) AS ticker,
    COALESCE(emb.sector, mb.sector, '') AS sector,
    mb.mb_score,
    COALESCE(mb.mb_stage1, false) AS mb_stage1,
    emb.emb_score,
    COALESCE(emb.emb_stage1, false) AS emb_stage1,
    emb.vcp_ratio,
    emb.delivery_delta,
    emb.rvol_z_score,
    COALESCE(NULLIF(fp.raw_cmp, 0.0), lp.latest_close, CASE WHEN fp.upside_pct IS NOT NULL AND fp.upside_pct != -100.0 THEN fp.fair_price / (1.0 + fp.upside_pct/100.0) END) AS cmp,
    fp.fair_price,
    fp.upside_pct,
    COALESCE(fp.verdict, '') AS verdict,
    COALESCE(fp.valid_model_count, 0) AS valid_model_count,
    (SELECT latest_emb FROM latest_dates)::VARCHAR AS as_of_date
FROM emb
FULL OUTER JOIN mb ON emb.ticker = mb.ticker
LEFT JOIN fp ON COALESCE(emb.ticker, mb.ticker) = fp.ticker
LEFT JOIN latest_prices lp ON COALESCE(emb.ticker, mb.ticker) = lp.ticker
WHERE emb.ticker IS NOT NULL OR mb.ticker IS NOT NULL;
`

	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("querying golden triangle data from duckdb: %w", err)
	}
	defer rows.Close()

	var report GoldenReport
	report.IndexName = indexName

	var allConstituents []GoldenConstituent
	var upsides []float64

	for rows.Next() {
		var c GoldenConstituent
		var asOfStr string
		var mbScorePtr, embScorePtr, vcpPtr, delivPtr, rvolPtr *float64
		var cmpPtr, fpPtr, upsidePtr *float64

		err := rows.Scan(
			&c.Ticker,
			&c.Sector,
			&mbScorePtr,
			&c.MBPassedStage1,
			&embScorePtr,
			&c.EMBPassedStage1,
			&vcpPtr,
			&delivPtr,
			&rvolPtr,
			&cmpPtr,
			&fpPtr,
			&upsidePtr,
			&c.MOSVerdict,
			&c.ValidModelCount,
			&asOfStr,
		)
		if err != nil {
			return nil, fmt.Errorf("scanning golden row: %w", err)
		}

		c.MBScore = mbScorePtr
		c.EMBScore = embScorePtr
		c.VCPRatio = vcpPtr
		c.DeliveryDelta = delivPtr
		c.RVOLZScore = rvolPtr
		c.CMP = cmpPtr
		c.FairPrice = fpPtr
		c.UpsidePct = upsidePtr

		if report.AsOfDate.IsZero() && asOfStr != "" {
			if t, err := time.Parse("2006-01-02", asOfStr); err == nil {
				report.AsOfDate = t
			}
		}

		c.ValuationScore = ComputeValuationScore(c.UpsidePct)
		c.CompositeScore = ComputeCompositeScore(c.MBScore, c.EMBScore, c.ValuationScore)
		ClassifyRegime(&c)

		allConstituents = append(allConstituents, c)
		if c.UpsidePct != nil {
			upsides = append(upsides, *c.UpsidePct)
		}
	}

	if report.AsOfDate.IsZero() {
		report.AsOfDate = time.Now()
	}

	// Calculate median upside
	if len(upsides) > 0 {
		sort.Float64s(upsides)
		mid := len(upsides) / 2
		if len(upsides)%2 == 0 {
			report.Census.CohortMedianUpside = (upsides[mid-1] + upsides[mid]) / 2.0
		} else {
			report.Census.CohortMedianUpside = upsides[mid]
		}
	}

	report.Census.TotalEvaluated = len(allConstituents)

	// Partition into regime lists
	for _, c := range allConstituents {
		switch c.Regime {
		case RegimeGoldenCore:
			report.Census.GoldenCoreCount++
			report.GoldenCore = append(report.GoldenCore, c)
		case RegimeHighVelocity:
			report.Census.HighVelocityCount++
			report.HighVelocity = append(report.HighVelocity, c)
			if c.UpsidePct != nil && *c.UpsidePct <= -60.0 {
				report.OverhangAlerts = append(report.OverhangAlerts, c)
			}
		case RegimeValueBreakout:
			report.Census.ValueBreakoutCount++
			report.ValueBreakouts = append(report.ValueBreakouts, c)
		case RegimeCompounderSale:
			report.Census.CompounderSaleCount++
			report.CompounderSale = append(report.CompounderSale, c)
		default:
			report.Census.MonitorCount++
		}
	}

	// Sort lists by composite score descending
	sort.Slice(report.GoldenCore, func(i, j int) bool {
		return report.GoldenCore[i].CompositeScore > report.GoldenCore[j].CompositeScore
	})
	sort.Slice(report.HighVelocity, func(i, j int) bool {
		return report.HighVelocity[i].CompositeScore > report.HighVelocity[j].CompositeScore
	})
	sort.Slice(report.ValueBreakouts, func(i, j int) bool {
		return report.ValueBreakouts[i].CompositeScore > report.ValueBreakouts[j].CompositeScore
	})
	sort.Slice(report.CompounderSale, func(i, j int) bool {
		return report.CompounderSale[i].CompositeScore > report.CompounderSale[j].CompositeScore
	})
	sort.Slice(report.OverhangAlerts, func(i, j int) bool {
		if report.OverhangAlerts[i].UpsidePct != nil && report.OverhangAlerts[j].UpsidePct != nil {
			return *report.OverhangAlerts[i].UpsidePct < *report.OverhangAlerts[j].UpsidePct
		}
		return report.OverhangAlerts[i].CompositeScore > report.OverhangAlerts[j].CompositeScore
	})

	report.AllRanked = allConstituents
	sort.Slice(report.AllRanked, func(i, j int) bool {
		return report.AllRanked[i].CompositeScore > report.AllRanked[j].CompositeScore
	})

	return &report, nil
}
