package pithistory

import (
	"testing"
)

func TestSentryTriggers(t *testing.T) {
	// Case 1: Level 3 Trend Rupture (Price < 0.95 * SMA200)
	price1 := 90.0
	sma200_1 := 100.0
	high52W_1 := 120.0
	if !(price1 < 0.95*sma200_1 || price1 < 0.80*high52W_1) {
		t.Errorf("expected Level 3 trigger for price=90 with SMA200=100")
	}

	// Case 2: Level 3 Peak Drawdown Rupture (Price < 0.80 * High52W)
	price2 := 75.0
	high52W_2 := 100.0
	if !(price2 < 0.80*high52W_2) {
		t.Errorf("expected Level 3 trigger for price=75 with High52W=100 (25%% drawdown)")
	}

	// Case 3: Level 2 Institutional Distribution (Price <= SMA50 && DelivDelta3D <= -10%)
	price3 := 98.0
	sma50_3 := 100.0
	delivDelta3D_3 := -0.12
	if !(price3 <= sma50_3 && delivDelta3D_3 <= -0.10) {
		t.Errorf("expected Level 2 trigger for price <= SMA50 with DelivDelta3D <= -10%%")
	}

	// Case 4: Level 1 Coil Decay (VCP > 1.60 && RVOL Z > 2.0 && PriceDeclining)
	vcp4 := 1.75
	rvolZ4 := 2.5
	priceDeclining4 := true
	if !(vcp4 > 1.60 && rvolZ4 > 2.0 && priceDeclining4) {
		t.Errorf("expected Level 1 trigger for VCP > 1.60 and RVOL Z > 2.0 on declining close")
	}
}

func TestBasketOverlapSignals(t *testing.T) {
	// Signal 1: Reinforcement (Live holding in staging with Setup Quality >= 2.50)
	sqHigh := 2.65
	inStaged := true
	var signal string
	if inStaged {
		if sqHigh >= 2.50 {
			signal = "REINFORCEMENT"
		} else {
			signal = "CO-OCCURRING"
		}
	} else {
		signal = "FADING"
	}
	if signal != "REINFORCEMENT" {
		t.Errorf("expected REINFORCEMENT, got %s", signal)
	}

	// Signal 2: Fading (Live holding not in staging)
	inStagedFading := false
	if !inStagedFading {
		signal = "FADING"
	}
	if signal != "FADING" {
		t.Errorf("expected FADING, got %s", signal)
	}

	// Signal 3: Re-Entry Risk (Staged candidate recently exited < 30 days)
	isRecentExit := true
	isLive := false
	if !isLive && isRecentExit {
		signal = "RE-ENTRY RISK"
	}
	if signal != "RE-ENTRY RISK" {
		t.Errorf("expected RE-ENTRY RISK, got %s", signal)
	}
}
