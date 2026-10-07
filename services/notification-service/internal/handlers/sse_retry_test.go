package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/omarmaarouf18/wael-app/notification-service/internal/bus"
	"github.com/omarmaarouf18/wael-app/notification-service/internal/models"
	"github.com/omarmaarouf18/wael-app/notification-service/internal/store"
	"github.com/omarmaarouf18/wael-app/shared/infra/jwtutil"
)

type mockRetryBus struct {
	mu           sync.Mutex
	subscribeFn  func(ctx context.Context) (<-chan *bus.AccountEvent, func(), error)
	publishCalls []any
}

func (m *mockRetryBus) Publish(ctx context.Context, n *models.Notification) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.publishCalls = append(m.publishCalls, n)
	return nil
}

func (m *mockRetryBus) Subscribe(ctx context.Context, userID string) (<-chan *models.Notification, func(), error) {
	ch := make(chan *models.Notification)
	return ch, func() {}, nil
}

func (m *mockRetryBus) PublishAccountEvent(ctx context.Context, evt *bus.AccountEvent) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.publishCalls = append(m.publishCalls, evt)
	return nil
}

func (m *mockRetryBus) SubscribeAccountEvents(ctx context.Context) (<-chan *bus.AccountEvent, func(), error) {
	m.mu.Lock()
	fn := m.subscribeFn
	m.mu.Unlock()
	if fn != nil {
		return fn(ctx)
	}
	ch := make(chan *bus.AccountEvent)
	return ch, func() {}, nil
}

func TestAccountEventsSubscriber_RetryOnFailureAndChannelClose(t *testing.T) {
	jwtutil.Init("test-secret-0123456789abcdef0123456789")

	// 1. First subscribe fails then succeeds -> events are processed
	t.Run("FirstSubscribeFailsThenSucceeds", func(t *testing.T) {
		var attempts int32
		evtChan := make(chan *bus.AccountEvent, 10)

		mb := &mockRetryBus{
			subscribeFn: func(ctx context.Context) (<-chan *bus.AccountEvent, func(), error) {
				n := atomic.AddInt32(&attempts, 1)
				if n == 1 {
					return nil, nil, errors.New("redis connection refused")
				}
				return evtChan, func() {}, nil
			},
		}

		s := New(store.NewMemoryStore(), mb, "gw-secret", "internal")
		s.AccountSubInitialBackoff = 20 * time.Millisecond
		stop := s.StartAccountEventsListener(context.Background())
		defer stop()

		// Before 2nd attempt succeeds, health should report degraded
		// Check health endpoint
		req := httptest.NewRequest(http.MethodGet, "/health", nil)
		rec := httptest.NewRecorder()
		s.Health(rec, req)
		var initialHealth map[string]any
		_ = json.NewDecoder(rec.Body).Decode(&initialHealth)
		if initialHealth["status"] != "degraded" {
			t.Fatalf("expected initial health degraded while unsubscribed, got %v", initialHealth)
		}

		// Wait up to 1 second for 2nd attempt to succeed
		deadline := time.Now().Add(1 * time.Second)
		for time.Now().Before(deadline) {
			if s.AccountEventsSubscribed() {
				break
			}
			time.Sleep(10 * time.Millisecond)
		}

		if !s.AccountEventsSubscribed() {
			t.Fatalf("subscriber never transitioned to subscribed state after initial failure (attempts=%d)", atomic.LoadInt32(&attempts))
		}

		// Verify health is now ok
		rec2 := httptest.NewRecorder()
		s.Health(rec2, req)
		var okHealth map[string]any
		_ = json.NewDecoder(rec2.Body).Decode(&okHealth)
		if okHealth["status"] != "ok" || okHealth["subscribed"] != true {
			t.Fatalf("expected health ok/subscribed, got %v", okHealth)
		}

		// Open a stream to verify event processing
		tok, err := jwtutil.GenerateTokenWithSession("user-retry-1", "user", "r1@example.com", "sid-retry-1")
		if err != nil {
			t.Fatal(err)
		}
		_, cancelStream, streamDone := openTestStream(t, s, tok)
		defer cancelStream()

		if s.Limiter.ActiveSlots("user-retry-1") != 1 {
			t.Fatalf("expected 1 active slot, got %d", s.Limiter.ActiveSlots("user-retry-1"))
		}

		// Send account revocation event on the channel
		evtChan <- &bus.AccountEvent{
			Action: "ACCOUNT_SUSPENDED",
			UserID: "user-retry-1",
			SID:    "sid-retry-1",
		}

		select {
		case <-streamDone:
		case <-time.After(1 * time.Second):
			t.Fatal("stream was not closed after account event arrived on retried subscription")
		}
	})

	// 2. Channel closes -> resubscribes
	t.Run("ChannelClosesResubscribes", func(t *testing.T) {
		var subCount int32
		ch1 := make(chan *bus.AccountEvent, 10)
		ch2 := make(chan *bus.AccountEvent, 10)

		mb := &mockRetryBus{
			subscribeFn: func(ctx context.Context) (<-chan *bus.AccountEvent, func(), error) {
				n := atomic.AddInt32(&subCount, 1)
				if n == 1 {
					return ch1, func() {}, nil
				}
				return ch2, func() {}, nil
			},
		}

		s := New(store.NewMemoryStore(), mb, "gw-secret", "internal")
		s.AccountSubInitialBackoff = 20 * time.Millisecond
		stop := s.StartAccountEventsListener(context.Background())
		defer stop()

		// Wait until first subscription is active
		for i := 0; i < 50; i++ {
			if s.AccountEventsSubscribed() {
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
		if !s.AccountEventsSubscribed() {
			t.Fatal("expected first subscription to be active")
		}

		// Close first channel to simulate network drop
		close(ch1)

		// Wait for subscriber to resubscribe to ch2
		deadline := time.Now().Add(1 * time.Second)
		resubscribed := false
		for time.Now().Before(deadline) {
			if atomic.LoadInt32(&subCount) >= 2 && s.AccountEventsSubscribed() {
				resubscribed = true
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
		if !resubscribed {
			t.Fatalf("subscriber did not resubscribe after channel closed (subCount=%d)", atomic.LoadInt32(&subCount))
		}

		// Open stream and verify event on ch2 closes it
		tok, err := jwtutil.GenerateTokenWithSession("user-retry-2", "user", "r2@example.com", "sid-retry-2")
		if err != nil {
			t.Fatal(err)
		}
		_, cancelStream, streamDone := openTestStream(t, s, tok)
		defer cancelStream()

		ch2 <- &bus.AccountEvent{
			Action: "ACCOUNT_DELETED",
			UserID: "user-retry-2",
			SID:    "sid-retry-2",
		}

		select {
		case <-streamDone:
		case <-time.After(1 * time.Second):
			t.Fatal("stream was not closed after event on resubscribed channel")
		}
	})

	// 3. Context cancel stops loop without leaking goroutines
	t.Run("ContextCancelStopsLoopWithoutGoroutineLeak", func(t *testing.T) {
		time.Sleep(50 * time.Millisecond)
		baselineGoroutines := runtime.NumGoroutine()

		mb := &mockRetryBus{
			subscribeFn: func(ctx context.Context) (<-chan *bus.AccountEvent, func(), error) {
				return nil, nil, errors.New("simulated continuous outage")
			},
		}

		s := New(store.NewMemoryStore(), mb, "gw-secret", "internal")
		s.AccountSubInitialBackoff = 20 * time.Millisecond
		stop := s.StartAccountEventsListener(context.Background())

		// Let it loop/retry several times
		time.Sleep(100 * time.Millisecond)

		// Cancel context via stop()
		stop()

		// Wait up to 2 seconds for goroutines to settle back to baseline
		var finalGoroutines int
		for i := 0; i < 40; i++ {
			time.Sleep(50 * time.Millisecond)
			finalGoroutines = runtime.NumGoroutine()
			if finalGoroutines <= baselineGoroutines {
				break
			}
		}

		if finalGoroutines > baselineGoroutines {
			t.Fatalf("goroutine leak: baseline=%d, final=%d", baselineGoroutines, finalGoroutines)
		}
	})
}
