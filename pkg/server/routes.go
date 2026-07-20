package server

import (
	"io/fs"
	"net/http"
)

// registerRoutes wires all HTTP routes onto s.mux.
func (s *Server) registerRoutes() {
	// Sub-FS rooted at pkg/server/static/
	staticFS, err := fs.Sub(staticFiles, "static")
	if err != nil {
		panic("server: failed to sub static FS: " + err.Error())
	}

	// ── API routes ──────────────────────────────────────────────────────────
	s.mux.HandleFunc("GET /api/portfolios", s.handlePortfolios)
	s.mux.HandleFunc("GET /api/portfolio/{name}/weights", s.handleWeights)
	s.mux.HandleFunc("GET /api/portfolio/{name}/holdings", s.handleHoldings)
	s.mux.HandleFunc("GET /api/portfolio/{name}/drift", s.handleDrift)
	s.mux.HandleFunc("GET /api/portfolio/{name}/orders", s.handleOrders)
	s.mux.HandleFunc("GET /api/portfolio/{name}/monitor", s.handleMonitor)
	s.mux.HandleFunc("POST /api/portfolio/{name}/backtest", s.handleBacktest)
	s.mux.HandleFunc("POST /api/portfolio/{name}/execute", s.handleExecute)
	s.mux.HandleFunc("GET /api/quotes", s.broadcaster.ServeSSE)
	s.mux.HandleFunc("GET /api/cache/status", s.handleCacheStatus)
	s.mux.HandleFunc("GET /api/daemon/history", s.handleDaemonHistory)

	// ── Static files ─────────────────────────────────────────────────────────
	// Vendor files get long-term caching; everything else is no-cache.
	s.mux.Handle("GET /static/vendor/", withLongCache(http.StripPrefix("/static/", http.FileServerFS(staticFS))))
	s.mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServerFS(staticFS)))

	// Root: serve index.html
	s.mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFileFS(w, r, staticFS, "index.html")
	})
}

// withLongCache wraps a handler to add a one-year max-age Cache-Control header.
func withLongCache(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		h.ServeHTTP(w, r)
	})
}
