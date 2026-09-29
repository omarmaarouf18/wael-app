// Package main is the notification-service entrypoint (skeleton; delivery wiring lands later).
package main

import (
	"fmt"
	"net/http"
)

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	fmt.Println("notification-service skeleton listening (wiring pending)")
	_ = http.ListenAndServe(":3004", mux)
}
