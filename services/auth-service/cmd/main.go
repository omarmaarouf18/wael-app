// Command auth-service is the authentication API: signup, login, OTP,
// JWT refresh, and two-phase password reset.
package main

import (
	"context"
	"crypto/tls"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/omarmaarouf18/wael-app/auth-service/internal/config"
	"github.com/omarmaarouf18/wael-app/auth-service/internal/handlers"
	"github.com/omarmaarouf18/wael-app/auth-service/internal/mailer"
	"github.com/omarmaarouf18/wael-app/auth-service/internal/notify"
	"github.com/omarmaarouf18/wael-app/auth-service/internal/otp"
	"github.com/omarmaarouf18/wael-app/auth-service/internal/store"
	"github.com/omarmaarouf18/wael-app/shared/infra/handlerutil"
	"github.com/omarmaarouf18/wael-app/shared/infra/jwtutil"
	"github.com/omarmaarouf18/wael-app/shared/infra/ratelimit"
	"github.com/omarmaarouf18/wael-app/shared/infra/redact"
	"github.com/omarmaarouf18/wael-app/shared/infra/tlsutil"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("[AUTH] %v", err)
	}
	jwtutil.Init(cfg.JWTSecret)
	if err := notify.InitClient(cfg.TLSCertPath, cfg.TLSKeyPath, cfg.TLSCAPath); err != nil {
		log.Fatalf("[AUTH] notify mTLS client: %v", err)
	}

	ctx := context.Background()
	var st store.Store = store.NewMemoryStore()
	if cfg.MongoURI != "" {
		ms, err := store.NewMongoStore(ctx, cfg.MongoURI, cfg.MongoDatabase)
		if err != nil {
			log.Fatalf("[AUTH] mongo (%s): %v", redact.RedactURI(cfg.MongoURI), err)
		}
		st = ms
		log.Printf("[AUTH] using MongoDB database %s", cfg.MongoDatabase)
	} else {
		log.Printf("[AUTH] MONGO_URI empty: using in-process memory store (localhost dev only)")
	}

	var codes otp.Store = otp.NewMemoryStore()
	var lockout handlers.Lockout = handlers.NewMemoryLockout()
	if cfg.RedisURI != "" {
		rdb, err := ratelimit.NewRedisClient(cfg.RedisURI)
		if err != nil {
			log.Fatalf("[AUTH] redis: %v (uri=%s)", err, redact.RedactURI(cfg.RedisURI))
		}
		defer func() { _ = rdb.Close() }()
		jwtutil.SetRedisClient(rdb)
		codes = otp.NewRedisStore(rdb, "auth")
		lockout = handlers.NewRedisLockout(ratelimit.NewAuthRateLimiter(rdb, "auth"))
	} else {
		log.Printf("[AUTH] REDIS_URI empty: using in-process code/lockout stores (localhost dev only)")
	}

	var sender mailer.Sender = mailer.LogSender{}
	if cfg.ResendAPIKey != "" {
		sender = &mailer.ResendSender{APIKey: cfg.ResendAPIKey, From: cfg.ResendFromEmail}
	}

	srv := handlers.New(st, codes, lockout, sender, cfg.AppEnv, cfg.GatewaySecret)
	srv.NotifyURL = cfg.NotificationURL
	srv.NotifyToken = cfg.InternalServiceToken
	mux := http.NewServeMux()
	mux.HandleFunc("/health", handlers.Health)
	mux.HandleFunc("/auth/signup", srv.Signup)
	mux.HandleFunc("/auth/verify-otp", srv.VerifyOTP)
	mux.HandleFunc("/auth/login", srv.Login)
	mux.HandleFunc("/auth/refresh", srv.Refresh)
	mux.HandleFunc("/auth/reset/request", srv.RequestReset)
	mux.HandleFunc("/auth/reset/verify", srv.VerifyResetCode)
	mux.HandleFunc("/auth/reset/confirm", srv.ConfirmReset)
	mux.HandleFunc("/auth/me", srv.Me)

	var handler http.Handler = mux
	handler = srv.GatewayAuth(handler)
	handler = handlerutil.MaxBytesMiddleware(1 << 20)(handler)

	addr := ":" + cfg.Port
	if cfg.TLSEnabled() {
		if cfg.TLSCAPath != "" {
			tlsCfg, err := tlsutil.LoadServerTLSConfig(cfg.TLSCertPath, cfg.TLSKeyPath, cfg.TLSCAPath)
			if err != nil {
				log.Fatalf("[AUTH] server mTLS: %v", err)
			}
			httpSrv := &http.Server{Addr: addr, Handler: handler, ReadHeaderTimeout: 5 * time.Second, TLSConfig: tlsCfg}
			fmt.Printf("auth-service listening HTTPS+mTLS on %s\n", addr)
			log.Fatal(httpSrv.ListenAndServeTLS("", ""))
			return
		}
		httpSrv := &http.Server{
			Addr:              addr,
			Handler:           handler,
			ReadHeaderTimeout: 5 * time.Second,
			TLSConfig:         &tls.Config{MinVersion: tls.VersionTLS12},
		}
		fmt.Printf("auth-service listening HTTPS on %s\n", addr)
		log.Fatal(httpSrv.ListenAndServeTLS(cfg.TLSCertPath, cfg.TLSKeyPath))
		return
	}
	httpSrv := &http.Server{Addr: addr, Handler: handler, ReadHeaderTimeout: 5 * time.Second}
	fmt.Printf("auth-service listening HTTP on %s\n", addr)
	log.Fatal(httpSrv.ListenAndServe())
}
