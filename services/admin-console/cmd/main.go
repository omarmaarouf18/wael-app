// admin-console is the thin admin proxy and static UI for wael-app
// (ADR-0008). It holds INTERNAL_SERVICE_TOKEN, serves the admin pages on the
// admin subdomain behind Caddy, and forwards a fixed set of requests to the
// internal admin listeners. It makes no authorization decisions.
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
	"os/signal"
	"syscall"
	"time"

	"github.com/omarmaarouf18/wael-app/admin-console/internal/config"
	"github.com/omarmaarouf18/wael-app/admin-console/internal/proxy"
	"github.com/omarmaarouf18/wael-app/admin-console/internal/server"
	"github.com/omarmaarouf18/wael-app/admin-console/web"
	"github.com/omarmaarouf18/wael-app/shared/infra/tlsutil"
)

func runCheckEnv(stdout, stderr io.Writer) int {
	if _, err := config.Load(); err != nil {
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
		log.Fatalf("[ADMIN-CONSOLE] %v", err)
	}

	client := &http.Client{}
	switch {
	case cfg.MTLSClientEnabled():
		tlsCfg, err := tlsutil.LoadClientTLSConfig(cfg.TLSCertPath, cfg.TLSKeyPath, cfg.TLSCAPath)
		if err != nil {
			log.Fatalf("[ADMIN-CONSOLE] mTLS client config: %v", err)
		}
		client.Transport = &http.Transport{TLSClientConfig: tlsCfg}
	case !cfg.Dev():
		log.Fatalf("[ADMIN-CONSOLE] mTLS client config required outside dev")
	}

	p, err := proxy.New(proxy.Options{
		InternalToken:  cfg.InternalServiceToken,
		AuthURL:        cfg.AuthAdminURL,
		AcademyURL:     cfg.AcademyAdminURL,
		TrustedProxies: cfg.TrustedProxies,
		Client:         client,
		MaxPDFBytes:    cfg.MaxPDFBytes,
	})
	if err != nil {
		log.Fatalf("[ADMIN-CONSOLE] %v", err)
	}
	handler, err := server.New(p, web.Files)
	if err != nil {
		log.Fatalf("[ADMIN-CONSOLE] static assets: %v", err)
	}

	srv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    16 << 10,
	}
	serve := srv.ListenAndServe
	scheme := "HTTP"
	switch {
	case cfg.TLSEnabled():
		srv.TLSConfig = &tls.Config{MinVersion: tls.VersionTLS12}
		serve = func() error { return srv.ListenAndServeTLS(cfg.TLSCertPath, cfg.TLSKeyPath) }
		scheme = "HTTPS"
	case !cfg.Dev():
		log.Fatalf("[ADMIN-CONSOLE] plain HTTP not permitted outside dev: TLS is required")
	}

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-stop
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	}()

	log.Printf("[ADMIN-CONSOLE] env=%s listening %s on :%s auth_admin=%s academy_admin=%s mtls-client=%t trusted-proxies=%d max-pdf-bytes=%d",
		cfg.AppEnv, scheme, cfg.Port, cfg.AuthAdminURL, cfg.AcademyAdminURL, cfg.MTLSClientEnabled(), len(cfg.TrustedProxies), cfg.MaxPDFBytes)
	if err := serve(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("[ADMIN-CONSOLE] %v", err)
	}
}
