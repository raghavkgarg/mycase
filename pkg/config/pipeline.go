package config

import (
	"os"

	"gopkg.in/yaml.v3"
)

// ScheduleConfig holds configuration for the autopilot scheduler.
type ScheduleConfig struct {
	Frequency       string   `yaml:"frequency"`         // "quarterly", "monthly", "drift-triggered"
	Day             string   `yaml:"day"`               // "first_trading_day", "last_trading_day", or day number ("2", "15")
	Notify          []string `yaml:"notify"`            // alert channels: ["telegram", "discord"]
	AutoExecute     bool     `yaml:"auto_execute"`      // if true, skip confirmation (dangerous)
	DriftTriggerPct float64  `yaml:"drift_trigger_pct"` // mid-cycle drift % that triggers early rebalance (0 = disabled)
	ProposalTTLDays int      `yaml:"proposal_ttl_days"` // days before unconfirmed proposal expires (default 7)
}

// PipelineConfig holds the resolved pipeline configuration.
type PipelineConfig struct {
	Indices               []string       `yaml:"indices"`
	Files                 []string       `yaml:"files"`
	File                  string         `yaml:"file"`
	Strategy              string         `yaml:"strategy"`
	TopN                  int            `yaml:"top_n"`
	GoldenCopyPath        string         `yaml:"golden_copy_path"`
	Capital               int            `yaml:"capital"`
	PurchaseDate          string         `yaml:"purchase_date"`
	RebalanceTolerancePct float64        `yaml:"rebalance_tolerance_pct"`
	HysteresisRankBuffer  int            `yaml:"hysteresis_rank_buffer"`
	Schedule              ScheduleConfig `yaml:"schedule"`
	Broker                string         `yaml:"broker"`        // "zerodha" or "schwab"
	SchwabConfig          string         `yaml:"schwab_config"` // path to schwab.json
	SchwabToken           string         `yaml:"schwab_token"`  // path to schwab_token.json
}

type rawPipelineConfig struct {
	Indices               []string       `yaml:"indices"`
	Files                 any            `yaml:"files"`
	File                  any            `yaml:"file"`
	Strategy              any            `yaml:"strategy"`
	TopN                  any            `yaml:"top_n"`
	GoldenCopyPath        any            `yaml:"golden_copy_path"`
	Capital               any            `yaml:"capital"`
	PurchaseDate          any            `yaml:"purchase_date"`
	RebalanceTolerancePct any            `yaml:"rebalance_tolerance_pct"`
	HysteresisRankBuffer  any            `yaml:"hysteresis_rank_buffer"`
	Schedule              ScheduleConfig `yaml:"schedule"`
	Broker                string         `yaml:"broker"`
	SchwabConfig          string         `yaml:"schwab_config"`
	SchwabToken           string         `yaml:"schwab_token"`
}

// resolveFirst extracts T from val (which may be a scalar or a []any from multi-doc YAML).
func resolveFirst[T any](val any, defaultVal T) T {
	if val == nil {
		return defaultVal
	}
	if v, ok := val.(T); ok {
		return v
	}
	if slice, ok := val.([]any); ok && len(slice) > 0 {
		if v, ok := slice[0].(T); ok {
			return v
		}
		var temp any = slice[0]
		switch any(defaultVal).(type) {
		case int:
			if f, ok := temp.(float64); ok {
				var ret any = int(f)
				return ret.(T)
			}
			if i, ok := temp.(int); ok {
				var ret any = i
				return ret.(T)
			}
		case float64:
			if f, ok := temp.(float64); ok {
				var ret any = f
				return ret.(T)
			}
			if i, ok := temp.(int); ok {
				var ret any = float64(i)
				return ret.(T)
			}
		}
	}
	switch any(defaultVal).(type) {
	case int:
		if f, ok := val.(float64); ok {
			var ret any = int(f)
			return ret.(T)
		}
	case float64:
		if i, ok := val.(int); ok {
			var ret any = float64(i)
			return ret.(T)
		}
	}
	return defaultVal
}

func (cfg *PipelineConfig) UnmarshalYAML(value *yaml.Node) error {
	type alias rawPipelineConfig
	var a alias
	if err := value.Decode(&a); err != nil {
		return err
	}
	cfg.Indices = a.Indices
	var files []string
	extractFiles := func(val any) {
		if val == nil {
			return
		}
		if s, ok := val.(string); ok && s != "" {
			files = append(files, s)
		} else if slice, ok := val.([]any); ok {
			for _, item := range slice {
				if s, ok := item.(string); ok && s != "" {
					files = append(files, s)
				}
			}
		}
	}
	extractFiles(a.File)
	extractFiles(a.Files)
	cfg.Files = files
	if len(files) > 0 {
		cfg.File = files[0]
	}

	cfg.Strategy = resolveFirst(a.Strategy, "balanced")
	cfg.TopN = resolveFirst(a.TopN, 20)
	cfg.GoldenCopyPath = resolveFirst(a.GoldenCopyPath, "data/microsmall.csv")
	cfg.Capital = resolveFirst(a.Capital, 100000)
	cfg.PurchaseDate = resolveFirst(a.PurchaseDate, "2026-01-01")
	tol := resolveFirst(a.RebalanceTolerancePct, 0.10)
	if tol < 0 {
		tol = 0.10
	}
	cfg.RebalanceTolerancePct = tol
	buf := resolveFirst(a.HysteresisRankBuffer, 5)
	if buf < 0 {
		buf = 5
	}
	cfg.HysteresisRankBuffer = buf
	// Schedule config — struct fields are decoded directly by YAML, apply defaults.
	cfg.Schedule = a.Schedule
	if cfg.Schedule.Frequency == "" {
		cfg.Schedule.Frequency = "quarterly"
	}
	if cfg.Schedule.Day == "" {
		cfg.Schedule.Day = "first_trading_day"
	}
	if cfg.Schedule.ProposalTTLDays <= 0 {
		cfg.Schedule.ProposalTTLDays = 7
	}

	// Broker config
	cfg.Broker = a.Broker
	if cfg.Broker == "" {
		cfg.Broker = "zerodha"
	}
	cfg.SchwabConfig = a.SchwabConfig
	if cfg.SchwabConfig == "" {
		cfg.SchwabConfig = "config/schwab.json"
	}
	cfg.SchwabToken = a.SchwabToken
	if cfg.SchwabToken == "" {
		cfg.SchwabToken = "config/schwab_token.json"
	}
	return nil
}

// LoadPipelineConfig reads and parses a pipeline YAML config file.
// Returns an error if the file cannot be opened or parsed.
func LoadPipelineConfig(path string) (*PipelineConfig, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var cfg PipelineConfig
	if err := yaml.NewDecoder(f).Decode(&cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}
