package config

import (
	"encoding/json"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestPipelineConfig_UnmarshalYAML(t *testing.T) {
	tests := []struct {
		name          string
		yamlData      string
		expectIndices []string
		expectFiles   []string
		expectFile    string
	}{
		{
			name: "indices only",
			yamlData: `
indices:
  - sp500
  - smallcap250
`,
			expectIndices: []string{"sp500", "smallcap250"},
			expectFiles:   nil,
			expectFile:    "",
		},
		{
			name: "single file only",
			yamlData: `
file: data/qtum.csv
`,
			expectIndices: nil,
			expectFiles:   []string{"data/qtum.csv"},
			expectFile:    "data/qtum.csv",
		},
		{
			name: "files list only",
			yamlData: `
files:
  - data/qtum.csv
  - data/microsmall.csv
`,
			expectIndices: nil,
			expectFiles:   []string{"data/qtum.csv", "data/microsmall.csv"},
			expectFile:    "data/qtum.csv",
		},
		{
			name: "both indices and files together",
			yamlData: `
indices:
  - sp500
file: data/qtum.csv
`,
			expectIndices: []string{"sp500"},
			expectFiles:   []string{"data/qtum.csv"},
			expectFile:    "data/qtum.csv",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var cfg PipelineConfig
			if err := yaml.Unmarshal([]byte(tt.yamlData), &cfg); err != nil {
				t.Fatalf("unexpected unmarshal error: %v", err)
			}

			if len(cfg.Indices) != len(tt.expectIndices) {
				t.Errorf("expected indices %v, got %v", tt.expectIndices, cfg.Indices)
			}
			if len(cfg.Files) != len(tt.expectFiles) {
				t.Errorf("expected files %v, got %v", tt.expectFiles, cfg.Files)
			}
			if cfg.File != tt.expectFile {
				t.Errorf("expected file %q, got %q", tt.expectFile, cfg.File)
			}
		})
	}
}

func TestPipelineConfig_Snapshot(t *testing.T) {
	cfg := PipelineConfig{
		Strategy:       "us_quality_momentum",
		TopN:           20,
		GoldenCopyPath: "data/us_sp500.csv",
		Capital:        100000,
		Broker:         "schwab",
	}
	snap := cfg.Snapshot()
	if snap == "" {
		t.Fatal("Snapshot() returned empty string")
	}

	// Snapshot must round-trip back into an equivalent config.
	var got PipelineConfig
	if err := json.Unmarshal([]byte(snap), &got); err != nil {
		t.Fatalf("Snapshot() produced invalid JSON: %v", err)
	}
	if got.Strategy != cfg.Strategy {
		t.Errorf("Strategy: got %q, want %q", got.Strategy, cfg.Strategy)
	}
	if got.TopN != cfg.TopN {
		t.Errorf("TopN: got %d, want %d", got.TopN, cfg.TopN)
	}
}

func TestLoadPipelineConfigForProfile(t *testing.T) {
	// Test loading the real config/pipeline.yaml
	path := Path("pipeline.yaml")
	
	// Default profile
	defCfg, err := LoadPipelineConfigForProfile(path, "")
	if err != nil {
		t.Fatalf("failed to load default profile: %v", err)
	}
	if defCfg.Strategy != "multibagger" {
		t.Errorf("expected default strategy=multibagger, got %q", defCfg.Strategy)
	}
	if defCfg.GoldenCopyPath != "data/microsmall.csv" {
		t.Errorf("expected default golden=data/microsmall.csv, got %q", defCfg.GoldenCopyPath)
	}

	// Early multibagger profile
	embCfg, err := LoadPipelineConfigForProfile(path, "earlymb")
	if err != nil {
		t.Fatalf("failed to load earlymb profile: %v", err)
	}
	if embCfg.Strategy != "earlymb" {
		t.Errorf("expected strategy=earlymb, got %q", embCfg.Strategy)
	}
	if embCfg.GoldenCopyPath != "data/earlymb_live.csv" {
		t.Errorf("expected golden=data/earlymb_live.csv, got %q", embCfg.GoldenCopyPath)
	}
	if embCfg.TopN != 12 {
		t.Errorf("expected top_n=12, got %d", embCfg.TopN)
	}

	// US momentum profile
	usCfg, err := LoadPipelineConfigForProfile(path, "us-momentum")
	if err != nil {
		t.Fatalf("failed to load us-momentum profile: %v", err)
	}
	if usCfg.Strategy != "us_quality_momentum" {
		t.Errorf("expected strategy=us_quality_momentum, got %q", usCfg.Strategy)
	}
	if usCfg.Broker != "schwab" {
		t.Errorf("expected broker=schwab, got %q", usCfg.Broker)
	}
	if usCfg.GoldenCopyPath != "data/us_portfolio.csv" {
		t.Errorf("expected golden=data/us_portfolio.csv, got %q", usCfg.GoldenCopyPath)
	}
}
