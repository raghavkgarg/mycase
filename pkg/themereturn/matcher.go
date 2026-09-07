package themereturn

import (
	"encoding/csv"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/raghavkgarg/mycase/pkg/config"
	"github.com/raghavkgarg/mycase/pkg/csvloader"
)

// MatchedTheme holds resolved symbols for active and lifecycle views.
type MatchedTheme struct {
	Name             string
	Prefix           string
	CSVPath          string
	ActiveSymbols    []string // Symbols currently in Golden Copy CSV (after prior theme deduction)
	LifecycleSymbols []string // Active symbols + historically exited proposal symbols
	ExitedSymbols    []string // Symbols that were pruned during rebalancings
	SymbolWeights    map[string]float64
}

// CleanTicker strips exchange prefixes like "NSE:" or "BSE:".
func CleanTicker(ticker string) string {
	parts := strings.Split(ticker, ":")
	if len(parts) > 1 {
		return strings.ToUpper(strings.TrimSpace(parts[1]))
	}
	return strings.ToUpper(strings.TrimSpace(ticker))
}

// resolveFilePath resolves a relative path from the current directory or parent directories.
func resolveFilePath(p string) string {
	if p == "" {
		return ""
	}
	if _, err := os.Stat(p); err == nil {
		return p
	}
	check := p
	for i := 0; i < 3; i++ {
		check = filepath.Join("..", check)
		if _, err := os.Stat(check); err == nil {
			return check
		}
	}
	return p
}

// ResolveTheme resolves a theme by name, keyword, or direct CSV path.
func ResolveTheme(themeArg, explicitCSV, themesConfigPath string) (*MatchedTheme, error) {
	if themesConfigPath == "" {
		themesConfigPath = "config/themes.json"
	}
	resolvedConfigPath := resolveFilePath(themesConfigPath)

	themeConfigs, err := config.LoadThemes(resolvedConfigPath)
	if err != nil {
		return nil, fmt.Errorf("loading themes config: %w", err)
	}

	var matchedConfig *config.ThemeConfig
	matchedIdx := -1

	// If explicit CSV provided
	if explicitCSV != "" {
		for i, tc := range themeConfigs {
			if strings.EqualFold(filepath.Clean(tc.CSVPath), filepath.Clean(explicitCSV)) {
				cfg := tc
				matchedConfig = &cfg
				matchedIdx = i
				break
			}
		}
		if matchedConfig == nil {
			matchedConfig = &config.ThemeConfig{
				Name:    filepath.Base(explicitCSV),
				Prefix:  "Theme",
				CSVPath: explicitCSV,
			}
		}
	} else if themeArg != "" {
		cleanArg := strings.ToLower(strings.TrimSpace(themeArg))
		cleanArg = strings.TrimPrefix(cleanArg, "--")
		cleanArg = strings.TrimPrefix(cleanArg, "-")
		cleanArg = strings.TrimPrefix(cleanArg, "theme ")
		cleanArg = strings.TrimPrefix(cleanArg, "my ")

		for i, tc := range themeConfigs {
			cleanName := strings.ToLower(tc.Name)
			cleanPrefix := strings.ToLower(tc.Prefix)
			cleanPath := strings.ToLower(tc.CSVPath)

			if strings.Contains(cleanName, cleanArg) ||
				strings.Contains(cleanPrefix, cleanArg) ||
				strings.Contains(cleanPath, cleanArg) {
				cfg := tc
				matchedConfig = &cfg
				matchedIdx = i
				break
			}
		}
	}

	if matchedConfig == nil {
		// Default to MicroSmall if not matched
		for i, tc := range themeConfigs {
			if strings.Contains(strings.ToLower(tc.Name), "microsmall") {
				cfg := tc
				matchedConfig = &cfg
				matchedIdx = i
				break
			}
		}
	}

	if matchedConfig == nil {
		return nil, fmt.Errorf("unable to resolve theme: %q", themeArg)
	}

	// 1. If part of themes.json, determine prior theme claims (to match `holdings` logic)
	priorClaimed := make(map[string]bool)
	if matchedIdx > 0 {
		for i := 0; i < matchedIdx; i++ {
			pCSV := resolveFilePath(themeConfigs[i].CSVPath)
			if pMap, err := csvloader.LoadMyAllCSV(pCSV); err == nil {
				for t := range pMap {
					priorClaimed[CleanTicker(t)] = true
				}
			}
		}
	}

	csvPath := resolveFilePath(matchedConfig.CSVPath)

	// 2. Load active symbols from Golden Copy CSV
	activeMap, err := csvloader.LoadMyAllCSV(csvPath)
	if err != nil {
		return nil, fmt.Errorf("loading Golden Copy CSV %s: %w", csvPath, err)
	}

	var activeSymbols []string
	weights := make(map[string]float64)

	// Read CSV directly to preserve ordering and target weights
	if f, err := os.Open(csvPath); err == nil {
		r := csv.NewReader(f)
		records, _ := r.ReadAll()
		f.Close()
		for i, row := range records {
			if i == 0 || len(row) < 1 {
				continue
			}
			sym := CleanTicker(row[0])
			if priorClaimed[sym] {
				// Claimed by an earlier theme in themes.json
				continue
			}
			activeSymbols = append(activeSymbols, sym)
			if len(row) >= 2 {
				var w float64
				fmt.Sscanf(row[1], "%f", &w)
				weights[sym] = w
			}
		}
	} else {
		for raw := range activeMap {
			sym := CleanTicker(raw)
			if !priorClaimed[sym] {
				activeSymbols = append(activeSymbols, sym)
			}
		}
	}

	// 3. Discover historical proposal lifecycle symbols
	allSymbolsMap := make(map[string]bool)
	for _, s := range activeSymbols {
		allSymbolsMap[s] = true
	}

	// Extract keyword for proposal discovery (careful with order!)
	nameLower := strings.ToLower(matchedConfig.Name)
	keyword := "microsmall"
	if strings.Contains(nameLower, "microsmall") {
		keyword = "microsmall"
	} else if strings.Contains(nameLower, "hydrogen") {
		keyword = "hydrogen"
	} else if strings.Contains(nameLower, "ai") {
		keyword = "aitheme"
	} else if strings.Contains(nameLower, "modular") || strings.Contains(nameLower, "micro") {
		keyword = "modularmicro"
	} else if strings.Contains(nameLower, "kk") {
		keyword = "myall"
	}

	// Search proposals directory
	proposalsDir := resolveFilePath("data/candidates/proposals")
	if entries, err := os.ReadDir(proposalsDir); err == nil {
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".csv") {
				continue
			}
			if strings.Contains(strings.ToLower(e.Name()), keyword) {
				fullPath := filepath.Join(proposalsDir, e.Name())
				if pf, err := os.Open(fullPath); err == nil {
					pr := csv.NewReader(pf)
					if precords, err := pr.ReadAll(); err == nil {
						for pi, prow := range precords {
							if pi == 0 || len(prow) == 0 {
								continue
							}
							sym := CleanTicker(prow[0])
							if !priorClaimed[sym] {
								allSymbolsMap[sym] = true
							}
						}
					}
					pf.Close()
				}
			}
		}
	}

	var lifecycleSymbols []string
	var exitedSymbols []string

	for sym := range allSymbolsMap {
		lifecycleSymbols = append(lifecycleSymbols, sym)
		// If not in active Golden Copy, it is an exited rebalancing symbol
		isActive := false
		for _, as := range activeSymbols {
			if as == sym {
				isActive = true
				break
			}
		}
		if !isActive {
			exitedSymbols = append(exitedSymbols, sym)
		}
	}

	return &MatchedTheme{
		Name:             matchedConfig.Name,
		Prefix:           matchedConfig.Prefix,
		CSVPath:          matchedConfig.CSVPath,
		ActiveSymbols:    activeSymbols,
		LifecycleSymbols: lifecycleSymbols,
		ExitedSymbols:    exitedSymbols,
		SymbolWeights:    weights,
	}, nil
}
