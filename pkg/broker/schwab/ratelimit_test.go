package schwab

import (
	"context"
	"testing"
	"time"

	"golang.org/x/time/rate"
)

// TestNewClientLimiterConfig verifies the default client is constructed with a
// token bucket matching Schwab's 120 req/min ceiling: burst of 120, refill of
// 2 tokens/sec.
func TestNewClientLimiterConfig(t *testing.T) {
	c := NewClient(nil)
	if c.limiter == nil {
		t.Fatal("expected non-nil limiter")
	}
	if got := c.limiter.Burst(); got != rateLimitBurst {
		t.Errorf("limiter burst = %d, want %d", got, rateLimitBurst)
	}
	if got := float64(c.limiter.Limit()); got != rateLimitPerSecond {
		t.Errorf("limiter rate = %v tokens/sec, want %v", got, rateLimitPerSecond)
	}
	if rateLimitPerSecond*60 != rateLimitPerMinute {
		t.Errorf("rate config inconsistent: %v/sec * 60 != %d/min", rateLimitPerSecond, rateLimitPerMinute)
	}
}

// TestExecuteRequestRespectsContextCancellation ensures a send blocked on an
// exhausted limiter returns the context error rather than hanging. We drain the
// bucket, then call executeRequest with an already-cancelled context.
func TestExecuteRequestRespectsContextCancellation(t *testing.T) {
	c := NewClient(nil)
	// Drain all burst tokens so the next Wait must block for a refill.
	if !drain(c.limiter, rateLimitBurst) {
		t.Fatal("failed to drain limiter burst")
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	_, err := c.executeRequest(ctx, "GET", "http://example.invalid/x", nil, "tok")
	if err == nil {
		t.Fatal("expected error from cancelled context, got nil")
	}
	if ctx.Err() == nil {
		t.Fatal("test setup: context should be cancelled")
	}
}

// TestLimiterPacesAfterBurst confirms the limiter admits the initial burst
// without delay, then forces a wait for subsequent requests. Uses a small
// dedicated limiter so the timing is fast and deterministic.
func TestLimiterPacesAfterBurst(t *testing.T) {
	// 10 tokens/sec, burst 3: first 3 reservations are immediate, the 4th must
	// wait ~100ms for a refill.
	lim := rate.NewLimiter(rate.Limit(10), 3)
	ctx := context.Background()

	start := time.Now()
	for i := range 3 {
		if err := lim.Wait(ctx); err != nil {
			t.Fatalf("burst token %d: unexpected error %v", i, err)
		}
	}
	if elapsed := time.Since(start); elapsed > 20*time.Millisecond {
		t.Errorf("burst of 3 took %v, expected near-instant", elapsed)
	}

	// 4th token must wait for a refill (~100ms at 10/sec).
	waitStart := time.Now()
	if err := lim.Wait(ctx); err != nil {
		t.Fatalf("4th token: unexpected error %v", err)
	}
	if waited := time.Since(waitStart); waited < 50*time.Millisecond {
		t.Errorf("4th token waited %v, expected a refill delay (~100ms)", waited)
	}
}

// drain consumes n tokens immediately from the limiter, returning false if any
// token is not immediately available.
func drain(l *rate.Limiter, n int) bool {
	for range n {
		if !l.Allow() {
			return false
		}
	}
	return true
}
