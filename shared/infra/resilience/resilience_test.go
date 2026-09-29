package resilience

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sony/gobreaker/v2"
)

func TestResilienceClient_RetryIdempotent(t *testing.T) {
	var attempts int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&attempts, 1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer ts.Close()

	client := NewClient(http.DefaultClient, "test-retry-idempotent", 2, 50*time.Millisecond)
	// We want to speed up backoff during tests
	client.initialBackoff = 1 * time.Millisecond
	client.maxBackoff = 2 * time.Millisecond

	// 1. Test idempotent request (GET) -> Expect 3 total attempts (1 initial + 2 retries)
	req, _ := http.NewRequest("GET", ts.URL, nil)
	resp, err := client.Do(req)
	if err == nil {
		resp.Body.Close()
		t.Fatal("expected error from all-failing server")
	}

	finalAttempts := atomic.LoadInt32(&attempts)
	if finalAttempts != 3 {
		t.Errorf("expected 3 total attempts for GET, got %d", finalAttempts)
	}

	// Reset attempts count
	atomic.StoreInt32(&attempts, 0)

	// 2. Test non-idempotent request (POST) -> Expect exactly 1 attempt (no retries)
	reqPOST, _ := http.NewRequest("POST", ts.URL, nil)
	respPOST, errPOST := client.Do(reqPOST)
	if errPOST == nil {
		respPOST.Body.Close()
		t.Fatal("expected error from all-failing server")
	}

	finalPOSTAttempts := atomic.LoadInt32(&attempts)
	if finalPOSTAttempts != 1 {
		t.Errorf("expected exactly 1 attempt for POST, got %d", finalPOSTAttempts)
	}
}

func TestResilienceClient_CircuitBreakerTripAndRecover(t *testing.T) {
	var failMode int32 = 1 // 1 for fail, 0 for succeed
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.LoadInt32(&failMode) == 1 {
			w.WriteHeader(http.StatusInternalServerError)
		} else {
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer ts.Close()

	client := NewClient(http.DefaultClient, "test-breaker-trip", 0, 50*time.Millisecond)
	// Modify breaker settings for quick cooldown in test
	client.breaker = gobreaker.NewCircuitBreaker[*http.Response](gobreaker.Settings{
		Name:        "test-breaker-trip",
		MaxRequests: 1,                     // 1 success in half-open will close it
		Timeout:     50 * time.Millisecond, // 50ms cooldown in open state
		ReadyToTrip: func(counts gobreaker.Counts) bool {
			return counts.ConsecutiveFailures >= 3 // trip after 3 consecutive failures
		},
	})

	// 1. Force failures until the breaker trips
	req, _ := http.NewRequest("GET", ts.URL, nil)
	for i := 0; i < 3; i++ {
		_, err := client.Do(req)
		if err == nil {
			t.Fatal("expected error")
		}
	}

	// 2. The 4th request should fail immediately with breaker open error without reaching the server
	_, err := client.Do(req)
	if err != gobreaker.ErrOpenState {
		t.Fatalf("expected breaker open state error, got: %v", err)
	}

	// 3. Wait for the cooldown timeout to expire
	time.Sleep(60 * time.Millisecond)

	// 4. Set server to succeed, request should succeed and close the breaker
	atomic.StoreInt32(&failMode, 0)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("expected request to succeed in half-open state, got error: %v", err)
	}
	resp.Body.Close()

	// 5. Breaker should be closed now, verify subsequent request works
	resp2, err2 := client.Do(req)
	if err2 != nil {
		t.Fatalf("expected closed breaker request to succeed, got error: %v", err2)
	}
	resp2.Body.Close()
}

func TestResilienceClient_AuthorizationFailClosed(t *testing.T) {
	// Tripped breaker on authorization-lookup must result in access denied
	client := NewClient(http.DefaultClient, "test-auth-fail-closed", 0, 50*time.Millisecond)
	// Trip breaker immediately
	client.breaker = gobreaker.NewCircuitBreaker[*http.Response](gobreaker.Settings{
		Name: "test-auth-fail-closed",
		ReadyToTrip: func(counts gobreaker.Counts) bool {
			return true
		},
	})

	// Execute first request to trip it
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer ts.Close()
	req, _ := http.NewRequest("GET", ts.URL, nil)
	_, _ = client.Do(req)

	// Verify breaker is open
	if client.breaker.State() != gobreaker.StateOpen {
		t.Fatal("expected breaker to be open")
	}

	// Simulated verifyToken/canAccessChannel logic
	verifyTokenSim := func() (bool, error) {
		req, _ := http.NewRequest("GET", ts.URL, nil)
		resp, err := client.Do(req)
		if err != nil {
			// This represents ErrOpenState or network failure
			return false, err
		}
		defer resp.Body.Close()
		return resp.StatusCode == http.StatusOK, nil
	}

	// Run authorization sim
	allowed, err := verifyTokenSim()
	if allowed {
		t.Fatal("expected authorization to fail closed when breaker is open")
	}
	if err != gobreaker.ErrOpenState {
		t.Fatalf("expected error to be ErrOpenState, got: %v", err)
	}
}

type trackingBody struct {
	io.ReadCloser
	closed *int32
}

func (tb *trackingBody) Close() error {
	atomic.StoreInt32(tb.closed, 1)
	return tb.ReadCloser.Close()
}

type trackingRoundTripper struct {
	closedStates []*int32
}

func (tr *trackingRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	closed := new(int32)
	tr.closedStates = append(tr.closedStates, closed)
	return &http.Response{
		StatusCode: http.StatusInternalServerError,
		Body: &trackingBody{
			ReadCloser: io.NopCloser(strings.NewReader("internal server error")),
			closed:     closed,
		},
		Header: make(http.Header),
	}, nil
}

func TestResilienceClient_ConnectionLeak(t *testing.T) {
	tr := &trackingRoundTripper{}
	httpClient := &http.Client{Transport: tr}
	client := NewClient(httpClient, "test-connection-leak", 2, 50*time.Millisecond)
	client.initialBackoff = 1 * time.Millisecond
	client.maxBackoff = 2 * time.Millisecond

	req, _ := http.NewRequest("GET", "http://example.com/test", nil)
	resp, err := client.Do(req)

	// Since it retries and fails, we expect err != nil
	if err == nil {
		if resp != nil {
			resp.Body.Close()
		}
		t.Fatal("expected error from all-failing roundtripper")
	}

	// We expect 3 attempts made, meaning 3 responses returned by Transport.RoundTrip.
	if len(tr.closedStates) != 3 {
		t.Fatalf("expected 3 attempts, got %d", len(tr.closedStates))
	}

	// ALL 3 response bodies must be closed to avoid connection leaks!
	for i, closed := range tr.closedStates {
		if atomic.LoadInt32(closed) != 1 {
			t.Errorf("Response body for attempt %d was NOT closed!", i)
		}
	}
}

func TestResilienceClient_ExtraCoverage(t *testing.T) {
	// 1. Retry behavior on transient failures
	t.Run("TransientFailuresRetryAndSuccess", func(t *testing.T) {
		var attempts int32
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			curr := atomic.AddInt32(&attempts, 1)
			if curr < 3 {
				// Fail the first 2 attempts
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			// Succeed on the 3rd attempt
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("success"))
		}))
		defer ts.Close()

		client := NewClient(http.DefaultClient, "test-transient-success", 2, 100*time.Millisecond)
		client.initialBackoff = 1 * time.Millisecond
		client.maxBackoff = 2 * time.Millisecond

		req, _ := http.NewRequest("GET", ts.URL, nil)
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("expected request to eventually succeed, got error: %v", err)
		}
		defer resp.Body.Close()

		body, _ := io.ReadAll(resp.Body)
		if string(body) != "success" {
			t.Errorf("expected body 'success', got %q", string(body))
		}

		finalAttempts := atomic.LoadInt32(&attempts)
		if finalAttempts != 3 {
			t.Errorf("expected exactly 3 attempts (2 failures + 1 success), got %d", finalAttempts)
		}
	})

	// 2. Max retry count exhausted
	t.Run("MaxRetryCountExhausted", func(t *testing.T) {
		var attempts int32
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			atomic.AddInt32(&attempts, 1)
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer ts.Close()

		// Max retries = 1 -> total attempts = 2
		client := NewClient(http.DefaultClient, "test-max-retry", 1, 100*time.Millisecond)
		client.initialBackoff = 1 * time.Millisecond
		client.maxBackoff = 2 * time.Millisecond

		req, _ := http.NewRequest("GET", ts.URL, nil)
		resp, err := client.Do(req)
		if err == nil {
			resp.Body.Close()
			t.Fatal("expected error when retry count is exhausted")
		}

		finalAttempts := atomic.LoadInt32(&attempts)
		if finalAttempts != 2 {
			t.Errorf("expected exactly 2 attempts (1 initial + 1 retry), got %d", finalAttempts)
		}
	})

	// 3. Timeout handling (per-attempt timeout)
	t.Run("PerAttemptTimeoutRetry", func(t *testing.T) {
		var attempts int32
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			curr := atomic.AddInt32(&attempts, 1)
			if curr == 1 {
				// Sleep to trigger timeout on first attempt
				time.Sleep(100 * time.Millisecond)
			}
			w.WriteHeader(http.StatusOK)
		}))
		defer ts.Close()

		// Attempt timeout = 10ms (so 100ms sleep triggers it)
		// Max retries = 1
		client := NewClient(http.DefaultClient, "test-attempt-timeout", 1, 10*time.Millisecond)
		client.initialBackoff = 1 * time.Millisecond
		client.maxBackoff = 2 * time.Millisecond

		req, _ := http.NewRequest("GET", ts.URL, nil)
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("expected request to succeed on retry after timeout, got error: %v", err)
		}
		defer resp.Body.Close()

		finalAttempts := atomic.LoadInt32(&attempts)
		if finalAttempts != 2 {
			t.Errorf("expected 2 attempts (1st timed out, 2nd succeeded), got %d", finalAttempts)
		}
	})
}

func TestGetBreakerStats(t *testing.T) {
	cbSettings := gobreaker.Settings{
		Name: "test-breaker-stats-service",
	}
	cb := gobreaker.NewCircuitBreaker[*http.Response](cbSettings)
	RegisterBreaker("test-breaker-stats-service", cb)

	stats := GetBreakerStats()
	found := false
	for _, s := range stats {
		if s.Name == "test-breaker-stats-service" {
			found = true
			if s.State != "closed" {
				t.Errorf("Expected state 'closed', got %q", s.State)
			}
		}
	}
	if !found {
		t.Errorf("GetBreakerStats did not return registered breaker 'test-breaker-stats-service'")
	}
}

func TestResilienceRoundTripper(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer ts.Close()

	rt := NewRoundTripper(http.DefaultTransport, "test-rt-service", 2, 5*time.Second)
	req, _ := http.NewRequest("GET", ts.URL, nil)

	resp, err := rt.RoundTrip(req)
	if err != nil {
		t.Fatalf("RoundTrip failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected 200 OK, got %d", resp.StatusCode)
	}
}

func TestResilienceRoundTripper_StreamingResponseSurvivesBodyRead(t *testing.T) {
	// Regression guard: the per-attempt timeout context must NOT be cancelled
	// when the breaker closure returns. Streamed responses (SSE) must remain
	// readable for their full lifetime, bounded only by the caller's request
	// context — otherwise every proxied stream through the API gateway is
	// truncated right after the first buffered chunk.
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fl := w.(http.Flusher)
		for i := 0; i < 5; i++ {
			fmt.Fprintf(w, "data: tick %d\n\n", i)
			fl.Flush()
			time.Sleep(120 * time.Millisecond)
		}
	}))
	defer ts.Close()

	rt := NewRoundTripper(http.DefaultTransport, "test-streaming-rt", 0, 100*time.Millisecond)
	req, _ := http.NewRequest("GET", ts.URL, nil)

	resp, err := rt.RoundTrip(req)
	if err != nil {
		t.Fatalf("RoundTrip failed: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("reading streamed body failed (stream was likely cancelled early): %v", err)
	}
	for i := 0; i < 5; i++ {
		want := fmt.Sprintf("data: tick %d", i)
		if !strings.Contains(string(body), want) {
			t.Errorf("streamed body missing %q; got body=%q", want, string(body))
		}
	}
}

func TestResilienceClient_StreamingResponseBodyReadable(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fl := w.(http.Flusher)
		for i := 0; i < 5; i++ {
			fmt.Fprintf(w, "data: tick %d\n\n", i)
			fl.Flush()
			time.Sleep(120 * time.Millisecond)
		}
	}))
	defer ts.Close()

	client := NewClient(&http.Client{Transport: http.DefaultTransport}, "test-streaming-client", 0, 100*time.Millisecond)
	req, _ := http.NewRequest("GET", ts.URL, nil)

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Do failed: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("reading streamed body failed (stream was likely cancelled early): %v", err)
	}
	for i := 0; i < 5; i++ {
		want := fmt.Sprintf("data: tick %d", i)
		if !strings.Contains(string(body), want) {
			t.Errorf("streamed body missing %q; got body=%q", want, string(body))
		}
	}
}

type dummyReadWriteCloser struct {
	io.ReadCloser
}

func (d *dummyReadWriteCloser) Write(p []byte) (n int, err error) {
	return len(p), nil
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func TestResilienceRoundTripper_SwitchingProtocolsPreserved(t *testing.T) {
	dummyBody := &dummyReadWriteCloser{ReadCloser: io.NopCloser(strings.NewReader("websocket stream"))}
	mockRT := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusSwitchingProtocols,
			Body:       dummyBody,
			Header:     make(http.Header),
		}, nil
	})

	rt := NewRoundTripper(mockRT, "test-ws-upgrade", 0, 500*time.Millisecond)
	req, _ := http.NewRequest("GET", "http://example.com/ws", nil)

	resp, err := rt.RoundTrip(req)
	if err != nil {
		t.Fatalf("RoundTrip failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusSwitchingProtocols {
		t.Errorf("expected 101 Switching Protocols, got %d", resp.StatusCode)
	}
	if _, ok := resp.Body.(io.ReadWriteCloser); !ok {
		t.Errorf("expected resp.Body to implement io.ReadWriteCloser for WebSocket upgrades, got %T", resp.Body)
	}
}
