# syntax=docker/dockerfile:1

# ---- build stage ----
FROM golang:1.26-alpine AS build
WORKDIR /src

# Pull deps first so they cache across edits.
COPY go.mod go.sum ./
RUN go mod download

COPY . .

# CGO disabled → fully static binaries, safe to ship to a scratch image.
ENV CGO_ENABLED=0 GOOS=linux

RUN go build -trimpath -ldflags="-s -w" -o /out/api    ./cmd/api
RUN go build -trimpath -ldflags="-s -w" -o /out/worker ./cmd/worker

# ---- runtime stage ----
FROM alpine:3.20
WORKDIR /app

# ca-certificates so HTTPS (CoinGecko, APNs) works.
RUN apk add --no-cache ca-certificates && \
    addgroup -S app && adduser -S app -G app

COPY --from=build /out/api    /app/api
COPY --from=build /out/worker /app/worker
COPY migrations /app/migrations

USER app
EXPOSE 8080

# Default command runs the api; fly.toml's worker process group overrides it.
CMD ["/app/api"]
