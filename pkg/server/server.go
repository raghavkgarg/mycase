package server

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/raghavkgarg/mycase/pkg/attribution"
	"github.com/raghavkgarg/mycase/pkg/broker"
	"github.com/raghavkgarg/mycase/pkg/cache"
	"github.com/raghavkgarg/mycase/pkg/config"
	"github.com/raghavkgarg/mycase/pkg/marketdata"
)

// MarketDataFetcher is the market-data surface the dashboard handlers need.
// *datafetcher.Router satisfies it. Defined here (consumer-side) so pkg/server
// depends only on the shapes it uses and stays mockable in tests.
type MarketDataFetcher interface {
	FetchHistoricalDataWithTimestamps(ctx context.Context, ticker, rangeStr string) (*marketdata.HistoricalData, error)
	FetchHistoricalByDateRange(ctx context.Context, ticker string, from, to time.Time) (*marketdata.HistoricalData, error)
	FetchFundamentals(ctx context.Context, tickers []string) (map[string]marketdata.Fundamentals, error)
	NormalizeBenchmarkSymbol(symbol string) string
}

// Server holds all dependencies for the web dashboard.
type Server struct {
	broker      broker.Broker
	fetcher     attribution.PriceFetcher // nil → performance tab reports "unavailable"
	router      MarketDataFetcher        // nil → dashboard data handlers fall back to Yahoo direct
	cache       *cache.Cache
	mux         *http.ServeMux
	broadcaster *SSEBroadcaster
	alertCfg    config.AlertConfig
}

// Option configures a Server at construction.
type Option func(*Server)

// WithFetcher supplies the price fetcher used by the performance tab (typically
// a *datafetcher.Router). Without it, the performance endpoint returns
// available=false rather than fabricating a NAV series.
func WithFetcher(f attribution.PriceFetcher) Option {
	return func(s *Server) { s.fetcher = f }
}

// WithRouter supplies the market-data router used by the holdings/monitor/
// performance data handlers (typically a *datafetcher.Router) so US tickers
// route through Schwab instead of hitting Yahoo directly. Without it, those
// handlers fall back to Yahoo Finance.
func WithRouter(r MarketDataFetcher) Option {
	return func(s *Server) { s.router = r }
}

// New creates a Server, wires up all routes, and returns it ready to serve.
func New(b broker.Broker, c *cache.Cache, alertCfg config.AlertConfig, opts ...Option) *Server {
	s := &Server{
		broker:      b,
		cache:       c,
		alertCfg:    alertCfg,
		mux:         http.NewServeMux(),
		broadcaster: newSSEBroadcaster(),
	}
	for _, opt := range opts {
		opt(s)
	}
	s.registerRoutes()
	return s
}

// ListenAndServe starts the SSE broadcaster and serves HTTP until ctx is cancelled.
// Returns nil on graceful shutdown (ErrServerClosed).
func (s *Server) ListenAndServe(ctx context.Context, addr string) error {
	go s.broadcaster.BroadcastLoop(ctx, s.broker, 5*time.Second)

	srv := &http.Server{
		Addr:    addr,
		Handler: s.mux,
	}

	go func() {
		<-ctx.Done()
		srv.Shutdown(context.Background()) //nolint:errcheck
	}()

	if err := srv.ListenAndServe(); errors.Is(err, http.ErrServerClosed) {
		return nil
	} else {
		return err
	}
}
