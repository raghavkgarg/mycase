package costs

// Transaction cost constants for Indian equity delivery.
// STT, Stamp Duty, and SEBI charges are set by regulation; DP charge is CDSL rate.
// Exchange transaction charges (NSE 0.00297%) are excluded per R6 scope.
const (
	STTBuyRate       = 0.001    // 0.1% of buy trade value
	STTSellRate      = 0.001    // 0.1% of sell trade value
	StampDutyBuyRate = 0.00015  // 0.015% of buy trade value (sell: nil)
	SEBIRate         = 0.000001 // 0.0001% of trade value (both sides)
	DPChargePerISIN  = 15.93    // ₹15.93 flat per ISIN per sell day (CDSL)
)

// DefaultZerodha is a zero-brokerage CostModel for Zerodha equity delivery.
var DefaultZerodha = CostModel{Brokerage: 0}

// CostModel holds configurable per-order parameters.
type CostModel struct {
	Brokerage float64 // per-order flat brokerage in ₹; Zerodha CNC = 0
}

// CostBreakdown holds the per-component breakdown for a single order.
type CostBreakdown struct {
	STT        float64
	StampDuty  float64
	DP         float64
	Brokerage  float64
	SEBI       float64
	Total      float64
	TradeValue float64
	CostRatio  float64 // Total / TradeValue; 0 when TradeValue == 0
}

// Calculate returns the cost breakdown for an equity delivery order.
// txType must be "BUY" or "SELL" (case-insensitive). qty × price = trade value.
func (m CostModel) Calculate(txType string, qty int, price float64) CostBreakdown {
	tradeValue := float64(qty) * price
	if tradeValue <= 0 {
		return CostBreakdown{}
	}

	var bd CostBreakdown
	bd.TradeValue = tradeValue
	bd.Brokerage = m.Brokerage
	bd.SEBI = SEBIRate * tradeValue

	isSell := len(txType) > 0 && (txType[0] == 'S' || txType[0] == 's')
	if isSell {
		bd.STT = STTSellRate * tradeValue
		bd.DP = DPChargePerISIN
	} else {
		bd.STT = STTBuyRate * tradeValue
		bd.StampDuty = StampDutyBuyRate * tradeValue
	}

	bd.Total = bd.STT + bd.StampDuty + bd.DP + bd.Brokerage + bd.SEBI
	bd.CostRatio = bd.Total / tradeValue
	return bd
}
