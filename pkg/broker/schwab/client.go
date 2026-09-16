package schwab

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"golang.org/x/time/rate"

	"github.com/raghavkgarg/mycase/pkg/logging"
	"github.com/raghavkgarg/mycase/pkg/rawcapture"
)

const (
	traderBaseURL     = "https://api.schwabapi.com/trader/v1"
	marketDataBaseURL = "https://api.schwabapi.com/marketdata/v1"

	defaultTimeout = 15 * time.Second

	// Schwab rate limit: 120 requests per minute. Enforced by a token-bucket
	// limiter (golang.org/x/time/rate): a burst of up to rateLimitBurst tokens
	// refilled at rateLimitPerSecond tokens/sec, keeping steady-state traffic
	// under the 120/min ceiling while allowing an initial burst.
	rateLimitPerMinute = 120
	rateLimitPerSecond = rateLimitPerMinute / 60.0 // 2 tokens/sec
	rateLimitBurst     = rateLimitPerMinute        // allow up to a full minute's budget as burst
)

// Client is the Schwab API HTTP client. It handles Bearer token injection,
// auto-refresh on 401, and rate limiting (token bucket, 120 req/min).
type Client struct {
	httpClient *http.Client
	tokenMgr   *TokenManager

	// Base URLs (overridable for testing)
	traderBase     string
	marketDataBase string

	// Rate limiting: token bucket sized to Schwab's 120 req/min ceiling.
	limiter *rate.Limiter
}

// NewClient creates a Schwab API client with the given token manager.
func NewClient(tokenMgr *TokenManager) *Client {
	return &Client{
		httpClient:     &http.Client{Timeout: defaultTimeout},
		tokenMgr:       tokenMgr,
		traderBase:     traderBaseURL,
		marketDataBase: marketDataBaseURL,
		limiter:        rate.NewLimiter(rate.Limit(rateLimitPerSecond), rateLimitBurst),
	}
}

// SetMarketDataBase overrides the market data base URL (for testing).
func (c *Client) SetMarketDataBase(url string) {
	c.marketDataBase = url
}

// SetTraderBase overrides the trader base URL (for testing).
func (c *Client) SetTraderBase(url string) {
	c.traderBase = url
}

// APIError represents a non-2xx response from the Schwab API.
type APIError struct {
	Body       map[string]any
	Message    string
	StatusCode int
}

func (e *APIError) Error() string {
	if e.Message != "" {
		return fmt.Sprintf("schwab API %d: %s", e.StatusCode, e.Message)
	}
	return fmt.Sprintf("schwab API %d: %v", e.StatusCode, e.Body)
}

// GetTrader makes an authenticated GET request to the Trader API.
func (c *Client) GetTrader(ctx context.Context, path string) (*http.Response, error) {
	return c.doRequest(ctx, "GET", c.traderBase+path, nil)
}

// PostTrader makes an authenticated POST request to the Trader API.
func (c *Client) PostTrader(ctx context.Context, path string, body io.Reader) (*http.Response, error) {
	return c.doRequest(ctx, "POST", c.traderBase+path, body)
}

// GetMarketData makes an authenticated GET request to the Market Data API.
func (c *Client) GetMarketData(ctx context.Context, path string) (*http.Response, error) {
	return c.doRequest(ctx, "GET", c.marketDataBase+path, nil)
}

// doRequest performs an authenticated HTTP request with auto-refresh on 401.
func (c *Client) doRequest(ctx context.Context, method, url string, body io.Reader) (*http.Response, error) {
	// Offline replay: serve a recorded body from the archive and skip token
	// fetch, rate limiter, and network entirely (no-op unless MYCASE_REPLAY).
	if resp, ok := c.replay(ctx, url); ok {
		return resp, nil
	}

	token, err := c.tokenMgr.GetAccessToken(ctx)
	if err != nil {
		return nil, err
	}

	resp, err := c.executeRequest(ctx, method, url, body, token)
	if err != nil {
		return nil, err
	}

	// If 401, try one refresh and retry
	if resp.StatusCode == http.StatusUnauthorized {
		resp.Body.Close()

		// Force a refresh by invalidating current token
		c.tokenMgr.mu.Lock()
		if c.tokenMgr.token != nil {
			c.tokenMgr.token.ExpiresAt = 0
		}
		c.tokenMgr.mu.Unlock()

		token, err = c.tokenMgr.GetAccessToken(ctx)
		if err != nil {
			return nil, fmt.Errorf("re-auth after 401 failed: %w", err)
		}

		resp, err = c.executeRequest(ctx, method, url, body, token)
		if err != nil {
			return nil, err
		}
	}

	if resp.StatusCode >= 400 {
		defer resp.Body.Close()
		return nil, parseAPIError(resp)
	}

	return resp, nil
}

// executeRequest builds and sends a single HTTP request. It blocks on the
// rate limiter before sending, so every send (including the post-401 retry)
// consumes one token from the 120 req/min budget.
func (c *Client) executeRequest(ctx context.Context, method, url string, body io.Reader, token string) (*http.Response, error) {
	if err := c.limiter.Wait(ctx); err != nil {
		return nil, fmt.Errorf("schwab: rate limiter wait: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	start := time.Now()
	resp, err := c.httpClient.Do(req)
	if err != nil {
		slog.WarnContext(ctx, "schwab.request_error", "method", method, "url", logging.TruncateURL(url), "err", err, "duration_ms", time.Since(start).Milliseconds())
		return nil, err
	}
	logging.LogResponse(ctx, slog.Default(), method, url, resp.StatusCode, time.Since(start))

	// Archive the raw body for offline replay/triage (no-op unless MYCASE_CAPTURE
	// is set). Only 2xx bodies are captured; error bodies flow to parseAPIError.
	// Token/auth responses never reach here (they use auth.go's tokenURL path),
	// so no credentials are archived.
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		endpoint, symbol := captureLabels(url)
		resp.Body = rawcapture.Capture("schwab", endpoint, symbol, resp.Body)
	}
	return resp, nil
}

// replay serves a recorded response body for url from the raw archive, as a
// synthetic 200 response. Returns ok=false when replay is disabled or no
// matching archive file exists (caller then proceeds with a live request).
func (c *Client) replay(ctx context.Context, url string) (*http.Response, bool) {
	endpoint, symbol := captureLabels(url)
	bodyRC, ok := rawcapture.Replay("schwab", endpoint, symbol)
	if !ok {
		return nil, false
	}
	slog.InfoContext(ctx, "schwab.replay_hit", "endpoint", endpoint, "symbol", symbol)
	req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
	return &http.Response{
		StatusCode: http.StatusOK,
		Status:     "200 OK (replay)",
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       bodyRC,
		Request:    req,
	}, true
}

// captureLabels derives a short endpoint name and primary symbol from a Schwab
// request URL, for the raw-capture archive filename. Best-effort: on any parse
// issue it falls back to a generic label.
func captureLabels(rawURL string) (endpoint, symbol string) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "request", ""
	}
	// Last non-empty path segment is the logical endpoint
	// (e.g. .../marketdata/v1/quotes → "quotes",
	//       .../instruments → "instruments").
	segs := strings.Split(strings.Trim(u.Path, "/"), "/")
	endpoint = "request"
	for _, seg := range slices.Backward(segs) {
		if seg != "" {
			endpoint = seg
			break
		}
	}
	q := u.Query()
	// Schwab uses "symbol" (instruments/fundamentals) and "symbols" (quotes).
	symbol = q.Get("symbol")
	if symbol == "" {
		symbol = q.Get("symbols")
	}
	return endpoint, symbol
}

// parseAPIError extracts error details from a non-2xx response.
func parseAPIError(resp *http.Response) *APIError {
	apiErr := &APIError{StatusCode: resp.StatusCode}
	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err == nil {
		apiErr.Body = body
		if msg, ok := body["message"].(string); ok {
			apiErr.Message = msg
		} else if msg, ok := body["error"].(string); ok {
			apiErr.Message = msg
		}
	}
	return apiErr
}

// DecodeJSON reads and decodes a JSON response body into the given target.
func DecodeJSON[T any](resp *http.Response, target *T) error {
	defer resp.Body.Close()
	if err := json.NewDecoder(resp.Body).Decode(target); err != nil {
		return fmt.Errorf("failed to decode schwab response: %w", err)
	}
	return nil
}

// AccountHashEntry represents one account's number and hash from the Schwab API.
type AccountHashEntry struct {
	AccountNumber string `json:"accountNumber"`
	HashValue     string `json:"hashValue"`
}

// FetchAccountHash retrieves account hashes from the Schwab Trader API
// and returns the first account's hash value. This is needed to construct API
// paths like /accounts/{hash}/positions.
func (c *Client) FetchAccountHash(ctx context.Context) (string, error) {
	resp, err := c.GetTrader(ctx, "/accounts/accountNumbers")
	if err != nil {
		return "", fmt.Errorf("fetching account numbers: %w", err)
	}

	var entries []AccountHashEntry
	if err := DecodeJSON(resp, &entries); err != nil {
		return "", fmt.Errorf("decoding account numbers: %w", err)
	}
	if len(entries) == 0 {
		return "", fmt.Errorf("no accounts found — ensure your Schwab app has account access permissions")
	}
	return entries[0].HashValue, nil
}
