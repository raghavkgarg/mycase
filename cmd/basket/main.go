package main

import (
	"bufio"
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"

	"mycase/pkg/csvloader"
	"mycase/pkg/datafetcher"
	"mycase/pkg/executor"
	"mycase/pkg/kiteclient"
	"mycase/pkg/optimizer"
	"mycase/pkg/printer"
)

func main() {
	// 1. Parse CLI arguments
	liveMode := false
	basketFilename := "data/basket.csv"

	args := os.Args[1:]
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--live" {
			liveMode = true
		} else if arg == "--" {
			if i+1 < len(args) {
				i++
				cleaned := cleanArg(args[i])
				if cleaned != "" {
					if strings.HasSuffix(cleaned, ".csv") {
						basketFilename = "data/" + cleaned
					} else {
						basketFilename = "data/" + cleaned + ".csv"
					}
				}
			}
		} else {
			cleaned := cleanArg(arg)
			if cleaned != "" {
				if strings.HasSuffix(cleaned, ".csv") {
					basketFilename = "data/" + cleaned
				} else {
					basketFilename = "data/" + cleaned + ".csv"
				}
			}
		}
	}

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

	// Load target basket weights
	basket, basketKeys, err := csvloader.LoadBasketCSV(basketFilename)
	if err != nil {
		fmt.Printf("Error loading basket config: %v\n", err)
		return
	}

	// 2. Present Menu
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
		return
	}

	// 3. Fetch market data (real or mock)
	quoteData, currentHoldings, err := datafetcher.FetchMarketData(isMock, client, basketKeys)
	if err != nil {
		fmt.Printf("Failed to fetch market data: %v\n", err)
		return
	}

	var basketOrders []executor.BasketOrder
	var finalQuantities []int
	var printedPreview bool
	var snapshotText string

	if choice == "1" {
		// Fresh buy flow
		fmt.Print("Enter total investment amount in Rupees: ")
		amtInput, _ := reader.ReadString('\n')
		amtStr := strings.TrimSpace(amtInput)
		if amtStr == "" {
			fmt.Println("No amount entered. Exiting...")
			return
		}
		totalInvestment, err := strconv.ParseFloat(amtStr, 64)
		if err != nil {
			fmt.Printf("Invalid amount: %v\n", err)
			return
		}

		for {
			basketOrders = nil
			rawQuantities := optimizer.OptimizeFreshBuy(basketKeys, basket, quoteData, currentHoldings, totalInvestment)

			// Create final BUY orders for additional shares
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
		// Rebalance flow
		var totalCurrentPortfolioValue float64
		for _, inst := range basketKeys {
			parts := strings.Split(inst, ":")
			symbol := parts[len(parts)-1]
			ltp := quoteData[inst]
			qty := currentHoldings[symbol]
			totalCurrentPortfolioValue += float64(qty) * ltp
		}
		fmt.Printf("Total Current Basket Portfolio Value: ₹%.2f\n", totalCurrentPortfolioValue)

		fmt.Print("Enter fresh cash/investment to add during rebalance (press Enter for ₹0): ")
		freshInput, _ := reader.ReadString('\n')
		freshStr := strings.TrimSpace(freshInput)
		var freshMoney float64
		if freshStr != "" {
			var err error
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


	// 4. Confirm and Execute orders
	executor.ExecuteBasketOrders(
		basketOrders,
		quoteData,
		currentHoldings,
		finalQuantities,
		basketKeys,
		basket,
		client,
		isMock,
		printedPreview,
		snapshotText,
		reader,
	)
}

func cleanArg(arg string) string {
	for strings.HasPrefix(arg, "-") {
		arg = arg[1:]
	}
	return strings.TrimSpace(arg)
}

