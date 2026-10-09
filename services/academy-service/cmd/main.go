// academy-service provides course catalog, level management, subject metadata,
// entitlements, access requests, and administrative operations for wael-app.
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

	"github.com/omarmaarouf18/wael-app/academy-service/internal/config"
	"github.com/omarmaarouf18/wael-app/academy-service/internal/handlers"
	"github.com/omarmaarouf18/wael-app/academy-service/internal/limiter"
	"github.com/omarmaarouf18/wael-app/academy-service/internal/notify"
	"github.com/omarmaarouf18/wael-app/academy-service/internal/store"
	"github.com/omarmaarouf18/wael-app/shared/infra/jwtutil"
	"github.com/omarmaarouf18/wael-app/shared/infra/ratelimit"
	"github.com/omarmaarouf18/wael-app/shared/infra/redact"
	"github.com/omarmaarouf18/wael-app/shared/infra/storage"
	"github.com/omarmaarouf18/wael-app/shared/infra/tlsutil"
)

func runCheckEnv(stdout, stderr io.Writer) int {
	_, err := config.Load()
	if err != nil {
		fmt.Fprintf(stderr, "check-env: failed to validate configuration: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "check-env: ok\n")
	return 0
}

func main() {
	checkEnv := flag.Bool("check-env", false, "Validate environment configuration and exit")
	flag.Parse()

	if *checkEnv {
		os.Exit(runCheckEnv(os.Stdout, os.Stderr))
	}

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("[ACADEMY] %v", err)
	}
	dev := cfg.AppEnv == "local" || cfg.AppEnv == "test"

	jwtutil.Init(cfg.JWTSecret)

	var tierLimiter limiter.TierLimiter
	if cfg.RedisURI != "" {
		rdb, err := ratelimit.NewRedisClient(cfg.RedisURI)
		if err != nil {
			log.Fatalf("[ACADEMY] redis (%s): %v", redact.RedactURI(cfg.RedisURI), err)
		}
		defer func() { _ = rdb.Close() }()
		jwtutil.SetRedisClient(rdb)
		log.Printf("[ACADEMY] redis connected: %s", redact.RedactURI(cfg.RedisURI))
		tierLimiter = limiter.NewRedisTierLimiter(rdb, cfg.RateLimitRead, cfg.RateLimitPlay, cfg.RateLimitDownload, cfg.RateLimitWrite)
	} else if !dev {
		log.Fatalf("[ACADEMY] redis is required outside dev")
	}

	ctx := context.Background()
	var st store.Store
	if cfg.MongoURI != "" {
		ms, err := store.NewMongoStore(ctx, cfg.MongoURI, cfg.MongoDatabase)
		if err != nil {
			log.Fatalf("[ACADEMY] mongo (%s): %v", redact.RedactURI(cfg.MongoURI), err)
		}
		st = ms
		log.Printf("[ACADEMY] active store: MongoDB (database: %s)", cfg.MongoDatabase)
	} else {
		if !dev {
			log.Fatalf("[ACADEMY] memory store not permitted outside dev: MONGO_URI is required")
		}
		st = store.NewMemoryStore()
		log.Printf("[ACADEMY] active store: in-process memory (localhost dev only)")
	}

	// Idempotently seed levels at startup
	if err := st.SeedLevels(ctx); err != nil {
		log.Fatalf("[ACADEMY] seed levels: %v", err)
	}
	log.Printf("[ACADEMY] levels seeded successfully")

	srv := handlers.New(st, cfg.AppEnv, cfg.GatewaySecret, cfg.InternalServiceToken, cfg.AuthServiceURL, cfg.ExposePriceToStudents, cfg.SupportWhatsApp)
	srv.Limiter = tierLimiter
	srv.AuthAdminURL = cfg.AuthAdminURL
	srv.NotifyURL = cfg.NotificationServiceURL
	srv.NotifyToken = cfg.InternalServiceToken
	srv.TermsURL = cfg.TermsURL
	srv.PrivacyURL = cfg.PrivacyURL
	srv.MinVersion = cfg.MinVersion
	srv.LatestVersion = cfg.LatestVersion
	srv.UpdateURL = cfg.UpdateURL
	srv.WarnIfNoSupportWhatsApp(ctx)

	// Encrypted PDF storage (ADR-0009). The key policy is enforced by
	// config.Load and again by storage: outside local/test a missing or
	// invalid DOCUMENT_ENCRYPTION_KEY stops startup. The key is never logged.
	files, err := storage.NewLocalStorage(cfg.StorageDir, cfg.DocumentEncryptionKey, cfg.AppEnv)
	if err != nil {
		log.Fatalf("[ACADEMY] file storage: %v", err)
	}
	defer func() { _ = files.Close() }()
	srv.Files = files
	srv.MaxPDFBytes = cfg.MaxPDFBytes
	srv.FeaturesFiles = cfg.FeaturesFiles
	log.Printf("[ACADEMY] app-config features.files=%t", cfg.FeaturesFiles)
	log.Printf("[ACADEMY] file storage: %s (max PDF %d bytes)", cfg.StorageDir, cfg.MaxPDFBytes)

	if err := notify.InitClient(cfg.TLSCertPath, cfg.TLSKeyPath, cfg.TLSCAPath); err != nil {
		log.Fatalf("[ACADEMY] notify client: %v", err)
	}

	// Verify client for admin tokens (POST {AUTH_ADMIN_URL}/internal/admin/verify):
	// mTLS when client certs are configured, plain client with the 3 s handler
	// timeout in dev. Outside dev TLS is already required, so this fails closed.
	verifyClient, err := buildVerifyClient(cfg)
	if err != nil {
		log.Fatalf("[ACADEMY] admin verify client: %v", err)
	}
	srv.VerifyClient = verifyClient

	// Build and start admin listener on internal network
	adminRunner, err := buildServer(cfg, cfg.AdminListenAddr, srv.AdminHandler())
	if err != nil {
		log.Fatalf("[ACADEMY] admin listener: %v", err)
	}
	go func() {
		fmt.Printf("academy-service admin listener %s on %s\n", adminRunner.desc, cfg.AdminListenAddr)
		if err := adminRunner.serve(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("[ACADEMY] admin listener: %v", err)
		}
	}()

	addr := ":" + cfg.Port
	publicRunner, err := buildServer(cfg, addr, srv.PublicHandler())
	if err != nil {
		log.Fatalf("[ACADEMY] %v", err)
	}
	fmt.Printf("academy-service listening %s on %s\n", publicRunner.desc, addr)
	log.Fatal(publicRunner.serve())
}

type serverRunner struct {
	server *http.Server
	serve  func() error
	desc   string
}

// buildVerifyClient constructs the outgoing mTLS client used to verify admin
// tokens against auth-service POST /internal/admin/verify (3 s timeout, no
// caching so revocation is immediate). Outside dev, client certs are required
// (fail closed); in dev a plain client is accepted per configuration.
func buildVerifyClient(cfg *config.Config) (*http.Client, error) {
	dev := cfg.AppEnv == "local" || cfg.AppEnv == "test"
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
	if !dev {
		return nil, errors.New("verify client requires mTLS outside dev: TLS_CERT_PATH, TLS_KEY_PATH and TLS_CA_PATH are required")
	}
	return &http.Client{Timeout: 3 * time.Second}, nil
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
