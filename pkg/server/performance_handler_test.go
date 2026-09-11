package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/raghavkgarg/mycase/pkg/broker"
	"github.com/raghavkgarg/mycase/pkg/config"
	"github.com/raghavkgarg/mycase/pkg/yfinance"
)

// stubFetcher satisfies attribution.PriceFetcher with canned data.
type stubFetcher struct {
	data map[string]*yfinance.HistoricalData
}

func (s *stubFetcher) FetchHistoricalByDateRange(_ context.Context, ticker string, _, _ time.Time) (*yfinance.HistoricalData, error) {
	return s.data[ticker], nil
}

// newTestServer builds a Server without registering routes twice; we call the
// handler directly so no live listener is needed.
func newTestServer(opts ...Option) *Server {
	return New(&broker.MockBroker{}, nil, config.AlertConfig{}, opts...)
}

func TestHandlePerformance_NilFetcherUnavailable(t *testing.T) {
	s := newTestServer() // no WithFetcher → fetcher nil
	req := httptest.NewRequest(http.MethodGet, "/api/portfolio/sp500/performance", nil)
	req.SetPathValue("name", "sp500")
	rec := httptest.NewRecorder()

	s.handlePerformance(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if avail, _ := body["available"].(bool); avail {
		t.Errorf("expected available=false with nil fetcher, got %v", body["available"])
	}
	if _, ok := body["message"]; !ok {
		t.Error("expected a message explaining unavailability")
	}
}

func TestHandlePerformance_UnknownPortfolio404(t *testing.T) {
	s := newTestServer(WithFetcher(&stubFetcher{data: map[string]*yfinance.HistoricalData{}}))
	req := httptest.NewRequest(http.MethodGet, "/api/portfolio/does_not_exist_xyz/performance", nil)
	req.SetPathValue("name", "does_not_exist_xyz")
	rec := httptest.NewRecorder()

	s.handlePerformance(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 for missing portfolio CSV", rec.Code)
	}
}

func TestWithFetcher_SetsField(t *testing.T) {
	f := &stubFetcher{}
	s := newTestServer(WithFetcher(f))
	if s.fetcher == nil {
		t.Error("WithFetcher did not set the fetcher field")
	}
	if s.fetcher != f {
		t.Error("WithFetcher stored a different fetcher than provided")
	}
}
