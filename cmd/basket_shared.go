package cmd

import (
	"bufio"
	"context"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/raghavkgarg/mycase/pkg/broker"
	"github.com/raghavkgarg/mycase/pkg/config"
	"github.com/raghavkgarg/mycase/pkg/csvloader"
	"github.com/raghavkgarg/mycase/pkg/portfolio"
	"github.com/raghavkgarg/mycase/pkg/stockpicker"
	"github.com/raghavkgarg/mycase/pkg/themedb"
)

type sharedConstituentInfo struct {
	ticker          string
	symbol          string
	totalBrokerQty  int
	claimedByOther  int
	currentRecorded int
	otherNames      string
	targetWeight    float64
	ltp             float64
	valueClaimed    float64
}

// reconcileSharedHoldings detects tickers held across multiple themes in themes.json,
// prints a cross-theme audit table, prompts the user with smart defaults, adjusts currentHoldings,
// and returns target buy overrides for rebalancing.
func reconcileSharedHoldings(
	ctx context.Context,
	basketFilename string,
	basketKeys []string,
	basket map[string]float64,
	quoteData map[string]float64,
	currentHoldings map[string]int,
	reader *bufio.Reader,
	sharesFlag string,
	autoYes bool,
) (map[string]int, string, error) {
	if stockpicker.IsUSIndex(basketFilename) || broker.IsUSBroker(broker.BrokerName()) {
		return nil, "", nil
	}

	themeConfigs, err := config.LoadThemes(config.Path("themes.json"))
	if err != nil || len(themeConfigs) == 0 {
		return nil, "", nil
	}

	uName := csvloader.GetUniverseName(basketFilename)
	var currentTheme *config.ThemeConfig
	var otherThemes []config.ThemeConfig

	for _, tc := range themeConfigs {
		tcUName := csvloader.GetUniverseName(tc.CSVPath)
		if strings.EqualFold(tcUName, uName) || strings.EqualFold(filepath.Clean(tc.CSVPath), filepath.Clean(basketFilename)) {
			curr := tc
			currentTheme = &curr
		} else {
			otherThemes = append(otherThemes, tc)
		}
	}

	if currentTheme == nil {
		return nil, "", nil
	}

	// Map other theme tickers
	otherThemeTickers := make(map[string][]string) // cleanSymbol -> list of other theme names
	for _, otc := range otherThemes {
		pCSV := resolveBasketFile(otc.CSVPath)
		pMap, _, err := csvloader.LoadBasketCSV(pCSV)
		if err != nil {
			if allMap, aErr := csvloader.LoadMyAllCSV(pCSV); aErr == nil {
				for inst := range allMap {
					sym := portfolio.CleanTicker(inst)
					otherThemeTickers[sym] = append(otherThemeTickers[sym], otc.Name)
				}
			}
		} else {
			for inst := range pMap {
				sym := portfolio.CleanTicker(inst)
				otherThemeTickers[sym] = append(otherThemeTickers[sym], otc.Name)
			}
		}
	}

	// Query DuckDB for existing constituent shares across themes
	var dbShares map[string]map[string]int
	if tdb, err := themedb.Open(""); err == nil {
		dbShares, _ = tdb.GetActiveConstituentShares(ctx, basketKeys)
	}

	// Parse CLI --shares override flag if provided
	flagOverrides := make(map[string]int)
	if sharesFlag != "" {
		for _, part := range strings.Split(sharesFlag, ",") {
			kv := strings.Split(strings.TrimSpace(part), ":")
			if len(kv) == 2 {
				sym := portfolio.CleanTicker(strings.TrimSpace(kv[0]))
				if qty, err := strconv.Atoi(strings.TrimSpace(kv[1])); err == nil {
					flagOverrides[sym] = qty
				}
			}
		}
	}

	// Identify and analyze shared tickers
	var sharedList []sharedConstituentInfo
	sharedMap := make(map[string]*sharedConstituentInfo)
	var totalGrossDematValue float64
	var totalClaimedValue float64

	for _, inst := range basketKeys {
		sym := portfolio.CleanTicker(inst)
		ltp := quoteData[inst]
		qty := currentHoldings[sym]
		totalGrossDematValue += float64(qty) * ltp

		if len(otherThemeTickers[sym]) > 0 && qty > 0 {
			currentRecorded := 0
			claimedByOther := 0
			if dbShares != nil {
				if symMap, ok := dbShares[sym]; ok {
					currentRecorded = symMap[currentTheme.Name]
					if currentRecorded == 0 {
						currentRecorded = symMap[uName]
					}
					for _, otc := range otherThemes {
						if q, exists := symMap[otc.Name]; exists {
							claimedByOther += q
						} else if q, exists2 := symMap[csvloader.GetUniverseName(otc.CSVPath)]; exists2 {
							claimedByOther += q
						}
					}
				}
			}

			// Fallback for cold start: if other theme has it and current recorded is 0,
			// Demat shares belong to other theme
			if claimedByOther == 0 && qty > 0 && currentRecorded == 0 {
				claimedByOther = qty
			}

			valClaimed := float64(claimedByOther) * ltp
			totalClaimedValue += valClaimed

			info := sharedConstituentInfo{
				ticker:          inst,
				symbol:          sym,
				totalBrokerQty:  qty,
				claimedByOther:  claimedByOther,
				currentRecorded: currentRecorded,
				otherNames:      strings.Join(otherThemeTickers[sym], ", "),
				targetWeight:    basket[inst],
				ltp:             ltp,
				valueClaimed:    valClaimed,
			}
			sharedList = append(sharedList, info)
			sharedMap[sym] = &info
		}
	}

	if len(sharedList) == 0 {
		return nil, currentTheme.Name, nil
	}

	mktCfg := broker.LoadMarketConfig()
	netCurrentPortfolioValue := totalGrossDematValue - totalClaimedValue
	buyOverrides := make(map[string]int)

	// Display upfront Audit Table
	fmt.Printf("\n══════════════════════════════════════════════════════════════════════════════\n")
	fmt.Printf("                   CROSS-THEME SHARED HOLDINGS AUDIT                          \n")
	fmt.Printf("══════════════════════════════════════════════════════════════════════════════\n")
	fmt.Println("The following instruments in this basket are also held by other themes:")
	fmt.Println()
	fmt.Printf("  %-16s | %-9s | %-13s | %s\n", "Ticker", "Demat Qty", "Claimed Value", "Claiming Themes")
	fmt.Println("  ----------------------------------------------------------------------------")
	var claimedTickers []string
	for _, info := range sharedList {
		claimedTickers = append(claimedTickers, info.symbol)
		fmt.Printf("  %-16s | %-9d | %s%-12.2f | %s\n",
			info.ticker, info.totalBrokerQty, mktCfg.Currency, info.valueClaimed, info.otherNames)
	}
	fmt.Println("  ----------------------------------------------------------------------------")
	fmt.Printf("  Total Gross Demat Value:       %s%.2f\n", mktCfg.Currency, totalGrossDematValue)
	fmt.Printf("  Less Other Themes' Claims:    -%s%.2f (%s)\n",
		mktCfg.Currency, totalClaimedValue, strings.Join(claimedTickers, ", "))
	fmt.Println("  ----------------------------------------------------------------------------")
	fmt.Printf("  %s Net Baseline:   %s%.2f\n", currentTheme.Name, mktCfg.Currency, netCurrentPortfolioValue)
	fmt.Printf("══════════════════════════════════════════════════════════════════════════════\n")

	// Process each shared holding with the user
	for _, info := range sharedList {
		sym := info.symbol
		inst := info.ticker
		targetWeight := info.targetWeight
		ltp := info.ltp
		totalBrokerQty := info.totalBrokerQty
		currentRecorded := info.currentRecorded
		claimedByOther := info.claimedByOther

		if targetWeight > 0.0 {
			// Case 1: Active Target (e.g. LIQUIDCASE @ 35%)
			targetAlloc := netCurrentPortfolioValue * targetWeight
			targetQty := int(targetAlloc/ltp + 0.5)

			optionAShares := targetQty - currentRecorded
			if optionAShares < 0 {
				optionAShares = 0
			}

			optionBShares := 0
			if totalBrokerQty < targetQty {
				optionBShares = targetQty - totalBrokerQty
			}

			resolvedBuyQty := optionAShares

			if overrideQty, hasOverride := flagOverrides[sym]; hasOverride {
				resolvedBuyQty = overrideQty
				fmt.Printf("\n[Shared Holding] %s: using CLI flag override %d shares to BUY (Broker has %d)\n",
					inst, resolvedBuyQty, totalBrokerQty)
			} else if autoYes {
				resolvedBuyQty = optionAShares
				fmt.Printf("\n[Shared Holding] %s: auto-selected Option A (%d shares to BUY)\n", inst, resolvedBuyQty)
			} else if reader != nil {
				fmt.Printf("\n--- Shared Holding: %s (Target Weight: %.1f%%) ---\n", inst, targetWeight*100.0)
				fmt.Printf("  • Total in Demat (Kite):          %d shares\n", totalBrokerQty)
				fmt.Printf("  • Claimed by %s:        %d shares\n", info.otherNames, claimedByOther)
				fmt.Printf("  • %s Net Value:   %s%.2f\n", currentTheme.Name, mktCfg.Currency, netCurrentPortfolioValue)
				fmt.Println("  ──────────────────────────────────────────────────────────────────")
				fmt.Printf("  • Option A (Isolated / Dedicated):  %d shares to BUY → [Default] Fresh allocation (%.1f%% of %s%.0f)\n",
					optionAShares, targetWeight*100.0, mktCfg.Currency, netCurrentPortfolioValue)
				fmt.Printf("  • Option B (Consider other holding):  %d shares to BUY → Shared pool (buys %d as Demat has %d shares)\n",
					optionBShares, optionBShares, totalBrokerQty)
				fmt.Println("  ──────────────────────────────────────────────────────────────────")
				fmt.Printf("Enter shares to BUY for '%s' [Press Enter for %d, 'B' or '0' for Option B, or enter custom qty]: ",
					currentTheme.Name, optionAShares)

				input, _ := reader.ReadString('\n')
				trimmed := strings.TrimSpace(input)
				if trimmed == "" {
					resolvedBuyQty = optionAShares
				} else if strings.EqualFold(trimmed, "b") || trimmed == "0" {
					resolvedBuyQty = optionBShares
				} else if n, err := strconv.Atoi(trimmed); err == nil && n >= 0 {
					resolvedBuyQty = n
				} else {
					fmt.Printf("Unrecognized input %q. Defaulting to Option A (%d shares).\n", trimmed, optionAShares)
					resolvedBuyQty = optionAShares
				}
			}

			buyOverrides[sym] = resolvedBuyQty
			currentHoldings[sym] = currentRecorded
			parts := strings.Split(inst, ":")
			if len(parts) > 1 {
				currentHoldings[parts[1]] = currentRecorded
			}

		} else {
			// Case 2: Exit in this theme, but active holding in another theme (e.g. NETWEB @ 0.0%)
			// Default is to protect the other theme (sell 0 shares)
			sharesToSell := 0

			if overrideQty, hasOverride := flagOverrides[sym]; hasOverride {
				sharesToSell = overrideQty
				fmt.Printf("\n[Shared Holding] %s: using CLI flag override %d shares to SELL\n", inst, sharesToSell)
			} else if autoYes {
				sharesToSell = 0
				fmt.Printf("\n[Shared Holding] %s: auto-protected %s (0 shares to SELL)\n", inst, info.otherNames)
			} else if reader != nil {
				fmt.Printf("\n--- Shared Holding: %s (Target Weight: 0.0%% - Exit) ---\n", inst)
				fmt.Printf("  • Total in Demat (Kite):          %d shares (Value: %s%.2f)\n",
					totalBrokerQty, mktCfg.Currency, float64(totalBrokerQty)*ltp)
				fmt.Printf("  • Claimed by %s:        %d shares (Active holding in other theme)\n",
					info.otherNames, claimedByOther)
				fmt.Println("  ──────────────────────────────────────────────────────────────────")
				fmt.Printf("  • Option A (Protect %s): 0 shares to SELL → [Default] Keep in Demat for other theme\n", info.otherNames)
				fmt.Printf("  • Option B (Liquidate from Demat): %d shares to SELL → Sell all shares from Demat\n", totalBrokerQty)
				fmt.Println("  ──────────────────────────────────────────────────────────────────")
				fmt.Printf("Enter shares of %s to SELL from '%s' [Press Enter for 0 (Protect), or enter qty to sell]: ",
					sym, currentTheme.Name)

				input, _ := reader.ReadString('\n')
				trimmed := strings.TrimSpace(input)
				if trimmed == "" || trimmed == "0" || strings.EqualFold(trimmed, "a") {
					sharesToSell = 0
				} else if n, err := strconv.Atoi(trimmed); err == nil && n >= 0 {
					sharesToSell = n
				} else {
					fmt.Printf("Unrecognized input %q. Defaulting to 0 (Protect).\n", trimmed)
					sharesToSell = 0
				}
			}

			// If selling 0: attribute 0 to current holdings so diff = 0 - 0 = 0 (no order generated).
			// If selling N: attribute N to current holdings so diff = 0 - N = -N (SELL N).
			currentHoldings[sym] = sharesToSell
			parts := strings.Split(inst, ":")
			if len(parts) > 1 {
				currentHoldings[parts[1]] = sharesToSell
			}
			buyOverrides[sym] = 0 // target is 0
		}
	}

	return buyOverrides, uName, nil
}

// recordPostExecutionState records the updated theme holdings into data/mycase.db
// so subsequent rebalances have accurate, isolated starting positions.
func recordPostExecutionState(
	ctx context.Context,
	themeName string,
	basketKeys []string,
	basket map[string]float64,
	quoteData map[string]float64,
	currentHoldings map[string]int,
	finalQuantities []int,
	totalTargetValue float64,
) {
	if themeName == "" {
		return
	}
	tdb, err := themedb.Open("")
	if err != nil {
		return
	}

	err = tdb.RecordBasketRebalance(
		ctx, themeName, basketKeys, basket, quoteData, currentHoldings, finalQuantities, totalTargetValue,
	)
	if err == nil {
		fmt.Printf("Recorded updated constituent holdings for %q in data/mycase.db\n", themeName)
	}

	// Also ensure other themes that held shared assets have a baseline record if never committed
	themeConfigs, err := config.LoadThemes(config.Path("themes.json"))
	if err != nil {
		return
	}
	for _, tc := range themeConfigs {
		otherUName := csvloader.GetUniverseName(tc.CSVPath)
		if strings.EqualFold(otherUName, themeName) || strings.EqualFold(tc.Name, themeName) {
			continue
		}
		has, _ := tdb.HasTheme(ctx, otherUName)
		if !has {
			// Seed other theme baseline from its CSV
			pCSV := resolveBasketFile(tc.CSVPath)
			if pMap, _, err := csvloader.LoadBasketCSV(pCSV); err == nil && len(pMap) > 0 {
				var otherKeys []string
				var otherQtys []int
				for k := range pMap {
					otherKeys = append(otherKeys, k)
					sym := portfolio.CleanTicker(k)
					qty := currentHoldings[sym]
					otherQtys = append(otherQtys, qty)
				}
				_ = tdb.RecordBasketRebalance(ctx, otherUName, otherKeys, pMap, quoteData, currentHoldings, otherQtys, 0.0)
			}
		}
	}
}
