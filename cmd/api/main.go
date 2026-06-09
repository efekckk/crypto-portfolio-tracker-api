package main

import (
	"context"
	"log"
	"net/http"
	"os"

	"github.com/efekckk/crypto-portfolio-tracker-api/internal/api"
	"github.com/efekckk/crypto-portfolio-tracker-api/internal/eval"
	"github.com/efekckk/crypto-portfolio-tracker-api/internal/storage"
	"github.com/efekckk/crypto-portfolio-tracker-api/internal/virtual"
)

func main() {
	ctx := context.Background()
	dsn := envOr("DATABASE_URL", "")
	if dsn == "" {
		log.Fatal("DATABASE_URL is required")
	}
	pg, err := storage.Open(ctx, dsn)
	if err != nil {
		log.Fatalf("open postgres: %v", err)
	}
	defer pg.Close()

	markets := eval.NewCoinGeckoMarketsClient(
		envOr("COINGECKO_BASE_URL", "https://api.coingecko.com/api/v3"),
		os.Getenv("COINGECKO_API_KEY"),
		nil,
	)
	pricing := virtual.NewPricingService(markets, "usd")

	srv := api.NewServer(
		storage.NewDeviceRepo(pg.Pool),
		storage.NewAlertRepo(pg.Pool),
		storage.NewHoldingRepo(pg.Pool),
		storage.NewVirtualPortfolioRepo(pg.Pool),
		storage.NewVirtualTradeRepo(pg.Pool),
		pricing,
	)

	addr := ":" + envOr("PORT", "8080")
	log.Printf("listening on %s", addr)
	if err := http.ListenAndServe(addr, srv); err != nil {
		log.Fatal(err)
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
