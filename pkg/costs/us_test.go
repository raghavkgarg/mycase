package costs

import (
	"math"
	"testing"
	"time"
)

func almostEqual(a, b, epsilon float64) bool {
	return math.Abs(a-b) < epsilon
}

func TestUSCostModelBuy(t *testing.T) {
	m := USCostModel{Commission: 0}
	bd := m.Calculate("BUY", 100, 185.00)

	if bd.TradeValue != 18500.00 {
		t.Errorf("TradeValue = %v, want 18500.00", bd.TradeValue)
	}
	// Buy has no SEC or TAF fees
	if bd.SECFee != 0 {
		t.Errorf("SECFee = %v, want 0 for BUY", bd.SECFee)
	}
	if bd.TAF != 0 {
		t.Errorf("TAF = %v, want 0 for BUY", bd.TAF)
	}
	if bd.Total != 0 {
		t.Errorf("Total = %v, want 0 for BUY with $0 commission", bd.Total)
	}
}

func TestUSCostModelSell(t *testing.T) {
	m := USCostModel{Commission: 0}
	bd := m.Calculate("SELL", 100, 185.00)

	// SEC fee: 0.000008 × $18500 = $0.148
	expectedSEC := 0.000008 * 18500.0
	if !almostEqual(bd.SECFee, expectedSEC, 0.001) {
		t.Errorf("SECFee = %v, want ~%v", bd.SECFee, expectedSEC)
	}

	// TAF: $0.000166 × 100 shares = $0.0166
	expectedTAF := 0.000166 * 100
	if !almostEqual(bd.TAF, expectedTAF, 0.0001) {
		t.Errorf("TAF = %v, want ~%v", bd.TAF, expectedTAF)
	}

	// Total should be tiny — under $0.20 for a $18,500 trade
	if bd.Total > 0.20 {
		t.Errorf("Total = $%.4f, expected under $0.20 for $18,500 sell", bd.Total)
	}

	// Cost ratio should be negligible
	if bd.CostRatio > 0.00002 {
		t.Errorf("CostRatio = %v, expected negligible for US equity", bd.CostRatio)
	}
}

func TestUSCostModelTAFCap(t *testing.T) {
	// Large qty: TAF per share ($0.000166) vs cap ($0.01/share)
	// At 100 shares: 0.000166 * 100 = 0.0166, cap = 0.01 * 100 = 1.00
	// TAF is below cap, so no capping
	m := DefaultUS
	bd := m.Calculate("SELL", 100, 500.0)
	expectedTAF := 0.000166 * 100
	if !almostEqual(bd.TAF, expectedTAF, 0.0001) {
		t.Errorf("TAF = %v, want %v (no cap hit)", bd.TAF, expectedTAF)
	}
}

func TestUSCostModelZeroQty(t *testing.T) {
	m := DefaultUS
	bd := m.Calculate("BUY", 0, 100.0)
	if bd.TradeValue != 0 {
		t.Errorf("zero qty should produce zero TradeValue")
	}
	if bd.Total != 0 {
		t.Errorf("zero qty should produce zero Total")
	}
}

func TestUSCostModelWithCommission(t *testing.T) {
	// Hypothetical broker with $4.95 commission
	m := USCostModel{Commission: 4.95}
	bd := m.Calculate("BUY", 50, 200.0)
	if bd.Commission != 4.95 {
		t.Errorf("Commission = %v, want 4.95", bd.Commission)
	}
	if bd.Total != 4.95 {
		t.Errorf("Total = %v, want 4.95 (commission only for BUY)", bd.Total)
	}
}

func TestClassifyUSSellShortTerm(t *testing.T) {
	purchaseDate := time.Now().AddDate(0, -6, 0) // 6 months ago
	w := ClassifyUSSell("AAPL", 10, 185.0, 150.0, purchaseDate, false)

	if w.Class != USTaxShortTerm {
		t.Errorf("Class = %v, want Short-Term", w.Class)
	}
	// Gain: (185-150) × 10 = $350
	expectedGain := 350.0
	if !almostEqual(w.EstimatedGain, expectedGain, 0.01) {
		t.Errorf("EstimatedGain = %v, want %v", w.EstimatedGain, expectedGain)
	}
	// Tax: 37% × $350 = $129.50
	expectedTax := 0.37 * 350.0
	if !almostEqual(w.EstimatedTax, expectedTax, 0.01) {
		t.Errorf("EstimatedTax = %v, want %v", w.EstimatedTax, expectedTax)
	}
	if w.WashSaleRisk {
		t.Error("WashSaleRisk should be false")
	}
}

func TestClassifyUSSellLongTerm(t *testing.T) {
	purchaseDate := time.Now().AddDate(-2, 0, 0) // 2 years ago
	w := ClassifyUSSell("MSFT", 20, 380.0, 250.0, purchaseDate, false)

	if w.Class != USTaxLongTerm {
		t.Errorf("Class = %v, want Long-Term", w.Class)
	}
	// Gain: (380-250) × 20 = $2600
	expectedGain := 2600.0
	if !almostEqual(w.EstimatedGain, expectedGain, 0.01) {
		t.Errorf("EstimatedGain = %v, want %v", w.EstimatedGain, expectedGain)
	}
	// Tax: 20% × $2600 = $520
	expectedTax := 0.20 * 2600.0
	if !almostEqual(w.EstimatedTax, expectedTax, 0.01) {
		t.Errorf("EstimatedTax = %v, want %v", w.EstimatedTax, expectedTax)
	}
}

func TestClassifyUSSellWashSale(t *testing.T) {
	purchaseDate := time.Now().AddDate(0, -3, 0) // 3 months ago
	// Selling at a loss with recent buy = wash sale risk
	w := ClassifyUSSell("GOOG", 5, 140.0, 170.0, purchaseDate, true)

	if w.Class != USTaxShortTerm {
		t.Errorf("Class = %v, want Short-Term", w.Class)
	}
	// Loss: (140-170) × 5 = -$150
	expectedGain := -150.0
	if !almostEqual(w.EstimatedGain, expectedGain, 0.01) {
		t.Errorf("EstimatedGain = %v, want %v", w.EstimatedGain, expectedGain)
	}
	// No tax on a loss
	if w.EstimatedTax != 0 {
		t.Errorf("EstimatedTax = %v, want 0 for a loss", w.EstimatedTax)
	}
	if !w.WashSaleRisk {
		t.Error("WashSaleRisk should be true")
	}
	if w.Note == "" {
		t.Error("Note should contain wash sale warning")
	}
}

func TestClassifyUSSellUnknownDate(t *testing.T) {
	w := ClassifyUSSell("NVDA", 10, 800.0, 500.0, time.Time{}, false)

	if w.Class != USTaxUnknown {
		t.Errorf("Class = %v, want Unknown", w.Class)
	}
	if w.HoldingDays != -1 {
		t.Errorf("HoldingDays = %d, want -1", w.HoldingDays)
	}
	// Should still estimate gain
	expectedGain := 3000.0
	if !almostEqual(w.EstimatedGain, expectedGain, 0.01) {
		t.Errorf("EstimatedGain = %v, want %v", w.EstimatedGain, expectedGain)
	}
}
