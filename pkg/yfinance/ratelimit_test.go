package yfinance

import (
	"context"
	"net/http"
	"testing"

	"golang.org/x/time/rate"
)

// TestRateLimiterDefaults verifies the package limiter is configured to the
// documented Yahoo pacing (10 req/s, burst 20).
func TestRateLimiterDefaults(t *testing.T) {
	// Capture and restore the package limiter so the test is isolated.
	orig := limiter
	t.Cleanup(func() { limiter = orig })

	if got := limiter.Burst(); got != yahooRateBurst {
		t.Errorf("limiter burst = %d, want %d", got, yahooRateBurst)
	}
	if got := float64(limiter.Limit()); got != yahooRatePerSecond {
		t.Errorf("limiter rate = %v/s, want %v", got, yahooRatePerSecond)
	}
}

// TestSetRateLimiter confirms the override hook swaps the limiter and ignores nil.
func TestSetRateLimiter(t *testing.T) {
	orig := limiter
	t.Cleanup(func() { limiter = orig })

	custom := rate.NewLimiter(rate.Limit(1), 1)
	SetRateLimiter(custom)
	if limiter != custom {
		t.Fatal("SetRateLimiter did not install the custom limiter")
	}

	SetRateLimiter(nil) // must be ignored
	if limiter != custom {
		t.Fatal("SetRateLimiter(nil) must not clear the limiter")
	}
}

// TestExecuteYFinanceRequestRespectsCancellation ensures that when the bucket is
// exhausted, a request with an already-cancelled context returns promptly with
// the context error rather than hanging or sending.
func TestExecuteYFinanceRequestRespectsCancellation(t *testing.T) {
	orig := limiter
	t.Cleanup(func() { limiter = orig })

	// Drain-proof limiter: zero rate, zero burst → Wait always blocks until ctx.
	limiter = rate.NewLimiter(0, 0)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", "http://example.invalid/x", nil)
	if err != nil {
		t.Fatalf("building request: %v", err)
	}
	client := newYFinanceHTTPClient(0, nil)

	if _, err := executeYFinanceRequest(client, req); err == nil {
		t.Fatal("expected error from cancelled context on exhausted limiter, got nil")
	}
}

// TestWaitRateAllowsUnderBurst verifies waitRate returns immediately while burst
// tokens remain.
func TestWaitRateAllowsUnderBurst(t *testing.T) {
	orig := limiter
	t.Cleanup(func() { limiter = orig })

	limiter = rate.NewLimiter(rate.Limit(yahooRatePerSecond), yahooRateBurst)
	ctx := context.Background()
	for i := range yahooRateBurst {
		if err := waitRate(ctx); err != nil {
			t.Fatalf("waitRate under burst (i=%d) returned error: %v", i, err)
		}
	}
}
