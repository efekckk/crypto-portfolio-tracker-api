# crypto-portfolio-tracker-api

Backend for the Crypto Portfolio Tracker iOS app. Evaluates user-defined
crypto price alerts on a cron tick and delivers them via APNs push.

## Stack
- Go 1.22+
- chi (HTTP router)
- Postgres
- APNs HTTP/2
- Hosted on Fly.io

## Run locally
    go mod tidy
    go test ./...
    PORT=8080 go run ./cmd/api

Health check: `curl localhost:8080/health` → `{"status":"ok"}`

## Companion iOS app
[efekckk/crypto-portfolio-tracker](https://github.com/efekckk/crypto-portfolio-tracker)

## Deploy (Fly.io)

```bash
# 1. one-time bootstrap
flyctl auth login
flyctl launch --copy-config --name cryptoportfolio-alerts --region fra

# 2. attach Postgres
flyctl postgres create --name cryptoportfolio-db --region fra
flyctl postgres attach cryptoportfolio-db --app cryptoportfolio-alerts

# 3. APNs secrets (replace with your values)
flyctl secrets set \
  APNS_KEY_ID=ABCDE12345 \
  APNS_TEAM_ID=1A2B3C4D5E \
  APNS_BUNDLE_ID=com.foneria.cryptoportfolio \
  --app cryptoportfolio-alerts
flyctl secrets set APNS_AUTH_KEY="$(cat AuthKey_ABCDE12345.p8)" --app cryptoportfolio-alerts

# 4. CoinGecko Demo key (optional)
flyctl secrets set COINGECKO_API_KEY=your-demo-key --app cryptoportfolio-alerts

# 5. deploy both processes (api + worker)
flyctl deploy --app cryptoportfolio-alerts

# 6. tail logs
flyctl logs --app cryptoportfolio-alerts
```

The Dockerfile builds both `cmd/api` and `cmd/worker` into a single image; `fly.toml` runs them as separate process groups.
