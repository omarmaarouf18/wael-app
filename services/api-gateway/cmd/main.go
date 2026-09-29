// Command api-gateway is the edge reverse proxy (rate limit, safe headers, mTLS to backends).
package main

import (
	"crypto/tls"
	"fmt"
	"log"
	"net/http"
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

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("[GATEWAY] %v (redis=%s)", err, redact.RedactURI("<see REDIS_URI>"))
	}
	log.Printf("[GATEWAY] domain=%s tls=%t mtls-client=%t", cfg.AppDomain, cfg.TLSEnabled(), cfg.MTLSClientEnabled())

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
	}
	transport := resilience.NewRoundTripper(baseTransport, "api-gateway", 2, 2*time.Second)

	rl := middleware.NewRateLimiter(ratelimit.NewRateLimiter(rdb, 100, time.Minute, "gateway"), cfg.TrustedProxyIPs)

	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		handlerutil.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	for _, route := range cfg.Routes {
		h, err := proxy.New(route, cfg.GatewaySecret, cfg.InternalServiceToken, cfg.TrustedProxyIPs, transport)
		if err != nil {
			log.Fatalf("[GATEWAY] route %s: %v", route.Prefix, err)
		}
		mux.Handle(route.Prefix, h)
		log.Printf("[GATEWAY] route %s -> %s", route.Prefix, route.Target)
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
	srv := &http.Server{Addr: addr, Handler: handler, ReadHeaderTimeout: 5 * time.Second}
	fmt.Printf("api-gateway listening HTTP on %s\n", addr)
	log.Fatal(srv.ListenAndServe())
}
