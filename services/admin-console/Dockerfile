# ===========================================================================
# Multi-stage Dockerfile — Go Service (wael-app / admin-console)
# ===========================================================================
# dev:  live reload, built from this directory (docker compose, target dev).
# prod: built from the repository root (the publish workflow passes
#       -f services/admin-console/Dockerfile with the root as context).
# The static pages in web/ are embedded in the binary (go:embed).

# ---- Development Stage ---------------------------------------------------
FROM golang:1.26.6-alpine AS dev

RUN apk add --no-cache git curl \
    && go install github.com/air-verse/air@v1.52.2

WORKDIR /app

COPY go.mod go.sum* ./
RUN go mod download 2>/dev/null || true

COPY . .

EXPOSE 3005

CMD ["air", "-c", ".air.toml"]

# ---- Build Stage ---------------------------------------------------------
FROM golang:1.26.6-alpine AS build

WORKDIR /app/services/admin-console

COPY shared/infra /app/shared/infra
COPY services/admin-console/go.mod services/admin-console/go.sum* ./
RUN go mod download 2>/dev/null || true

COPY services/admin-console/ .

RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /bin/service ./cmd

# ---- Production Stage ----------------------------------------------------
FROM alpine:3.20 AS prod

RUN apk --no-cache add ca-certificates curl \
    && addgroup -S appgroup && adduser -S appuser -G appgroup

COPY --from=build /bin/service /bin/service

EXPOSE 3005

# Strict probe: verifies the local CA, no -k, no HTTP fallback (W-08).
HEALTHCHECK --interval=10s --timeout=5s --start-period=5s --retries=3 \
  CMD curl -fsS --cacert /app/certs/ca.crt https://localhost:3005/healthz || exit 1

USER appuser

ENTRYPOINT ["/bin/service"]
