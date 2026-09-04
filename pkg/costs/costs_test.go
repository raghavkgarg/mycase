package costs

import (
	"math"
	"testing"
	"time"
)

// ---- CostModel.Calculate tests ----

func TestCalculate_BuyComponents(t *testing.T) {
	// 100 shares × ₹500 = ₹50,000 buy
	// STT     = 0.1%  × 50000 = 50.00
	// Stamp   = 0.015% × 50000 =  7.50
	// SEBI    = 0.0001% × 50000 =  0.05
	// DP      = 0 (buy)
	// Brokerage = 0
	// Total   = 57.55
	bd := DefaultZerodha.Calculate("BUY", 100, 500)
	assertClose(t, "TradeValue", bd.TradeValue, 50000)
	assertClose(t, "STT", bd.STT, 50.00)
	assertClose(t, "StampDuty", bd.StampDuty, 7.50)
	assertClose(t, "SEBI", bd.SEBI, 0.05)
	assertClose(t, "DP", bd.DP, 0)
	assertClose(t, "Total", bd.Total, 57.55)
	assertClose(t, "CostRatio", bd.CostRatio, 57.55/50000)
}

func TestCalculate_SellComponents(t *testing.T) {
	// 50 shares × ₹1000 = ₹50,000 sell
	// STT   = 0.1% × 50000 = 50.00
	// Stamp = 0 (sell)
	// SEBI  = 0.0001% × 50000 = 0.05
	// DP    = 15.05 (flat per ISIN)
	// Total = 65.10
	bd := DefaultZerodha.Calculate("SELL", 50, 1000)
	assertClose(t, "TradeValue", bd.TradeValue, 50000)
	assertClose(t, "STT", bd.STT, 50.00)
	assertClose(t, "StampDuty", bd.StampDuty, 0)
	assertClose(t, "DP", bd.DP, DPChargePerISIN)
	assertClose(t, "SEBI", bd.SEBI, 0.05)
	assertClose(t, "Total", bd.Total, 50.00+DPChargePerISIN+0.05)
}

func TestCalculate_ZeroQtyReturnsEmpty(t *testing.T) {
	bd := DefaultZerodha.Calculate("BUY", 0, 1000)
	if bd.Total != 0 || bd.TradeValue != 0 {
		t.Errorf("zero qty: want empty breakdown, got total=%.4f", bd.Total)
	}
}

func TestCalculate_ZeroPriceReturnsEmpty(t *testing.T) {
	bd := DefaultZerodha.Calculate("SELL", 10, 0)
	if bd.Total != 0 {
		t.Errorf("zero price: want empty breakdown, got total=%.4f", bd.Total)
	}
}

func TestCalculate_CustomBrokerage(t *testing.T) {
	// Flat ₹20 brokerage per order
	m := CostModel{Brokerage: 20}
	bd := m.Calculate("BUY", 1, 100)
	if bd.Brokerage != 20 {
		t.Errorf("brokerage = %.2f, want 20", bd.Brokerage)
	}
}

// Micro-transaction: 1 share of a ₹50 stock on sell
// DP alone (₹15.05) dominates; cost ratio should be > 0.5%.
func TestCalculate_MicroTransactionHighRatio(t *testing.T) {
	bd := DefaultZerodha.Calculate("SELL", 1, 50)
	// DP=15.05, STT=0.05, SEBI≈0, Total≈15.10, ratio ≈ 30.20%
	if bd.CostRatio < 0.005 {
		t.Errorf("expected high cost ratio for micro-tx, got %.4f", bd.CostRatio)
	}
}

func TestCalculate_CostRatioConsistency(t *testing.T) {
	bd := DefaultZerodha.Calculate("BUY", 200, 300)
	want := bd.Total / bd.TradeValue
	if math.Abs(bd.CostRatio-want) > 1e-9 {
		t.Errorf("CostRatio %.9f != Total/TradeValue %.9f", bd.CostRatio, want)
	}
}

// ---- ClassifySell tests ----

func TestClassifySell_UnknownPurchaseDate(t *testing.T) {
	w := ClassifySell("RELIANCE", 10, 2500, 2000, time.Time{})
	if w.Class != TaxUnknown {
		t.Errorf("class = %v, want TaxUnknown", w.Class)
	}
	if w.HoldingDays != -1 {
		t.Errorf("HoldingDays = %d, want -1", w.HoldingDays)
	}
	// gain = (2500-2000)*10 = 5000
	assertClose(t, "EstimatedGain", w.EstimatedGain, 5000)
	if w.Note == "" {
		t.Error("Note should not be empty for unknown class")
	}
}

func TestClassifySell_STCG(t *testing.T) {
	// Bought 6 months ago → STCG
	purchaseDate := time.Now().AddDate(0, -6, 0)
	w := ClassifySell("TCS", 5, 4000, 3500, purchaseDate)
	if w.Class != TaxSTCG {
		t.Errorf("class = %v, want TaxSTCG", w.Class)
	}
	if w.HoldingDays >= STCGThresholdDays {
		t.Errorf("HoldingDays = %d, expected < %d", w.HoldingDays, STCGThresholdDays)
	}
	// gain = (4000-3500)*5 = 2500; tax = 2500 * 20% = 500
	assertClose(t, "EstimatedGain", w.EstimatedGain, 2500)
	assertClose(t, "EstimatedTax", w.EstimatedTax, 500)
}

func TestClassifySell_LTCG_AboveExemption(t *testing.T) {
	// Bought 2 years ago → LTCG; gain = ₹2,00,000 → taxable = 200000-125000 = 75000, tax = 75000*12.5% = 9375
	purchaseDate := time.Now().AddDate(-2, 0, 0)
	w := ClassifySell("INFY", 100, 2000, 0, purchaseDate) // avgCost=0 → gain unknown
	if w.Class != TaxLTCG {
		t.Errorf("class = %v, want TaxLTCG", w.Class)
	}
	if w.HoldingDays < STCGThresholdDays {
		t.Errorf("HoldingDays = %d, expected >= %d", w.HoldingDays, STCGThresholdDays)
	}
	// avgCost=0 → gain=0, tax=0
	if w.EstimatedGain != 0 || w.EstimatedTax != 0 {
		t.Errorf("gain/tax should be 0 when avgCost=0: gain=%.0f tax=%.0f", w.EstimatedGain, w.EstimatedTax)
	}
}

func TestClassifySell_LTCG_TaxCalculation(t *testing.T) {
	// Bought > 365 days ago; gain = (500-200)*400 = 120000
	// Taxable LTCG = 120000 - 125000 = negative → below exemption → no tax
	purchaseDate := time.Now().AddDate(-2, 0, 0)
	w := ClassifySell("WABAG", 400, 500, 200, purchaseDate)
	if w.Class != TaxLTCG {
		t.Errorf("class = %v, want TaxLTCG", w.Class)
	}
	assertClose(t, "EstimatedGain", w.EstimatedGain, 120000)
	// gain < exemption → tax = 0
	if w.EstimatedTax != 0 {
		t.Errorf("tax = %.2f, want 0 (below LTCG exemption)", w.EstimatedTax)
	}
}

func TestClassifySell_LTCG_TaxAboveExemption(t *testing.T) {
	// gain = (1000-500)*500 = 250000; taxable = 250000-125000 = 125000; tax = 125000*12.5% = 15625
	purchaseDate := time.Now().AddDate(-2, 0, 0)
	w := ClassifySell("SWSOLAR", 500, 1000, 500, purchaseDate)
	assertClose(t, "EstimatedGain", w.EstimatedGain, 250000)
	assertClose(t, "EstimatedTax", w.EstimatedTax, 15625)
}

func TestClassifySell_TaxClassString(t *testing.T) {
	cases := []struct {
		c    TaxClass
		want string
	}{
		{TaxSTCG, "STCG"},
		{TaxLTCG, "LTCG"},
		{TaxUnknown, "UNKNOWN"},
	}
	for _, tc := range cases {
		if s := tc.c.String(); s != tc.want {
			t.Errorf("TaxClass(%d).String() = %q, want %q", tc.c, s, tc.want)
		}
	}
}

// ---- helper ----

func assertClose(t *testing.T, name string, got, want float64) {
	t.Helper()
	if math.Abs(got-want) > 1e-9 {
		t.Errorf("%s = %.6f, want %.6f", name, got, want)
	}
}
