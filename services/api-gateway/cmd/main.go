// Package main is the api-gateway entrypoint (skeleton; full proxy wiring lands in gateway+auth step).
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
	fmt.Println("api-gateway skeleton listening (wiring pending)")
	_ = http.ListenAndServe(":8080", mux)
}
