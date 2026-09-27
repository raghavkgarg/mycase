package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadUserDefaults_YAML(t *testing.T) {
	defaults := LoadUserDefaults(Path("defaults.yaml"))
	if defaults.Market != "india" {
		t.Errorf("expected market=india, got %q", defaults.Market)
	}
	if defaults.Broker != "zerodha" {
		t.Errorf("expected broker=zerodha, got %q", defaults.Broker)
	}
	if defaults.Index != "niftytotalmarket" {
		t.Errorf("expected index=niftytotalmarket, got %q", defaults.Index)
	}
	if defaults.Method != "multibagger" {
		t.Errorf("expected method=multibagger, got %q", defaults.Method)
	}
	if defaults.TopN != 20 {
		t.Errorf("expected top_n=20, got %d", defaults.TopN)
	}
	if defaults.EDGAR.UserAgent == "" {
		t.Errorf("expected EDGAR user agent to be populated, got empty")
	}
}

func TestCompareLegacyDefaultsJSONWithDefaultsYAML(t *testing.T) {
	// 1. Verify India defaults
	indiaPath := filepath.Join("..", "..", "data", "backups", "legacy_config", "defaults.india.json")
	var legacyIndia UserDefaults
	if f, err := os.Open(indiaPath); err == nil {
		defer f.Close()
		_ = json.NewDecoder(f).Decode(&legacyIndia)
	} else {
		// The legacy JSON backups live under data/backups/legacy_config/, which is
		// gitignored — present only on the machine that ran the migration. This is a
		// one-time migration-parity check; skip (don't fail) when the fixtures are
		// absent, as they are in any clean checkout / CI.
		t.Skipf("legacy fixture absent (%s); skipping migration-parity check", indiaPath)
	}

	yamlPath := findDefaultsYAMLPath("defaults.yaml")
	doc, err := loadDefaultsYAML(yamlPath)
	if err != nil {
		t.Fatalf("failed to load defaults.yaml: %v", err)
	}

	indiaMkt := doc.Markets["india"]
	if indiaMkt.Broker != legacyIndia.Broker {
		t.Errorf("India broker mismatch: legacy=%s, yaml=%s", legacyIndia.Broker, indiaMkt.Broker)
	}
	if indiaMkt.DefaultIndex != legacyIndia.Index {
		t.Errorf("India index mismatch: legacy=%s, yaml=%s", legacyIndia.Index, indiaMkt.DefaultIndex)
	}
	if indiaMkt.DefaultStrategy != legacyIndia.Method {
		t.Errorf("India method mismatch: legacy=%s, yaml=%s", legacyIndia.Method, indiaMkt.DefaultStrategy)
	}
	if indiaMkt.TopN != legacyIndia.TopN {
		t.Errorf("India TopN mismatch: legacy=%d, yaml=%d", legacyIndia.TopN, indiaMkt.TopN)
	}
	if indiaMkt.Range != legacyIndia.Range {
		t.Errorf("India Range mismatch: legacy=%s, yaml=%s", legacyIndia.Range, indiaMkt.Range)
	}

	// 2. Verify US defaults
	usPath := filepath.Join("..", "..", "data", "backups", "legacy_config", "defaults.us.json")
	var legacyUS UserDefaults
	if f, err := os.Open(usPath); err == nil {
		defer f.Close()
		_ = json.NewDecoder(f).Decode(&legacyUS)
	} else {
		t.Fatalf("failed to open defaults.us.json: %v", err)
	}

	usMkt := doc.Markets["us"]
	if usMkt.Broker != legacyUS.Broker {
		t.Errorf("US broker mismatch: legacy=%s, yaml=%s", legacyUS.Broker, usMkt.Broker)
	}
	if usMkt.DefaultIndex != legacyUS.Index {
		t.Errorf("US index mismatch: legacy=%s, yaml=%s", legacyUS.Index, usMkt.DefaultIndex)
	}
	if usMkt.DefaultStrategy != legacyUS.Method {
		t.Errorf("US method mismatch: legacy=%s, yaml=%s", legacyUS.Method, usMkt.DefaultStrategy)
	}
	if usMkt.TopN != legacyUS.TopN {
		t.Errorf("US TopN mismatch: legacy=%d, yaml=%d", legacyUS.TopN, usMkt.TopN)
	}
	if usMkt.Range != legacyUS.Range {
		t.Errorf("US Range mismatch: legacy=%s, yaml=%s", legacyUS.Range, usMkt.Range)
	}

	// 3. Verify System plumbing (Logging, Raw, Scheduler)
	if doc.System.Logging.Dir != legacyIndia.Logging.Dir {
		t.Errorf("Logging Dir mismatch: legacy=%s, yaml=%s", legacyIndia.Logging.Dir, doc.System.Logging.Dir)
	}
	if doc.System.Logging.Level != legacyIndia.Logging.Level {
		t.Errorf("Logging Level mismatch: legacy=%s, yaml=%s", legacyIndia.Logging.Level, doc.System.Logging.Level)
	}
	if doc.System.Logging.File != nil && legacyIndia.Logging.File != nil {
		if *doc.System.Logging.File != *legacyIndia.Logging.File {
			t.Errorf("Logging File mismatch: legacy=%v, yaml=%v", *legacyIndia.Logging.File, *doc.System.Logging.File)
		}
	} else if doc.System.Logging.File != legacyIndia.Logging.File {
		t.Errorf("Logging File nil mismatch: legacy=%v, yaml=%v", legacyIndia.Logging.File, doc.System.Logging.File)
	}
	if doc.System.Logging.RetainDays != legacyIndia.Logging.RetainDays {
		t.Errorf("Logging RetainDays mismatch: legacy=%d, yaml=%d", legacyIndia.Logging.RetainDays, doc.System.Logging.RetainDays)
	}

	if doc.System.Raw.RetainDays != legacyIndia.Raw.RetainDays {
		t.Errorf("Raw RetainDays mismatch: legacy=%d, yaml=%d", legacyIndia.Raw.RetainDays, doc.System.Raw.RetainDays)
	}
	if doc.System.Raw.MaxSizeMB != legacyIndia.Raw.MaxSizeMB {
		t.Errorf("Raw MaxSizeMB mismatch: legacy=%d, yaml=%d", legacyIndia.Raw.MaxSizeMB, doc.System.Raw.MaxSizeMB)
	}

	if doc.System.Scheduler.EnableEOD != legacyIndia.Scheduler.EnableEOD {
		t.Errorf("Scheduler EnableEOD mismatch: legacy=%v, yaml=%v", legacyIndia.Scheduler.EnableEOD, doc.System.Scheduler.EnableEOD)
	}
	if doc.System.Scheduler.EnableDrift != legacyIndia.Scheduler.EnableDrift {
		t.Errorf("Scheduler EnableDrift mismatch: legacy=%v, yaml=%v", legacyIndia.Scheduler.EnableDrift, doc.System.Scheduler.EnableDrift)
	}
	if doc.System.Scheduler.EnableRebalance != legacyIndia.Scheduler.EnableRebalance {
		t.Errorf("Scheduler EnableRebalance mismatch: legacy=%v, yaml=%v", legacyIndia.Scheduler.EnableRebalance, doc.System.Scheduler.EnableRebalance)
	}
	if doc.System.Scheduler.CloseOffsetMin != legacyIndia.Scheduler.CloseOffsetMin {
		t.Errorf("Scheduler CloseOffsetMin mismatch: legacy=%d, yaml=%d", legacyIndia.Scheduler.CloseOffsetMin, doc.System.Scheduler.CloseOffsetMin)
	}
	if doc.System.Scheduler.MaxRunMin != legacyIndia.Scheduler.MaxRunMin {
		t.Errorf("Scheduler MaxRunMin mismatch: legacy=%d, yaml=%d", legacyIndia.Scheduler.MaxRunMin, doc.System.Scheduler.MaxRunMin)
	}
	if doc.System.Scheduler.FailAlertAfter != legacyIndia.Scheduler.FailAlertAfter {
		t.Errorf("Scheduler FailAlertAfter mismatch: legacy=%d, yaml=%d", legacyIndia.Scheduler.FailAlertAfter, doc.System.Scheduler.FailAlertAfter)
	}
	if doc.System.Scheduler.EnableReport != legacyIndia.Scheduler.EnableReport {
		t.Errorf("Scheduler EnableReport mismatch: legacy=%v, yaml=%v", legacyIndia.Scheduler.EnableReport, doc.System.Scheduler.EnableReport)
	}

	// 4. Verify EDGAR integration
	if doc.Markets["us"].EDGAR.Enabled != legacyIndia.EDGAR.Enabled {
		t.Errorf("EDGAR Enabled mismatch: legacy=%v, yaml=%v", legacyIndia.EDGAR.Enabled, doc.Markets["us"].EDGAR.Enabled)
	}
	if doc.Markets["us"].EDGAR.UserAgent != legacyIndia.EDGAR.UserAgent {
		t.Errorf("EDGAR UserAgent mismatch: legacy=%s, yaml=%s", legacyIndia.EDGAR.UserAgent, doc.Markets["us"].EDGAR.UserAgent)
	}
	if doc.Markets["us"].EDGAR.FactsTTLDays != legacyIndia.EDGAR.FactsTTLDays {
		t.Errorf("EDGAR FactsTTLDays mismatch: legacy=%d, yaml=%d", legacyIndia.EDGAR.FactsTTLDays, doc.Markets["us"].EDGAR.FactsTTLDays)
	}
	if doc.Markets["us"].EDGAR.CIKTTLDays != legacyIndia.EDGAR.CIKTTLDays {
		t.Errorf("EDGAR CIKTTLDays mismatch: legacy=%d, yaml=%d", legacyIndia.EDGAR.CIKTTLDays, doc.Markets["us"].EDGAR.CIKTTLDays)
	}
}

func TestLoadHardFilters_YAML_IndiaMultibagger(t *testing.T) {
	filters, err := LoadHardFilters(Path("defaults.yaml"), "multibagger")
	if err != nil {
		t.Fatalf("unexpected error loading multibagger filters: %v", err)
	}
	if filters == nil {
		t.Fatal("expected non-nil filters")
	}
	if filters.MinROCE != 0.12 {
		t.Errorf("expected MinROCE=0.12, got %.2f", filters.MinROCE)
	}
	if filters.MinPromoterPercent != 0.25 {
		t.Errorf("expected MinPromoterPercent=0.25, got %.2f", filters.MinPromoterPercent)
	}
	if filters.MaxPledgedPercent != 0.05 {
		t.Errorf("expected MaxPledgedPercent=0.05, got %.2f", filters.MaxPledgedPercent)
	}
	if filters.MinEntryScore != 40.0 {
		t.Errorf("expected MinEntryScore=40.0, got %.1f", filters.MinEntryScore)
	}
}

func TestLoadHardFilters_YAML_EarlyMBAlias(t *testing.T) {
	filters, err := LoadHardFilters(Path("defaults.yaml"), "earlymb")
	if err != nil {
		t.Fatalf("unexpected error loading earlymb filters: %v", err)
	}
	if filters == nil {
		t.Fatal("expected non-nil filters")
	}
	if filters.ScoreWeightVCPTightness != 25.0 {
		t.Errorf("expected ScoreWeightVCPTightness=25.0, got %.1f", filters.ScoreWeightVCPTightness)
	}
	if filters.MinProximity52WHigh != 0.85 {
		t.Errorf("expected MinProximity52WHigh=0.85, got %.2f", filters.MinProximity52WHigh)
	}
}

func TestLoadHardFilters_YAML_USQualityMomentum(t *testing.T) {
	filters, err := LoadHardFilters(Path("defaults.yaml"), "us_quality_momentum")
	if err != nil {
		t.Fatalf("unexpected error loading US filters: %v", err)
	}
	if filters == nil {
		t.Fatal("expected non-nil filters")
	}
	if filters.ScoreWeightROIC != 20.0 {
		t.Errorf("expected ScoreWeightROIC=20.0, got %.1f", filters.ScoreWeightROIC)
	}
	if filters.ScoreWeightFCFYieldUS != 20.0 {
		t.Errorf("expected ScoreWeightFCFYieldUS=20.0, got %.1f", filters.ScoreWeightFCFYieldUS)
	}
}

func TestLoadMFSConfig_YAML(t *testing.T) {
	cfg, err := LoadMFSConfig(Path("defaults.yaml"), "multibagger")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg == nil {
		t.Fatal("expected non-nil MFSConfig")
	}
	if cfg.ROE != 0.20 {
		t.Errorf("expected ROE=0.20, got %.2f", cfg.ROE)
	}
}

func TestCompareLegacyMFSWithDefaultsYAML(t *testing.T) {
	legacyPath := filepath.Join("..", "..", "data", "backups", "legacy_config", "mfs.json")
	file, err := os.Open(legacyPath)
	if err != nil {
		// Gitignored migration fixture; skip when absent (clean checkout / CI). See
		// the note in TestCompareLegacyDefaultsJSONWithDefaultsYAML.
		t.Skipf("legacy fixture absent (%s); skipping migration-parity check", legacyPath)
	}
	defer file.Close()

	var legacy MFSStrategies
	if err := json.NewDecoder(file).Decode(&legacy); err != nil {
		t.Fatalf("failed to decode legacy mfs.json: %v", err)
	}

	yamlPath := Path("defaults.yaml")

	// 1. Compare all filter sets
	for stratName, legacyFilter := range legacy.Filters {
		t.Run("filter_"+stratName, func(t *testing.T) {
			currFilter, err := LoadHardFilters(yamlPath, stratName)
			if err != nil {
				t.Fatalf("failed to load %s from defaults.yaml: %v", stratName, err)
			}
			if currFilter == nil {
				t.Fatalf("strategy %s not found in defaults.yaml", stratName)
			}

			// Compare every key
			if legacyFilter.MinMarketCap != currFilter.MinMarketCap {
				t.Errorf("MinMarketCap mismatch: legacy=%v, curr=%v", legacyFilter.MinMarketCap, currFilter.MinMarketCap)
			}
			if legacyFilter.MaxMarketCap != currFilter.MaxMarketCap {
				t.Errorf("MaxMarketCap mismatch: legacy=%v, curr=%v", legacyFilter.MaxMarketCap, currFilter.MaxMarketCap)
			}
			if legacyFilter.MinADV != currFilter.MinADV {
				t.Errorf("MinADV mismatch: legacy=%v, curr=%v", legacyFilter.MinADV, currFilter.MinADV)
			}
			if legacyFilter.MinCFOPAT != currFilter.MinCFOPAT {
				t.Errorf("MinCFOPAT mismatch: legacy=%v, curr=%v", legacyFilter.MinCFOPAT, currFilter.MinCFOPAT)
			}
			if legacyFilter.MinPromoterPercent != currFilter.MinPromoterPercent {
				t.Errorf("MinPromoterPercent mismatch: legacy=%v, curr=%v", legacyFilter.MinPromoterPercent, currFilter.MinPromoterPercent)
			}
			if legacyFilter.CheckEarningsTrend != currFilter.CheckEarningsTrend {
				t.Errorf("CheckEarningsTrend mismatch: legacy=%v, curr=%v", legacyFilter.CheckEarningsTrend, currFilter.CheckEarningsTrend)
			}
			if legacyFilter.Check200DaySMA != currFilter.Check200DaySMA {
				t.Errorf("Check200DaySMA mismatch: legacy=%v, curr=%v", legacyFilter.Check200DaySMA, currFilter.Check200DaySMA)
			}
			if legacyFilter.Min200DaySMARatio != currFilter.Min200DaySMARatio {
				t.Errorf("Min200DaySMARatio mismatch: legacy=%v, curr=%v", legacyFilter.Min200DaySMARatio, currFilter.Min200DaySMARatio)
			}
			if legacyFilter.MaxPledgedPercent != currFilter.MaxPledgedPercent {
				t.Errorf("MaxPledgedPercent mismatch: legacy=%v, curr=%v", legacyFilter.MaxPledgedPercent, currFilter.MaxPledgedPercent)
			}
			if legacyFilter.MinROCE != currFilter.MinROCE {
				t.Errorf("MinROCE mismatch: legacy=%v, curr=%v", legacyFilter.MinROCE, currFilter.MinROCE)
			}
			if legacyFilter.MaxDebtToEquity != currFilter.MaxDebtToEquity {
				t.Errorf("MaxDebtToEquity mismatch: legacy=%v, curr=%v", legacyFilter.MaxDebtToEquity, currFilter.MaxDebtToEquity)
			}
			if legacyFilter.MinInterestCoverage != currFilter.MinInterestCoverage {
				t.Errorf("MinInterestCoverage mismatch: legacy=%v, curr=%v", legacyFilter.MinInterestCoverage, currFilter.MinInterestCoverage)
			}
			if legacyFilter.MaxCapExYoYMultiplier != currFilter.MaxCapExYoYMultiplier {
				t.Errorf("MaxCapExYoYMultiplier mismatch: legacy=%v, curr=%v", legacyFilter.MaxCapExYoYMultiplier, currFilter.MaxCapExYoYMultiplier)
			}
			if legacyFilter.MaxDSODeteriorationPct != currFilter.MaxDSODeteriorationPct {
				t.Errorf("MaxDSODeteriorationPct mismatch: legacy=%v, curr=%v", legacyFilter.MaxDSODeteriorationPct, currFilter.MaxDSODeteriorationPct)
			}
			if legacyFilter.VolumeBreakoutLookbackDays != currFilter.VolumeBreakoutLookbackDays {
				t.Errorf("VolumeBreakoutLookbackDays mismatch: legacy=%v, curr=%v", legacyFilter.VolumeBreakoutLookbackDays, currFilter.VolumeBreakoutLookbackDays)
			}
			if legacyFilter.VolumeBreakoutMultiplier != currFilter.VolumeBreakoutMultiplier {
				t.Errorf("VolumeBreakoutMultiplier mismatch: legacy=%v, curr=%v", legacyFilter.VolumeBreakoutMultiplier, currFilter.VolumeBreakoutMultiplier)
			}
			if legacyFilter.MaxStocksPerSector != currFilter.MaxStocksPerSector {
				t.Errorf("MaxStocksPerSector mismatch: legacy=%v, curr=%v", legacyFilter.MaxStocksPerSector, currFilter.MaxStocksPerSector)
			}
			if len(legacyFilter.SectorMaxStocks) > 0 {
				for sec, capVal := range legacyFilter.SectorMaxStocks {
					if currFilter.SectorMaxStocks[sec] != capVal {
						t.Errorf("SectorMaxStocks[%s] mismatch: legacy=%v, curr=%v", sec, capVal, currFilter.SectorMaxStocks[sec])
					}
				}
			}
			if legacyFilter.MaxSectorWeightCap != currFilter.MaxSectorWeightCap {
				t.Errorf("MaxSectorWeightCap mismatch: legacy=%v, curr=%v", legacyFilter.MaxSectorWeightCap, currFilter.MaxSectorWeightCap)
			}
			if legacyFilter.MaxStockWeightCap != currFilter.MaxStockWeightCap {
				t.Errorf("MaxStockWeightCap mismatch: legacy=%v, curr=%v", legacyFilter.MaxStockWeightCap, currFilter.MaxStockWeightCap)
			}
			if legacyFilter.MinEntryScore != currFilter.MinEntryScore {
				t.Errorf("MinEntryScore mismatch: legacy=%v, curr=%v", legacyFilter.MinEntryScore, currFilter.MinEntryScore)
			}
			if legacyFilter.MinHoldingScore != currFilter.MinHoldingScore {
				t.Errorf("MinHoldingScore mismatch: legacy=%v, curr=%v", legacyFilter.MinHoldingScore, currFilter.MinHoldingScore)
			}
			if legacyFilter.AllowCashOnSectorCapExhaustion != currFilter.AllowCashOnSectorCapExhaustion {
				t.Errorf("AllowCashOnSectorCapExhaustion mismatch: legacy=%v, curr=%v", legacyFilter.AllowCashOnSectorCapExhaustion, currFilter.AllowCashOnSectorCapExhaustion)
			}
			if legacyFilter.PEGFloor != currFilter.PEGFloor {
				t.Errorf("PEGFloor mismatch: legacy=%v, curr=%v", legacyFilter.PEGFloor, currFilter.PEGFloor)
			}
			if legacyFilter.MaxPEG != currFilter.MaxPEG {
				t.Errorf("MaxPEG mismatch: legacy=%v, curr=%v", legacyFilter.MaxPEG, currFilter.MaxPEG)
			}
			if legacyFilter.MinRSPercentile != currFilter.MinRSPercentile {
				t.Errorf("MinRSPercentile mismatch: legacy=%v, curr=%v", legacyFilter.MinRSPercentile, currFilter.MinRSPercentile)
			}
			if legacyFilter.MinCROIC != currFilter.MinCROIC {
				t.Errorf("MinCROIC mismatch: legacy=%v, curr=%v", legacyFilter.MinCROIC, currFilter.MinCROIC)
			}
			if legacyFilter.ScoreWeightRevAcc != currFilter.ScoreWeightRevAcc {
				t.Errorf("ScoreWeightRevAcc mismatch: legacy=%v, curr=%v", legacyFilter.ScoreWeightRevAcc, currFilter.ScoreWeightRevAcc)
			}
			if legacyFilter.ScoreWeightAssetTurnover != currFilter.ScoreWeightAssetTurnover {
				t.Errorf("ScoreWeightAssetTurnover mismatch: legacy=%v, curr=%v", legacyFilter.ScoreWeightAssetTurnover, currFilter.ScoreWeightAssetTurnover)
			}
			if legacyFilter.ScoreWeightPEG != currFilter.ScoreWeightPEG {
				t.Errorf("ScoreWeightPEG mismatch: legacy=%v, curr=%v", legacyFilter.ScoreWeightPEG, currFilter.ScoreWeightPEG)
			}
			if legacyFilter.ScoreWeightROCE != currFilter.ScoreWeightROCE {
				t.Errorf("ScoreWeightROCE mismatch: legacy=%v, curr=%v", legacyFilter.ScoreWeightROCE, currFilter.ScoreWeightROCE)
			}
			if legacyFilter.ScoreWeightVolumeBreakout != currFilter.ScoreWeightVolumeBreakout {
				t.Errorf("ScoreWeightVolumeBreakout mismatch: legacy=%v, curr=%v", legacyFilter.ScoreWeightVolumeBreakout, currFilter.ScoreWeightVolumeBreakout)
			}
			if legacyFilter.ScoreWeightRelativeStrength != currFilter.ScoreWeightRelativeStrength {
				t.Errorf("ScoreWeightRelativeStrength mismatch: legacy=%v, curr=%v", legacyFilter.ScoreWeightRelativeStrength, currFilter.ScoreWeightRelativeStrength)
			}
			if legacyFilter.MinROE != currFilter.MinROE {
				t.Errorf("MinROE mismatch: legacy=%v, curr=%v", legacyFilter.MinROE, currFilter.MinROE)
			}
			if legacyFilter.MaxNetNPA != currFilter.MaxNetNPA {
				t.Errorf("MaxNetNPA mismatch: legacy=%v, curr=%v", legacyFilter.MaxNetNPA, currFilter.MaxNetNPA)
			}
			if legacyFilter.MinCAR != currFilter.MinCAR {
				t.Errorf("MinCAR mismatch: legacy=%v, curr=%v", legacyFilter.MinCAR, currFilter.MinCAR)
			}
			if legacyFilter.MinROA != currFilter.MinROA {
				t.Errorf("MinROA mismatch: legacy=%v, curr=%v", legacyFilter.MinROA, currFilter.MinROA)
			}
			if legacyFilter.ScoreWeightEPVMOS != currFilter.ScoreWeightEPVMOS {
				t.Errorf("ScoreWeightEPVMOS mismatch: legacy=%v, curr=%v", legacyFilter.ScoreWeightEPVMOS, currFilter.ScoreWeightEPVMOS)
			}
			if legacyFilter.ScoreWeight5YValPercentile != currFilter.ScoreWeight5YValPercentile {
				t.Errorf("ScoreWeight5YValPercentile mismatch: legacy=%v, curr=%v", legacyFilter.ScoreWeight5YValPercentile, currFilter.ScoreWeight5YValPercentile)
			}
			if legacyFilter.ScoreWeightSectorZScore != currFilter.ScoreWeightSectorZScore {
				t.Errorf("ScoreWeightSectorZScore mismatch: legacy=%v, curr=%v", legacyFilter.ScoreWeightSectorZScore, currFilter.ScoreWeightSectorZScore)
			}
			if legacyFilter.ScoreWeightShillerYield != currFilter.ScoreWeightShillerYield {
				t.Errorf("ScoreWeightShillerYield mismatch: legacy=%v, curr=%v", legacyFilter.ScoreWeightShillerYield, currFilter.ScoreWeightShillerYield)
			}
			if legacyFilter.ScoreWeightCashRealization != currFilter.ScoreWeightCashRealization {
				t.Errorf("ScoreWeightCashRealization mismatch: legacy=%v, curr=%v", legacyFilter.ScoreWeightCashRealization, currFilter.ScoreWeightCashRealization)
			}
			if legacyFilter.ScoreWeightFCFYield != currFilter.ScoreWeightFCFYield {
				t.Errorf("ScoreWeightFCFYield mismatch: legacy=%v, curr=%v", legacyFilter.ScoreWeightFCFYield, currFilter.ScoreWeightFCFYield)
			}
			if legacyFilter.ScoreWeightShareholderYield != currFilter.ScoreWeightShareholderYield {
				t.Errorf("ScoreWeightShareholderYield mismatch: legacy=%v, curr=%v", legacyFilter.ScoreWeightShareholderYield, currFilter.ScoreWeightShareholderYield)
			}
			if legacyFilter.ScoreWeightSmartMoneyDelta != currFilter.ScoreWeightSmartMoneyDelta {
				t.Errorf("ScoreWeightSmartMoneyDelta mismatch: legacy=%v, curr=%v", legacyFilter.ScoreWeightSmartMoneyDelta, currFilter.ScoreWeightSmartMoneyDelta)
			}
			if legacyFilter.ScoreWeightMarginInflection != currFilter.ScoreWeightMarginInflection {
				t.Errorf("ScoreWeightMarginInflection mismatch: legacy=%v, curr=%v", legacyFilter.ScoreWeightMarginInflection, currFilter.ScoreWeightMarginInflection)
			}
			if legacyFilter.ScoreWeightROIC != currFilter.ScoreWeightROIC {
				t.Errorf("ScoreWeightROIC mismatch: legacy=%v, curr=%v", legacyFilter.ScoreWeightROIC, currFilter.ScoreWeightROIC)
			}
			if legacyFilter.ScoreWeightFCFYieldUS != currFilter.ScoreWeightFCFYieldUS {
				t.Errorf("ScoreWeightFCFYieldUS mismatch: legacy=%v, curr=%v", legacyFilter.ScoreWeightFCFYieldUS, currFilter.ScoreWeightFCFYieldUS)
			}
			if legacyFilter.ScoreWeightMomentum12M != currFilter.ScoreWeightMomentum12M {
				t.Errorf("ScoreWeightMomentum12M mismatch: legacy=%v, curr=%v", legacyFilter.ScoreWeightMomentum12M, currFilter.ScoreWeightMomentum12M)
			}
			if legacyFilter.ScoreWeightEarningsQuality != currFilter.ScoreWeightEarningsQuality {
				t.Errorf("ScoreWeightEarningsQuality mismatch: legacy=%v, curr=%v", legacyFilter.ScoreWeightEarningsQuality, currFilter.ScoreWeightEarningsQuality)
			}
			if legacyFilter.ScoreWeightShareholderYieldUS != currFilter.ScoreWeightShareholderYieldUS {
				t.Errorf("ScoreWeightShareholderYieldUS mismatch: legacy=%v, curr=%v", legacyFilter.ScoreWeightShareholderYieldUS, currFilter.ScoreWeightShareholderYieldUS)
			}
			if legacyFilter.ScoreWeightLowVol != currFilter.ScoreWeightLowVol {
				t.Errorf("ScoreWeightLowVol mismatch: legacy=%v, curr=%v", legacyFilter.ScoreWeightLowVol, currFilter.ScoreWeightLowVol)
			}
			if legacyFilter.FundamentalsLagDays != currFilter.FundamentalsLagDays {
				t.Errorf("FundamentalsLagDays mismatch: legacy=%v, curr=%v", legacyFilter.FundamentalsLagDays, currFilter.FundamentalsLagDays)
			}
			if legacyFilter.ShareholdingLagDays != currFilter.ShareholdingLagDays {
				t.Errorf("ShareholdingLagDays mismatch: legacy=%v, curr=%v", legacyFilter.ShareholdingLagDays, currFilter.ShareholdingLagDays)
			}
			if legacyFilter.DeliveryDataLagDays != currFilter.DeliveryDataLagDays {
				t.Errorf("DeliveryDataLagDays mismatch: legacy=%v, curr=%v", legacyFilter.DeliveryDataLagDays, currFilter.DeliveryDataLagDays)
			}
			if legacyFilter.EarningsBlackoutDaysBefore != currFilter.EarningsBlackoutDaysBefore {
				t.Errorf("EarningsBlackoutDaysBefore mismatch: legacy=%v, curr=%v", legacyFilter.EarningsBlackoutDaysBefore, currFilter.EarningsBlackoutDaysBefore)
			}
			if legacyFilter.RegimeBenchmarkSMAPeriod != currFilter.RegimeBenchmarkSMAPeriod {
				t.Errorf("RegimeBenchmarkSMAPeriod mismatch: legacy=%v, curr=%v", legacyFilter.RegimeBenchmarkSMAPeriod, currFilter.RegimeBenchmarkSMAPeriod)
			}
			if legacyFilter.RegimeMinConfidenceFloor != currFilter.RegimeMinConfidenceFloor {
				t.Errorf("RegimeMinConfidenceFloor mismatch: legacy=%v, curr=%v", legacyFilter.RegimeMinConfidenceFloor, currFilter.RegimeMinConfidenceFloor)
			}
			if legacyFilter.MinEffectiveScoreThreshold != currFilter.MinEffectiveScoreThreshold {
				t.Errorf("MinEffectiveScoreThreshold mismatch: legacy=%v, curr=%v", legacyFilter.MinEffectiveScoreThreshold, currFilter.MinEffectiveScoreThreshold)
			}
			if legacyFilter.MinProximity52WHigh != currFilter.MinProximity52WHigh {
				t.Errorf("MinProximity52WHigh mismatch: legacy=%v, curr=%v", legacyFilter.MinProximity52WHigh, currFilter.MinProximity52WHigh)
			}
			if legacyFilter.MinBaseDurationWeeks != currFilter.MinBaseDurationWeeks {
				t.Errorf("MinBaseDurationWeeks mismatch: legacy=%v, curr=%v", legacyFilter.MinBaseDurationWeeks, currFilter.MinBaseDurationWeeks)
			}
			if legacyFilter.RVOLWinsorizeMultiplier != currFilter.RVOLWinsorizeMultiplier {
				t.Errorf("RVOLWinsorizeMultiplier mismatch: legacy=%v, curr=%v", legacyFilter.RVOLWinsorizeMultiplier, currFilter.RVOLWinsorizeMultiplier)
			}
			if legacyFilter.ScoreWeightIdiosyncraticRS != currFilter.ScoreWeightIdiosyncraticRS {
				t.Errorf("ScoreWeightIdiosyncraticRS mismatch: legacy=%v, curr=%v", legacyFilter.ScoreWeightIdiosyncraticRS, currFilter.ScoreWeightIdiosyncraticRS)
			}
			if legacyFilter.ScoreWeightVCPTightness != currFilter.ScoreWeightVCPTightness {
				t.Errorf("ScoreWeightVCPTightness mismatch: legacy=%v, curr=%v", legacyFilter.ScoreWeightVCPTightness, currFilter.ScoreWeightVCPTightness)
			}
			if legacyFilter.ScoreWeightVolumeFootprint != currFilter.ScoreWeightVolumeFootprint {
				t.Errorf("ScoreWeightVolumeFootprint mismatch: legacy=%v, curr=%v", legacyFilter.ScoreWeightVolumeFootprint, currFilter.ScoreWeightVolumeFootprint)
			}
			if legacyFilter.ScoreWeightDeliveryDelta != currFilter.ScoreWeightDeliveryDelta {
				t.Errorf("ScoreWeightDeliveryDelta mismatch: legacy=%v, curr=%v", legacyFilter.ScoreWeightDeliveryDelta, currFilter.ScoreWeightDeliveryDelta)
			}
			if legacyFilter.ScoreWeightUpside != currFilter.ScoreWeightUpside {
				t.Errorf("ScoreWeightUpside mismatch: legacy=%v, curr=%v", legacyFilter.ScoreWeightUpside, currFilter.ScoreWeightUpside)
			}
			if legacyFilter.ScoreWeightMOSBand != currFilter.ScoreWeightMOSBand {
				t.Errorf("ScoreWeightMOSBand mismatch: legacy=%v, curr=%v", legacyFilter.ScoreWeightMOSBand, currFilter.ScoreWeightMOSBand)
			}
			if legacyFilter.ScoreWeightAgreement != currFilter.ScoreWeightAgreement {
				t.Errorf("ScoreWeightAgreement mismatch: legacy=%v, curr=%v", legacyFilter.ScoreWeightAgreement, currFilter.ScoreWeightAgreement)
			}
			if legacyFilter.ScoreWeightConvergence != currFilter.ScoreWeightConvergence {
				t.Errorf("ScoreWeightConvergence mismatch: legacy=%v, curr=%v", legacyFilter.ScoreWeightConvergence, currFilter.ScoreWeightConvergence)
			}
			if legacyFilter.ScoreWeightDebtSafety != currFilter.ScoreWeightDebtSafety {
				t.Errorf("ScoreWeightDebtSafety mismatch: legacy=%v, curr=%v", legacyFilter.ScoreWeightDebtSafety, currFilter.ScoreWeightDebtSafety)
			}
			if legacyFilter.ScoreWeightRevenueCAGR != currFilter.ScoreWeightRevenueCAGR {
				t.Errorf("ScoreWeightRevenueCAGR mismatch: legacy=%v, curr=%v", legacyFilter.ScoreWeightRevenueCAGR, currFilter.ScoreWeightRevenueCAGR)
			}
			if legacyFilter.ScoreWeightEarningsAccel != currFilter.ScoreWeightEarningsAccel {
				t.Errorf("ScoreWeightEarningsAccel mismatch: legacy=%v, curr=%v", legacyFilter.ScoreWeightEarningsAccel, currFilter.ScoreWeightEarningsAccel)
			}
		})
	}

	// 2. Compare MFS factor weights
	for stratName, legacyWeights := range legacy.Strategies {
		t.Run("weights_"+stratName, func(t *testing.T) {
			currWeights, err := LoadMFSConfig(yamlPath, stratName)
			if err != nil {
				t.Fatalf("failed to load weights for %s from defaults.yaml: %v", stratName, err)
			}
			if currWeights == nil {
				t.Fatalf("weights for strategy %s not found", stratName)
			}

			if legacyWeights.Sharpe != currWeights.Sharpe {
				t.Errorf("Sharpe mismatch: legacy=%v, curr=%v", legacyWeights.Sharpe, currWeights.Sharpe)
			}
			if legacyWeights.Sortino != currWeights.Sortino {
				t.Errorf("Sortino mismatch: legacy=%v, curr=%v", legacyWeights.Sortino, currWeights.Sortino)
			}
			if legacyWeights.Return != currWeights.Return {
				t.Errorf("Return mismatch: legacy=%v, curr=%v", legacyWeights.Return, currWeights.Return)
			}
			if legacyWeights.Alpha != currWeights.Alpha {
				t.Errorf("Alpha mismatch: legacy=%v, curr=%v", legacyWeights.Alpha, currWeights.Alpha)
			}
			if legacyWeights.Volatility != currWeights.Volatility {
				t.Errorf("Volatility mismatch: legacy=%v, curr=%v", legacyWeights.Volatility, currWeights.Volatility)
			}
			if legacyWeights.Beta != currWeights.Beta {
				t.Errorf("Beta mismatch: legacy=%v, curr=%v", legacyWeights.Beta, currWeights.Beta)
			}
			if legacyWeights.Treynor != currWeights.Treynor {
				t.Errorf("Treynor mismatch: legacy=%v, curr=%v", legacyWeights.Treynor, currWeights.Treynor)
			}
			if legacyWeights.Ulcer != currWeights.Ulcer {
				t.Errorf("Ulcer mismatch: legacy=%v, curr=%v", legacyWeights.Ulcer, currWeights.Ulcer)
			}
			if legacyWeights.PEGRatio != currWeights.PEGRatio {
				t.Errorf("PEGRatio mismatch: legacy=%v, curr=%v", legacyWeights.PEGRatio, currWeights.PEGRatio)
			}
			if legacyWeights.ROE != currWeights.ROE {
				t.Errorf("ROE mismatch: legacy=%v, curr=%v", legacyWeights.ROE, currWeights.ROE)
			}
			if legacyWeights.ForwardPE != currWeights.ForwardPE {
				t.Errorf("ForwardPE mismatch: legacy=%v, curr=%v", legacyWeights.ForwardPE, currWeights.ForwardPE)
			}
			if legacyWeights.OperatingMargins != currWeights.OperatingMargins {
				t.Errorf("OperatingMargins mismatch: legacy=%v, curr=%v", legacyWeights.OperatingMargins, currWeights.OperatingMargins)
			}
			if legacyWeights.PBRatio != currWeights.PBRatio {
				t.Errorf("PBRatio mismatch: legacy=%v, curr=%v", legacyWeights.PBRatio, currWeights.PBRatio)
			}
			if legacyWeights.NetDebtEBITDA != currWeights.NetDebtEBITDA {
				t.Errorf("NetDebtEBITDA mismatch: legacy=%v, curr=%v", legacyWeights.NetDebtEBITDA, currWeights.NetDebtEBITDA)
			}
			if legacyWeights.MarketCap != currWeights.MarketCap {
				t.Errorf("MarketCap mismatch: legacy=%v, curr=%v", legacyWeights.MarketCap, currWeights.MarketCap)
			}
			if legacyWeights.InsidersPercent != currWeights.InsidersPercent {
				t.Errorf("InsidersPercent mismatch: legacy=%v, curr=%v", legacyWeights.InsidersPercent, currWeights.InsidersPercent)
			}
		})
	}
}
