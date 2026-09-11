package yfinance

import (
	"net/http"
	"net/url"
	"strings"
	"time"
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
func executeYFinanceRequest(client *http.Client, req *http.Request) (*http.Response, error) {
	resp, err := client.Do(req)
	if err != nil && isProxyFailure(err) {
		directClient := &http.Client{
			Timeout:   client.Timeout,
			Transport: &http.Transport{Proxy: nil},
			Jar:       client.Jar,
		}
		retryReq := req.Clone(req.Context())
		if directResp, directErr := directClient.Do(retryReq); directErr == nil {
			return directResp, nil
		}
	}
	return resp, err
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
