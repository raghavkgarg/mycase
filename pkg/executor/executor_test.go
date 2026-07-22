package executor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/raghavkgarg/mycase/pkg/broker"
)

func TestSaveSuccessAndErrorLog(t *testing.T) {
	tmpNow := "999999_999999"

	// Cleanup test artifacts
	defer func() {
		_ = os.Remove(filepath.Join("Order", "Order_"+tmpNow+".txt"))
		_ = os.Remove(filepath.Join("Error", "Order_"+tmpNow+".txt"))
		_ = os.Remove(filepath.Join("Error", "Order_"+tmpNow+".json"))
	}()

	SaveSuccessLog("SNAPSHOT TEST", "Placed 1 order", tmpNow)
	if _, err := os.Stat(filepath.Join("Order", "Order_" + tmpNow + ".txt")); os.IsNotExist(err) {
		t.Fatalf("expected Order log to exist")
	}

	failedSpecs := []FailedOrderSpec{
		{TradingSymbol: "TEST", Quantity: 10, ErrorReason: "rate limit"},
	}
	jsonPath := SaveErrorLog("SNAPSHOT TEST", "Failed 1 order", failedSpecs, tmpNow)
	if jsonPath == "" {
		t.Fatalf("expected jsonPath to be returned")
	}
	if _, err := os.Stat(jsonPath); os.IsNotExist(err) {
		t.Fatalf("expected Error JSON payload to exist")
	}

	latest, err := FindLatestErrorPayload()
	if err != nil {
		t.Fatalf("FindLatestErrorPayload failed: %v", err)
	}
	if !strings.HasSuffix(latest, ".json") {
		t.Fatalf("expected latest file to end with .json, got %s", latest)
	}
}

func TestExecuteRetryPayloadMock(t *testing.T) {
	tmpNow := "999999_888888"
	jsonPath := filepath.Join("Error", "Order_"+tmpNow+".json")

	_ = os.MkdirAll("Error", 0755)
	defer func() {
		_ = os.Remove(jsonPath)
		_ = os.Remove(filepath.Join("Error", "Order_"+tmpNow+".txt"))
	}()

	failedSpecs := []FailedOrderSpec{
		{TradingSymbol: "RELIANCE", Exchange: "NSE", TransactionType: "BUY", Quantity: 5, Price: 2400.0},
	}
	SaveErrorLog("SNAPSHOT TEST", "Failed order", failedSpecs, tmpNow)

	mb := &broker.MockBroker{}
	ExecuteRetryPayload(jsonPath, mb, nil)

	// Since mock broker succeeds, the JSON retry file should be deleted
	if _, err := os.Stat(jsonPath); !os.IsNotExist(err) {
		t.Fatalf("expected JSON retry file to be deleted after 100%% retry success")
	}
}
