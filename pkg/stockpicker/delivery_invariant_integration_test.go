//go:build integration

// These tests exercise the live NSE delivery-data fetch path (via the nselib
// Python subprocess) and therefore require network access and a working nselib
// install. They are gated behind the `integration` build tag so the default
// `make test` stays hermetic and offline-safe; run them via `make test-integration`.
package stockpicker

import (
	"testing"
	"time"

	"github.com/raghavkgarg/mycase/pkg/yfinance"
)

func TestEnrichDeliveryHistory_LiveCUPID(t *testing.T) {
	funds := map[string]yfinance.Fundamentals{
		"NSE:CUPID": {Sector: "Consumer Defensive"},
	}
	enrichDeliveryHistory(t.Context(), []string{"NSE:CUPID"}, funds)
	f := funds["NSE:CUPID"]
	t.Logf("Enriched CUPID Delivery History len: %d, DeliveryPct: %.2f", len(f.DeliveryHistory), f.DeliveryPct)
	if len(f.DeliveryHistory) < 25 {
		t.Fatalf("expected >= 25 records, got %d", len(f.DeliveryHistory))
	}
	delta, avg5, avg20, err := yfinance.GetDeliveryDelta(f.DeliveryHistory, time.Now(), 1)
	if err != nil {
		t.Fatalf("GetDeliveryDelta error: %v", err)
	}
	t.Logf("Enriched CUPID Delta: %+0.4f (5D: %.2f%%, 20D: %.2f%%)", delta, avg5*100, avg20*100)
}

func TestEnrichDeliveryHistory_MultipleTickers(t *testing.T) {
	funds := map[string]yfinance.Fundamentals{
		"NSE:CUPID":   {Sector: "Consumer Defensive"},
		"NSE:IPCALAB": {Sector: "Healthcare"},
	}
	enrichDeliveryHistory(t.Context(), []string{"NSE:CUPID", "NSE:IPCALAB"}, funds)
	for sym, f := range funds {
		t.Logf("%s: DeliveryHistory len=%d", sym, len(f.DeliveryHistory))
		if len(f.DeliveryHistory) < 25 {
			t.Errorf("%s: expected >= 25 records, got %d", sym, len(f.DeliveryHistory))
		}
	}
}
