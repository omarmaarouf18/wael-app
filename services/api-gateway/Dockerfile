# ===========================================================================
# Multi-stage Dockerfile — Go Service (wael-app / api-gateway)
# ===========================================================================
# Stages:
#   dev   → live-reload via air (used by docker-compose)
#   build → compile static binary
#   prod  → minimal image (used by GHCR)
# ===========================================================================

# ---- Development Stage ---------------------------------------------------
FROM golang:1.27.0-alpine AS dev

RUN apk add --no-cache git \
    && go install github.com/air-verse/air@v1.52.2

WORKDIR /app

COPY go.mod go.sum* ./
RUN go mod download 2>/dev/null || true

COPY . .

EXPOSE 8080

CMD ["air", "-c", ".air.toml"]

# ---- Build Stage ---------------------------------------------------------
FROM golang:1.27.0-alpine AS build

WORKDIR /app/services/api-gateway

COPY shared/infra /app/shared/infra
COPY services/api-gateway/go.mod services/api-gateway/go.sum* ./
RUN go mod download 2>/dev/null || true

COPY services/api-gateway/ .

RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /bin/service ./cmd/main.go

# ---- Production Stage ----------------------------------------------------
FROM alpine:3.24 AS prod

RUN apk --no-cache add ca-certificates curl \
    && addgroup -S appgroup && adduser -S appuser -G appgroup

COPY --from=build /bin/service /bin/service

EXPOSE 8080

HEALTHCHECK --interval=10s --timeout=5s --start-period=5s --retries=3 \
  CMD curl -fsS --cacert /app/certs/ca.crt https://localhost:8080/health || exit 1

USER appuser

ENTRYPOINT ["/bin/service"]
