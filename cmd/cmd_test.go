package cmd

import (
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

func TestParsePerfDate_Empty(t *testing.T) {
	loc := time.UTC
	got, err := parsePerfDate("", loc)
	if err != nil {
		t.Fatalf("empty string should return today, got error: %v", err)
	}
	now := time.Now().UTC()
	if got.Year() != now.Year() || got.Month() != now.Month() || got.Day() != now.Day() {
		t.Errorf("empty string should return today: got %v", got)
	}
}

func TestParsePerfDate_ISO(t *testing.T) {
	loc := time.UTC
	got, err := parsePerfDate("2026-07-19", loc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Year() != 2026 || got.Month() != 7 || got.Day() != 19 {
		t.Errorf("YYYY-MM-DD: want 2026-07-19, got %v", got)
	}
}

func TestParsePerfDate_Compact(t *testing.T) {
	loc := time.UTC
	got, err := parsePerfDate("20260719", loc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Year() != 2026 || got.Month() != 7 || got.Day() != 19 {
		t.Errorf("YYYYMMDD: want 2026-07-19, got %v", got)
	}
}

func TestParsePerfDate_Invalid(t *testing.T) {
	loc := time.UTC
	_, err := parsePerfDate("not-a-date", loc)
	if err == nil {
		t.Error("expected error for invalid date format")
	}
}

func TestCleanBasketArg_NoLeadingDashes(t *testing.T) {
	if got := cleanBasketArg("MICROSMALL"); got != "MICROSMALL" {
		t.Errorf("no dashes: want MICROSMALL, got %q", got)
	}
}

func TestCleanBasketArg_LeadingDashes(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"--MICROSMALL", "MICROSMALL"},
		{"-microsmall", "microsmall"},
		{"---nifty50", "nifty50"},
		{"nifty50", "nifty50"},
	}
	for _, tc := range cases {
		got := cleanBasketArg(tc.input)
		if got != tc.want {
			t.Errorf("cleanBasketArg(%q) = %q; want %q", tc.input, got, tc.want)
		}
	}
}

func unmarshalPipelineConfig(data []byte) (*PipelineConfig, error) {
	var cfg PipelineConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func TestPipelineConfig_UnmarshalYAML_Defaults(t *testing.T) {
	yamlData := []byte("indices:\n  - nifty50\n")
	cfg, err := unmarshalPipelineConfig(yamlData)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Strategy != "balanced" {
		t.Errorf("default strategy: want balanced, got %q", cfg.Strategy)
	}
	if cfg.TopN != 20 {
		t.Errorf("default TopN: want 20, got %d", cfg.TopN)
	}
	if cfg.Capital != 100000 {
		t.Errorf("default capital: want 100000, got %d", cfg.Capital)
	}
	if cfg.HysteresisRankBuffer != 5 {
		t.Errorf("default hysteresis buffer: want 5, got %d", cfg.HysteresisRankBuffer)
	}
}

func TestPipelineConfig_UnmarshalYAML_Explicit(t *testing.T) {
	yamlData := []byte(`indices:
  - nifty50
strategy: aggressive
top_n: 15
capital: 200000
`)
	cfg, err := unmarshalPipelineConfig(yamlData)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Strategy != "aggressive" {
		t.Errorf("explicit strategy: want aggressive, got %q", cfg.Strategy)
	}
	if cfg.TopN != 15 {
		t.Errorf("explicit TopN: want 15, got %d", cfg.TopN)
	}
	if cfg.Capital != 200000 {
		t.Errorf("explicit capital: want 200000, got %d", cfg.Capital)
	}
}

func TestPipelineConfig_UnmarshalYAML_NegativeTolerance(t *testing.T) {
	yamlData := []byte("indices:\n  - nifty50\nrebalance_tolerance_pct: -0.5\n")
	cfg, err := unmarshalPipelineConfig(yamlData)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.RebalanceTolerancePct != 0.10 {
		t.Errorf("negative tolerance should clamp to 0.10, got %f", cfg.RebalanceTolerancePct)
	}
}
