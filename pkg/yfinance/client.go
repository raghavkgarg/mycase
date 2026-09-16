package yfinance

import (
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/raghavkgarg/mycase/pkg/rawcapture"
)

// newYFinanceHTTPClient returns an *http.Client configured for Yahoo Finance.
// It uses ProxyFromEnvironment, but explicitly bypasses broker-only proxies
// (such as staticip.in) that only whitelist broker APIs and reject Yahoo Finance.
func newYFinanceHTTPClient(timeout time.Duration, jar http.CookieJar) *http.Client {
	transport := &http.Transport{
		Proxy: func(req *http.Request) (*url.URL, error) {
			proxyURL, err := http.ProxyFromEnvironment(req)
			if err != nil || proxyURL == nil {
				return nil, err
			}
			// staticip.in is a Zerodha-specific static proxy that rejects Yahoo Finance with 403 Forbidden.
			if strings.Contains(strings.ToLower(proxyURL.Host), "staticip.in") {
				return nil, nil
			}
			return proxyURL, nil
		},
	}
	return &http.Client{
		Timeout:   timeout,
		Transport: transport,
		Jar:       jar,
	}
}

// executeYFinanceRequest executes an HTTP request, automatically falling back
// to a direct connection if the environment proxy fails with 403 Forbidden or a proxy error.
// It first blocks on the process-wide Yahoo rate limiter (see ratelimit.go), so
// every request routed through here is paced.
func executeYFinanceRequest(client *http.Client, req *http.Request) (*http.Response, error) {
	// Offline replay: serve a recorded body from the archive, skipping the rate
	// limiter and network (no-op unless MYCASE_REPLAY).
	if resp, ok := replayYFinance(req); ok {
		return resp, nil
	}
	if err := waitRate(req.Context()); err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil && isProxyFailure(err) {
		directClient := &http.Client{
			Timeout:   client.Timeout,
			Transport: &http.Transport{Proxy: nil},
			Jar:       client.Jar,
		}
		retryReq := req.Clone(req.Context())
		if directResp, directErr := directClient.Do(retryReq); directErr == nil {
			return captureYFinance(directResp), nil
		}
	}
	return captureYFinance(resp), err
}

// replayYFinance serves a recorded Yahoo response body for req from the raw
// archive as a synthetic 200 response. Returns ok=false when replay is disabled
// or no matching archive file exists.
func replayYFinance(req *http.Request) (*http.Response, bool) {
	endpoint, symbol := yfinanceCaptureLabels(req)
	bodyRC, ok := rawcapture.Replay("yahoo", endpoint, symbol)
	if !ok {
		return nil, false
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Status:     "200 OK (replay)",
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       bodyRC,
		Request:    req,
	}, true
}

// captureYFinance archives a successful (2xx) Yahoo response body for offline
// replay/triage (no-op unless MYCASE_CAPTURE is set), returning the response
// with a replacement body so callers are unchanged. nil / non-2xx pass through.
func captureYFinance(resp *http.Response) *http.Response {
	if resp == nil || resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return resp
	}
	endpoint, symbol := yfinanceCaptureLabels(resp.Request)
	resp.Body = rawcapture.Capture("yahoo", endpoint, symbol, resp.Body)
	return resp
}

// yfinanceCaptureLabels derives a short endpoint name and primary symbol from a
// Yahoo request for the raw-capture archive filename. Best-effort.
func yfinanceCaptureLabels(req *http.Request) (endpoint, symbol string) {
	if req == nil || req.URL == nil {
		return "request", ""
	}
	u := req.URL
	// Yahoo encodes the symbol as the last path segment on quoteSummary /
	// timeseries / chart endpoints; the endpoint name is the segment before it
	// (e.g. .../quoteSummary/AAPL → endpoint "quoteSummary", symbol "AAPL").
	segs := strings.Split(strings.Trim(u.Path, "/"), "/")
	var nonEmpty []string
	for _, s := range segs {
		if s != "" {
			nonEmpty = append(nonEmpty, s)
		}
	}
	switch len(nonEmpty) {
	case 0:
		endpoint = "request"
	case 1:
		endpoint = nonEmpty[0]
	default:
		endpoint = nonEmpty[len(nonEmpty)-2]
		symbol = nonEmpty[len(nonEmpty)-1]
	}
	// Prefer an explicit ?symbol= when present (timeseries carries it in query).
	if s := u.Query().Get("symbol"); s != "" {
		symbol = s
	}
	return endpoint, symbol
}

func isProxyFailure(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "forbidden") ||
		strings.Contains(msg, "proxy") ||
		strings.Contains(msg, "tunnel")
}
