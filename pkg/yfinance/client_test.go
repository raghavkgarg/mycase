package yfinance

import (
	"errors"
	"net/http"
	"os"
	"testing"
	"time"
)

func TestYFinanceProxyBypass(t *testing.T) {
	// Set the Zerodha static IP proxy
	origProxy := os.Getenv("HTTP_PROXY")
	origHTTPSProxy := os.Getenv("HTTPS_PROXY")
	origNoProxy := os.Getenv("NO_PROXY")
	defer func() {
		os.Setenv("HTTP_PROXY", origProxy)
		os.Setenv("HTTPS_PROXY", origHTTPSProxy)
		os.Setenv("NO_PROXY", origNoProxy)
	}()

	os.Setenv("HTTP_PROXY", "http://user:pass@dc-mum-601.staticip.in:443")
	os.Setenv("HTTPS_PROXY", "http://user:pass@dc-mum-601.staticip.in:443")
	os.Unsetenv("NO_PROXY")

	client := newYFinanceHTTPClient(5*time.Second, nil)
	transport, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("expected *http.Transport, got %T", client.Transport)
	}

	req, err := http.NewRequest("GET", "https://query1.finance.yahoo.com/v8/finance/chart/TENNIND.NS?range=1d&interval=1d", nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}

	proxyURL, err := transport.Proxy(req)
	if err != nil {
		t.Fatalf("unexpected error from Proxy: %v", err)
	}
	if proxyURL != nil {
		t.Errorf("expected staticip.in to be bypassed (nil proxyURL), got %v", proxyURL)
	}
}

func TestIsProxyFailure(t *testing.T) {
	tests := []struct {
		err  error
		want bool
	}{
		{errors.New("Get \"https://query1.finance.yahoo.com/...\": Forbidden"), true},
		{errors.New("CONNECT tunnel failed, response 403"), true},
		{errors.New("proxy error: connection refused"), true},
		{errors.New("context deadline exceeded"), false},
		{errors.New("connection reset by peer"), false},
		{nil, false},
	}

	for _, tc := range tests {
		got := isProxyFailure(tc.err)
		if got != tc.want {
			t.Errorf("isProxyFailure(%v) = %v, want %v", tc.err, got, tc.want)
		}
	}
}
