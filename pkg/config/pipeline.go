package config

import "gopkg.in/yaml.v3"

// PipelineConfig holds the resolved pipeline configuration.
type PipelineConfig struct {
	Indices               []string `yaml:"indices"`
	Strategy              string   `yaml:"strategy"`
	TopN                  int      `yaml:"top_n"`
	GoldenCopyPath        string   `yaml:"golden_copy_path"`
	Capital               int      `yaml:"capital"`
	PurchaseDate          string   `yaml:"purchase_date"`
	RebalanceTolerancePct float64  `yaml:"rebalance_tolerance_pct"`
	HysteresisRankBuffer  int      `yaml:"hysteresis_rank_buffer"`
}

type rawPipelineConfig struct {
	Indices               []string `yaml:"indices"`
	Strategy              any      `yaml:"strategy"`
	TopN                  any      `yaml:"top_n"`
	GoldenCopyPath        any      `yaml:"golden_copy_path"`
	Capital               any      `yaml:"capital"`
	PurchaseDate          any      `yaml:"purchase_date"`
	RebalanceTolerancePct any      `yaml:"rebalance_tolerance_pct"`
	HysteresisRankBuffer  any      `yaml:"hysteresis_rank_buffer"`
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
	return nil
}
