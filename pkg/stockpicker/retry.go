package stockpicker

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/raghavkgarg/mycase/pkg/selectiontracker"
	"github.com/raghavkgarg/mycase/pkg/yfinance"
)

// RetryFailedSnapshotCandidates reads an existing PIT snapshot, identifies candidates that
// suffered upstream data fetch failures, retries their price & fundamental fetches with backoff,
// re-evaluates Stage-1 filters and scores, and updates both the JSON snapshot and DuckDB.
func RetryFailedSnapshotCandidates(ctx context.Context, indexName, method, asOfDate string) (*PITRunSnapshot, error) {
	cleanIndex := strings.NewReplacer(",", "_", " ", "_", "^", "").Replace(indexName)
	fileName := fmt.Sprintf("%s_%s_%s.json", cleanIndex, method, asOfDate)
	snapPath := filepath.Join(PITSnapshotDir, fileName)

	// If exact date file not found, try to locate latest for the index and method
	if _, err := os.Stat(snapPath); os.IsNotExist(err) {
		files, _ := filepath.Glob(filepath.Join(PITSnapshotDir, fmt.Sprintf("%s_%s_*.json", cleanIndex, method)))
		if len(files) == 0 {
			return nil, fmt.Errorf("no PIT snapshots found for %s (%s)", indexName, method)
		}
		sort.Strings(files)
		snapPath = files[len(files)-1]
		slog.WarnContext(ctx, "pit.snapshot_date_fallback", "requested", asOfDate, "using", filepath.Base(snapPath))
	}

	data, err := os.ReadFile(snapPath)
	if err != nil {
		return nil, fmt.Errorf("read snapshot %s: %w", snapPath, err)
	}

	var snap PITRunSnapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		return nil, fmt.Errorf("unmarshal snapshot: %w", err)
	}

	// 1. Identify failed candidates (excluding non-existent dummy placeholder tickers)
	var failedTickers []string
	for t, c := range snap.Candidates {
		if strings.Contains(t, "DUMMY") {
			continue
		}
		if c.DataFetchFailed || (!c.PassedStage1 && c.RejectionReason == "") || strings.HasPrefix(c.RejectionReason, "DATA_FETCH_FAILED") {
			failedTickers = append(failedTickers, t)
		}
	}
	sort.Strings(failedTickers)

	if len(failedTickers) == 0 {
		fmt.Printf("No data fetch failures found in snapshot %s. All %d constituents have valid data.\n",
			filepath.Base(snapPath), len(snap.Candidates))
		return &snap, nil
	}

	fmt.Printf("\n=== RETRYING DATA RETRIEVAL FOR %d FAILED TICKERS (%s, As-Of: %s) ===\n",
		len(failedTickers), snap.IndexName, snap.AsOfDate)

	// 2. Fetch prices with retry and backoff
	fullHistory, activeKeys, stillFailed := FetchHistoricalPrices(ctx, failedTickers)

	// 3. Fetch fundamentals for recovered active tickers
	fundamentals := make(map[string]yfinance.Fundamentals)
	if len(activeKeys) > 0 {
		slog.InfoContext(ctx, "pit.fundamentals_fetch", "recovered", len(activeKeys))
		fMap, fErr := yfinance.FetchFundamentals(ctx, activeKeys)
		if fErr == nil {
			fundamentals = fMap
		}
	}

	cfg, _ := LoadStrategyConfig(snap.Method)
	if cfg != nil {
		InjectGovernance(fundamentals, cfg.Governance)
	}

	// 4. Run Stage-1 safety filters on recovered tickers
	tracker := selectiontracker.New()
	tracker.InitialCount = len(failedTickers)
	for _, f := range stillFailed {
		tracker.RecordFetchFailure(f, "DATA_FETCH_FAILED: historical price bars unavailable")
	}

	var survivors []string
	if cfg != nil && cfg.HardFilters != nil && len(activeKeys) > 0 {
		survivors = ApplySafetyFilters(ctx, activeKeys, snap.Method, cfg.HardFilters, fundamentals, fullHistory, tracker, nil)
	} else {
		survivors = activeKeys
	}
	survivorSet := make(map[string]bool)
	for _, s := range survivors {
		survivorSet[s] = true
	}

	// 5. Fetch Benchmark prices for scoring
	benchSym := GetBenchmarkSymbolForIndex(snap.IndexName, activeKeys)
	benchHist, _ := yfinance.FetchHistoricalDataWithTimestamps(ctx, benchSym, "1y")
	var benchCloses []float64
	if benchHist != nil {
		benchCloses = benchHist.Closes
	}

	// 6. Update candidate details
	recoveredCount := 0
	newSurvivorsCount := 0
	for _, t := range failedTickers {
		if _, ok := fullHistory[t]; ok {
			recoveredCount++
			sec := fundamentals[t].Sector
			if sec == "" {
				sec = snap.Candidates[t].Sector
			}

			if survivorSet[t] {
				newSurvivorsCount++
				hist := fullHistory[t]
				f := fundamentals[t]

				compRS, _, _, _ := yfinance.CalculateCompositeRS(hist.Closes, benchCloses, t)
				vcpRatio, _ := yfinance.CalculateVCPTightness(hist.Closes, hist.Opens)
				rvolZ := yfinance.CalculateWinsorizedRVOLZScore(hist.Volumes, 5, 50, 4.0)
				ppScore, _ := yfinance.CalculateDecayedPocketPivot(hist.Closes, hist.Opens, hist.Volumes, 10, 0.25)
				delivDelta, _, _, dErr := yfinance.GetDeliveryDelta(f.DeliveryHistory, time.Now(), 1)

				wIdioRS, wVCP, wVol, wDeliv := 25.0, 25.0, 25.0, 25.0
				if cfg != nil && cfg.HardFilters != nil {
					if cfg.HardFilters.ScoreWeightIdiosyncraticRS > 0 {
						wIdioRS = cfg.HardFilters.ScoreWeightIdiosyncraticRS
					}
					if cfg.HardFilters.ScoreWeightVCPTightness > 0 {
						wVCP = cfg.HardFilters.ScoreWeightVCPTightness
					}
					if cfg.HardFilters.ScoreWeightVolumeFootprint > 0 {
						wVol = cfg.HardFilters.ScoreWeightVolumeFootprint
					}
					if cfg.HardFilters.ScoreWeightDeliveryDelta > 0 {
						wDeliv = cfg.HardFilters.ScoreWeightDeliveryDelta
					}
				}

				p1 := NormScore(compRS, CompositeRSBounds, wIdioRS, false)
				p2 := NormScore(vcpRatio, VCPRatioBounds, wVCP, true)
				p3a := NormScore(rvolZ, RVOLZBounds, wVol*0.5, false)
				p3b := NormScore(ppScore, PocketPivotBounds, wVol*0.5, false)
				p4 := NormScore(delivDelta, DeliveryDeltaBounds, wDeliv, false)
				rawScore := p1 + p2 + p3a + p3b + p4
				effScore := rawScore * snap.RegimeMultiplier

				snap.Candidates[t] = CandidateScoreDetail{
					Ticker:                     t,
					PassedStage1:               true,
					DataFetchFailed:            false,
					Pillar4InsufficientHistory: (dErr != nil),
					RawScore:                   rawScore,
					EffectiveScore:             effScore,
					CompositeRS:                compRS,
					VCPRatio:                   vcpRatio,
					RVOLZScore:                 rvolZ,
					DecayedPP:                  ppScore,
					DeliveryDelta:              delivDelta,
					Selected:                   false,
					FinalWeight:                0.0,
					Sector:                     sec,
				}
			} else {
				reason := tracker.SafetyReasons[t]
				if reason == "" {
					reason = "Failed Stage-1 safety criteria"
				}
				snap.Candidates[t] = CandidateScoreDetail{
					Ticker:          t,
					PassedStage1:    false,
					DataFetchFailed: false,
					RejectionReason: reason,
					Sector:          sec,
				}
			}
		} else {
			snap.Candidates[t] = CandidateScoreDetail{
				Ticker:          t,
				PassedStage1:    false,
				DataFetchFailed: true,
				RejectionReason: "DATA_FETCH_FAILED: historical price bars unavailable",
				Sector:          snap.Candidates[t].Sector,
			}
		}
	}

	// 7. Recount Stage-1 survivors
	totalStage1 := 0
	for _, c := range snap.Candidates {
		if c.PassedStage1 {
			totalStage1++
		}
	}
	snap.Stage1Count = totalStage1

	// 8. Save updated snapshot to disk
	savedPath, err := SaveRunSnapshot(&snap)
	if err != nil {
		return nil, fmt.Errorf("saving updated snapshot: %w", err)
	}
	slog.InfoContext(ctx, "pit.snapshot_saved", "path", savedPath)

	fmt.Printf("\n=== PIT RETRY SUMMARY (%s) ===\n", snap.AsOfDate)
	fmt.Printf("  * Tickers Retried             : %d\n", len(failedTickers))
	fmt.Printf("  * Successfully Recovered Data : %d\n", recoveredCount)
	fmt.Printf("  * Newly Passed Stage 1        : %d\n", newSurvivorsCount)
	fmt.Printf("  * Eliminated by Safety Filters: %d\n", recoveredCount-newSurvivorsCount)
	fmt.Printf("  * Still Failed (Dummy/Dead)   : %d\n", len(stillFailed))
	fmt.Printf("  * Updated Stage-1 Pool Total  : %d survivors\n", snap.Stage1Count)
	fmt.Println("====================================================")

	return &snap, nil
}
