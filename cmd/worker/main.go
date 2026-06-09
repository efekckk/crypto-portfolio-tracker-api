package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"github.com/efekckk/crypto-portfolio-tracker-api/internal/eval"
	"github.com/efekckk/crypto-portfolio-tracker-api/internal/push"
	"github.com/efekckk/crypto-portfolio-tracker-api/internal/storage"
	"github.com/efekckk/crypto-portfolio-tracker-api/internal/worker"
)

func main() {
	cfg := loadConfig()

	pg, err := storage.Open(context.Background(), cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("open postgres: %v", err)
	}
	defer pg.Close()

	if cfg.MigrationsPath != "" {
		if err := storage.Migrate(cfg.DatabaseURL, "file://"+cfg.MigrationsPath); err != nil {
			log.Fatalf("migrate: %v", err)
		}
	}

	markets := eval.NewCoinGeckoMarketsClient(cfg.CoinGeckoBaseURL, cfg.CoinGeckoAPIKey, nil)
	alerts := storage.NewAlertRepo(pg.Pool)
	holdings := storage.NewHoldingRepo(pg.Pool)
	firings := storage.NewFiringRepo(pg.Pool)
	devices := storage.NewDeviceRepo(pg.Pool)

	pusher, err := push.NewAPNsPusher(
		[]byte(cfg.APNsAuthKey),
		cfg.APNsKeyID, cfg.APNsTeamID, cfg.APNsBundleID,
	)
	if err != nil {
		log.Fatalf("apns init: %v", err)
	}

	runner := &worker.Runner{
		Evaluator: &eval.Evaluator{Alerts: alerts, Holdings: holdings, Markets: markets, Currency: cfg.Currency},
		Firings:   firings,
		Devices:   devices,
		Pusher:    pusher,
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	interval := time.Duration(cfg.IntervalSeconds) * time.Second
	log.Printf("worker starting; interval=%s currency=%s", interval, cfg.Currency)

	// Run an immediate first tick so the worker doesn't sit idle for `interval`
	// seconds on boot.
	runTick(ctx, runner)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			log.Println("worker shutting down")
			return
		case <-ticker.C:
			runTick(ctx, runner)
		}
	}
}

func runTick(ctx context.Context, r *worker.Runner) {
	tickCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	count, err := r.Tick(tickCtx, time.Now())
	if err != nil {
		log.Printf("tick error: %v", err)
		return
	}
	if count > 0 {
		log.Printf("tick fired %d alerts", count)
	}
}

// config bundles all environment-driven parameters.
type config struct {
	DatabaseURL      string
	CoinGeckoBaseURL string
	CoinGeckoAPIKey  string
	APNsAuthKey      string
	APNsKeyID        string
	APNsTeamID       string
	APNsBundleID     string
	Currency         string
	IntervalSeconds  int
	MigrationsPath   string
}

func loadConfig() config {
	c := config{
		DatabaseURL:      mustEnv("DATABASE_URL"),
		CoinGeckoBaseURL: envOr("COINGECKO_BASE_URL", "https://api.coingecko.com/api/v3"),
		CoinGeckoAPIKey:  os.Getenv("COINGECKO_API_KEY"),
		APNsAuthKey:      mustEnv("APNS_AUTH_KEY"),
		APNsKeyID:        mustEnv("APNS_KEY_ID"),
		APNsTeamID:       mustEnv("APNS_TEAM_ID"),
		APNsBundleID:     mustEnv("APNS_BUNDLE_ID"),
		Currency:         envOr("WORKER_CURRENCY", "usd"),
		IntervalSeconds:  envOrInt("WORKER_INTERVAL_SECONDS", 60),
		MigrationsPath:   resolveMigrationsPath(),
	}
	return c
}

func resolveMigrationsPath() string {
	if v := os.Getenv("MIGRATIONS_PATH"); v != "" {
		abs, err := filepath.Abs(v)
		if err == nil {
			return abs
		}
	}
	return ""
}

func mustEnv(k string) string {
	v := os.Getenv(k)
	if v == "" {
		log.Fatalf("%s is required", k)
	}
	return v
}

func envOr(k, fallback string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return fallback
}

func envOrInt(k string, fallback int) int {
	if v := os.Getenv(k); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return fallback
}
