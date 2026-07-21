package config

import (
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
