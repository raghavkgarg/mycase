package config

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// Config represents Zerodha Kite API credentials
type Config struct {
	APIKey      string `json:"api_key"`
	APISecret   string `json:"api_secret,omitempty"`
	AccessToken string `json:"access_token"`
}

// LoadConfig reads configuration from config/config.json
func LoadConfig(filename string) (*Config, error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var cfg Config
	err = json.NewDecoder(file).Decode(&cfg)
	if err != nil {
		return nil, err
	}
	return &cfg, nil
}

// SaveConfig writes config to the specified path
func SaveConfig(filename string, cfg *Config) error {
	dir := filepath.Dir(filename)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	file, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer file.Close()

	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	return encoder.Encode(cfg)
}

// ThemeConfig represents a configuration for a specific holdings theme/group
type ThemeConfig struct {
	Name    string `json:"name"`
	Prefix  string `json:"prefix"`
	CSVPath string `json:"csv_path"`
}

// LoadThemes reads the themes configuration from config/themes.json
// If the file does not exist, it returns default hardcoded themes.
func LoadThemes(filename string) ([]ThemeConfig, error) {
	file, err := os.Open(filename)
	if err != nil {
		// Fallback to default configs
		return []ThemeConfig{
			{Name: "My Managed", Prefix: "My", CSVPath: "data/myall.csv"},
			{Name: "AI Theme Advice", Prefix: "AI Theme", CSVPath: "data/aitheme.csv"},
			{Name: "Micro Theme Advice", Prefix: "Micro Theme", CSVPath: "data/modularmicro.csv"},
		}, nil
	}
	defer file.Close()

	var themes []ThemeConfig
	err = json.NewDecoder(file).Decode(&themes)
	if err != nil {
		return nil, err
	}
	return themes, nil
}

// MFSConfig represents the weight parameters for Multi-Factor Scoring optimization
type MFSConfig struct {
	Sharpe           float64 `json:"sharpe"`
	Sortino          float64 `json:"sortino"`
	Return           float64 `json:"return"`
	Alpha            float64 `json:"alpha"`
	Volatility       float64 `json:"volatility"`
	Beta             float64 `json:"beta"`
	Treynor          float64 `json:"treynor"`
	Ulcer            float64 `json:"ulcer"`
	PEGRatio         float64 `json:"peg_ratio"`
	ROE              float64 `json:"roe"`
	ForwardPE        float64 `json:"forward_pe"`
	OperatingMargins float64 `json:"operating_margins"`
	PBRatio          float64 `json:"pb_ratio"`
	NetDebtEBITDA    float64 `json:"net_debt_ebitda"`
	MarketCap        float64 `json:"market_cap"`
	InsidersPercent  float64 `json:"insiders_percent"`
}

// HardFilters represents criteria constraints for stock picking pre-selection
type HardFilters struct {
	MinMarketCap                float64  `json:"min_market_cap"`
	MaxMarketCap                float64  `json:"max_market_cap"`
	MinADV                      float64  `json:"min_adv"`
	MinCFOPAT                   float64  `json:"min_cfo_pat"`
	MinFCF                      *float64 `json:"min_fcf"`
	MinPromoterPercent          float64  `json:"min_promoter_percent"`
	CheckEarningsTrend          bool     `json:"check_earnings_trend"`
	Check200DaySMA              bool     `json:"check_200day_sma"`
	MaxPledgedPercent           float64  `json:"max_pledged_percent"`
	MinROCE                     float64  `json:"min_roce"`
	MaxDebtToEquity             float64  `json:"max_debt_to_equity"`
	MinInterestCoverage         float64  `json:"min_interest_coverage"`
	MaxCapExYoYMultiplier       float64  `json:"max_capex_yoy_multiplier"`
	MaxDSODeteriorationPct      float64  `json:"max_dso_deterioration_pct"`
	VolumeBreakoutLookbackDays  int      `json:"volume_breakout_lookback_days"`
	VolumeBreakoutMultiplier    float64  `json:"volume_breakout_multiplier"`
	MaxStocksPerSector          int      `json:"max_stocks_per_sector"`
	MaxSectorWeightCap          float64  `json:"max_sector_weight_cap"`
	PEGFloor                    float64  `json:"peg_floor"`
	MaxPEG                      float64  `json:"max_peg"`
	CheckGrossMargin            bool     `json:"check_gross_margin"`
	MinRSPercentile             float64  `json:"min_rs_percentile"`
	MinCROIC                    float64  `json:"min_croic"`
	ScoreWeightRevAcc           float64  `json:"score_weight_rev_acc"`
	ScoreWeightAssetTurnover    float64  `json:"score_weight_asset_turnover"`
	ScoreWeightPEG              float64  `json:"score_weight_peg"`
	ScoreWeightROCE             float64  `json:"score_weight_roce"`
	ScoreWeightVolumeBreakout   float64  `json:"score_weight_volume_breakout"`
	ScoreWeightRelativeStrength float64  `json:"score_weight_relative_strength"`
}

// MFSStrategies wrapper containing the mapping of strategies and filters
type MFSStrategies struct {
	Strategies map[string]MFSConfig   `json:"strategies"`
	Filters    map[string]HardFilters `json:"filters"`
}

// LoadHardFilters reads hard selection filters for a specific strategy from config/mfs.json
func LoadHardFilters(filename string, strategy string) (*HardFilters, error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var wrapper MFSStrategies
	err = json.NewDecoder(file).Decode(&wrapper)
	if err != nil {
		return nil, err
	}

	if f, ok := wrapper.Filters[strategy]; ok {
		return &f, nil
	}
	return nil, nil
}

// GovernanceWrapper wraps the map of pledged percentages
type GovernanceWrapper struct {
	PledgedPercentages map[string]float64 `json:"pledged_percentages"`
}

// LoadGovernance reads promoter pledging percentages from a JSON file
func LoadGovernance(filename string) (map[string]float64, error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var wrapper GovernanceWrapper
	err = json.NewDecoder(file).Decode(&wrapper)
	if err != nil {
		return nil, err
	}
	return wrapper.PledgedPercentages, nil
}

// LoadMFSConfig reads factors weights config for a specific strategy from config/mfs.json
// If the file does not exist or strategy is not found, it returns default strategy weights.
func LoadMFSConfig(filename string, strategy string) (*MFSConfig, error) {
	defaultConfig := &MFSConfig{
		Sharpe:     0.20,
		Sortino:    0.20,
		Return:     0.15,
		Alpha:      0.15,
		Volatility: 0.10,
		Beta:       0.10,
		Treynor:    0.05,
		Ulcer:      0.05,
	}

	file, err := os.Open(filename)
	if err != nil {
		// Fallback to default
		return defaultConfig, nil
	}
	defer file.Close()

	var wrapper MFSStrategies
	err = json.NewDecoder(file).Decode(&wrapper)
	if err != nil {
		return nil, err
	}

	if cfg, ok := wrapper.Strategies[strategy]; ok {
		return &cfg, nil
	}

	// Fallback if strategy name not found
	return defaultConfig, nil
}

// LoadCSVLinks reads the index URL mapping from config/csvlinks.json
func LoadCSVLinks(filename string) (map[string]string, error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var links map[string]string
	err = json.NewDecoder(file).Decode(&links)
	if err != nil {
		return nil, err
	}
	return links, nil
}
