// Package edgar is a low-level client for SEC EDGAR's public data API
// (data.sec.gov / www.sec.gov), used to source authoritative US fundamentals
// (operating cash flow, net income, and annual statement series) that Schwab's
// thin TTM fundamentals endpoint cannot supply. See docs/08-edgar-design.md and
// docs/07-datasources.md §4.3.
//
// Layering: this is a Layer-1 package. It imports only the leaf packages
// marketdata (the shared Fundamentals DTO it populates) and cache (for its own
// persistence via cache.Conn()). It owns its DuckDB tables (edgar_cik_map,
// edgar_facts) and must never be imported by cache. It deliberately does NOT
// import broker/schwab (L2) — the tiny US-prefix strip helper is duplicated
// locally instead (stripUSPrefix) to keep the import direction downward.
//
// EDGAR access rules (enforced here): a descriptive User-Agent declaring
// identity + contact is mandatory (missing/generic → IP block), and the fair-
// access limit is ≤ 10 requests/second across all of a user's machines. The
// client paces every request through a token-bucket rate limiter.
package edgar

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"golang.org/x/time/rate"

	"github.com/raghavkgarg/mycase/pkg/cache"
	"github.com/raghavkgarg/mycase/pkg/logging"
)

const (
	defaultTimeout = 15 * time.Second

	// SEC fair-access ceiling: 10 requests/second. We pace at exactly 10/s with
	// a burst of 10 so a batch of fundamentals fetches never trips the block.
	rateLimitPerSecond = 10

	// Base hosts. www.sec.gov serves the static ticker→CIK file; data.sec.gov
	// serves the XBRL/company APIs. Both overridable for tests.
	defaultWWWBase  = "https://www.sec.gov"
	defaultDataBase = "https://data.sec.gov"

	// Default per-source freshness (overridable via config). companyfacts is
	// stable between quarterly filings; the CIK map changes only on new listings.
	defaultFactsTTL = 80 * 24 * time.Hour
	defaultCIKTTL   = 7 * 24 * time.Hour
)

// Client is the SEC EDGAR HTTP client. It injects the mandatory User-Agent,
// paces requests through a 10 req/s limiter, and persists its CIK map and
// company-facts blobs in the shared DuckDB cache.
type Client struct {
	httpClient *http.Client
	limiter    *rate.Limiter
	cache      *cache.Cache
	logger     *slog.Logger

	userAgent string
	wwwBase   string
	dataBase  string

	factsTTL time.Duration
	cikTTL   time.Duration
}

// Option configures a Client.
type Option func(*Client)

// WithFactsTTL overrides the companyfacts cache freshness window.
func WithFactsTTL(d time.Duration) Option {
	return func(c *Client) {
		if d > 0 {
			c.factsTTL = d
		}
	}
}

// WithCIKTTL overrides the ticker→CIK map cache freshness window.
func WithCIKTTL(d time.Duration) Option {
	return func(c *Client) {
		if d > 0 {
			c.cikTTL = d
		}
	}
}

// WithLogger sets the logger (defaults to slog.Default()).
func WithLogger(l *slog.Logger) Option {
	return func(c *Client) {
		if l != nil {
			c.logger = l
		}
	}
}

// NewClient constructs an EDGAR client. userAgent is mandatory and must declare
// identity + contact (e.g. "mycase/1.0 you@example.com"); an empty or obviously
// generic value returns an error because EDGAR blocks such requests. c may be
// nil — the client then operates without a persistent cache (every call hits
// the network, still rate-limited), which is useful for tests.
func NewClient(userAgent string, c *cache.Cache, opts ...Option) (*Client, error) {
	ua := strings.TrimSpace(userAgent)
	if err := validateUserAgent(ua); err != nil {
		return nil, err
	}
	cl := &Client{
		httpClient: &http.Client{Timeout: defaultTimeout},
		limiter:    rate.NewLimiter(rate.Limit(rateLimitPerSecond), rateLimitPerSecond),
		cache:      c,
		logger:     slog.Default(),
		userAgent:  ua,
		wwwBase:    defaultWWWBase,
		dataBase:   defaultDataBase,
		factsTTL:   defaultFactsTTL,
		cikTTL:     defaultCIKTTL,
	}
	for _, o := range opts {
		o(cl)
	}
	return cl, nil
}

// validateUserAgent rejects empty or placeholder UAs that EDGAR would block.
func validateUserAgent(ua string) error {
	if ua == "" {
		return fmt.Errorf("edgar: User-Agent is required (SEC blocks requests without one); set edgar.user_agent to \"appname/version contact@example.com\"")
	}
	// Reject the shipped placeholder so a misconfigured deploy fails loudly
	// rather than getting the whole host IP-blocked by SEC.
	if strings.Contains(ua, "set-your-contact@example.com") {
		return fmt.Errorf("edgar: User-Agent is still the placeholder %q; set edgar.user_agent to your real appname/version + contact email", ua)
	}
	// A usable UA should carry a contact (an '@' or a URL) so SEC can reach the
	// operator. This is EDGAR's stated expectation, not a hard syntactic rule.
	if !strings.Contains(ua, "@") && !strings.Contains(ua, "http") {
		return fmt.Errorf("edgar: User-Agent %q should include a contact email or URL per SEC fair-access policy", ua)
	}
	return nil
}

// SetBaseURLs overrides the www and data base URLs (for testing against an
// httptest server). Either may be empty to leave it unchanged.
func (c *Client) SetBaseURLs(wwwBase, dataBase string) {
	if wwwBase != "" {
		c.wwwBase = wwwBase
	}
	if dataBase != "" {
		c.dataBase = dataBase
	}
}

// getJSON performs a rate-limited, User-Agent-stamped GET and returns the raw
// body bytes. It never logs the response body — only method, (truncated) URL,
// status, and duration, per the logging/API rules. A non-2xx status is an error.
func (c *Client) getJSON(ctx context.Context, url string) ([]byte, error) {
	if err := c.limiter.Wait(ctx); err != nil {
		return nil, fmt.Errorf("edgar: rate limiter wait: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	// EDGAR requires a descriptive UA and JSON accept. We deliberately do NOT
	// set Accept-Encoding: gzip — Go's transport adds it and decompresses
	// transparently only when the caller leaves it unset.
	req.Header.Set("User-Agent", c.userAgent)
	req.Header.Set("Accept", "application/json")

	logging.LogRequest(ctx, c.logger, http.MethodGet, url)
	start := time.Now()
	resp, err := c.httpClient.Do(req)
	if err != nil {
		c.logger.WarnContext(ctx, "edgar.request_error",
			"url", logging.TruncateURL(url), "err", err,
			"duration_ms", time.Since(start).Milliseconds())
		return nil, fmt.Errorf("edgar: GET %s: %w", logging.TruncateURL(url), err)
	}
	defer resp.Body.Close()

	logging.LogResponse(ctx, c.logger, http.MethodGet, url, resp.StatusCode, time.Since(start))

	if resp.StatusCode == http.StatusNotFound {
		// A missing CIK/facts file is an expected "no data" condition, not a
		// hard failure — surface it as a sentinel so callers skip gracefully.
		return nil, errNotFound
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("edgar: GET %s: status %d", logging.TruncateURL(url), resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("edgar: read body %s: %w", logging.TruncateURL(url), err)
	}
	return body, nil
}

// errNotFound is the sentinel for a 404 from EDGAR (unknown CIK or missing
// facts file). Callers treat it as "no data for this ticker" and skip.
var errNotFound = fmt.Errorf("edgar: resource not found")

// stripUSPrefix removes a "US:", "NYSE:", or "NASDAQ:" prefix from a ticker.
// Duplicated locally (rather than importing broker/schwab, which is L2) to keep
// this L1 package's imports strictly downward.
func stripUSPrefix(ticker string) string {
	for _, prefix := range []string{"US:", "NYSE:", "NASDAQ:"} {
		if strings.HasPrefix(ticker, prefix) {
			return ticker[len(prefix):]
		}
	}
	return ticker
}
