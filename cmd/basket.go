package cmd

import (
	"bufio"
	"context"
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/urfave/cli/v3"

	"github.com/raghavkgarg/mycase/pkg/broker"
	"github.com/raghavkgarg/mycase/pkg/broker/zerodha"
	"github.com/raghavkgarg/mycase/pkg/costs"
	"github.com/raghavkgarg/mycase/pkg/csvloader"
	"github.com/raghavkgarg/mycase/pkg/datafetcher"
	"github.com/raghavkgarg/mycase/pkg/executor"
	"github.com/raghavkgarg/mycase/pkg/optimizer"
	"github.com/raghavkgarg/mycase/pkg/printer"
	"github.com/raghavkgarg/mycase/pkg/stockpicker"
)

var BasketCommand = &cli.Command{
	Name:  "basket",
	Usage: "Execute or preview basket orders on Zerodha",
	Flags: []cli.Flag{
		&cli.BoolFlag{Name: "live", Usage: "Use live Zerodha API (default: dry-run mock mode)"},
		&cli.StringFlag{Name: "file", Value: "data/basket.csv", Usage: "Path to basket CSV file"},
	},
	Action: func(ctx context.Context, c *cli.Command) error {
		filename := c.String("file")
		// Also accept first positional arg as basket name (backward compat)
		if arg := c.Args().Get(0); arg != "" {
			cleaned := cleanBasketArg(arg)
			if cleaned != "" {
				if strings.HasSuffix(cleaned, ".csv") {
					filename = "data/" + cleaned
				} else {
					filename = "data/" + cleaned + ".csv"
				}
			}
		}
		return runBasketWithParams(ctx, c.Bool("live"), filename)
	},
}

func runBasketWithParams(ctx context.Context, liveMode bool, basketFilename string) error {
	fmt.Println("====================================================================")
	fmt.Println("                 Go Mycase Basket Engine                         ")
	if liveMode {
		fmt.Println("                 [LIVE MODE]                                        ")
	} else {
		fmt.Println("                 [DRY RUN / MOCK MODE]                              ")
	}
	fmt.Println("====================================================================")
	fmt.Printf("Loading basket configuration: %s\n", basketFilename)

	b := zerodha.New(liveMode, "config/config.json")

	basket, basketKeys, err := csvloader.LoadBasketCSV(basketFilename)
	if err != nil {
		return fmt.Errorf("loading basket config: %w", err)
	}

	if stockpicker.IsUSIndex(basketFilename) {
		fmt.Printf("\n[Basket Engine] US market portfolio detected (%s). Zerodha execution only supports Indian stocks (NSE/BSE). Skipping basket execution.\n", basketFilename)
		return nil
	}

	fmt.Println("\nSelect an action:")
	fmt.Println("1. Fresh Buy (Invest dynamic amount)")
	fmt.Println("2. Rebalance (Align holdings to target weights)")
	fmt.Println("3. Exit")
	fmt.Print("Enter choice (1-3): ")

	reader := bufio.NewReader(os.Stdin)
	choiceInput, _ := reader.ReadString('\n')
	choice := strings.TrimSpace(choiceInput)

	if choice == "3" || choice == "" {
		fmt.Println("Exiting...")
		return nil
	}

	quoteData, currentHoldings, holdingDetails, err := datafetcher.FetchMarketData(ctx, b, basketKeys)
	if err != nil {
		return fmt.Errorf("fetching market data: %w", err)
	}

	var basketOrders []broker.Order
	var finalQuantities []int
	var printedPreview bool
	var snapshotText string

	if choice == "1" {
		fmt.Print("Enter total investment amount in Rupees: ")
		amtInput, _ := reader.ReadString('\n')
		amtStr := strings.TrimSpace(amtInput)
		if amtStr == "" {
			fmt.Println("No amount entered. Exiting...")
			return nil
		}
		totalInvestment, err := strconv.ParseFloat(amtStr, 64)
		if err != nil {
			return fmt.Errorf("invalid amount: %w", err)
		}

		for {
			basketOrders = nil
			rawQuantities := optimizer.OptimizeFreshBuy(basketKeys, basket, quoteData, currentHoldings, totalInvestment)

			for i, inst := range basketKeys {
				parts := strings.Split(inst, ":")
				symbol := parts[len(parts)-1]
				ltp := quoteData[inst]

				currentQty := currentHoldings[symbol]
				finalQty := rawQuantities[i]
				diff := finalQty - currentQty

				if diff > 0 {
					bufferPrice := ltp + 2.0
					roundedPrice := math.Round(bufferPrice*10.0) / 10.0
					basketOrders = append(basketOrders, broker.Order{
						TradingSymbol:   symbol,
						Exchange:        "NSE",
						TransactionType: "BUY",
						Quantity:        diff,
						OrderType:       "LIMIT",
						Product:         "CNC",
						Ltp:             ltp,
						Price:           roundedPrice,
					})
				} else if diff < 0 {
					bufferPrice := ltp - 2.0
					roundedPrice := math.Round(bufferPrice*10.0) / 10.0
					basketOrders = append(basketOrders, broker.Order{
						TradingSymbol:   symbol,
						Exchange:        "NSE",
						TransactionType: "SELL",
						Quantity:        int(math.Abs(float64(diff))),
						OrderType:       "LIMIT",
						Product:         "CNC",
						Ltp:             ltp,
						Price:           roundedPrice,
					})
				}
			}

			finalQuantities = rawQuantities
			snapshotText = printer.PrintPreviewTable(basketKeys, basket, quoteData, currentHoldings, finalQuantities, holdingDetails)
			printedPreview = true

			fmt.Print("\nEnter a new investment amount to recalculate (or press Enter to proceed to execution): ")
			adjustInput, _ := reader.ReadString('\n')
			adjustStr := strings.TrimSpace(adjustInput)
			if adjustStr == "" {
				break
			}
			newAmt, err := strconv.ParseFloat(adjustStr, 64)
			if err != nil {
				fmt.Printf("Invalid amount: %v\n", err)
				break
			}
			totalInvestment = newAmt
		}

	} else if choice == "2" {
		var totalCurrentPortfolioValue float64
		for _, inst := range basketKeys {
			parts := strings.Split(inst, ":")
			symbol := parts[len(parts)-1]
			totalCurrentPortfolioValue += float64(currentHoldings[symbol]) * quoteData[inst]
		}
		fmt.Printf("Total Current Basket Portfolio Value: ₹%.2f\n", totalCurrentPortfolioValue)

		fmt.Print("Enter fresh cash/investment to add during rebalance (press Enter for ₹0): ")
		freshInput, _ := reader.ReadString('\n')
		freshStr := strings.TrimSpace(freshInput)
		var freshMoney float64
		if freshStr != "" {
			freshMoney, err = strconv.ParseFloat(freshStr, 64)
			if err != nil {
				fmt.Printf("Invalid amount: %v. Assuming ₹0.\n", err)
				freshMoney = 0
			}
		}

		totalTargetValue := totalCurrentPortfolioValue + freshMoney
		fmt.Printf("Target Portfolio Value (Current + Fresh Cash): ₹%.2f\n", totalTargetValue)

		for _, inst := range basketKeys {
			parts := strings.Split(inst, ":")
			symbol := parts[len(parts)-1]
			targetWeight := basket[inst]
			ltp := quoteData[inst]
			currentQty := currentHoldings[symbol]

			targetAllocation := totalTargetValue * targetWeight
			targetQty := int(targetAllocation/ltp + 0.5)
			finalQuantities = append(finalQuantities, targetQty)

			diff := targetQty - currentQty
			if diff != 0 {
				var txType string
				var bufferPrice float64
				if diff > 0 {
					txType = "BUY"
					bufferPrice = ltp + 2.0
				} else {
					txType = "SELL"
					bufferPrice = ltp - 2.0
				}
				roundedPrice := math.Round(bufferPrice*10.0) / 10.0
				basketOrders = append(basketOrders, broker.Order{
					TradingSymbol:   symbol,
					Exchange:        "NSE",
					TransactionType: txType,
					Quantity:        int(math.Abs(float64(diff))),
					OrderType:       "LIMIT",
					Product:         "CNC",
					Ltp:             ltp,
					Price:           roundedPrice,
				})
			}
		}
	}

	basketOrders = applyTransactionFilters(basketOrders, quoteData, b, basket)

	executor.ExecuteBasketOrders(
		basketOrders, quoteData, currentHoldings, finalQuantities,
		basketKeys, basket, b, printedPreview, snapshotText, reader, holdingDetails,
	)
	return nil
}

// applyTransactionFilters runs the micro-transaction cost filter and prints
// STCG/LTCG tax warnings for any SELL orders. Returns the filtered order list.
func applyTransactionFilters(
	orders []broker.Order,
	quotes map[string]float64,
	b broker.Broker,
	basket map[string]float64,
) []broker.Order {
	if len(orders) == 0 {
		return orders
	}

	const microTxThreshold = 0.005 // 0.5% cost-to-value threshold

	kept, filtered := optimizer.FilterMicroTransactionsWithExits(orders, quotes, costs.DefaultZerodha, microTxThreshold, basket)

	if len(filtered) > 0 {
		fmt.Printf("\n--- Micro-Transaction Filter (cost > %.1f%% of trade value) ---\n", microTxThreshold*100)
		for _, o := range filtered {
			price := o.Price
			if price <= 0 {
				price = quotes["NSE:"+o.TradingSymbol]
			}
			bd := costs.DefaultZerodha.Calculate(o.TransactionType, o.Quantity, price)
			fmt.Printf("  SKIPPED %s %s × %d  trade=₹%.0f  costs=₹%.2f (%.2f%%)\n",
				o.TransactionType, o.TradingSymbol, o.Quantity,
				bd.TradeValue, bd.Total, bd.CostRatio*100)
		}
	}

	printCostSummary(kept, quotes)
	printTaxWarnings(kept, b)

	return kept
}

// printCostSummary shows estimated transaction costs for the orders that will execute.
func printCostSummary(orders []broker.Order, quotes map[string]float64) {
	if len(orders) == 0 {
		return
	}
	var totalCosts, totalValue float64
	for _, o := range orders {
		price := o.Price
		if price <= 0 {
			price = quotes["NSE:"+o.TradingSymbol]
		}
		bd := costs.DefaultZerodha.Calculate(o.TransactionType, o.Quantity, price)
		totalCosts += bd.Total
		totalValue += bd.TradeValue
	}
	fmt.Printf("\nEstimated transaction costs: ₹%.2f on ₹%.0f traded (%.3f%%)\n",
		totalCosts, totalValue, (totalCosts/totalValue)*100)
}

// printTaxWarnings fetches holdings and prints STCG/LTCG warnings for SELL orders.
func printTaxWarnings(orders []broker.Order, b broker.Broker) {
	var sells []broker.Order
	for _, o := range orders {
		if o.TransactionType == "SELL" {
			sells = append(sells, o)
		}
	}
	if len(sells) == 0 {
		return
	}

	holdings, err := b.GetHoldings()
	holdingMap := make(map[string]broker.Holding, len(holdings))
	if err == nil {
		for _, h := range holdings {
			holdingMap[h.TradingSymbol] = h
		}
	}

	fmt.Println("\n--- Tax Warning (Finance Act 2024) ---")
	for _, o := range sells {
		h := holdingMap[o.TradingSymbol]
		price := o.Price
		if price <= 0 {
			price = o.Ltp
		}
		// PurchaseDate is not available from broker API; ClassifySell handles zero time.
		w := costs.ClassifySell(o.TradingSymbol, o.Quantity, price, h.AveragePrice, time.Time{})
		fmt.Println(" ", w.Note)
		if w.EstimatedGain > 0 && w.EstimatedTax > 0 {
			fmt.Printf("    Estimated gain: ₹%.0f  |  Estimated tax: ₹%.0f\n", w.EstimatedGain, w.EstimatedTax)
		} else if w.EstimatedGain > 0 {
			fmt.Printf("    Estimated gain: ₹%.0f\n", w.EstimatedGain)
		}
	}
}

func cleanBasketArg(arg string) string {
	for strings.HasPrefix(arg, "-") {
		arg = arg[1:]
	}
	return strings.TrimSpace(arg)
}
