package schwab

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
)

const (
	traderBaseURL     = "https://api.schwabapi.com/trader/v1"
	marketDataBaseURL = "https://api.schwabapi.com/marketdata/v1"

	defaultTimeout = 15 * time.Second

	// Schwab rate limit: 120 requests per minute.
	rateLimitPerMinute = 120
)

// Client is the Schwab API HTTP client. It handles Bearer token injection,
// auto-refresh on 401, and basic rate limiting.
type Client struct {
	httpClient *http.Client
	tokenMgr   *TokenManager

	// Base URLs (overridable for testing)
	traderBase     string
	marketDataBase string

	// Rate limiting
	mu          sync.Mutex
	requestLog  []time.Time
}

// NewClient creates a Schwab API client with the given token manager.
func NewClient(tokenMgr *TokenManager) *Client {
	return &Client{
		httpClient:     &http.Client{Timeout: defaultTimeout},
		tokenMgr:       tokenMgr,
		traderBase:     traderBaseURL,
		marketDataBase: marketDataBaseURL,
		requestLog:     make([]time.Time, 0, rateLimitPerMinute),
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
	StatusCode int
	Message    string
	Body       map[string]any
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
	if err := c.waitForRateLimit(ctx); err != nil {
		return nil, err
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

// executeRequest builds and sends a single HTTP request.
func (c *Client) executeRequest(ctx context.Context, method, url string, body io.Reader, token string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	c.recordRequest()
	return c.httpClient.Do(req)
}

// waitForRateLimit blocks until a request slot is available.
func (c *Client) waitForRateLimit(ctx context.Context) error {
	for {
		c.mu.Lock()
		now := time.Now()
		cutoff := now.Add(-1 * time.Minute)

		// Prune old entries
		valid := c.requestLog[:0]
		for _, t := range c.requestLog {
			if t.After(cutoff) {
				valid = append(valid, t)
			}
		}
		c.requestLog = valid

		if len(c.requestLog) < rateLimitPerMinute {
			c.mu.Unlock()
			return nil
		}

		// Calculate wait time until oldest entry expires
		oldest := c.requestLog[0]
		waitUntil := oldest.Add(1 * time.Minute)
		c.mu.Unlock()

		waitDur := time.Until(waitUntil)
		if waitDur <= 0 {
			continue
		}

		select {
		case <-time.After(waitDur):
			// retry
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// recordRequest logs the current time for rate limiting.
func (c *Client) recordRequest() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.requestLog = append(c.requestLog, time.Now())
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
