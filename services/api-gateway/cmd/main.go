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
		srv := &http.Server{
			Addr:              addr,
			Handler:           handler,
			ReadHeaderTimeout: 5 * time.Second,
			TLSConfig:         &tls.Config{MinVersion: tls.VersionTLS12},
		}
		fmt.Printf("api-gateway listening HTTPS on %s\n", addr)
		log.Fatal(srv.ListenAndServeTLS(certFile, keyFile))
		return
	}
	if !dev {
		log.Fatalf("[GATEWAY] plain HTTP not permitted outside dev")
	}
	srv := &http.Server{Addr: addr, Handler: handler, ReadHeaderTimeout: 5 * time.Second}
	fmt.Printf("api-gateway listening HTTP on %s\n", addr)
	log.Fatal(srv.ListenAndServe())
}
