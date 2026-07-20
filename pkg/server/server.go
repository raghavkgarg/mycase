package server

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/raghavkgarg/mycase/pkg/broker"
	"github.com/raghavkgarg/mycase/pkg/cache"
	"github.com/raghavkgarg/mycase/pkg/config"
)

// Server holds all dependencies for the web dashboard.
type Server struct {
	broker      broker.Broker
	cache       *cache.Cache
	alertCfg    config.AlertConfig
	mux         *http.ServeMux
	broadcaster *SSEBroadcaster
}

// New creates a Server, wires up all routes, and returns it ready to serve.
func New(b broker.Broker, c *cache.Cache, alertCfg config.AlertConfig) *Server {
	s := &Server{
		broker:      b,
		cache:       c,
		alertCfg:    alertCfg,
		mux:         http.NewServeMux(),
		broadcaster: newSSEBroadcaster(),
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
