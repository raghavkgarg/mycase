package cmd

import (
	"bufio"
	"context"
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"

	"github.com/urfave/cli/v3"

	"github.com/gkgarg24/mycase/pkg/csvloader"
	"github.com/gkgarg24/mycase/pkg/datafetcher"
	"github.com/gkgarg24/mycase/pkg/executor"
	"github.com/gkgarg24/mycase/pkg/kiteclient"
	"github.com/gkgarg24/mycase/pkg/optimizer"
	"github.com/gkgarg24/mycase/pkg/printer"
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

	client, isMock := kiteclient.LoadAndInitClient("config/config.json", liveMode)

	basket, basketKeys, err := csvloader.LoadBasketCSV(basketFilename)
	if err != nil {
		return fmt.Errorf("loading basket config: %w", err)
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

	quoteData, currentHoldings, err := datafetcher.FetchMarketData(isMock, client, basketKeys)
	if err != nil {
		return fmt.Errorf("fetching market data: %w", err)
	}

	var basketOrders []executor.BasketOrder
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
				buyQty := finalQty - currentQty

				if buyQty > 0 {
					bufferPrice := ltp + 2.0
					roundedPrice := math.Round(bufferPrice*10.0) / 10.0
					basketOrders = append(basketOrders, executor.BasketOrder{
						TradingSymbol:   symbol,
						Exchange:        "NSE",
						TransactionType: "BUY",
						Quantity:        buyQty,
						OrderType:       "LIMIT",
						Product:         "CNC",
						Ltp:             ltp,
						Price:           roundedPrice,
					})
				}
			}

			finalQuantities = rawQuantities
			snapshotText = printer.PrintPreviewTable(basketKeys, basket, quoteData, currentHoldings, finalQuantities)
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
				basketOrders = append(basketOrders, executor.BasketOrder{
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

	executor.ExecuteBasketOrders(
		basketOrders, quoteData, currentHoldings, finalQuantities,
		basketKeys, basket, client, isMock, printedPreview, snapshotText, reader,
	)
	return nil
}

func cleanBasketArg(arg string) string {
	for strings.HasPrefix(arg, "-") {
		arg = arg[1:]
	}
	return strings.TrimSpace(arg)
}
