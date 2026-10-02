# ===========================================================================
# Multi-stage Dockerfile — Go Service (wael-app / auth-service)
# ===========================================================================

# ---- Development Stage ---------------------------------------------------
FROM golang:1.26.6-alpine AS dev

RUN apk add --no-cache git curl \
    && go install github.com/air-verse/air@v1.52.2

WORKDIR /app

COPY go.mod go.sum* ./
RUN go mod download 2>/dev/null || true

COPY . .

EXPOSE 3002

CMD ["air", "-c", ".air.toml"]

# ---- Build Stage ---------------------------------------------------------
FROM golang:1.26.6-alpine AS build

WORKDIR /app/services/auth-service

COPY shared/infra /app/shared/infra
COPY services/auth-service/go.mod services/auth-service/go.sum* ./
RUN go mod download 2>/dev/null || true

COPY services/auth-service/ .

RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /bin/service ./cmd/main.go && \
    CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /bin/onboard-admin ./cmd/onboard-admin/main.go && \
    CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /bin/revoke-admin ./cmd/revoke-admin/main.go

# ---- Production Stage ----------------------------------------------------
FROM alpine:3.20 AS prod

RUN apk --no-cache add ca-certificates curl \
    && addgroup -S appgroup && adduser -S appuser -G appgroup

COPY --from=build /bin/service /bin/service
COPY --from=build /bin/onboard-admin /bin/onboard-admin
COPY --from=build /bin/revoke-admin /bin/revoke-admin

EXPOSE 3002

HEALTHCHECK --interval=10s --timeout=5s --start-period=5s --retries=3 \
  CMD curl -f -s -k --cert /app/certs/auth-service.crt --key /app/certs/auth-service.key https://localhost:3002/health || curl -f -s -k https://localhost:3002/health || curl -f -s http://localhost:3002/health || exit 1

USER appuser

ENTRYPOINT ["/bin/service"]
