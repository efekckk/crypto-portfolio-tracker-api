package eval

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const marketsJSON = `[
  {
    "id": "bitcoin",
    "symbol": "btc",
    "name": "Bitcoin",
    "current_price": 75000,
    "price_change_percentage_24h_in_currency": 1.2,
    "price_change_percentage_7d_in_currency": -3.4,
    "price_change_percentage_30d_in_currency": 12.5
  },
  {
    "id": "ethereum",
    "symbol": "eth",
    "name": "Ethereum",
    "current_price": 4000
  }
]`

func TestFetchMarkets_RequestShape(t *testing.T) {
	var captured *http.Request
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured = r
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(marketsJSON))
	}))
	defer srv.Close()

	c := NewCoinGeckoMarketsClient(srv.URL, "secret-key", srv.Client())
	_, err := c.FetchMarkets(context.Background(), []string{"bitcoin", "ethereum"}, "usd")
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}

	if captured.URL.Path != "/coins/markets" {
		t.Fatalf("path: %s", captured.URL.Path)
	}
	q := captured.URL.Query()
	if q.Get("vs_currency") != "usd" {
		t.Fatalf("vs_currency: %s", q.Get("vs_currency"))
	}
	if q.Get("ids") != "bitcoin,ethereum" {
		t.Fatalf("ids: %s", q.Get("ids"))
	}
	if q.Get("price_change_percentage") != "24h,7d,30d" {
		t.Fatalf("price_change_percentage: %s", q.Get("price_change_percentage"))
	}
	if captured.Header.Get("x-cg-demo-api-key") != "secret-key" {
		t.Fatalf("missing api key header: %v", captured.Header)
	}
}

func TestFetchMarkets_OmitsAPIKeyHeader_WhenUnset(t *testing.T) {
	var captured *http.Request
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured = r
		_, _ = w.Write([]byte(marketsJSON))
	}))
	defer srv.Close()

	c := NewCoinGeckoMarketsClient(srv.URL, "", srv.Client())
	_, err := c.FetchMarkets(context.Background(), []string{"bitcoin"}, "usd")
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if got := captured.Header.Get("x-cg-demo-api-key"); got != "" {
		t.Fatalf("expected no api key header, got %q", got)
	}
}

func TestFetchMarkets_DecodesAllFields(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(marketsJSON))
	}))
	defer srv.Close()

	c := NewCoinGeckoMarketsClient(srv.URL, "", srv.Client())
	coins, err := c.FetchMarkets(context.Background(), []string{"bitcoin", "ethereum"}, "usd")
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if len(coins) != 2 {
		t.Fatalf("expected 2 coins, got %d", len(coins))
	}

	btc := coins[0]
	if btc.ID != "bitcoin" || btc.Symbol != "btc" || btc.Name != "Bitcoin" || btc.CurrentPrice != 75000 {
		t.Fatalf("btc fields: %+v", btc)
	}
	if btc.PriceChangePercentage24h == nil || *btc.PriceChangePercentage24h != 1.2 {
		t.Fatalf("btc 24h: %v", btc.PriceChangePercentage24h)
	}
	if btc.PriceChangePercentage7d == nil || *btc.PriceChangePercentage7d != -3.4 {
		t.Fatalf("btc 7d: %v", btc.PriceChangePercentage7d)
	}
	if btc.PriceChangePercentage30d == nil || *btc.PriceChangePercentage30d != 12.5 {
		t.Fatalf("btc 30d: %v", btc.PriceChangePercentage30d)
	}

	eth := coins[1]
	if eth.PriceChangePercentage24h != nil {
		t.Fatalf("eth 24h should be nil (missing in response), got %v", eth.PriceChangePercentage24h)
	}
}

func TestFetchMarkets_EmptyIDsReturnsEmpty(t *testing.T) {
	// No server needed — should short-circuit.
	c := NewCoinGeckoMarketsClient("http://unreachable.invalid", "", http.DefaultClient)
	coins, err := c.FetchMarkets(context.Background(), nil, "usd")
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if len(coins) != 0 {
		t.Fatalf("expected empty, got %d", len(coins))
	}
}

func TestFetchMarkets_NonOKStatus_ReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"status":{"error_code":429,"error_message":"slow down"}}`))
	}))
	defer srv.Close()

	c := NewCoinGeckoMarketsClient(srv.URL, "", srv.Client())
	_, err := c.FetchMarkets(context.Background(), []string{"btc"}, "usd")
	if err == nil {
		t.Fatal("expected error on 429")
	}
	if !strings.Contains(err.Error(), "429") {
		t.Fatalf("expected status code in error, got: %v", err)
	}
}

func TestFetchMarkets_RejectsEmptyVsCurrency(t *testing.T) {
	c := NewCoinGeckoMarketsClient("http://example.test", "", http.DefaultClient)
	_, err := c.FetchMarkets(context.Background(), []string{"btc"}, "")
	if err == nil {
		t.Fatal("expected error for empty vsCurrency")
	}
}

// Sanity check: MarketsClient interface is satisfied by the concrete type.
func TestCoinGeckoClient_ImplementsMarketsClient(t *testing.T) {
	var _ MarketsClient = (*CoinGeckoMarketsClient)(nil)
}
