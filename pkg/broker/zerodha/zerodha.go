package zerodha

import (
	"fmt"
	"math"

	kiteconnect "github.com/zerodha/gokiteconnect/v4"

	"github.com/raghavkgarg/mycase/pkg/broker"
	"github.com/raghavkgarg/mycase/pkg/config"
)

// ZerodhaBroker is a live Kite Connect implementation of broker.Broker.
type ZerodhaBroker struct {
	client *kiteconnect.Client
}

// New returns a Broker. Falls back to MockBroker when liveMode is false or
// credentials are absent/placeholder.
func New(liveMode bool, configPath string) broker.Broker {
	if !liveMode {
		return &broker.MockBroker{}
	}
	cfg, err := config.LoadConfig(configPath)
	if err != nil || cfg.APIKey == "" || cfg.AccessToken == "" ||
		cfg.APIKey == "your_api_key" || cfg.AccessToken == "your_access_token" {
		fmt.Printf("Note: Could not load valid credentials from %s, running in Mock mode.\n", configPath)
		return &broker.MockBroker{}
	}
	c := kiteconnect.New(cfg.APIKey)
	c.SetAccessToken(cfg.AccessToken)
	return &ZerodhaBroker{client: c}
}

func (z *ZerodhaBroker) IsMock() bool { return false }

func (z *ZerodhaBroker) GetQuotes(keys []string) (map[string]float64, error) {
	kiteQuote, err := z.client.GetQuote(keys...)
	if err != nil {
		return nil, fmt.Errorf("zerodha GetQuote: %w", err)
	}
	result := make(map[string]float64, len(keys))
	for _, k := range keys {
		result[k] = kiteQuote[k].LastPrice
	}
	return result, nil
}

// GetHoldings fetches holdings and CNC positions from Kite, merges them, and
// calculates PnLPct for each entry.
func (z *ZerodhaBroker) GetHoldings() ([]broker.Holding, error) {
	kiteHoldings, err := z.client.GetHoldings()
	if err != nil {
		return nil, fmt.Errorf("failed to fetch holdings from Zerodha Kite: %w", err)
	}

	var rawHoldings []broker.Holding
	for _, kh := range kiteHoldings {
		rawHoldings = append(rawHoldings, broker.Holding{
			TradingSymbol: kh.Tradingsymbol,
			Exchange:      kh.Exchange,
			Quantity:      kh.Quantity,
			T1Quantity:    kh.T1Quantity,
			AveragePrice:  kh.AveragePrice,
			LastPrice:     kh.LastPrice,
			PnL:           kh.PnL,
		})
	}

	kitePositions, err := z.client.GetPositions()
	if err != nil {
		fmt.Printf("Warning: Failed to fetch positions from Zerodha Kite: %v\n", err)
	} else {
		for _, pos := range kitePositions.Net {
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
				rawHoldings = append(rawHoldings, broker.Holding{
					TradingSymbol: pos.Tradingsymbol,
					Exchange:      pos.Exchange,
					T2Quantity:    pos.Quantity,
					AveragePrice:  pos.AveragePrice,
					LastPrice:     pos.LastPrice,
					PnL:           pos.PnL,
				})
			}
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

func (z *ZerodhaBroker) PlaceOrder(variety string, order broker.Order) (broker.OrderResult, error) {
	execPrice := math.Round(order.Ltp*10.0) / 10.0
	if order.Price != 0 {
		execPrice = order.Price
	}
	resp, err := z.client.PlaceOrder(variety, kiteconnect.OrderParams{
		Exchange:        order.Exchange,
		Tradingsymbol:   order.TradingSymbol,
		Product:         order.Product,
		OrderType:       "LIMIT",
		TransactionType: order.TransactionType,
		Quantity:        order.Quantity,
		Price:           execPrice,
	})
	if err != nil {
		return broker.OrderResult{}, err
	}
	return broker.OrderResult{OrderID: resp.OrderID}, nil
}

func (z *ZerodhaBroker) PlaceGTT(order broker.Order) (broker.OrderResult, error) {
	resp, err := z.client.PlaceGTT(kiteconnect.GTTParams{
		Tradingsymbol:   order.TradingSymbol,
		Exchange:        order.Exchange,
		LastPrice:       order.Ltp,
		TransactionType: order.TransactionType,
		Product:         order.Product,
		Trigger: &kiteconnect.GTTSingleLegTrigger{
			TriggerParams: kiteconnect.TriggerParams{
				TriggerValue: order.TriggerPrice,
				LimitPrice:   order.Price,
				Quantity:     float64(order.Quantity),
			},
		},
	})
	if err != nil {
		return broker.OrderResult{}, err
	}
	return broker.OrderResult{TriggerID: resp.TriggerID}, nil
}

var _ broker.Broker = (*ZerodhaBroker)(nil)
