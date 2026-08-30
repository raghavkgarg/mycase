package stockpicker

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const PITSnapshotDir = "data/pit_snapshots"

type CandidateScoreDetail struct {
	Ticker          string  `json:"ticker"`
	PassedStage1    bool    `json:"passed_stage1"`
	RejectionReason string  `json:"rejection_reason,omitempty"`
	RawScore        float64 `json:"raw_score"`
	EffectiveScore  float64 `json:"effective_score"`
	CompositeRS     float64 `json:"composite_rs"`
	VCPRatio        float64 `json:"vcp_ratio"`
	RVOLZScore      float64 `json:"rvol_z_score"`
	DecayedPP       float64 `json:"decayed_pp"`
	DeliveryDelta   float64 `json:"delivery_delta"`
	Selected        bool    `json:"selected"`
	FinalWeight     float64 `json:"final_weight"`
	Sector          string  `json:"sector"`
}

type PITRunSnapshot struct {
	AsOfDate          string                          `json:"as_of_date"`
	IndexName         string                          `json:"index_name"`
	Method            string                          `json:"method"`
	RegimeMultiplier  float64                         `json:"regime_multiplier"`
	TotalConstituents int                             `json:"total_constituents"`
	Stage1Count       int                             `json:"stage1_count"`
	SelectedCount     int                             `json:"selected_count"`
	Candidates        map[string]CandidateScoreDetail `json:"candidates"`
}

func SaveRunSnapshot(snap *PITRunSnapshot) (string, error) {
	if err := os.MkdirAll(PITSnapshotDir, 0755); err != nil {
		return "", err
	}
	cleanIndex := strings.NewReplacer(",", "_", " ", "_", "^", "").Replace(snap.IndexName)
	fileName := fmt.Sprintf("%s_%s_%s.json", cleanIndex, snap.Method, snap.AsOfDate)
	filePath := filepath.Join(PITSnapshotDir, fileName)

	data, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(filePath, data, 0644); err != nil {
		return "", err
	}
	return filePath, nil
}

func LoadPreviousSnapshot(indexName, method, currentDateStr string) (*PITRunSnapshot, error) {
	if err := os.MkdirAll(PITSnapshotDir, 0755); err != nil {
		return nil, err
	}
	cleanIndex := strings.NewReplacer(",", "_", " ", "_", "^", "").Replace(indexName)
	prefix := fmt.Sprintf("%s_%s_", cleanIndex, method)

	entries, err := os.ReadDir(PITSnapshotDir)
	if err != nil {
		return nil, err
	}

	var matchedFiles []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasPrefix(name, prefix) && strings.HasSuffix(name, ".json") {
			datePart := strings.TrimSuffix(strings.TrimPrefix(name, prefix), ".json")
			if datePart < currentDateStr {
				matchedFiles = append(matchedFiles, name)
			}
		}
	}

	if len(matchedFiles) == 0 {
		return nil, nil
	}
	sort.Strings(matchedFiles)
	latestPrevFile := matchedFiles[len(matchedFiles)-1]

	data, err := os.ReadFile(filepath.Join(PITSnapshotDir, latestPrevFile))
	if err != nil {
		return nil, err
	}
	var snap PITRunSnapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		return nil, err
	}
	return &snap, nil
}
