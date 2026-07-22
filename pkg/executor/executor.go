package executor

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/raghavkgarg/mycase/pkg/broker"
	"github.com/raghavkgarg/mycase/pkg/market"
	"github.com/raghavkgarg/mycase/pkg/printer"
	"github.com/raghavkgarg/mycase/pkg/yfinance"
)

type FailedOrderSpec struct {
	TradingSymbol   string  `json:"trading_symbol"`
	Exchange        string  `json:"exchange"`
	TransactionType string  `json:"transaction_type"`
	Quantity        int     `json:"quantity"`
	Price           float64 `json:"price"`
	Product         string  `json:"product"`
	ErrorReason     string  `json:"error_reason"`
	OrderVariety    string  `json:"order_variety"`
	IsGTT           bool    `json:"is_gtt"`
}

type RetryPayload struct {
	Timestamp    string            `json:"timestamp"`
	FailedOrders []FailedOrderSpec `json:"failed_orders"`
}

func placeOrderWithRetry(b broker.Broker, variety string, order broker.Order) (broker.OrderResult, error) {
	var result broker.OrderResult
	var err error
	for attempt := 1; attempt <= 3; attempt++ {
		result, err = b.PlaceOrder(variety, order)
		if err == nil {
			return result, nil
		}
		if attempt < 3 {
			time.Sleep(500 * time.Millisecond)
		}
	}
	return result, err
}

func placeGTTWithRetry(b broker.Broker, order broker.Order) (broker.OrderResult, error) {
	var result broker.OrderResult
	var err error
	for attempt := 1; attempt <= 3; attempt++ {
		result, err = b.PlaceGTT(order)
		if err == nil {
			return result, nil
		}
		if attempt < 3 {
			time.Sleep(500 * time.Millisecond)
		}
	}
	return result, err
}

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

	if reader != nil {
		fmt.Print("Do you want to execute these orders? (y/n): ")
		confirmInput, _ := reader.ReadString('\n')
		if strings.ToLower(strings.TrimSpace(confirmInput)) != "y" {
			fmt.Println("Cancelled order execution.")
			return
		}
	}

	isMarketHours := market.CheckMarketHours()
	orderVariety := "regular"
	isGTT := false

	if !isMarketHours && reader != nil {
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

	nowStr := time.Now().Format("060102_150405")

	if b.IsMock() {
		modeStr := "Regular"
		if isGTT {
			modeStr = "GTT"
		} else if orderVariety == "amo" {
			modeStr = "AMO"
		}
		mockMsg := fmt.Sprintf("\n[MOCK] Execute simulated successfully (Dry Run) using %s mode.\n", modeStr)
		fmt.Print(mockMsg)
		SaveSuccessLog(snapshotText, mockMsg, nowStr)
		return
	}

	fmt.Println("\nExecuting orders live...")
	loc, err := time.LoadLocation("Asia/Kolkata")
	if err != nil {
		loc = time.FixedZone("IST", 5.5*60*60)
	}

	var successLines []string
	var failedLines []string
	var failedSpecs []FailedOrderSpec

	for i, order := range basketOrders {
		if i > 0 {
			time.Sleep(200 * time.Millisecond) // Rate limit throttle (max 10 req/s)
		}
		ltp := quoteData["NSE:"+order.TradingSymbol]
		if ltp == 0 {
			ltp = order.Ltp
		}
		orderNow := time.Now().In(loc).Format("2006-01-02 15:04:05")

		if isGTT {
			triggerPrice, limitPrice := market.CalculateGTTParams(ltp, order.TransactionType)
			gttOrder := order
			gttOrder.TriggerPrice = triggerPrice
			gttOrder.Price = limitPrice
			gttOrder.Ltp = ltp

			result, err := placeGTTWithRetry(b, gttOrder)
			if err != nil {
				errMsg := err.Error()
				failedLines = append(failedLines, fmt.Sprintf("Error placing GTT order for %s: %v", order.TradingSymbol, errMsg))
				failedSpecs = append(failedSpecs, FailedOrderSpec{
					TradingSymbol:   order.TradingSymbol,
					Exchange:        order.Exchange,
					TransactionType: order.TransactionType,
					Quantity:        order.Quantity,
					Price:           limitPrice,
					Product:         order.Product,
					ErrorReason:     errMsg,
					OrderVariety:    "gtt",
					IsGTT:           true,
				})
				fmt.Printf("Error placing GTT order for %s: %v\n", order.TradingSymbol, errMsg)
			} else {
				line := fmt.Sprintf("Placed GTT order (Trigger ID: %d) for %d shares of %s (Trigger: ₹%.2f, Limit: ₹%.2f) at %s",
					result.TriggerID, order.Quantity, order.TradingSymbol, triggerPrice, limitPrice, orderNow)
				successLines = append(successLines, line)
				fmt.Println(line)
			}
		} else {
			execPrice := math.Round(ltp*10.0) / 10.0
			regOrder := order
			regOrder.Price = execPrice
			regOrder.Ltp = ltp

			result, err := placeOrderWithRetry(b, orderVariety, regOrder)
			if err != nil {
				errMsg := err.Error()
				failedLines = append(failedLines, fmt.Sprintf("Error placing order for %s: %v", order.TradingSymbol, errMsg))
				failedSpecs = append(failedSpecs, FailedOrderSpec{
					TradingSymbol:   order.TradingSymbol,
					Exchange:        order.Exchange,
					TransactionType: order.TransactionType,
					Quantity:        order.Quantity,
					Price:           execPrice,
					Product:         order.Product,
					ErrorReason:     errMsg,
					OrderVariety:    orderVariety,
					IsGTT:           false,
				})
				fmt.Printf("Error placing order for %s: %v\n", order.TradingSymbol, errMsg)
			} else {
				line := fmt.Sprintf("Placed %s order %s for %d shares of %s @ ₹%.2f at %s",
					strings.ToUpper(orderVariety), result.OrderID, order.Quantity, order.TradingSymbol, execPrice, orderNow)
				successLines = append(successLines, line)
				fmt.Println(line)
			}
		}
	}

	// 1. Write successful orders log if any succeeded
	if len(successLines) > 0 {
		SaveSuccessLog(snapshotText, strings.Join(successLines, "\n"), nowStr)
	}

	// 2. Write error log & temp JSON if any failed
	if len(failedSpecs) > 0 {
		jsonPath := SaveErrorLog(snapshotText, strings.Join(failedLines, "\n"), failedSpecs, nowStr)

		// Prompt user for immediate retry if in interactive mode
		if reader != nil {
			fmt.Printf("\nThere were %d failed order(s). Do you want to retry placing them now? (y/n): ", len(failedSpecs))
			retryInput, _ := reader.ReadString('\n')
			if strings.ToLower(strings.TrimSpace(retryInput)) == "y" {
				ExecuteRetryPayload(jsonPath, b, reader)
			}
		}
	}
}

func SaveSuccessLog(snapshotText, logContent, nowStr string) {
	if err := os.MkdirAll("Order", 0755); err != nil {
		fmt.Printf("Failed to create Order directory: %v\n", err)
		return
	}
	filename := filepath.Join("Order", "Order_"+nowStr+".txt")
	content := snapshotText + "\n\nExecuting orders live...\n" + logContent + "\n"
	if err := os.WriteFile(filename, []byte(content), 0644); err != nil {
		fmt.Printf("Failed to save order log: %v\n", err)
	} else {
		fmt.Printf("\nSuccessful order details logged to %s\n", filename)
	}
}

func SaveErrorLog(snapshotText, logContent string, failedSpecs []FailedOrderSpec, nowStr string) string {
	if err := os.MkdirAll("Error", 0755); err != nil {
		fmt.Printf("Failed to create Error directory: %v\n", err)
		return ""
	}

	txtFilename := filepath.Join("Error", "Order_"+nowStr+".txt")
	txtContent := snapshotText + "\n\nExecuting orders live...\n" + logContent + "\n"
	_ = os.WriteFile(txtFilename, []byte(txtContent), 0644)

	jsonFilename := filepath.Join("Error", "Order_"+nowStr+".json")
	payload := RetryPayload{
		Timestamp:    nowStr,
		FailedOrders: failedSpecs,
	}
	bytes, err := json.MarshalIndent(payload, "", "  ")
	if err == nil {
		_ = os.WriteFile(jsonFilename, bytes, 0644)
	}

	fmt.Printf("\nFailed order details logged to %s and temporary payload %s\n", txtFilename, jsonFilename)
	return jsonFilename
}

// FindLatestErrorPayload returns the path to the newest JSON payload file in Error/
func FindLatestErrorPayload() (string, error) {
	entries, err := os.ReadDir("Error")
	if err != nil {
		return "", fmt.Errorf("cannot read Error directory: %w", err)
	}
	var jsonFiles []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".json") && strings.HasPrefix(e.Name(), "Order_") {
			jsonFiles = append(jsonFiles, filepath.Join("Error", e.Name()))
		}
	}
	if len(jsonFiles) == 0 {
		return "", fmt.Errorf("no pending JSON retry files found in Error/")
	}
	sort.Strings(jsonFiles)
	return jsonFiles[len(jsonFiles)-1], nil
}

// ExecuteRetryPayload retries placing failed orders from a JSON retry payload file.
func ExecuteRetryPayload(jsonPath string, b broker.Broker, reader *bufio.Reader) {
	if jsonPath == "" {
		var err error
		jsonPath, err = FindLatestErrorPayload()
		if err != nil {
			fmt.Printf("Retry error: %v\n", err)
			return
		}
	}

	data, err := os.ReadFile(jsonPath)
	if err != nil {
		fmt.Printf("Failed to read retry file %s: %v\n", jsonPath, err)
		return
	}

	var payload RetryPayload
	if err := json.Unmarshal(data, &payload); err != nil {
		fmt.Printf("Failed to parse JSON retry payload from %s: %v\n", jsonPath, err)
		return
	}

	if len(payload.FailedOrders) == 0 {
		fmt.Println("No failed orders to retry in payload.")
		_ = os.Remove(jsonPath)
		return
	}

	fmt.Printf("\nRetrying %d failed order(s) from %s...\n", len(payload.FailedOrders), jsonPath)

	// Refresh real-time LTP quotes before retrying
	var tickers []string
	for _, fo := range payload.FailedOrders {
		tickers = append(tickers, "NSE:"+fo.TradingSymbol)
	}

	quoteMap := make(map[string]float64)
	if b != nil && !b.IsMock() {
		if quotes, qerr := b.GetQuotes(tickers); qerr == nil {
			quoteMap = quotes
		}
	}
	if len(quoteMap) == 0 {
		ctx := context.Background()
		if yfQuotes, err := yfinance.FetchQuotes(ctx, tickers); err == nil {
			quoteMap = yfQuotes
		}
	}

	loc, err := time.LoadLocation("Asia/Kolkata")
	if err != nil {
		loc = time.FixedZone("IST", 5.5*60*60)
	}

	var remainingFailed []FailedOrderSpec
	var successLines []string

	for i, order := range payload.FailedOrders {
		if i > 0 {
			time.Sleep(200 * time.Millisecond) // Rate limiting throttle
		}

		key := "NSE:" + order.TradingSymbol
		ltp := quoteMap[key]
		if ltp == 0 {
			ltp = order.Price
		}
		orderNow := time.Now().In(loc).Format("2006-01-02 15:04:05")

		bo := broker.Order{
			TradingSymbol:   order.TradingSymbol,
			Exchange:        order.Exchange,
			TransactionType: order.TransactionType,
			Quantity:        order.Quantity,
			Price:           order.Price,
			Product:         order.Product,
		}

		if order.IsGTT {
			triggerPrice, limitPrice := market.CalculateGTTParams(ltp, order.TransactionType)
			bo.TriggerPrice = triggerPrice
			bo.Price = limitPrice
			bo.Ltp = ltp

			res, perr := placeGTTWithRetry(b, bo)
			if perr != nil {
				order.ErrorReason = perr.Error()
				remainingFailed = append(remainingFailed, order)
				fmt.Printf("Retry Error placing GTT order for %s: %v\n", order.TradingSymbol, perr)
			} else {
				line := fmt.Sprintf("Placed GTT order (Trigger ID: %d) for %d shares of %s (Trigger: ₹%.2f, Limit: ₹%.2f) at %s",
					res.TriggerID, order.Quantity, order.TradingSymbol, triggerPrice, limitPrice, orderNow)
				successLines = append(successLines, line)
				fmt.Println(line)
			}
		} else {
			execPrice := math.Round(ltp*10.0) / 10.0
			bo.Price = execPrice
			bo.Ltp = ltp

			variety := order.OrderVariety
			if variety == "" {
				variety = "regular"
			}

			res, perr := placeOrderWithRetry(b, variety, bo)
			if perr != nil {
				order.ErrorReason = perr.Error()
				remainingFailed = append(remainingFailed, order)
				fmt.Printf("Retry Error placing order for %s: %v\n", order.TradingSymbol, perr)
			} else {
				line := fmt.Sprintf("Placed %s order %s for %d shares of %s @ ₹%.2f at %s",
					strings.ToUpper(variety), res.OrderID, order.Quantity, order.TradingSymbol, execPrice, orderNow)
				successLines = append(successLines, line)
				fmt.Println(line)
			}
		}
	}

	retryNowStr := time.Now().Format("060102_150405")

	if len(successLines) > 0 {
		SaveSuccessLog("RETRY ORDER EXECUTION:", strings.Join(successLines, "\n"), retryNowStr)
	}

	if len(remainingFailed) == 0 {
		// All retried orders succeeded! Remove temp JSON file.
		_ = os.Remove(jsonPath)
		fmt.Printf("\nAll failed orders successfully placed! Removed temporary retry file %s\n", jsonPath)
	} else {
		// Update JSON payload with remaining failed specs
		payload.FailedOrders = remainingFailed
		payload.Timestamp = retryNowStr
		bytes, _ := json.MarshalIndent(payload, "", "  ")
		_ = os.WriteFile(jsonPath, bytes, 0644)
		fmt.Printf("\nRetry complete. %d order(s) still remaining in %s\n", len(remainingFailed), jsonPath)
	}
}
