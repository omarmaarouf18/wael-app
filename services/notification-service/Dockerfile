# ===========================================================================
# Multi-stage Dockerfile — Go Service (wael-app / notification-service)
# ===========================================================================

# ---- Development Stage ---------------------------------------------------
FROM golang:1.27.0-alpine AS dev

RUN apk add --no-cache git curl \
    && go install github.com/air-verse/air@v1.52.2

WORKDIR /app

COPY go.mod go.sum* ./
RUN go mod download 2>/dev/null || true

COPY . .

EXPOSE 3004

CMD ["air", "-c", ".air.toml"]

# ---- Build Stage ---------------------------------------------------------
FROM golang:1.27.0-alpine AS build

WORKDIR /app/services/notification-service

COPY shared/infra /app/shared/infra
COPY services/notification-service/go.mod services/notification-service/go.sum* ./
RUN go mod download 2>/dev/null || true

COPY services/notification-service/ .

RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /bin/service ./cmd/main.go

# ---- Production Stage ----------------------------------------------------
FROM alpine:3.24 AS prod

RUN apk --no-cache add ca-certificates curl \
    && addgroup -S appgroup && adduser -S appuser -G appgroup

COPY --from=build /bin/service /bin/service

EXPOSE 3004

HEALTHCHECK --interval=10s --timeout=5s --start-period=5s --retries=3 \
  CMD curl -fsS --cacert /app/certs/ca.crt --cert /app/certs/notification-service.crt --key /app/certs/notification-service.key https://localhost:3004/health || exit 1

USER appuser

ENTRYPOINT ["/bin/service"]
