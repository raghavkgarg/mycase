package config

import (
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// DefaultsYAML represents the full schema of config/defaults.yaml
type DefaultsYAML struct {
	ActiveMarket string                       `yaml:"active_market"`
	System       DefaultsSystemYAML           `yaml:"system"`
	Markets      map[string]MarketProfileYAML `yaml:"markets"`
}

type DefaultsSystemYAML struct {
	Logging   LoggingConfig   `yaml:"logging"`
	Raw       RawConfig       `yaml:"raw_capture"`
	Scheduler SchedulerConfig `yaml:"scheduler"`
}

type MarketProfileYAML struct {
	Broker          string                  `yaml:"broker"`
	Currency        string                  `yaml:"currency"`
	CurrencyCode    string                  `yaml:"currency_code"`
	Exchange        string                  `yaml:"exchange"`
	Benchmark       string                  `yaml:"benchmark"`
	Timezone        string                  `yaml:"timezone"`
	CloseTime       string                  `yaml:"close_time"`
	DefaultIndex    string                  `yaml:"default_index"`
	DefaultStrategy string                  `yaml:"default_strategy"`
	DefaultGolden   string                  `yaml:"default_golden"`
	TopN            int                     `yaml:"top_n"`
	Range           string                  `yaml:"range"`
	PipelineConfig  string                  `yaml:"pipeline_config"`
	EDGAR           EDGARConfig             `yaml:"edgar"`
	Strategies      map[string]StrategyYAML `yaml:"strategies"`
}

type StrategyYAML struct {
	AliasOf                     string         `yaml:"alias_of"`
	Description                 string         `yaml:"description"`
	MinMarketCap                float64        `yaml:"min_market_cap"`
	MaxMarketCap                float64        `yaml:"max_market_cap"`
	MinADV                      float64        `yaml:"min_adv"`
	MinCFOPAT                   float64        `yaml:"min_cfo_pat"`
	MinPromoterPercent          float64        `yaml:"min_promoter_percent"`
	CheckEarningsTrend          bool           `yaml:"check_earnings_trend"`
	Check200DaySMA              bool           `yaml:"check_200day_sma"`
	Min200DaySMARatio           float64        `yaml:"min_200day_sma_ratio"`
	MaxPledgedPercent           float64        `yaml:"max_pledged_percent"`
	MinROCE                     float64        `yaml:"min_roce"`
	MaxDebtToEquity             float64        `yaml:"max_debt_to_equity"`
	MinInterestCoverage         float64        `yaml:"min_interest_coverage"`
	MaxCapExYoYMultiplier       float64        `yaml:"max_capex_yoy_multiplier"`
	MaxDSODeteriorationPct      float64        `yaml:"max_dso_deterioration_pct"`
	VolumeBreakoutLookbackDays  int            `yaml:"volume_breakout_lookback_days"`
	VolumeBreakoutMultiplier    float64        `yaml:"volume_breakout_multiplier"`
	MaxStocksPerSector          int            `yaml:"max_stocks_per_sector"`
	SectorMaxStocks             map[string]int `yaml:"sector_max_stocks"`
	MaxSectorWeightCap          float64        `yaml:"max_sector_weight_cap"`
	MaxStockWeightCap           float64        `yaml:"max_stock_weight_cap"`
	MinEntryScore               float64        `yaml:"min_entry_score"`
	MinHoldingScore             float64        `yaml:"min_holding_score"`
	AllowCashOnSectorCapExhaust bool           `yaml:"allow_cash_on_sector_cap_exhaustion"`
	AllowCashReserve            bool           `yaml:"allow_cash_reserve"`
	PEGFloor                    float64        `yaml:"peg_floor"`
	MaxPEG                      float64        `yaml:"max_peg"`
	CheckGrossMargin            bool           `yaml:"check_gross_margin"`
	MinRSPercentile             float64        `yaml:"min_rs_percentile"`
	MinCROIC                    float64        `yaml:"min_croic"`
	ScoreWeightRevAcc           float64        `yaml:"score_weight_rev_acc"`
	ScoreWeightAssetTurnover    float64        `yaml:"score_weight_asset_turnover"`
	ScoreWeightPEG              float64        `yaml:"score_weight_peg"`
	ScoreWeightROCE             float64        `yaml:"score_weight_roce"`
	ScoreWeightVolumeBreakout   float64        `yaml:"score_weight_volume_breakout"`
	ScoreWeightRelativeStrength float64        `yaml:"score_weight_relative_strength"`
	MinROE                      float64        `yaml:"min_roe"`
	MaxNetNPA                   float64        `yaml:"max_net_npa"`
	MinCAR                      float64        `yaml:"min_car"`
	MinROA                      float64        `yaml:"min_roa"`
	ScoreWeightEPVMOS           float64        `yaml:"score_weight_epv_mos"`
	ScoreWeight5YValPercentile  float64        `yaml:"score_weight_5y_val_percentile"`
	ScoreWeightSectorZScore     float64        `yaml:"score_weight_sector_zscore"`
	ScoreWeightShillerYield     float64        `yaml:"score_weight_shiller_yield"`
	ScoreWeightCashRealization  float64        `yaml:"score_weight_cash_realization"`
	ScoreWeightFCFYield         float64        `yaml:"score_weight_fcf_yield"`
	ScoreWeightShareholderYield float64        `yaml:"score_weight_shareholder_yield"`
	ScoreWeightSmartMoneyDelta  float64        `yaml:"score_weight_smart_money_delta"`
	ScoreWeightMarginInflection float64        `yaml:"score_weight_margin_inflection"`
	MinFCF                      *float64       `yaml:"min_fcf"`
	ScoreWeightROIC             float64        `yaml:"score_weight_roic"`
	ScoreWeightFCFYieldUS       float64        `yaml:"score_weight_fcf_yield_us"`
	ScoreWeightMomentum12M      float64        `yaml:"score_weight_momentum_12m"`
	ScoreWeightEarningsQuality  float64        `yaml:"score_weight_earnings_quality"`
	ScoreWeightShareholderYieldUS float64      `yaml:"score_weight_shareholder_yield_us"`
	ScoreWeightLowVol           float64        `yaml:"score_weight_low_vol"`
	FundamentalsLagDays         int            `yaml:"fundamentals_lag_days"`
	ShareholdingLagDays         int            `yaml:"shareholding_lag_days"`
	DeliveryDataLagDays         int            `yaml:"delivery_data_lag_days"`
	EarningsBlackoutDaysBefore  int            `yaml:"earnings_blackout_days_before"`
	RegimeBenchmarkSMAPeriod    int            `yaml:"regime_benchmark_sma_period"`
	RegimeMinConfidenceFloor    float64        `yaml:"regime_min_confidence_floor"`
	MinEffectiveScoreThreshold  float64        `yaml:"min_effective_score_threshold"`
	MinProximity52WHigh         float64        `yaml:"min_proximity_52w_high"`
	MinBaseDurationWeeks        int            `yaml:"min_base_duration_weeks"`
	RVOLWinsorizeMultiplier     float64        `yaml:"rvol_winsorize_multiplier"`
	ScoreWeightIdiosyncraticRS  float64        `yaml:"score_weight_idiosyncratic_rs"`
	ScoreWeightVCPTightness     float64        `yaml:"score_weight_vcp_tightness"`
	ScoreWeightVolumeFootprint  float64        `yaml:"score_weight_volume_footprint"`
	ScoreWeightDeliveryDelta    float64        `yaml:"score_weight_delivery_delta"`
	ScoreWeightUpside           float64        `yaml:"score_weight_upside"`
	ScoreWeightMOSBand          float64        `yaml:"score_weight_mos_band"`
	ScoreWeightAgreement        float64        `yaml:"score_weight_agreement"`
	ScoreWeightConvergence      float64        `yaml:"score_weight_convergence"`
	ScoreWeightDebtSafety       float64        `yaml:"score_weight_debt_safety"`
	ScoreWeightRevenueCAGR      float64        `yaml:"score_weight_revenue_cagr"`
	ScoreWeightEarningsAccel    float64        `yaml:"score_weight_earnings_accel"`
	MFSWeights                  *MFSConfig     `yaml:"mfs_weights"`
}

// ToHardFilters maps StrategyYAML fields to HardFilters DTO
func (s *StrategyYAML) ToHardFilters() *HardFilters {
	return &HardFilters{
		MinFCF:                         s.MinFCF,
		MinMarketCap:                   s.MinMarketCap,
		MaxMarketCap:                   s.MaxMarketCap,
		MinADV:                         s.MinADV,
		MinCFOPAT:                      s.MinCFOPAT,
		MinPromoterPercent:             s.MinPromoterPercent,
		MaxPledgedPercent:              s.MaxPledgedPercent,
		MinROCE:                        s.MinROCE,
		MaxDebtToEquity:                s.MaxDebtToEquity,
		MinInterestCoverage:            s.MinInterestCoverage,
		MaxCapExYoYMultiplier:          s.MaxCapExYoYMultiplier,
		MaxDSODeteriorationPct:         s.MaxDSODeteriorationPct,
		VolumeBreakoutLookbackDays:     s.VolumeBreakoutLookbackDays,
		VolumeBreakoutMultiplier:       s.VolumeBreakoutMultiplier,
		MaxStocksPerSector:             s.MaxStocksPerSector,
		SectorMaxStocks:                s.SectorMaxStocks,
		MaxSectorWeightCap:             s.MaxSectorWeightCap,
		PEGFloor:                       s.PEGFloor,
		MaxPEG:                         s.MaxPEG,
		MinRSPercentile:                s.MinRSPercentile,
		MinCROIC:                       s.MinCROIC,
		ScoreWeightRevAcc:              s.ScoreWeightRevAcc,
		ScoreWeightAssetTurnover:       s.ScoreWeightAssetTurnover,
		ScoreWeightPEG:                 s.ScoreWeightPEG,
		ScoreWeightROCE:                s.ScoreWeightROCE,
		ScoreWeightVolumeBreakout:      s.ScoreWeightVolumeBreakout,
		ScoreWeightRelativeStrength:    s.ScoreWeightRelativeStrength,
		MinROE:                         s.MinROE,
		MaxNetNPA:                      s.MaxNetNPA,
		MinCAR:                         s.MinCAR,
		MinROA:                         s.MinROA,
		Min200DaySMARatio:              s.Min200DaySMARatio,
		MaxStockWeightCap:              s.MaxStockWeightCap,
		ScoreWeightEPVMOS:              s.ScoreWeightEPVMOS,
		ScoreWeight5YValPercentile:     s.ScoreWeight5YValPercentile,
		ScoreWeightSectorZScore:        s.ScoreWeightSectorZScore,
		ScoreWeightShillerYield:        s.ScoreWeightShillerYield,
		ScoreWeightCashRealization:     s.ScoreWeightCashRealization,
		ScoreWeightFCFYield:            s.ScoreWeightFCFYield,
		ScoreWeightShareholderYield:    s.ScoreWeightShareholderYield,
		ScoreWeightSmartMoneyDelta:     s.ScoreWeightSmartMoneyDelta,
		ScoreWeightMarginInflection:    s.ScoreWeightMarginInflection,
		ScoreWeightROIC:                s.ScoreWeightROIC,
		ScoreWeightFCFYieldUS:          s.ScoreWeightFCFYieldUS,
		ScoreWeightMomentum12M:         s.ScoreWeightMomentum12M,
		ScoreWeightEarningsQuality:     s.ScoreWeightEarningsQuality,
		ScoreWeightShareholderYieldUS:  s.ScoreWeightShareholderYieldUS,
		ScoreWeightLowVol:              s.ScoreWeightLowVol,
		FundamentalsLagDays:            s.FundamentalsLagDays,
		ShareholdingLagDays:            s.ShareholdingLagDays,
		DeliveryDataLagDays:            s.DeliveryDataLagDays,
		EarningsBlackoutDaysBefore:     s.EarningsBlackoutDaysBefore,
		RegimeBenchmarkSMAPeriod:       s.RegimeBenchmarkSMAPeriod,
		RegimeMinConfidenceFloor:       s.RegimeMinConfidenceFloor,
		MinEffectiveScoreThreshold:     s.MinEffectiveScoreThreshold,
		MinProximity52WHigh:            s.MinProximity52WHigh,
		MinBaseDurationWeeks:           s.MinBaseDurationWeeks,
		RVOLWinsorizeMultiplier:        s.RVOLWinsorizeMultiplier,
		ScoreWeightIdiosyncraticRS:     s.ScoreWeightIdiosyncraticRS,
		ScoreWeightVCPTightness:        s.ScoreWeightVCPTightness,
		ScoreWeightVolumeFootprint:     s.ScoreWeightVolumeFootprint,
		ScoreWeightDeliveryDelta:       s.ScoreWeightDeliveryDelta,
		CheckEarningsTrend:             s.CheckEarningsTrend,
		Check200DaySMA:                 s.Check200DaySMA,
		AllowCashOnSectorCapExhaustion: s.AllowCashOnSectorCapExhaust,
		CheckGrossMargin:               s.CheckGrossMargin,
		MinEntryScore:                  s.MinEntryScore,
		MinHoldingScore:                s.MinHoldingScore,
		ScoreWeightUpside:              s.ScoreWeightUpside,
		ScoreWeightMOSBand:             s.ScoreWeightMOSBand,
		ScoreWeightAgreement:           s.ScoreWeightAgreement,
		ScoreWeightConvergence:         s.ScoreWeightConvergence,
		ScoreWeightDebtSafety:          s.ScoreWeightDebtSafety,
		ScoreWeightRevenueCAGR:         s.ScoreWeightRevenueCAGR,
		ScoreWeightEarningsAccel:       s.ScoreWeightEarningsAccel,
	}
}

// loadDefaultsYAML parses config/defaults.yaml
func loadDefaultsYAML(path string) (*DefaultsYAML, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var doc DefaultsYAML
	if err := yaml.NewDecoder(f).Decode(&doc); err != nil {
		return nil, err
	}
	return &doc, nil
}

// findStrategyYAML searches for a strategy in defaults.yaml across all markets
func (doc *DefaultsYAML) findStrategyYAML(strategy string) *StrategyYAML {
	if strategy == "earlymb" {
		strategy = "early_multibagger"
	}
	// Check active market first
	if mkt, ok := doc.Markets[doc.ActiveMarket]; ok {
		if s, ok := mkt.Strategies[strategy]; ok {
			if s.AliasOf != "" {
				return doc.findStrategyYAML(s.AliasOf)
			}
			return &s
		}
	}
	// Check other markets
	for _, mkt := range doc.Markets {
		if s, ok := mkt.Strategies[strategy]; ok {
			if s.AliasOf != "" {
				return doc.findStrategyYAML(s.AliasOf)
			}
			return &s
		}
	}
	return nil
}

// findDefaultsYAMLPath returns the path to defaults.yaml if it exists
func findDefaultsYAMLPath(requestedPath string) string {
	if requestedPath != "" {
		if strings.HasSuffix(requestedPath, "defaults.yaml") || strings.HasSuffix(requestedPath, "defaults.yml") {
			if _, err := os.Stat(requestedPath); err == nil {
				return requestedPath
			}
		}
		if strings.HasSuffix(requestedPath, "defaults.json") || strings.HasSuffix(requestedPath, "mfs.json") {
			sibling := filepath.Join(filepath.Dir(requestedPath), "defaults.yaml")
			if _, err := os.Stat(sibling); err == nil {
				return sibling
			}
			// If requestedPath exists on disk (e.g. caller passed an explicit test or custom file)
			// and no sibling defaults.yaml exists next to it, do not hijack it with global defaults.yaml.
			if _, err := os.Stat(requestedPath); err == nil {
				return ""
			}
		}
	}
	// Try standard resolved Path("defaults.yaml")
	stdPath := Path("defaults.yaml")
	if _, err := os.Stat(stdPath); err == nil {
		return stdPath
	}
	// Check upward directories from working directory (e.g. running tests in subpackages)
	if cwd, err := os.Getwd(); err == nil {
		dir := cwd
		for i := 0; i < 5; i++ {
			candidate := filepath.Join(dir, "config", "defaults.yaml")
			if _, err := os.Stat(candidate); err == nil {
				return candidate
			}
			parent := filepath.Dir(dir)
			if parent == dir {
				break
			}
			dir = parent
		}
	}
	return ""
}
