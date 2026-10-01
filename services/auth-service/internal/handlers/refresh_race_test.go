package handlers

import (
	"encoding/json"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
)

// TestRefresh_SingleUseUnderConcurrency fails if the same refresh token can
// be redeemed more than once across 20 parallel requests.
func TestRefresh_SingleUseUnderConcurrency(t *testing.T) {
	s := testServer()
	rec := doRequest(t, s, http.MethodPost, "/auth/signup", map[string]string{
		"full_name": "Race User",
		"email":     "race@example.com",
		"phone":     "+201012345688",
		"password":  "password123",
	}, "")
	if rec.Code != http.StatusCreated {
		t.Fatalf("signup = %d (%s)", rec.Code, rec.Body.String())
	}
	var signup map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&signup); err != nil {
		t.Fatal(err)
	}
	rec = doRequest(t, s, http.MethodPost, "/auth/verify-otp", map[string]string{"email": "race@example.com", "code": signup["dev_otp"]}, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("verify = %d (%s)", rec.Code, rec.Body.String())
	}
	var tokens map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&tokens); err != nil {
		t.Fatal(err)
	}

	var okCount atomic.Int64
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			rec := doRequest(t, s, http.MethodPost, "/auth/refresh", map[string]string{"refresh_token": tokens["refresh_token"]}, "")
			if rec.Code == http.StatusOK {
				okCount.Add(1)
			}
		}()
	}
	wg.Wait()
	if got := okCount.Load(); got != 1 {
		t.Fatalf("refresh token redeemed %d times, want exactly 1", got)
	}
}
