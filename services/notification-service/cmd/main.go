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
	if cfg.RedisURI != "" {
		rdb, err := ratelimit.NewRedisClient(cfg.RedisURI)
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
	log.Printf("[NOTIF] stream caps: max concurrent=%d, open rate limit=%d/min", cfg.StreamMaxConcurrent, cfg.StreamOpenRateLimit)
	mux := http.NewServeMux()
	mux.HandleFunc("/health", handlers.Health)
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
