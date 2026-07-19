package executor

import (
	"bufio"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"

	kiteconnect "github.com/zerodha/gokiteconnect/v4"
	"github.com/gkgarg24/mycase/pkg/market"
	"github.com/gkgarg24/mycase/pkg/printer"
)

type BasketOrder struct {
	TradingSymbol   string
	Exchange        string
	TransactionType string
	Quantity        int
	OrderType       string
	Product         string
	Ltp             float64 // Raw LTP
	Price           float64 // Buffered price for GTT
}

func ExecuteBasketOrders(
	basketOrders []BasketOrder,
	quoteData map[string]float64,
	currentHoldings map[string]int,
	finalQuantities []int,
	basketKeys []string,
	basket map[string]float64,
	client *kiteconnect.Client,
	isMock bool,
	printedPreview bool,
	snapshotTextIn string,
	reader *bufio.Reader,
) {
	snapshotText := snapshotTextIn
	numOrders := len(basketOrders)
	if numOrders == 0 {
		fmt.Println("\nNo transactions required. Basket is perfectly balanced or investment too small.")
		return
	}

	if !printedPreview {
		snapshotText = printer.PrintPreviewTable(basketKeys, basket, quoteData, currentHoldings, finalQuantities)
	}

	fmt.Print("Do you want to execute these orders? (y/n): ")
	confirmInput, _ := reader.ReadString('\n')
	confirm := strings.ToLower(strings.TrimSpace(confirmInput))

	if confirm == "y" {
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
			modeChoice := strings.TrimSpace(modeInput)

			if modeChoice == "2" {
				isGTT = true
				fmt.Println("Selected: GTT (Good Till Triggered)")
			} else if modeChoice == "3" {
				orderVariety = "regular"
				fmt.Println("Selected: Regular Order")
			} else {
				orderVariety = "amo"
				fmt.Println("Selected: AMO (After Market Order)")
			}
		}

		if isMock {
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
				key := "NSE:" + order.TradingSymbol
				ltp := quoteData[key]

				if isGTT {
					triggerPrice, limitPrice := market.CalculateGTTParams(ltp, order.TransactionType)

					gttParams := kiteconnect.GTTParams{
						Tradingsymbol:   order.TradingSymbol,
						Exchange:        order.Exchange,
						LastPrice:       ltp,
						TransactionType: order.TransactionType,
						Product:         order.Product,
						Trigger: &kiteconnect.GTTSingleLegTrigger{
							TriggerParams: kiteconnect.TriggerParams{
								TriggerValue: triggerPrice,
								LimitPrice:   limitPrice,
								Quantity:     float64(order.Quantity),
							},
						},
					}

					resp, err := client.PlaceGTT(gttParams)
					orderNow := time.Now().In(loc).Format("2006-01-02 15:04:05")
					if err != nil {
						hasErrors = true
						line := fmt.Sprintf("Error placing GTT order for %s: %v", order.TradingSymbol, err)
						fmt.Println(line)
						logText.WriteString(line + "\n")
					} else {
						line := fmt.Sprintf("Placed GTT order (Trigger ID: %d) for %d shares of %s (Trigger: ₹%.2f, Limit: ₹%.2f) at %s",
							resp.TriggerID, order.Quantity, order.TradingSymbol, triggerPrice, limitPrice, orderNow)
						fmt.Println(line)
						logText.WriteString(line + "\n")
					}
				} else {
					execPrice := math.Round(ltp*10.0) / 10.0
					orderParams := kiteconnect.OrderParams{
						Exchange:        order.Exchange,
						Tradingsymbol:   order.TradingSymbol,
						Product:         order.Product,
						OrderType:       "LIMIT",
						TransactionType: order.TransactionType,
						Quantity:        order.Quantity,
						Price:           execPrice,
					}

					resp, err := client.PlaceOrder(orderVariety, orderParams)
					orderNow := time.Now().In(loc).Format("2006-01-02 15:04:05")
					if err != nil {
						hasErrors = true
						line := fmt.Sprintf("Error placing order for %s: %v", order.TradingSymbol, err)
						fmt.Println(line)
						logText.WriteString(line + "\n")
					} else {
						line := fmt.Sprintf("Placed %s order %s for %d shares of %s @ ₹%.2f at %s",
							strings.ToUpper(orderVariety), resp.OrderID, order.Quantity, order.TradingSymbol, execPrice, orderNow)
						fmt.Println(line)
						logText.WriteString(line + "\n")
					}
				}
			}
		}

		LogOrderDetails(snapshotText, logText.String(), isMock, hasErrors)

	} else {
		fmt.Println("Cancelled order execution.")
	}
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

	content := snapshotText + "\n" + logText
	if err := os.WriteFile(filename, []byte(content), 0644); err != nil {
		fmt.Printf("Failed to save order details to file: %v\n", err)
	} else {
		fmt.Printf("\nOrder details logged to %s\n", filename)
	}
}
