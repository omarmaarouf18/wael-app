// Command notification-service is the user inbox API: JWT-authenticated SSE
// stream, paginated list, mark-read, plus an internal push endpoint for
// trusted services (INTERNAL_SERVICE_TOKEN) with Redis pub/sub fan-out.
package main

import (
	"context"
	"crypto/tls"
	"fmt"
	"log"
	"net/http"
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

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("[NOTIF] %v", err)
	}
	jwtutil.Init(cfg.JWTSecret)

	ctx := context.Background()
	var st store.Store = store.NewMemoryStore()
	if cfg.MongoURI != "" {
		ms, err := store.NewMongoStore(ctx, cfg.MongoURI, cfg.MongoDatabase)
		if err != nil {
			log.Fatalf("[NOTIF] mongo (%s): %v", redact.RedactURI(cfg.MongoURI), err)
		}
		st = ms
		log.Printf("[NOTIF] using MongoDB database %s", cfg.MongoDatabase)
	} else {
		log.Printf("[NOTIF] MONGO_URI empty: using in-process memory store (localhost dev only)")
	}

	var b bus.Bus = bus.NewMemoryBus()
	if cfg.RedisURI != "" {
		rdb, err := ratelimit.NewRedisClient(cfg.RedisURI)
		if err != nil {
			log.Fatalf("[NOTIF] redis: %v (uri=%s)", err, redact.RedactURI(cfg.RedisURI))
		}
		defer func() { _ = rdb.Close() }()
		b = bus.NewRedisBus(rdb)
	} else {
		log.Printf("[NOTIF] REDIS_URI empty: using in-process fan-out bus (localhost dev only)")
	}

	srv := handlers.New(st, b, cfg.GatewaySecret, cfg.InternalServiceToken)
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
		httpSrv := &http.Server{
			Addr: addr, Handler: handler, ReadHeaderTimeout: 5 * time.Second,
			TLSConfig: &tls.Config{MinVersion: tls.VersionTLS12},
		}
		fmt.Printf("notification-service listening HTTPS on %s\n", addr)
		log.Fatal(httpSrv.ListenAndServeTLS(cfg.TLSCertPath, cfg.TLSKeyPath))
		return
	}
	httpSrv := &http.Server{Addr: addr, Handler: handler, ReadHeaderTimeout: 5 * time.Second}
	fmt.Printf("notification-service listening HTTP on %s\n", addr)
	log.Fatal(httpSrv.ListenAndServe())
}
