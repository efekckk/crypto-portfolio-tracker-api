package main

import (
	"context"
	"log"
	"net/http"
	"os"

	"github.com/efekckk/crypto-portfolio-tracker-api/internal/api"
	"github.com/efekckk/crypto-portfolio-tracker-api/internal/storage"
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

	srv := api.NewServer(
		storage.NewDeviceRepo(pg.Pool),
		storage.NewAlertRepo(pg.Pool),
		storage.NewHoldingRepo(pg.Pool),
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
