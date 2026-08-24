package schwab

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func newTestClient(t *testing.T, handler http.Handler) *Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	// Create a token manager with a valid token that won't expire
	dir := t.TempDir()
	tokenPath := dir + "/token.json"
	SaveToken(tokenPath, &Token{
		AccessToken:      "test_token",
		RefreshToken:     "test_refresh",
		ExpiresAt:        time.Now().Add(1 * time.Hour).Unix(),
		RefreshExpiresAt: time.Now().Add(7 * 24 * time.Hour).Unix(),
	})
	app := &AppConfig{ClientID: "test", ClientSecret: "test"}
	tm := NewTokenManager(app, tokenPath)

	client := NewClient(tm)
	// Override the HTTP client to use the test server
	client.httpClient = server.Client()
	// We need to override the base URLs — use a custom transport
	// Instead, we'll test the helper functions directly
	return client
}

func TestStripUSPrefix(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"US:AAPL", "AAPL"},
		{"NYSE:MSFT", "MSFT"},
		{"NASDAQ:GOOG", "GOOG"},
		{"AAPL", "AAPL"},
		{"NSE:RELIANCE", "NSE:RELIANCE"},
	}
	for _, tt := range tests {
		got := StripUSPrefix(tt.input)
		if got != tt.want {
			t.Errorf("StripUSPrefix(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestIsUSTicker(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"US:AAPL", true},
		{"NYSE:MSFT", true},
		{"NASDAQ:GOOG", true},
		{"NSE:RELIANCE", false},
		{"BSE:500325", false},
		{"AAPL", false},
	}
	for _, tt := range tests {
		got := IsUSTicker(tt.input)
		if got != tt.want {
			t.Errorf("IsUSTicker(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

func TestMapRangeToPeriod(t *testing.T) {
	tests := []struct {
		rangeStr   string
		wantType   string
		wantPeriod string
	}{
		{"1mo", "month", "1"},
		{"3mo", "month", "3"},
		{"6mo", "month", "6"},
		{"1y", "year", "1"},
		{"2y", "year", "2"},
		{"5y", "year", "5"},
		{"10y", "year", "10"},
		{"max", "year", "20"},
		{"unknown", "year", "1"},
	}
	for _, tt := range tests {
		gotType, gotPeriod := mapRangeToPeriod(tt.rangeStr)
		if gotType != tt.wantType || gotPeriod != tt.wantPeriod {
			t.Errorf("mapRangeToPeriod(%q) = (%q, %q), want (%q, %q)",
				tt.rangeStr, gotType, gotPeriod, tt.wantType, tt.wantPeriod)
		}
	}
}

func TestMapSchwabFundamentals(t *testing.T) {
	f := &Fundamental{
		PegRatio:             1.5,
		ReturnOnEquity:       25.0, // 25% — Schwab returns percentage
		PeRatio:              20.0,
		OperatingMarginTTM:   30.0, // 30%
		PbRatio:              5.0,
		MarketCap:            2500000.0, // $2.5 trillion in millions
		Vol3MonthAvg:         50000000,
		FreeCashFlowPerShare: 6.0,
		SharesOutstanding:    15000000000,
		TotalDebtToEquity:    1.2,
		RevenueTTM:           380000000000,
	}

	result := mapSchwabFundamentals(f)

	if result.PEGRatio != 1.5 {
		t.Errorf("PEGRatio = %v, want 1.5", result.PEGRatio)
	}
	if result.ROE != 0.25 {
		t.Errorf("ROE = %v, want 0.25 (decimal)", result.ROE)
	}
	if result.OperatingMargins != 0.30 {
		t.Errorf("OperatingMargins = %v, want 0.30", result.OperatingMargins)
	}
	if result.MarketCap != 2500000.0*1_000_000 {
		t.Errorf("MarketCap = %v, want %v", result.MarketCap, 2500000.0*1_000_000)
	}
	expectedFCF := 6.0 * 15000000000
	if result.FreeCashflow != expectedFCF {
		t.Errorf("FreeCashflow = %v, want %v", result.FreeCashflow, expectedFCF)
	}
}

func TestFetchQuotesResponseParsing(t *testing.T) {
	// Test that QuoteResponse JSON decodes correctly
	raw := `{
		"AAPL": {
			"assetMainType": "EQUITY",
			"symbol": "AAPL",
			"quote": {
				"lastPrice": 185.50,
				"openPrice": 184.00,
				"highPrice": 186.20,
				"lowPrice": 183.80,
				"closePrice": 184.90,
				"totalVolume": 52000000,
				"mark": 185.45
			}
		},
		"MSFT": {
			"assetMainType": "EQUITY",
			"symbol": "MSFT",
			"quote": {
				"lastPrice": 378.25,
				"openPrice": 376.00,
				"highPrice": 379.00,
				"lowPrice": 375.50,
				"closePrice": 377.80,
				"totalVolume": 28000000
			}
		}
	}`

	var qr QuoteResponse
	if err := json.Unmarshal([]byte(raw), &qr); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if len(qr) != 2 {
		t.Fatalf("len = %d, want 2", len(qr))
	}

	aapl := qr["AAPL"]
	if aapl.Quote.LastPrice != 185.50 {
		t.Errorf("AAPL lastPrice = %v, want 185.50", aapl.Quote.LastPrice)
	}
	if aapl.Quote.TotalVolume != 52000000 {
		t.Errorf("AAPL volume = %d, want 52000000", aapl.Quote.TotalVolume)
	}

	msft := qr["MSFT"]
	if msft.Quote.LastPrice != 378.25 {
		t.Errorf("MSFT lastPrice = %v, want 378.25", msft.Quote.LastPrice)
	}
}

func TestCandleListParsing(t *testing.T) {
	raw := `{
		"symbol": "AAPL",
		"empty": false,
		"candles": [
			{"open": 184.0, "high": 185.0, "low": 183.5, "close": 184.5, "volume": 50000000, "datetime": 1721606400000},
			{"open": 184.5, "high": 186.0, "low": 184.0, "close": 185.5, "volume": 48000000, "datetime": 1721692800000}
		]
	}`

	var cl CandleList
	if err := json.Unmarshal([]byte(raw), &cl); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if cl.Symbol != "AAPL" {
		t.Errorf("Symbol = %q, want AAPL", cl.Symbol)
	}
	if cl.Empty {
		t.Error("Empty = true, want false")
	}
	if len(cl.Candles) != 2 {
		t.Fatalf("len(Candles) = %d, want 2", len(cl.Candles))
	}

	// Verify millisecond → second conversion logic
	expectedTS := int64(1721606400000 / 1000) // 1721606400
	if cl.Candles[0].Datetime/1000 != expectedTS {
		t.Errorf("candle[0] timestamp/1000 = %d, want %d", cl.Candles[0].Datetime/1000, expectedTS)
	}
}

func TestFetchHistoricalDataMock(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/pricehistory" {
			http.NotFound(w, r)
			return
		}
		json.NewEncoder(w).Encode(CandleList{
			Symbol: "AAPL",
			Empty:  false,
			Candles: []Candle{
				{Open: 180, High: 182, Low: 179, Close: 181, Volume: 50e6, Datetime: 1721606400000},
				{Open: 181, High: 184, Low: 180, Close: 183, Volume: 48e6, Datetime: 1721692800000},
				{Open: 183, High: 185, Low: 182, Close: 184.5, Volume: 52e6, Datetime: 1721779200000},
			},
		})
	})

	server := httptest.NewServer(handler)
	defer server.Close()

	// Build client that points to test server
	client := buildTestClientWithURL(t, server.URL)

	hist, err := client.FetchHistoricalDataWithTimestamps(context.Background(), "AAPL", "3mo")
	if err != nil {
		t.Fatalf("FetchHistoricalDataWithTimestamps: %v", err)
	}

	if len(hist.Closes) != 3 {
		t.Fatalf("len(Closes) = %d, want 3", len(hist.Closes))
	}
	if hist.Closes[2] != 184.5 {
		t.Errorf("Closes[2] = %v, want 184.5", hist.Closes[2])
	}
	// Timestamps should be in seconds (not milliseconds)
	if hist.Timestamps[0] != 1721606400 {
		t.Errorf("Timestamps[0] = %d, want 1721606400", hist.Timestamps[0])
	}
}

func TestFetchQuotesMock(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/quotes" {
			http.NotFound(w, r)
			return
		}
		json.NewEncoder(w).Encode(QuoteResponse{
			"AAPL": {Symbol: "AAPL", Quote: QuoteDetail{LastPrice: 185.50}},
			"MSFT": {Symbol: "MSFT", Quote: QuoteDetail{LastPrice: 378.25}},
		})
	})

	server := httptest.NewServer(handler)
	defer server.Close()

	client := buildTestClientWithURL(t, server.URL)

	prices, err := client.FetchQuotes(context.Background(), []string{"US:AAPL", "US:MSFT"})
	if err != nil {
		t.Fatalf("FetchQuotes: %v", err)
	}

	if prices["US:AAPL"] != 185.50 {
		t.Errorf("AAPL price = %v, want 185.50", prices["US:AAPL"])
	}
	if prices["US:MSFT"] != 378.25 {
		t.Errorf("MSFT price = %v, want 378.25", prices["US:MSFT"])
	}
}

// buildTestClientWithURL creates a Client whose market data requests go to the test server.
func buildTestClientWithURL(t *testing.T, baseURL string) *Client {
	t.Helper()

	dir := t.TempDir()
	tokenPath := dir + "/token.json"
	SaveToken(tokenPath, &Token{
		AccessToken:      "test_token",
		RefreshToken:     "test_refresh",
		ExpiresAt:        time.Now().Add(1 * time.Hour).Unix(),
		RefreshExpiresAt: time.Now().Add(7 * 24 * time.Hour).Unix(),
	})
	app := &AppConfig{ClientID: "test", ClientSecret: "test"}
	tm := NewTokenManager(app, tokenPath)

	client := NewClient(tm)
	client.SetMarketDataBase(baseURL)
	return client
}
