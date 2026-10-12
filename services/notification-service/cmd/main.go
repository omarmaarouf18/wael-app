// Command notification-service is the user inbox API: JWT-authenticated SSE
// stream, paginated list, mark-read, plus an internal push endpoint for
// trusted services (INTERNAL_SERVICE_TOKEN) with Redis pub/sub fan-out.
package main

import (
	"context"
	"crypto/tls"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/omarmaarouf18/wael-app/notification-service/internal/bus"
	"github.com/omarmaarouf18/wael-app/notification-service/internal/config"
	"github.com/omarmaarouf18/wael-app/notification-service/internal/handlers"
	"github.com/omarmaarouf18/wael-app/notification-service/internal/store"
	"github.com/omarmaarouf18/wael-app/shared/infra/handlerutil"
	"github.com/omarmaarouf18/wael-app/shared/infra/jwtutil"
	"github.com/omarmaarouf18/wael-app/shared/infra/ratelimit"
	"github.com/omarmaarouf18/wael-app/shared/infra/redact"
	"github.com/omarmaarouf18/wael-app/shared/infra/tlsutil"
	"github.com/redis/go-redis/v9"
)

func runCheckEnv(stdout, stderr io.Writer) int {
	if _, err := config.Load(); err != nil {
		fmt.Fprintf(stderr, "check-env: %v\n", err)
		return 1
	}
	fmt.Fprintln(stdout, "check-env: ok")
	return 0
}

func main() {
	checkEnv := flag.Bool("check-env", false, "validate environment variables and exit")
	flag.Parse()
	if *checkEnv {
		os.Exit(runCheckEnv(os.Stdout, os.Stderr))
	}

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("[NOTIF] %v", err)
	}
	dev := cfg.AppEnv == "local" || cfg.AppEnv == "test"

	jwtutil.Init(cfg.JWTSecret)

	ctx := context.Background()
	var st store.Store
	if cfg.MongoURI != "" {
		ms, err := store.NewMongoStore(ctx, cfg.MongoURI, cfg.MongoDatabase)
		if err != nil {
			log.Fatalf("[NOTIF] mongo (%s): %v", redact.RedactURI(cfg.MongoURI), err)
		}
		st = ms
		log.Printf("[NOTIF] active notification store: MongoDB (database: %s)", cfg.MongoDatabase)
	} else {
		if !dev {
			log.Fatalf("[NOTIF] memory store not permitted outside dev: MONGO_URI is required")
		}
		st = store.NewMemoryStore()
		log.Printf("[NOTIF] active notification store: in-process memory (localhost dev only)")
	}

	var b bus.Bus
	var rdb *redis.Client
	if cfg.RedisURI != "" {
		var err error
		rdb, err = ratelimit.NewRedisClient(cfg.RedisURI)
		if err != nil {
			log.Fatalf("[NOTIF] redis: %v (uri=%s)", err, redact.RedactURI(cfg.RedisURI))
		}
		defer func() { _ = rdb.Close() }()
		jwtutil.SetRedisClient(rdb)
		b = bus.NewRedisBus(rdb)
		log.Printf("[NOTIF] active notification bus: Redis (%s)", redact.RedactURI(cfg.RedisURI))
	} else {
		if !dev {
			log.Fatalf("[NOTIF] memory bus not permitted outside dev: REDIS_URI is required")
		}
		b = bus.NewMemoryBus()
		log.Printf("[NOTIF] active notification bus: in-process memory (localhost dev only)")
	}

	srv := handlers.New(st, b, cfg.GatewaySecret, cfg.InternalServiceToken)
	srv.Limiter = handlers.NewStreamLimiter(cfg.StreamMaxConcurrent, cfg.StreamOpenRateLimit, time.Minute)
	stopAccountListener := srv.StartAccountEventsListener(ctx)
	defer stopAccountListener()
	log.Printf("[NOTIF] stream caps: max concurrent=%d, open rate limit=%d/min", cfg.StreamMaxConcurrent, cfg.StreamOpenRateLimit)

	// Broadcast admin API (SPEC Phase 7, ADR-0013) on the internal admin
	// listener: never published, mTLS where configured.
	admin := &handlers.AdminServer{
		Store:           st,
		Bus:             b,
		InternalToken:   cfg.InternalServiceToken,
		AuthAdminURL:    cfg.AuthAdminURL,
		AcademyAdminURL: cfg.AcademyAdminURL,
		Limiter:         sendLimiter(rdb),
	}
	verifyClient, err := buildVerifyClient(cfg)
	if err != nil {
		log.Fatalf("[NOTIF] admin verify client: %v", err)
	}
	admin.VerifyClient = verifyClient
	admin.UpstreamClient = buildUpstreamClient(cfg)
	// Resume any broadcast left queued or sending by a previous process.
	admin.ResumeBroadcasts(ctx)
	go func() {
		if err := serveAdmin(cfg, admin.AdminHandler()); err != nil && err != http.ErrServerClosed {
			log.Fatalf("[NOTIF] admin listener: %v", err)
		}
	}()
	mux := http.NewServeMux()
	mux.HandleFunc("/health", srv.Health)
	mux.HandleFunc("/notifications/stream", srv.Stream)
	mux.HandleFunc("/notifications/list", srv.List)
	mux.HandleFunc("/notifications/read", srv.MarkRead)
	mux.Handle("/internal/push", srv.InternalAuth(http.HandlerFunc(srv.Push)))

	var handler http.Handler = mux
	handler = srv.GatewayAuth(handler)
	handler = handlerutil.MaxBytesMiddleware(1 << 20)(handler)

	addr := ":" + cfg.Port
	if cfg.TLSEnabled() {
		if cfg.TLSCAPath != "" {
			tlsCfg, err := tlsutil.LoadServerTLSConfig(cfg.TLSCertPath, cfg.TLSKeyPath, cfg.TLSCAPath)
			if err != nil {
				log.Fatalf("[NOTIF] server mTLS: %v", err)
			}
			httpSrv := &http.Server{Addr: addr, Handler: handler, ReadHeaderTimeout: 5 * time.Second, TLSConfig: tlsCfg}
			fmt.Printf("notification-service listening HTTPS+mTLS on %s\n", addr)
			log.Fatal(httpSrv.ListenAndServeTLS("", ""))
			return
		}
		if !dev {
			log.Fatalf("[NOTIF] server TLS without client CA not permitted outside dev: TLS_CA_PATH is required")
		}
		httpSrv := &http.Server{
			Addr: addr, Handler: handler, ReadHeaderTimeout: 5 * time.Second,
			TLSConfig: &tls.Config{MinVersion: tls.VersionTLS12},
		}
		fmt.Printf("notification-service listening HTTPS on %s\n", addr)
		log.Fatal(httpSrv.ListenAndServeTLS(cfg.TLSCertPath, cfg.TLSKeyPath))
		return
	}
	if !dev {
		log.Fatalf("[NOTIF] plain HTTP not permitted outside dev: TLS is required")
	}
	httpSrv := &http.Server{Addr: addr, Handler: handler, ReadHeaderTimeout: 5 * time.Second}
	fmt.Printf("notification-service listening HTTP on %s\n", addr)
	log.Fatal(httpSrv.ListenAndServe())
}

// sendLimiter caps broadcast sends per admin per hour: Redis-backed in
// production, in-process memory where Redis is absent (dev only; production
// config already refuses to start without REDIS_URI).
func sendLimiter(rdb *redis.Client) handlers.SendLimiter {
	if rdb != nil {
		rl := ratelimit.NewRateLimiter(rdb, handlers.BroadcastSendHourlyLimit, time.Hour, "broadcast-send")
		return handlers.NewRedisSendLimiter(rl.CheckAndRecord)
	}
	return handlers.NewMemorySendLimiter(handlers.BroadcastSendHourlyLimit, time.Hour)
}

// buildVerifyClient constructs the outgoing mTLS client used to verify admin
// tokens against auth-service POST /internal/admin/verify (3 s timeout, no
// caching so revocation is immediate). Outside dev, client certs are required
// (fail closed); in dev a plain client is accepted per configuration.
func buildVerifyClient(cfg *config.Config) (*http.Client, error) {
	if cfg.TLSCertPath != "" && cfg.TLSKeyPath != "" && cfg.TLSCAPath != "" {
		tlsCfg, err := tlsutil.LoadClientTLSConfig(cfg.TLSCertPath, cfg.TLSKeyPath, cfg.TLSCAPath)
		if err != nil {
			return nil, fmt.Errorf("verify mTLS: %w", err)
		}
		return &http.Client{
			Timeout:   3 * time.Second,
			Transport: &http.Transport{TLSClientConfig: tlsCfg},
		}, nil
	}
	if cfg.AppEnv != "local" && cfg.AppEnv != "test" {
		return nil, fmt.Errorf("verify client requires mTLS outside dev: TLS_CERT_PATH, TLS_KEY_PATH and TLS_CA_PATH are required")
	}
	return &http.Client{Timeout: 3 * time.Second}, nil
}

// buildUpstreamClient constructs the outgoing client for audience-resolution
// calls to auth-service and academy-service (10 s timeout). mTLS when client
// certs are configured, plain in dev.
func buildUpstreamClient(cfg *config.Config) *http.Client {
	if cfg.TLSCertPath != "" && cfg.TLSKeyPath != "" && cfg.TLSCAPath != "" {
		tlsCfg, err := tlsutil.LoadClientTLSConfig(cfg.TLSCertPath, cfg.TLSKeyPath, cfg.TLSCAPath)
		if err != nil {
			log.Fatalf("[NOTIF] upstream mTLS: %v", err)
		}
		return &http.Client{
			Timeout:   10 * time.Second,
			Transport: &http.Transport{TLSClientConfig: tlsCfg},
		}
	}
	return &http.Client{Timeout: 10 * time.Second}
}

// serveAdmin runs the internal admin listener (ADR-0008: internal network
// only, never published) following the same TLS/mTLS policy as the main
// listener: outside dev, HTTPS + mTLS with client CA verification is strictly
// required; in dev, plain HTTP, TLS or mTLS are accepted per configuration.
func serveAdmin(cfg *config.Config, handler http.Handler) error {
	dev := cfg.AppEnv == "local" || cfg.AppEnv == "test"
	addr := cfg.AdminListenAddr
	if addr == "" {
		addr = ":9003"
	}
	if cfg.TLSEnabled() {
		if cfg.TLSCAPath != "" {
			tlsCfg, err := tlsutil.LoadServerTLSConfig(cfg.TLSCertPath, cfg.TLSKeyPath, cfg.TLSCAPath)
			if err != nil {
				return fmt.Errorf("admin server mTLS: %w", err)
			}
			httpSrv := &http.Server{Addr: addr, Handler: handler, ReadHeaderTimeout: 5 * time.Second, TLSConfig: tlsCfg}
			fmt.Printf("notification-service admin listener HTTPS+mTLS on %s\n", addr)
			return httpSrv.ListenAndServeTLS("", "")
		}
		if !dev {
			return fmt.Errorf("admin server TLS without client CA not permitted outside dev: TLS_CA_PATH is required")
		}
		httpSrv := &http.Server{
			Addr: addr, Handler: handler, ReadHeaderTimeout: 5 * time.Second,
			TLSConfig: &tls.Config{MinVersion: tls.VersionTLS12},
		}
		fmt.Printf("notification-service admin listener HTTPS on %s\n", addr)
		return httpSrv.ListenAndServeTLS(cfg.TLSCertPath, cfg.TLSKeyPath)
	}
	if !dev {
		return fmt.Errorf("admin plain HTTP not permitted outside dev: TLS is required")
	}
	httpSrv := &http.Server{Addr: addr, Handler: handler, ReadHeaderTimeout: 5 * time.Second}
	fmt.Printf("notification-service admin listener HTTP on %s\n", addr)
	return httpSrv.ListenAndServe()
}
