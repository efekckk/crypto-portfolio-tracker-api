// Package eval owns the cron-side evaluation pipeline: fetching markets,
// running alert conditions, and producing firing decisions.
package eval

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/efekckk/crypto-portfolio-tracker-api/internal/domain"
)

// MarketsClient fetches a market snapshot for a list of coin ids. The cron
// tick calls this once per pass; tests mock it via a fake implementation.
type MarketsClient interface {
	FetchMarkets(ctx context.Context, ids []string, vsCurrency string) ([]domain.Coin, error)
}

// CoinGeckoMarketsClient calls the public `/coins/markets` endpoint.
type CoinGeckoMarketsClient struct {
	baseURL    string
	apiKey     string
	httpClient *http.Client
}

// NewCoinGeckoMarketsClient builds a client against the given base URL
// (without a trailing slash, e.g. "https://api.coingecko.com/api/v3").
// apiKey is the CoinGecko Demo key; empty string means anonymous tier.
// httpClient is optional — pass nil to use a sensible default.
func NewCoinGeckoMarketsClient(baseURL, apiKey string, httpClient *http.Client) *CoinGeckoMarketsClient {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 15 * time.Second}
	}
	return &CoinGeckoMarketsClient{
		baseURL:    strings.TrimRight(baseURL, "/"),
		apiKey:     apiKey,
		httpClient: httpClient,
	}
}

// FetchMarkets calls /coins/markets and maps the response into domain coins.
// An empty `ids` slice returns an empty result without making a request.
func (c *CoinGeckoMarketsClient) FetchMarkets(ctx context.Context, ids []string, vsCurrency string) ([]domain.Coin, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	if vsCurrency == "" {
		return nil, errors.New("eval: vsCurrency is empty")
	}
	q := url.Values{}
	q.Set("vs_currency", vsCurrency)
	q.Set("ids", strings.Join(ids, ","))
	q.Set("price_change_percentage", "24h,7d,30d")

	endpoint := c.baseURL + "/coins/markets?" + q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("eval: build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	if c.apiKey != "" {
		req.Header.Set("x-cg-demo-api-key", c.apiKey)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("eval: markets request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("eval: markets returned %s: %s", resp.Status, string(body))
	}

	var raw []marketsCoinDTO
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("eval: decode markets: %w", err)
	}

	out := make([]domain.Coin, 0, len(raw))
	for _, r := range raw {
		out = append(out, domain.Coin{
			ID:                       r.ID,
			Symbol:                   r.Symbol,
			Name:                     r.Name,
			CurrentPrice:             r.CurrentPrice,
			PriceChangePercentage24h: r.PriceChangePercentage24h,
			PriceChangePercentage7d:  r.PriceChangePercentage7d,
			PriceChangePercentage30d: r.PriceChangePercentage30d,
		})
	}
	return out, nil
}

// marketsCoinDTO is the wire shape of a single coin row from
// /coins/markets. Unknown fields are tolerated.
type marketsCoinDTO struct {
	ID                       string   `json:"id"`
	Symbol                   string   `json:"symbol"`
	Name                     string   `json:"name"`
	CurrentPrice             float64  `json:"current_price"`
	PriceChangePercentage24h *float64 `json:"price_change_percentage_24h_in_currency,omitempty"`
	PriceChangePercentage7d  *float64 `json:"price_change_percentage_7d_in_currency,omitempty"`
	PriceChangePercentage30d *float64 `json:"price_change_percentage_30d_in_currency,omitempty"`
}
