package broker

// MockBroker implements Broker with static sample data for dry-run mode.
type MockBroker struct{}

func (m *MockBroker) IsMock() bool { return true }

func (m *MockBroker) GetQuotes(keys []string) (map[string]float64, error) {
	prices := map[string]float64{
		"NSE:RELIANCE": 2450.0,
		"NSE:TCS":      3200.0,
		"NSE:INFY":     1420.0,
	}
	for _, k := range keys {
		if _, ok := prices[k]; !ok {
			prices[k] = 500.0
		}
	}
	return prices, nil
}

func (m *MockBroker) GetHoldings() ([]Holding, error) {
	rawHoldings := []Holding{
		{TradingSymbol: "SAMHI", Exchange: "NSE", Quantity: 100, AveragePrice: 182.48, LastPrice: 172.69, PnL: -979.0},
		{TradingSymbol: "WABAG", Exchange: "NSE", Quantity: 50, AveragePrice: 1212.64, LastPrice: 2117.70, PnL: 45252.80},
		{TradingSymbol: "WAAREEENER", Exchange: "NSE", Quantity: 10, AveragePrice: 2535.08, LastPrice: 2863.40, PnL: 3283.20},
		{TradingSymbol: "ACE", Exchange: "NSE", T1Quantity: 10, AveragePrice: 972.40, LastPrice: 976.60, PnL: 42.0},
		{TradingSymbol: "PARACABLES", Exchange: "NSE", Quantity: 11, AveragePrice: 33.96, LastPrice: 67.01, PnL: 363.53},
		{TradingSymbol: "LT", Exchange: "BSE", Quantity: 2, AveragePrice: 4115.0, LastPrice: 3887.30, PnL: -455.40},
	}

	mockPositions := []struct {
		Tradingsymbol string
		Exchange      string
		Product       string
		Quantity      int
		AveragePrice  float64
		LastPrice     float64
		PnL           float64
	}{
		{Tradingsymbol: "SWSOLAR", Exchange: "BSE", Product: "CNC", Quantity: 9, AveragePrice: 234.51, LastPrice: 236.55, PnL: 18.36},
		{Tradingsymbol: "ADVAIT", Exchange: "BSE", Product: "CNC", Quantity: 1, AveragePrice: 2119.0, LastPrice: 2216.0, PnL: 97.0},
		{Tradingsymbol: "INOXINDIA", Exchange: "NSE", Product: "CNC", Quantity: 1, AveragePrice: 1906.90, LastPrice: 1990.50, PnL: 83.60},
	}

	for _, pos := range mockPositions {
		if pos.Product != "CNC" || pos.Quantity <= 0 {
			continue
		}
		found := false
		for i := range rawHoldings {
			if rawHoldings[i].TradingSymbol == pos.Tradingsymbol {
				existingQty := rawHoldings[i].Quantity + rawHoldings[i].T1Quantity + rawHoldings[i].T2Quantity
				newQty := existingQty + pos.Quantity
				if newQty > 0 {
					rawHoldings[i].AveragePrice = (float64(existingQty)*rawHoldings[i].AveragePrice + float64(pos.Quantity)*pos.AveragePrice) / float64(newQty)
				}
				rawHoldings[i].T2Quantity += pos.Quantity
				rawHoldings[i].PnL += pos.PnL
				if pos.Exchange == "NSE" {
					rawHoldings[i].Exchange = "NSE"
				}
				found = true
				break
			}
		}
		if !found {
			rawHoldings = append(rawHoldings, Holding{
				TradingSymbol: pos.Tradingsymbol,
				Exchange:      pos.Exchange,
				T2Quantity:    pos.Quantity,
				AveragePrice:  pos.AveragePrice,
				LastPrice:     pos.LastPrice,
				PnL:           pos.PnL,
			})
		}
	}

	for i := range rawHoldings {
		totalQty := rawHoldings[i].Quantity + rawHoldings[i].T1Quantity + rawHoldings[i].T2Quantity
		if invested := float64(totalQty) * rawHoldings[i].AveragePrice; invested > 0 {
			rawHoldings[i].PnLPct = (rawHoldings[i].PnL / invested) * 100.0
		}
	}
	return rawHoldings, nil
}

func (m *MockBroker) PlaceOrder(_ string, _ Order) (OrderResult, error) {
	return OrderResult{OrderID: "MOCK-DRY-RUN"}, nil
}

func (m *MockBroker) PlaceGTT(_ Order) (OrderResult, error) {
	return OrderResult{TriggerID: 99999}, nil
}

var _ Broker = (*MockBroker)(nil)
