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
