package stockpicker

import (
	"encoding/csv"
	"fmt"
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/raghavkgarg/mycase/pkg/selectiontracker"
	"github.com/raghavkgarg/mycase/pkg/yfinance"
)

type IncubatorCandidate struct {
	Ticker         string
	Sector         string
	RawScore       float64
	EffectiveScore float64
	HurdleGap      float64
	VCPRatio       float64
	CompositeRS    float64
	DeliveryDelta  float64
	CurrentPrice   float64
	BreakoutPivot  float64 // 52W High * 0.98
}

// GenerateIncubatorWatchlist creates an actionable watchlist of Stage-1 survivors coiling on the runway.
func GenerateIncubatorWatchlist(
	activeKeys []string,
	scores map[string]float64,
	fundamentals map[string]yfinance.Fundamentals,
	fullHistory map[string]*yfinance.HistoricalData,
	tracker *selectiontracker.Tracker,
	outPath string,
) ([]IncubatorCandidate, error) {
	regimeHurdle := 30.0
	regimeMult := 1.0
	if tracker != nil && tracker.RegimeMultiplier > 0 {
		regimeMult = tracker.RegimeMultiplier
		regimeHurdle = 30.0 / tracker.RegimeMultiplier
	}

	var candidates []IncubatorCandidate
	for _, t := range activeKeys {
		raw := scores[t]
		if raw <= 0 {
			continue
		}
		hurdleGap := regimeHurdle - raw
		f := fundamentals[t]
		hist := fullHistory[t]

		var vcp, compRS, currPrice, high52 float64
		if hist != nil && len(hist.Closes) > 0 {
			currPrice = hist.Closes[len(hist.Closes)-1]
			vcp, _ = yfinance.CalculateVCPTightness(hist.Closes, hist.Opens)
			compRS, _, _, _ = yfinance.CalculateCompositeRS(hist.Closes, nil, t)
			for _, c := range hist.Closes {
				if c > high52 {
					high52 = c
				}
			}
		}
		delivDelta, _, _, _ := yfinance.GetDeliveryDelta(f.DeliveryHistory, time.Now(), 1)

		candidates = append(candidates, IncubatorCandidate{
			Ticker:         t,
			Sector:         f.Sector,
			RawScore:       raw,
			EffectiveScore: raw * regimeMult,
			HurdleGap:      hurdleGap,
			VCPRatio:       vcp,
			CompositeRS:    compRS,
			DeliveryDelta:  delivDelta,
			CurrentPrice:   currPrice,
			BreakoutPivot:  high52 * 0.98,
		})
	}

	// Sort by HurdleGap ASC (smallest gap first), then by VCPRatio ASC (tightest coil first)
	sort.Slice(candidates, func(i, j int) bool {
		if math.Abs(candidates[i].HurdleGap-candidates[j].HurdleGap) < 1.0 {
			return candidates[i].VCPRatio < candidates[j].VCPRatio
		}
		return candidates[i].HurdleGap < candidates[j].HurdleGap
	})

	// Keep top 15 runway candidates
	if len(candidates) > 15 {
		candidates = candidates[:15]
	}

	if outPath != "" {
		if err := os.MkdirAll(filepath.Dir(outPath), 0755); err != nil {
			return nil, err
		}
		file, err := os.Create(outPath)
		if err != nil {
			return nil, err
		}
		defer file.Close()

		writer := csv.NewWriter(file)
		defer writer.Flush()

		_ = writer.Write([]string{
			"Ticker", "Sector", "Raw_Score", "Effective_Score", "Hurdle_Gap",
			"VCP_ATR", "Composite_RS_Pct", "Delivery_Delta_Pct", "Current_Price", "Breakout_Pivot_52W",
		})
		for _, c := range candidates {
			_ = writer.Write([]string{
				c.Ticker,
				c.Sector,
				fmt.Sprintf("%.1f", c.RawScore),
				fmt.Sprintf("%.1f", c.EffectiveScore),
				fmt.Sprintf("%+.1f", c.HurdleGap),
				fmt.Sprintf("%.2f", c.VCPRatio),
				fmt.Sprintf("%+.1f%%", c.CompositeRS*100.0),
				fmt.Sprintf("%+.1f%%", c.DeliveryDelta*100.0),
				fmt.Sprintf("%.2f", c.CurrentPrice),
				fmt.Sprintf("%.2f", c.BreakoutPivot),
			})
		}
		slog.Info("incubator.watchlist_generated", "setups", len(candidates), "path", outPath)
	}

	return candidates, nil
}
