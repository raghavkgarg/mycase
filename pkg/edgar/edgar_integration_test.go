//go:build integration

// Live EDGAR tests. These hit the real data.sec.gov / www.sec.gov endpoints and
// therefore require network access; they are gated behind the `integration`
// build tag so the default `make test` stays hermetic and offline-safe. Run via
// `make test-integration`. Set MYCASE_EDGAR_USER_AGENT to a real contact per
// SEC fair-access policy before running.
package edgar

import (
	"context"
	"os"
	"testing"
	"time"
)

func liveClient(t *testing.T) *Client {
	t.Helper()
	ua := os.Getenv("MYCASE_EDGAR_USER_AGENT")
	if ua == "" {
		t.Skip("set MYCASE_EDGAR_USER_AGENT to run live EDGAR tests")
	}
	cl, err := NewClient(ua, nil)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	return cl
}

func TestLiveCIKAndFacts_AAPL(t *testing.T) {
	cl := liveClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cik, ok, err := cl.CIK(ctx, "US:AAPL")
	if err != nil || !ok {
		t.Fatalf("CIK(AAPL): ok=%v err=%v", ok, err)
	}
	if cik != "0000320193" {
		t.Errorf("AAPL CIK = %q, want 0000320193", cik)
	}

	res, err := cl.FetchFundamentals(ctx, []string{"US:AAPL"})
	if err != nil {
		t.Fatalf("FetchFundamentals: %v", err)
	}
	f, ok := res["US:AAPL"]
	if !ok {
		t.Fatal("AAPL missing from live result")
	}
	// Sanity: Apple has substantial operating cash flow and a multi-year
	// revenue series. We assert shape/sign, not exact figures (they drift).
	if f.OperatingCashflow <= 0 {
		t.Errorf("live AAPL OperatingCashflow = %v, want > 0", f.OperatingCashflow)
	}
	if len(f.AnnualRevenue) < 2 {
		t.Errorf("live AAPL AnnualRevenue has %d entries, want >= 2", len(f.AnnualRevenue))
	}
	t.Logf("AAPL OCF=%.0f FCF=%.0f revYears=%d", f.OperatingCashflow, f.FreeCashflow, len(f.AnnualRevenue))
}
