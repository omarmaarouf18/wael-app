// Command auth-service is the authentication API: signup, login, OTP,
// JWT refresh, and two-phase password reset.
package main

import (
	"context"
	"crypto/tls"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
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
		log.Fatalf("[AUTH] %v", err)
	}
	dev := cfg.AppEnv == "local" || cfg.AppEnv == "test"

	jwtutil.Init(cfg.JWTSecret)
	if err := notify.InitClient(cfg.TLSCertPath, cfg.TLSKeyPath, cfg.TLSCAPath); err != nil {
		log.Fatalf("[AUTH] notify mTLS client: %v", err)
	}

	ctx := context.Background()
	var st store.Store
	if cfg.MongoURI != "" {
		ms, err := store.NewMongoStore(ctx, cfg.MongoURI, cfg.MongoDatabase)
		if err != nil {
			log.Fatalf("[AUTH] mongo (%s): %v", redact.RedactURI(cfg.MongoURI), err)
		}
		st = ms
		log.Printf("[AUTH] active user store: MongoDB (database: %s)", cfg.MongoDatabase)
	} else {
		if !dev {
			log.Fatalf("[AUTH] memory store not permitted outside dev: MONGO_URI is required")
		}
		st = store.NewMemoryStore()
		log.Printf("[AUTH] active user store: in-process memory (localhost dev only)")
	}

	var codes otp.Store
	var lockout handlers.Lockout
	if cfg.RedisURI != "" {
		rdb, err := ratelimit.NewRedisClient(cfg.RedisURI)
		if err != nil {
			log.Fatalf("[AUTH] redis: %v (uri=%s)", err, redact.RedactURI(cfg.RedisURI))
		}
		defer func() { _ = rdb.Close() }()
		jwtutil.SetRedisClient(rdb)
		codes = otp.NewRedisStore(rdb, "auth")
		lockout = handlers.NewRedisLockout(ratelimit.NewAuthRateLimiter(rdb, "auth"))
		log.Printf("[AUTH] active OTP and lockout store: Redis (%s)", redact.RedactURI(cfg.RedisURI))
	} else {
		if !dev {
			log.Fatalf("[AUTH] memory code/lockout stores not permitted outside dev: REDIS_URI is required")
		}
		codes = otp.NewMemoryStore()
		lockout = handlers.NewMemoryLockout()
		log.Printf("[AUTH] active OTP and lockout store: in-process memory (localhost dev only)")
	}

	var sender mailer.Sender
	if cfg.ResendAPIKey != "" {
		sender = &mailer.ResendSender{APIKey: cfg.ResendAPIKey, From: cfg.ResendFromEmail}
		log.Printf("[AUTH] active mail sender: Resend (%s)", cfg.ResendFromEmail)
	} else {
		if !dev {
			log.Fatalf("[AUTH] LogSender not permitted outside dev: RESEND_API_KEY is required")
		}
		sender = mailer.LogSender{}
		log.Printf("[AUTH] active mail sender: LogSender (localhost dev only)")
	}

	srv := handlers.New(st, codes, lockout, sender, cfg.AppEnv, cfg.GatewaySecret)
	srv.BlocklistHMACKey = cfg.BlocklistHMACKey
	srv.DefaultPhoneRegion = cfg.DefaultPhoneRegion
	srv.NotifyURL = cfg.NotificationURL
	srv.NotifyToken = cfg.InternalServiceToken
	srv.InternalToken = cfg.InternalServiceToken

	// Start admin listener on internal network
	adminHandler := srv.AdminHandler()
	adminSrv := &http.Server{
		Addr:              cfg.AdminListenAddr,
		Handler:           adminHandler,
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() {
		fmt.Printf("auth-service admin listener on %s\n", cfg.AdminListenAddr)
		if err := adminSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("[AUTH] admin listener: %v", err)
		}
	}()

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
		if !dev {
			log.Fatalf("[AUTH] server TLS without client CA not permitted outside dev: TLS_CA_PATH is required")
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
	if !dev {
		log.Fatalf("[AUTH] plain HTTP not permitted outside dev: TLS is required")
	}
	httpSrv := &http.Server{Addr: addr, Handler: handler, ReadHeaderTimeout: 5 * time.Second}
	fmt.Printf("auth-service listening HTTP on %s\n", addr)
	log.Fatal(httpSrv.ListenAndServe())
}
