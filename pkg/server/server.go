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
)

// Server holds all dependencies for the web dashboard.
type Server struct {
	broker      broker.Broker
	cache       *cache.Cache
	alertCfg    config.AlertConfig
	fetcher     attribution.PriceFetcher // nil → performance tab reports "unavailable"
	mux         *http.ServeMux
	broadcaster *SSEBroadcaster
}

// Option configures a Server at construction.
type Option func(*Server)

// WithFetcher supplies the price fetcher used by the performance tab (typically
// a *datafetcher.Router). Without it, the performance endpoint returns
// available=false rather than fabricating a NAV series.
func WithFetcher(f attribution.PriceFetcher) Option {
	return func(s *Server) { s.fetcher = f }
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
