package portfolio

import (
	"fmt"

	kiteconnect "github.com/zerodha/gokiteconnect/v4"
)

type Holding struct {
	TradingSymbol string
	Exchange      string
	Quantity      int
	T1Quantity    int
	T2Quantity    int
	AveragePrice  float64
	LastPrice     float64
	PnL           float64
	PnLPct        float64
}

// FetchAndMergeHoldings fetches holdings and positions from Zerodha Kite (or mocks them) and merges today's CNC positions into T+2 holdings.
func FetchAndMergeHoldings(client *kiteconnect.Client, isMock bool) ([]Holding, error) {
	var rawHoldings []Holding

	if isMock {
		// Mock holdings
		rawHoldings = []Holding{
			{TradingSymbol: "SAMHI", Exchange: "NSE", Quantity: 100, T1Quantity: 0, T2Quantity: 0, AveragePrice: 182.48, LastPrice: 172.69, PnL: -979.0},
			{TradingSymbol: "WABAG", Exchange: "NSE", Quantity: 50, T1Quantity: 0, T2Quantity: 0, AveragePrice: 1212.64, LastPrice: 2117.70, PnL: 45252.80},
			{TradingSymbol: "WAAREEENER", Exchange: "NSE", Quantity: 10, T1Quantity: 0, T2Quantity: 0, AveragePrice: 2535.08, LastPrice: 2863.40, PnL: 3283.20},
			{TradingSymbol: "ACE", Exchange: "NSE", Quantity: 0, T1Quantity: 10, T2Quantity: 0, AveragePrice: 972.40, LastPrice: 976.60, PnL: 42.0},
			{TradingSymbol: "PARACABLES", Exchange: "NSE", Quantity: 11, T1Quantity: 0, T2Quantity: 0, AveragePrice: 33.96, LastPrice: 67.01, PnL: 363.53},
			{TradingSymbol: "LT", Exchange: "BSE", Quantity: 2, T1Quantity: 0, T2Quantity: 0, AveragePrice: 4115.0, LastPrice: 3887.30, PnL: -455.40},
		}

		// Mock CNC positions bought today
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
			if pos.Product == "CNC" && pos.Quantity > 0 {
				found := false
				for i := range rawHoldings {
					if rawHoldings[i].TradingSymbol == pos.Tradingsymbol {
						existingQty := rawHoldings[i].Quantity + rawHoldings[i].T1Quantity + rawHoldings[i].T2Quantity
						existingInvested := float64(existingQty) * rawHoldings[i].AveragePrice
						posInvested := float64(pos.Quantity) * pos.AveragePrice
						newQty := existingQty + pos.Quantity
						if newQty > 0 {
							rawHoldings[i].AveragePrice = (existingInvested + posInvested) / float64(newQty)
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
						Quantity:      0,
						T1Quantity:    0,
						T2Quantity:    pos.Quantity,
						AveragePrice:  pos.AveragePrice,
						LastPrice:     pos.LastPrice,
						PnL:           pos.PnL,
					})
				}
			}
		}
	} else {
		// Live holdings from Zerodha Kite Connect
		kiteHoldings, err := client.GetHoldings()
		if err != nil {
			return nil, fmt.Errorf("failed to fetch holdings from Zerodha Kite: %w", err)
		}

		for _, kh := range kiteHoldings {
			rawHoldings = append(rawHoldings, Holding{
				TradingSymbol: kh.Tradingsymbol,
				Exchange:      kh.Exchange,
				Quantity:      kh.Quantity,
				T1Quantity:    kh.T1Quantity,
				T2Quantity:    0,
				AveragePrice:  kh.AveragePrice,
				LastPrice:     kh.LastPrice,
				PnL:           kh.PnL,
			})
		}

		// Live positions from Zerodha Kite Connect
		kitePositions, err := client.GetPositions()
		if err != nil {
			fmt.Printf("Warning: Failed to fetch positions from Zerodha Kite: %v\n", err)
		} else {
			for _, pos := range kitePositions.Net {
				if pos.Product == "CNC" && pos.Quantity > 0 {
					found := false
					for i := range rawHoldings {
						if rawHoldings[i].TradingSymbol == pos.Tradingsymbol {
							existingQty := rawHoldings[i].Quantity + rawHoldings[i].T1Quantity + rawHoldings[i].T2Quantity
							existingInvested := float64(existingQty) * rawHoldings[i].AveragePrice
							posInvested := float64(pos.Quantity) * pos.AveragePrice
							newQty := existingQty + pos.Quantity
							if newQty > 0 {
								rawHoldings[i].AveragePrice = (existingInvested + posInvested) / float64(newQty)
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
							Quantity:      0,
							T1Quantity:    0,
							T2Quantity:    pos.Quantity,
							AveragePrice:  pos.AveragePrice,
							LastPrice:     pos.LastPrice,
							PnL:           pos.PnL,
						})
					}
				}
			}
		}
	}

	// Calculate PnL % for each holding
	for i := range rawHoldings {
		totalQty := rawHoldings[i].Quantity + rawHoldings[i].T1Quantity + rawHoldings[i].T2Quantity
		invested := float64(totalQty) * rawHoldings[i].AveragePrice
		if invested > 0 {
			rawHoldings[i].PnLPct = (rawHoldings[i].PnL / invested) * 100.0
		} else {
			rawHoldings[i].PnLPct = 0.0
		}
	}

	return rawHoldings, nil
}
