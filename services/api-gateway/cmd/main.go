// Command api-gateway is the edge reverse proxy (rate limit, safe headers, mTLS to backends).
package main

import (
	"crypto/tls"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/omarmaarouf18/wael-app/api-gateway/internal/config"
	"github.com/omarmaarouf18/wael-app/api-gateway/internal/middleware"
	"github.com/omarmaarouf18/wael-app/api-gateway/internal/proxy"
	"github.com/omarmaarouf18/wael-app/shared/infra/handlerutil"
	"github.com/omarmaarouf18/wael-app/shared/infra/ratelimit"
	"github.com/omarmaarouf18/wael-app/shared/infra/redact"
	"github.com/omarmaarouf18/wael-app/shared/infra/resilience"
	"github.com/omarmaarouf18/wael-app/shared/infra/tlsutil"
)

func runCheckEnv(stdout, stderr io.Writer) int {
	if _, err := config.Load(); err != nil {
		fmt.Fprintf(stderr, "check-env: %v\n", err)
		return 1
	}
	fmt.Fprintln(stdout, "check-env: ok")
	return 0
}

// breakerName is the circuit breaker (and stats) name of one upstream.
func breakerName(route config.ServiceRoute) string {
	return "api-gateway-" + route.Name
}

// newMux builds the gateway routes. Every upstream gets its own resilience
// round tripper and therefore its own circuit breaker, so a failing upstream
// (for example academy answering 5xx) opens only its own breaker and never
// blocks the others (login keeps working). Retries stay limited to GET and
// HEAD inside the round tripper; POST and other methods are never retried.
func newMux(cfg *config.Config, base http.RoundTripper) (*http.ServeMux, error) {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		handlerutil.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	for _, route := range cfg.Routes {
		transport := resilience.NewRoundTripper(base, breakerName(route), 2, 2*time.Second)
		h, err := proxy.New(route, cfg.GatewaySecret, cfg.TrustedProxyIPs, transport)
		if err != nil {
			return nil, fmt.Errorf("route %s: %w", route.Prefix, err)
		}
		mux.Handle(route.Prefix, h)
		log.Printf("[GATEWAY] route %s -> %s (breaker %s)", route.Prefix, route.Target, breakerName(route))
	}
	return mux, nil
}

// serverTimeouts are the gateway's inbound connection limits (review P2).
type serverTimeouts struct {
	readHeader time.Duration // request line and headers (slowloris)
	read       time.Duration // whole request including the (1 MiB max) body
	idle       time.Duration // keep-alive connection between requests
}

// defaultServerTimeouts: WriteTimeout is deliberately left at 0 (none). A
// write timeout covers the whole response, so it would cut the SSE stream
// (/api/v1/notifications/stream, open for up to the token lifetime) and
// long downloads. The notification service already bounds each stream
// write with a per-write deadline. ReadTimeout only bounds reading the
// request; it does not end a streaming response (TestNewServer_
// StreamOutlivesReadTimeout covers HTTP/1.1 and HTTP/2).
var defaultServerTimeouts = serverTimeouts{
	readHeader: 5 * time.Second,
	read:       30 * time.Second,
	idle:       120 * time.Second,
}

func newServer(addr string, handler http.Handler, to serverTimeouts) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: to.readHeader,
		ReadTimeout:       to.read,
		IdleTimeout:       to.idle,
	}
}

func main() {
	checkEnv := flag.Bool("check-env", false, "validate environment variables and exit")
	flag.Parse()
	if *checkEnv {
		os.Exit(runCheckEnv(os.Stdout, os.Stderr))
	}

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("[GATEWAY] %v (redis=%s)", err, redact.RedactURI("<see REDIS_URI>"))
	}
	dev := cfg.AppEnv == "local" || cfg.AppEnv == "test"
	log.Printf("[GATEWAY] env=%s domain=%s tls=%t mtls-client=%t", cfg.AppEnv, cfg.AppDomain, cfg.TLSEnabled(), cfg.MTLSClientEnabled())

	rdb, err := ratelimit.NewRedisClient(cfg.RedisURI)
	if err != nil {
		log.Fatalf("[GATEWAY] redis unreachable: %v (uri=%s)", err, redact.RedactURI(cfg.RedisURI))
	}
	defer func() { _ = rdb.Close() }()

	baseTransport := http.DefaultTransport
	if cfg.MTLSClientEnabled() {
		tlsCfg, err := tlsutil.LoadClientTLSConfig(cfg.TLSCertPath, cfg.TLSKeyPath, cfg.TLSCAPath)
		if err != nil {
			log.Fatalf("[GATEWAY] mTLS client config: %v", err)
		}
		baseTransport = &http.Transport{TLSClientConfig: tlsCfg}
	} else if !dev {
		log.Fatalf("[GATEWAY] mTLS client config required outside dev")
	}

	rl := middleware.NewRateLimiter(ratelimit.NewRateLimiter(rdb, 100, time.Minute, "gateway"), cfg.TrustedProxyIPs)
	log.Printf("[GATEWAY] active rate limiter: Redis (%s)", redact.RedactURI(cfg.RedisURI))

	mux, err := newMux(cfg, baseTransport)
	if err != nil {
		log.Fatalf("[GATEWAY] %v", err)
	}

	// 1 MiB caps REQUEST bodies only. File uploads never pass the gateway:
	// students upload nothing (SPEC Section 1 decision 1) and admin calls go
	// admin-console -> academy admin listener (/internal/admin/*, ADR-0008),
	// which the gateway does not route. Response bodies are never capped or
	// buffered here: SSE streams and PDF downloads stream through unchanged.
	handler := handlerutil.MaxBytesMiddleware(1 << 20)(mux)
	handler = middleware.RateLimit(rl)(handler)
	handler = middleware.Logging(cfg.AllowedOrigin)(handler)

	addr := ":" + cfg.Port
	if cfg.TLSEnabled() {
		certFile := cfg.ExternalTLSCertPath
		keyFile := cfg.ExternalTLSKeyPath
		if certFile == "" || keyFile == "" {
			certFile = cfg.TLSCertPath
			keyFile = cfg.TLSKeyPath
		}
		srv := newServer(addr, handler, defaultServerTimeouts)
		srv.TLSConfig = &tls.Config{MinVersion: tls.VersionTLS12}
		fmt.Printf("api-gateway listening HTTPS on %s\n", addr)
		log.Fatal(srv.ListenAndServeTLS(certFile, keyFile))
		return
	}
	if !dev {
		log.Fatalf("[GATEWAY] plain HTTP not permitted outside dev")
	}
	srv := newServer(addr, handler, defaultServerTimeouts)
	fmt.Printf("api-gateway listening HTTP on %s\n", addr)
	log.Fatal(srv.ListenAndServe())
}
