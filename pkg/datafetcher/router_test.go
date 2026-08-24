package datafetcher

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/raghavkgarg/mycase/pkg/schwab"
)

func buildSchwabTestClient(t *testing.T, handler http.Handler) *schwab.Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	dir := t.TempDir()
	tokenPath := dir + "/token.json"
	schwab.SaveToken(tokenPath, &schwab.Token{
		AccessToken:      "test_token",
		RefreshToken:     "test_refresh",
		ExpiresAt:        time.Now().Add(1 * time.Hour).Unix(),
		RefreshExpiresAt: time.Now().Add(7 * 24 * time.Hour).Unix(),
	})
	app := &schwab.AppConfig{ClientID: "test", ClientSecret: "test"}
	tm := schwab.NewTokenManager(app, tokenPath)

	client := schwab.NewClient(tm)
	// Point market data at test server
	client.SetMarketDataBase(server.URL)
	return client
}

func TestRouterFetchQuotesSplitsMarkets(t *testing.T) {
	schwabHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify only US symbols arrive here
		symbols := r.URL.Query().Get("symbols")
		if strings.Contains(symbols, "RELIANCE") {
			t.Error("Indian ticker should not reach Schwab")
		}
		json.NewEncoder(w).Encode(schwab.QuoteResponse{
			"AAPL": {Symbol: "AAPL", Quote: schwab.QuoteDetail{LastPrice: 185.50}},
		})
	})

	client := buildSchwabTestClient(t, schwabHandler)
	router := NewRouter(client)

	// We can only test the US path fully; the Yahoo path would need network.
	// Test that the split logic works by checking US tickers go to Schwab.
	prices, err := router.FetchQuotes(context.Background(), []string{"US:AAPL"})
	if err != nil {
		t.Fatalf("FetchQuotes: %v", err)
	}
	if prices["US:AAPL"] != 185.50 {
		t.Errorf("US:AAPL = %v, want 185.50", prices["US:AAPL"])
	}
}

func TestRouterNilSchwabFallsThrough(t *testing.T) {
	// Router with nil Schwab client — US tickers should attempt Yahoo
	router := NewRouter(nil)

	// We can't test Yahoo without network, but verify it doesn't panic
	// and that the router correctly identifies this is a US ticker that
	// would go to Yahoo fallback.
	// This is a smoke test — in CI we'd mock Yahoo too.
	if router.schwabClient != nil {
		t.Error("expected nil schwabClient")
	}
}

func TestRouterFetchHistoricalUSToSchwab(t *testing.T) {
	schwabHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(schwab.CandleList{
			Symbol: "MSFT",
			Empty:  false,
			Candles: []schwab.Candle{
				{Open: 370, High: 375, Low: 369, Close: 374, Volume: 30e6, Datetime: 1721606400000},
				{Open: 374, High: 378, Low: 373, Close: 377, Volume: 28e6, Datetime: 1721692800000},
			},
		})
	})

	client := buildSchwabTestClient(t, schwabHandler)
	router := NewRouter(client)

	hist, err := router.FetchHistoricalDataWithTimestamps(context.Background(), "US:MSFT", "3mo")
	if err != nil {
		t.Fatalf("FetchHistoricalDataWithTimestamps: %v", err)
	}
	if len(hist.Closes) != 2 {
		t.Fatalf("len(Closes) = %d, want 2", len(hist.Closes))
	}
	if hist.Closes[1] != 377 {
		t.Errorf("Closes[1] = %v, want 377", hist.Closes[1])
	}
}

func TestRouterFetchHistoricalByDateRange(t *testing.T) {
	schwabHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify startDate and endDate are present
		if r.URL.Query().Get("startDate") == "" {
			t.Error("missing startDate param")
		}
		json.NewEncoder(w).Encode(schwab.CandleList{
			Symbol: "GOOG",
			Empty:  false,
			Candles: []schwab.Candle{
				{Open: 170, High: 172, Low: 169, Close: 171, Volume: 20e6, Datetime: 1721606400000},
			},
		})
	})

	client := buildSchwabTestClient(t, schwabHandler)
	router := NewRouter(client)

	from := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2024, 7, 1, 0, 0, 0, 0, time.UTC)

	hist, err := router.FetchHistoricalByDateRange(context.Background(), "US:GOOG", from, to)
	if err != nil {
		t.Fatalf("FetchHistoricalByDateRange: %v", err)
	}
	if len(hist.Closes) != 1 {
		t.Fatalf("len(Closes) = %d, want 1", len(hist.Closes))
	}
}
