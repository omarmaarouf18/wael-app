// Package main is the notification-service entrypoint (skeleton; delivery wiring lands later).
package main

import (
	"crypto/tls"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"
)

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "3004"
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	fmt.Println("notification-service skeleton listening (wiring pending)")
	srv := &http.Server{Addr: ":" + port, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	if cert, key := os.Getenv("TLS_CERT_PATH"), os.Getenv("TLS_KEY_PATH"); cert != "" && key != "" {
		srv.TLSConfig = &tls.Config{MinVersion: tls.VersionTLS12}
		log.Fatal(srv.ListenAndServeTLS(cert, key))
		return
	}
	log.Fatal(srv.ListenAndServe())
}
