// Package main is the auth-service entrypoint (skeleton; signup/login/OTP/JWT lands in gateway+auth step).
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
	fmt.Println("auth-service skeleton listening (wiring pending)")
	_ = http.ListenAndServe(":3002", mux)
}
