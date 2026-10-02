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

	// Build and start admin listener on internal network
	adminRunner, err := buildServer(cfg, cfg.AdminListenAddr, srv.AdminHandler())
	if err != nil {
		log.Fatalf("[AUTH] admin listener: %v", err)
	}
	go func() {
		fmt.Printf("auth-service admin listener %s on %s\n", adminRunner.desc, cfg.AdminListenAddr)
		if err := adminRunner.serve(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("[AUTH] admin listener: %v", err)
		}
	}()

	mux := http.NewServeMux()
	mux.HandleFunc("/health", handlers.Health)
	mux.HandleFunc("/auth/signup", srv.Signup)
	mux.HandleFunc("/auth/verify-otp", srv.VerifyOTP)
	mux.HandleFunc("/auth/login", srv.Login)
	mux.HandleFunc("/auth/logout", srv.Logout)
	mux.HandleFunc("/auth/refresh", srv.Refresh)
	mux.HandleFunc("/auth/reset/request", srv.RequestReset)
	mux.HandleFunc("/auth/reset/verify", srv.VerifyResetCode)
	mux.HandleFunc("/auth/reset/confirm", srv.ConfirmReset)
	mux.HandleFunc("/auth/me", srv.Me)

	var handler http.Handler = mux
	handler = srv.GatewayAuth(handler)
	handler = handlerutil.MaxBytesMiddleware(1 << 20)(handler)

	addr := ":" + cfg.Port
	publicRunner, err := buildServer(cfg, addr, handler)
	if err != nil {
		log.Fatalf("[AUTH] %v", err)
	}
	fmt.Printf("auth-service listening %s on %s\n", publicRunner.desc, addr)
	log.Fatal(publicRunner.serve())
}

type serverRunner struct {
	server *http.Server
	serve  func() error
	desc   string
}

// buildServer constructs an http.Server and its serve function following the
// TLS/mTLS policy:
//   - Outside dev: HTTPS + mTLS with client CA verification is strictly required.
//     TLS without client CA or plain HTTP returns an error (fail closed).
//   - Dev/local: plain HTTP, TLS, or mTLS are accepted per configuration.
func buildServer(cfg *config.Config, addr string, handler http.Handler) (*serverRunner, error) {
	dev := cfg.AppEnv == "local" || cfg.AppEnv == "test"
	if cfg.TLSEnabled() {
		if cfg.TLSCAPath != "" {
			tlsCfg, err := tlsutil.LoadServerTLSConfig(cfg.TLSCertPath, cfg.TLSKeyPath, cfg.TLSCAPath)
			if err != nil {
				return nil, fmt.Errorf("server mTLS: %w", err)
			}
			httpSrv := &http.Server{
				Addr:              addr,
				Handler:           handler,
				ReadHeaderTimeout: 5 * time.Second,
				TLSConfig:         tlsCfg,
			}
			return &serverRunner{
				server: httpSrv,
				serve:  func() error { return httpSrv.ListenAndServeTLS("", "") },
				desc:   "HTTPS+mTLS",
			}, nil
		}
		if !dev {
			return nil, errors.New("server TLS without client CA not permitted outside dev: TLS_CA_PATH is required")
		}
		httpSrv := &http.Server{
			Addr:              addr,
			Handler:           handler,
			ReadHeaderTimeout: 5 * time.Second,
			TLSConfig:         &tls.Config{MinVersion: tls.VersionTLS12},
		}
		return &serverRunner{
			server: httpSrv,
			serve:  func() error { return httpSrv.ListenAndServeTLS(cfg.TLSCertPath, cfg.TLSKeyPath) },
			desc:   "HTTPS",
		}, nil
	}

	if !dev {
		return nil, errors.New("plain HTTP not permitted outside dev: TLS is required")
	}
	httpSrv := &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
	}
	return &serverRunner{
		server: httpSrv,
		serve:  func() error { return httpSrv.ListenAndServe() },
		desc:   "HTTP",
	}, nil
}
