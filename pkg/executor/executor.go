package executor

import (
	"bufio"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/raghavkgarg/mycase/pkg/broker"
	"github.com/raghavkgarg/mycase/pkg/market"
	"github.com/raghavkgarg/mycase/pkg/printer"
)

func ExecuteBasketOrders(
	basketOrders []broker.Order,
	quoteData map[string]float64,
	currentHoldings map[string]int,
	finalQuantities []int,
	basketKeys []string,
	basket map[string]float64,
	b broker.Broker,
	printedPreview bool,
	snapshotTextIn string,
	reader *bufio.Reader,
) {
	snapshotText := snapshotTextIn
	if len(basketOrders) == 0 {
		fmt.Println("\nNo transactions required. Basket is perfectly balanced or investment too small.")
		return
	}

	if !printedPreview {
		snapshotText = printer.PrintPreviewTable(basketKeys, basket, quoteData, currentHoldings, finalQuantities)
	}

	fmt.Print("Do you want to execute these orders? (y/n): ")
	confirmInput, _ := reader.ReadString('\n')
	if strings.ToLower(strings.TrimSpace(confirmInput)) != "y" {
		fmt.Println("Cancelled order execution.")
		return
	}

	var logText strings.Builder
	hasErrors := false

	isMarketHours := market.CheckMarketHours()
	orderVariety := "regular"
	isGTT := false

	if !isMarketHours {
		fmt.Println("Select order placement mode:")
		fmt.Println("1) AMO (After Market Order)")
		fmt.Println("2) GTT (Good Till Triggered)")
		fmt.Println("3) Regular Order (Limit/Market)")
		fmt.Print("Enter choice (1-3) [default: 1]: ")
		modeInput, _ := reader.ReadString('\n')
		switch strings.TrimSpace(modeInput) {
		case "2":
			isGTT = true
			fmt.Println("Selected: GTT (Good Till Triggered)")
		case "3":
			fmt.Println("Selected: Regular Order")
		default:
			orderVariety = "amo"
			fmt.Println("Selected: AMO (After Market Order)")
		}
	}

	if b.IsMock() {
		modeStr := "Regular"
		if isGTT {
			modeStr = "GTT"
		} else if orderVariety == "amo" {
			modeStr = "AMO"
		}
		logText.WriteString(fmt.Sprintf("\n[MOCK] Execute simulated successfully (Dry Run) using %s mode.\n", modeStr))
		fmt.Print(logText.String())
	} else {
		logText.WriteString("\nExecuting orders live...\n")
		fmt.Println("\nExecuting orders live...")

		loc, err := time.LoadLocation("Asia/Kolkata")
		if err != nil {
			loc = time.FixedZone("IST", 5.5*60*60)
		}

		for _, order := range basketOrders {
			ltp := quoteData["NSE:"+order.TradingSymbol]
			orderNow := time.Now().In(loc).Format("2006-01-02 15:04:05")

			if isGTT {
				triggerPrice, limitPrice := market.CalculateGTTParams(ltp, order.TransactionType)
				gttOrder := order
				gttOrder.TriggerPrice = triggerPrice
				gttOrder.Price = limitPrice
				gttOrder.Ltp = ltp

				result, err := b.PlaceGTT(gttOrder)
				if err != nil {
					hasErrors = true
					line := fmt.Sprintf("Error placing GTT order for %s: %v", order.TradingSymbol, err)
					fmt.Println(line)
					logText.WriteString(line + "\n")
				} else {
					line := fmt.Sprintf("Placed GTT order (Trigger ID: %d) for %d shares of %s (Trigger: ₹%.2f, Limit: ₹%.2f) at %s",
						result.TriggerID, order.Quantity, order.TradingSymbol, triggerPrice, limitPrice, orderNow)
					fmt.Println(line)
					logText.WriteString(line + "\n")
				}
			} else {
				execPrice := math.Round(ltp*10.0) / 10.0
				regOrder := order
				regOrder.Price = execPrice
				regOrder.Ltp = ltp

				result, err := b.PlaceOrder(orderVariety, regOrder)
				if err != nil {
					hasErrors = true
					line := fmt.Sprintf("Error placing order for %s: %v", order.TradingSymbol, err)
					fmt.Println(line)
					logText.WriteString(line + "\n")
				} else {
					line := fmt.Sprintf("Placed %s order %s for %d shares of %s @ ₹%.2f at %s",
						strings.ToUpper(orderVariety), result.OrderID, order.Quantity, order.TradingSymbol, execPrice, orderNow)
					fmt.Println(line)
					logText.WriteString(line + "\n")
				}
			}
		}
	}

	LogOrderDetails(snapshotText, logText.String(), b.IsMock(), hasErrors)
}

func LogOrderDetails(snapshotText, logText string, isMock, hasErrors bool) {
	folder := "Order"
	if isMock || hasErrors {
		folder = "Error"
	}

	if err := os.MkdirAll(folder, 0755); err != nil {
		fmt.Printf("Failed to create directory %s: %v\n", folder, err)
		return
	}

	nowStr := time.Now().Format("060102_150405")
	filename := filepath.Join(folder, "Order_"+nowStr+".txt")

	if err := os.WriteFile(filename, []byte(snapshotText+"\n"+logText), 0644); err != nil {
		fmt.Printf("Failed to save order details to file: %v\n", err)
	} else {
		fmt.Printf("\nOrder details logged to %s\n", filename)
	}
}
