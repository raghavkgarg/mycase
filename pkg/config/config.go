package config

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// FetchPublicIPs returns the current host machine's public IPv4 and IPv6 addresses.
func FetchPublicIPs() (ipv4 string, ipv6 string) {
	v4URLs := []string{
		"https://ipinfo.io/ip",
		"https://icanhazip.com",
		"https://ifconfig.me/ip",
		"https://api.ipify.org",
	}
	v6URLs := []string{
		"https://icanhazip.com",
		"https://ifconfig.me/ip",
		"https://v6.ipify.org",
	}

	ipv4 = fetchIPFromURLs(v4URLs, "tcp4")
	v6Candidate := fetchIPFromURLs(v6URLs, "tcp6")
	if strings.Contains(v6Candidate, ":") {
		ipv6 = v6Candidate
	} else if strings.Contains(ipv4, ":") {
		ipv6 = ipv4
		ipv4 = ""
	}

	return ipv4, ipv6
}

func fetchIPFromURLs(urls []string, network string) string {
	client := &http.Client{
		Timeout: 2 * time.Second,
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, netName, addr string) (net.Conn, error) {
				var d net.Dialer
				return d.DialContext(ctx, network, addr)
			},
		},
	}

	for _, u := range urls {
		req, err := http.NewRequest("GET", u, nil)
		if err != nil {
			continue
		}
		req.Header.Set("User-Agent", "curl/7.68.0")
		resp, err := client.Do(req)
		if err != nil {
			continue
		}
		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			continue
		}
		ipStr := strings.TrimSpace(string(body))
		if len(ipStr) > 0 && !strings.Contains(ipStr, "<") && !strings.Contains(ipStr, " ") {
			return ipStr
		}
	}
	return ""
}

// FetchPublicIP retrieves current public IP (preferring IPv6 if available, else IPv4).
func FetchPublicIP() string {
	ipv4, ipv6 := FetchPublicIPs()
	if ipv6 != "" {
		return ipv6
	}
	return ipv4
}

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
	Name         string  `json:"name"`
	Prefix       string  `json:"prefix"`
	CSVPath      string  `json:"csv_path"`
	TargetWeight float64 `json:"target_weight,omitempty"`
}

// LoadThemes reads the themes configuration from config/themes.json
// If the file does not exist, it returns default hardcoded themes.
func LoadThemes(filename string) ([]ThemeConfig, error) {
	file, err := os.Open(filename)
	if err != nil {
		// Fallback to default configs
		return []ThemeConfig{
			{Name: "Theme KK Advise", Prefix: "My KK", CSVPath: "data/myall.csv", TargetWeight: 0.30},
			{Name: "Theme AI Advice", Prefix: "My AI", CSVPath: "data/aitheme.csv", TargetWeight: 0.20},
			{Name: "Theme Micro Advice", Prefix: "My Micro", CSVPath: "data/modularmicro.csv", TargetWeight: 0.30},
			{Name: "Theme Hydrogen Nuclear", Prefix: "My Hydrogen", CSVPath: "data/hydrogen.csv", TargetWeight: 0.00},
			{Name: "Theme Microsmall", Prefix: "My MicroSmall", CSVPath: "data/microsmall.csv", TargetWeight: 0.20},
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
	MinROE                      float64  `json:"min_roe"`
	MaxNetNPA                   float64  `json:"max_net_npa"`
	MinCAR                      float64  `json:"min_car"`
	MinROA                      float64  `json:"min_roa"`
	Min200DaySMARatio           float64  `json:"min_200day_sma_ratio"`
	MaxStockWeightCap           float64  `json:"max_stock_weight_cap"`
	ScoreWeightEPVMOS           float64  `json:"score_weight_epv_mos"`
	ScoreWeight5YValPercentile  float64  `json:"score_weight_5y_val_percentile"`
	ScoreWeightSectorZScore     float64  `json:"score_weight_sector_zscore"`
	ScoreWeightShillerYield     float64  `json:"score_weight_shiller_yield"`
	ScoreWeightCashRealization  float64  `json:"score_weight_cash_realization"`
	ScoreWeightFCFYield         float64  `json:"score_weight_fcf_yield"`
	ScoreWeightShareholderYield float64  `json:"score_weight_shareholder_yield"`
	ScoreWeightSmartMoneyDelta  float64  `json:"score_weight_smart_money_delta"`
	ScoreWeightMarginInflection float64  `json:"score_weight_margin_inflection"`

	// US Quality-Momentum scoring weights
	ScoreWeightROIC               float64 `json:"score_weight_roic"`
	ScoreWeightFCFYieldUS         float64 `json:"score_weight_fcf_yield_us"`
	ScoreWeightMomentum12M        float64 `json:"score_weight_momentum_12m"`
	ScoreWeightEarningsQuality    float64 `json:"score_weight_earnings_quality"`
	ScoreWeightShareholderYieldUS float64 `json:"score_weight_shareholder_yield_us"`
	ScoreWeightLowVol             float64 `json:"score_weight_low_vol"`
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

// UserDefaults holds user-level preference defaults loaded from config/defaults.json.
// These provide convenience defaults for CLI flags; explicit flags always override.
type UserDefaults struct {
	Broker         string        `json:"broker"`
	Market         string        `json:"market"`
	Index          string        `json:"index"`
	Method         string        `json:"method"`
	TopN           int           `json:"top_n"`
	Range          string        `json:"range"`
	PipelineConfig string        `json:"pipeline_config"`
	Logging        LoggingConfig `json:"logging"`
}

// LoggingConfig holds structured-logging defaults. CLI flags and env vars
// (MYCASE_LOG_LEVEL, MYCASE_LOG_DIR) override these; see main.go wiring.
type LoggingConfig struct {
	Dir        string `json:"dir"`         // directory for JSON log files (default: data/logs)
	Level      string `json:"level"`       // debug | info | warn | error (default: info)
	File       *bool  `json:"file"`        // write JSON log file (default: true; pointer so absence != false)
	RetainDays int    `json:"retain_days"` // days to keep log files (default: 14)
}

// LoadUserDefaults reads config/defaults.json and returns user preferences.
// Returns zero-value defaults if the file doesn't exist or is malformed.
func LoadUserDefaults(filename string) UserDefaults {
	var defaults UserDefaults
	file, err := os.Open(filename)
	if err != nil {
		return defaults
	}
	defer file.Close()
	_ = json.NewDecoder(file).Decode(&defaults)
	return defaults
}
