package resilience

import (
	"context"
	"fmt"
	"io"
	"log"
	"math"
	"math/rand"
	"net/http"
	"sync"
	"time"

	"github.com/sony/gobreaker/v2"
)

// cancelReadCloser invokes its cancel function exactly once when the response
// body is closed. This lets a per-attempt request context stay alive for the
// full lifetime of the body read (including streamed responses such as SSE)
// instead of being cancelled the moment the execute closure returns, while
// still guaranteeing eventual resource release on Close or on error paths.
type cancelReadCloser struct {
	closer     io.Closer
	reader     io.Reader
	cancelOnce sync.Once
	cancel     context.CancelFunc
}

func newCancelReadCloser(rc io.ReadCloser, cancel context.CancelFunc) *cancelReadCloser {
	return &cancelReadCloser{
		closer: rc,
		reader: io.LimitReader(rc, 10<<20),
		cancel: cancel,
	}
}

func (c *cancelReadCloser) Read(p []byte) (int, error) {
	return c.reader.Read(p)
}

func (c *cancelReadCloser) Close() error {
	err := c.closer.Close()
	c.cancelOnce.Do(c.cancel)
	return err
}

type BreakerStats struct {
	Name           string `json:"name"`
	State          string `json:"state"`
	Failures       uint32 `json:"failures"`
	ConsecFailures uint32 `json:"consecutive_failures"`
	Successes      uint32 `json:"successes"`
}

var (
	breakersMu sync.RWMutex
	breakers   = make(map[string]*gobreaker.CircuitBreaker[*http.Response])
)

func RegisterBreaker(name string, cb *gobreaker.CircuitBreaker[*http.Response]) {
	breakersMu.Lock()
	defer breakersMu.Unlock()
	breakers[name] = cb
}

func GetBreakerStats() []BreakerStats {
	breakersMu.RLock()
	defer breakersMu.RUnlock()

	stats := make([]BreakerStats, 0, len(breakers))
	for name, cb := range breakers {
		counts := cb.Counts()
		stats = append(stats, BreakerStats{
			Name:           name,
			State:          cb.State().String(),
			Failures:       counts.TotalFailures,
			ConsecFailures: counts.ConsecutiveFailures,
			Successes:      counts.TotalSuccesses,
		})
	}
	return stats
}

func backoffWithJitter(attempt int, initialBackoff, maxBackoff time.Duration) time.Duration {
	temp := float64(initialBackoff) * math.Pow(2, float64(attempt))
	if temp > float64(maxBackoff) {
		temp = float64(maxBackoff)
	}
	// Jitter: 50% to 150% of the backoff value
	// #nosec G404 //nolint:gosec -- math/rand is appropriate for network retry backoff jitter, cryptographic randomness is not needed
	jitter := 0.5 + rand.Float64()
	return time.Duration(temp * jitter)
}

type ResilienceClient struct {
	client         *http.Client
	breaker        *gobreaker.CircuitBreaker[*http.Response]
	maxRetries     int
	initialBackoff time.Duration
	maxBackoff     time.Duration
	attemptTimeout time.Duration
}

func NewClient(client *http.Client, serviceName string, maxRetries int, attemptTimeout time.Duration) *ResilienceClient {
	cbSettings := gobreaker.Settings{
		Name:        serviceName,
		MaxRequests: 3, // consecutive successes needed in half-open state to close
		Interval:    10 * time.Second,
		Timeout:     15 * time.Second, // cooldown period in open state
		ReadyToTrip: func(counts gobreaker.Counts) bool {
			return counts.ConsecutiveFailures >= 5
		},
		OnStateChange: func(name string, from, to gobreaker.State) {
			log.Printf("[SECURITY EVENT / DOWNSTREAM DEGRADED] Circuit breaker %s transitioned from %s to %s", name, from.String(), to.String())
		},
	}

	cb := gobreaker.NewCircuitBreaker[*http.Response](cbSettings)
	RegisterBreaker(serviceName, cb)

	return &ResilienceClient{
		client:         client,
		breaker:        cb,
		maxRetries:     maxRetries,
		initialBackoff: 100 * time.Millisecond,
		maxBackoff:     1 * time.Second,
		attemptTimeout: attemptTimeout,
	}
}

func (rc *ResilienceClient) Do(req *http.Request) (*http.Response, error) {
	isIdempotent := req.Method == http.MethodGet || req.Method == http.MethodHead
	maxAttempts := 1
	if isIdempotent {
		maxAttempts = rc.maxRetries + 1
	}

	var lastResp *http.Response
	var lastErr error

	for attempt := 0; attempt < maxAttempts; attempt++ {
		if attempt > 0 {
			backoff := backoffWithJitter(attempt-1, rc.initialBackoff, rc.maxBackoff)
			select {
			case <-req.Context().Done():
				return nil, req.Context().Err()
			case <-time.After(backoff):
			}
		}

		lastResp, lastErr = rc.breaker.Execute(func() (*http.Response, error) {
			var reqClone *http.Request
			if attempt > 0 {
				reqClone = req.Clone(req.Context())
			} else {
				reqClone = req
			}

			// Bound only the time-to-response-headers with the per-attempt
			// timeout. A context.WithTimeout deadline would keep ticking while
			// the caller reads the body and truncate streamed responses (SSE)
			// mid-flight, so the cancellation is driven by a stoppable timer
			// instead and ownership of cancel moves to the body wrapper below.
			timeoutCtx, cancel := context.WithCancel(reqClone.Context())
			timer := time.AfterFunc(rc.attemptTimeout, cancel)

			reqClone = reqClone.WithContext(timeoutCtx)
			// #nosec G704 //nolint:gosec -- Resilience client is a generic wrapper executing caller-supplied requests, SSRF is not applicable here
			resp, err := rc.client.Do(reqClone)
			if err != nil {
				timer.Stop()
				cancel()
				return nil, err
			}

			// Headers received: disarm the per-attempt timer. The body now
			// lives under the caller's original request context; cancel fires
			// when the body is closed (or immediately on any error path).
			// Protocol upgrades (101 Switching Protocols / WebSockets) must NOT
			// have their body wrapped, as httputil.ReverseProxy requires the
			// underlying io.ReadWriteCloser for bidirectional streaming.
			timer.Stop()
			if resp.StatusCode != http.StatusSwitchingProtocols {
				resp.Body = newCancelReadCloser(resp.Body, cancel)
			}

			if resp.StatusCode >= 500 {
				return resp, fmt.Errorf("HTTP status %d", resp.StatusCode)
			}

			return resp, nil
		})

		if lastErr != nil {
			if lastResp != nil && lastResp.Body != nil {
				_ = lastResp.Body.Close()
			}
			if lastErr == gobreaker.ErrOpenState || lastErr == gobreaker.ErrTooManyRequests {
				break
			}
			continue
		}

		return lastResp, nil
	}

	return lastResp, lastErr
}

type ResilienceRoundTripper struct {
	underlying     http.RoundTripper
	breaker        *gobreaker.CircuitBreaker[*http.Response]
	maxRetries     int
	initialBackoff time.Duration
	maxBackoff     time.Duration
	attemptTimeout time.Duration
}

func NewRoundTripper(underlying http.RoundTripper, serviceName string, maxRetries int, attemptTimeout time.Duration) *ResilienceRoundTripper {
	cbSettings := gobreaker.Settings{
		Name:        serviceName,
		MaxRequests: 3,
		Interval:    10 * time.Second,
		Timeout:     15 * time.Second,
		ReadyToTrip: func(counts gobreaker.Counts) bool {
			return counts.ConsecutiveFailures >= 5
		},
		OnStateChange: func(name string, from, to gobreaker.State) {
			log.Printf("[SECURITY EVENT / DOWNSTREAM DEGRADED] Circuit breaker %s transitioned from %s to %s", name, from.String(), to.String())
		},
	}

	cb := gobreaker.NewCircuitBreaker[*http.Response](cbSettings)
	RegisterBreaker(serviceName, cb)

	return &ResilienceRoundTripper{
		underlying:     underlying,
		breaker:        cb,
		maxRetries:     maxRetries,
		initialBackoff: 100 * time.Millisecond,
		maxBackoff:     1 * time.Second,
		attemptTimeout: attemptTimeout,
	}
}

func (rt *ResilienceRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	isIdempotent := req.Method == http.MethodGet || req.Method == http.MethodHead
	maxAttempts := 1
	if isIdempotent {
		maxAttempts = rt.maxRetries + 1
	}

	var lastResp *http.Response
	var lastErr error

	for attempt := 0; attempt < maxAttempts; attempt++ {
		if attempt > 0 {
			backoff := backoffWithJitter(attempt-1, rt.initialBackoff, rt.maxBackoff)
			select {
			case <-req.Context().Done():
				return nil, req.Context().Err()
			case <-time.After(backoff):
			}
		}

		lastResp, lastErr = rt.breaker.Execute(func() (*http.Response, error) {
			var reqClone *http.Request
			if attempt > 0 {
				reqClone = req.Clone(req.Context())
			} else {
				reqClone = req
			}

			// Bound only the time-to-response-headers with the per-attempt
			// timeout (see the matching comment in Do). The stoppable timer
			// plus cancel-on-body-close pattern keeps streamed response bodies
			// readable for their full lifetime instead of cancelling them as
			// soon as this closure returns.
			timeoutCtx, cancel := context.WithCancel(reqClone.Context())
			timer := time.AfterFunc(rt.attemptTimeout, cancel)

			reqClone = reqClone.WithContext(timeoutCtx)
			resp, err := rt.underlying.RoundTrip(reqClone)
			if err != nil {
				timer.Stop()
				cancel()
				return nil, err
			}

			// Headers received: disarm the per-attempt timer and hand cancel
			// to the body wrapper so it fires on Close rather than on return.
			// Protocol upgrades (101 Switching Protocols / WebSockets) must NOT
			// have their body wrapped, as httputil.ReverseProxy requires the
			// underlying io.ReadWriteCloser for bidirectional streaming.
			timer.Stop()
			if resp.StatusCode != http.StatusSwitchingProtocols {
				resp.Body = newCancelReadCloser(resp.Body, cancel)
			}

			if resp.StatusCode >= 500 {
				return resp, fmt.Errorf("HTTP status %d", resp.StatusCode)
			}

			return resp, nil
		})

		if lastErr != nil {
			if lastResp != nil && lastResp.Body != nil {
				_ = lastResp.Body.Close()
			}
			if lastErr == gobreaker.ErrOpenState || lastErr == gobreaker.ErrTooManyRequests {
				break
			}
			continue
		}

		return lastResp, nil
	}

	return lastResp, lastErr
}
