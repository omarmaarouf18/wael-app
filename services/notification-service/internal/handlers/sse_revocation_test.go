package handlers

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"runtime"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/omarmaarouf18/wael-app/notification-service/internal/bus"
	"github.com/omarmaarouf18/wael-app/notification-service/internal/models"
	"github.com/omarmaarouf18/wael-app/notification-service/internal/store"
	"github.com/omarmaarouf18/wael-app/shared/infra/jwtutil"
	"github.com/redis/go-redis/v9"
)

// pipeRecorder implements http.ResponseWriter and http.Flusher using a pipe so
// tests can observe SSE stream chunks and connection closure in real time.
type pipeRecorder struct {
	header    http.Header
	code      int
	bodyCh    chan string
	doneCh    chan struct{}
	closeOnce sync.Once
	writeErr  error
	mu        sync.Mutex
	isClosed  bool
}

func newPipeRecorder() *pipeRecorder {
	return &pipeRecorder{
		header: make(http.Header),
		bodyCh: make(chan string, 64),
		doneCh: make(chan struct{}),
	}
}

func (pr *pipeRecorder) Header() http.Header {
	return pr.header
}

func (pr *pipeRecorder) Write(b []byte) (int, error) {
	pr.mu.Lock()
	if pr.writeErr != nil {
		err := pr.writeErr
		pr.mu.Unlock()
		return 0, err
	}
	if pr.isClosed {
		pr.mu.Unlock()
		return 0, errors.New("closed pipe")
	}
	chunk := string(b)
	pr.mu.Unlock()

	select {
	case pr.bodyCh <- chunk:
	default:
	}
	return len(b), nil
}

func (pr *pipeRecorder) WriteHeader(statusCode int) {
	pr.code = statusCode
}

func (pr *pipeRecorder) Flush() {}

func (pr *pipeRecorder) close() {
	pr.closeOnce.Do(func() {
		pr.mu.Lock()
		pr.isClosed = true
		pr.mu.Unlock()
		close(pr.doneCh)
	})
}

// openTestStream starts a Stream handler in a background goroutine and waits
// for the initial ": connected\n\n" frame.
func openTestStream(t *testing.T, s *Server, token string) (*pipeRecorder, context.CancelFunc, chan struct{}) {
	t.Helper()
	rec := newPipeRecorder()
	handlerDone := make(chan struct{})

	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodGet, "/notifications/stream", nil).WithContext(ctx)
	req.Header.Set("X-Gateway-Secret", "gw-secret")
	req.Header.Set("Authorization", "Bearer "+token)

	go func() {
		defer close(handlerDone)
		defer rec.close()
		s.GatewayAuth(http.HandlerFunc(s.Stream)).ServeHTTP(rec, req)
	}()

	// Wait for connected frame
	select {
	case chunk := <-rec.bodyCh:
		if chunk != ": connected\n\n" {
			t.Fatalf("expected ': connected\\n\\n', got %q", chunk)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for ': connected\\n\\n' frame")
	case <-handlerDone:
		t.Fatal("stream handler exited before connecting")
	}

	return rec, cancel, handlerDone
}

// 1. publish revoke for user A closes A's streams and not B's
func TestSSE_PublishRevokeUser_ClosesOnlyUserA(t *testing.T) {
	jwtutil.Init("test-jwt-secret-0123456789abcdef")
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer rdb.Close()
	jwtutil.SetRedisClient(rdb)
	defer jwtutil.SetRedisClient(nil)

	b := bus.NewRedisBus(rdb)
	s := New(store.NewMemoryStore(), b, "gw-secret", "internal-123")
	stopListener := s.StartAccountEventsListener(context.Background())
	defer stopListener()

	tokA1, err := jwtutil.GenerateTokenWithSession("user-A", "user", "a@example.com", "sid-a-1")
	if err != nil {
		t.Fatal(err)
	}
	tokA2, err := jwtutil.GenerateTokenWithSession("user-A", "user", "a@example.com", "sid-a-2")
	if err != nil {
		t.Fatal(err)
	}
	tokB, err := jwtutil.GenerateTokenWithSession("user-B", "user", "b@example.com", "sid-b-1")
	if err != nil {
		t.Fatal(err)
	}

	_, cancelA1, doneA1 := openTestStream(t, s, tokA1)
	defer cancelA1()
	_, cancelA2, doneA2 := openTestStream(t, s, tokA2)
	defer cancelA2()
	recB, cancelB, doneB := openTestStream(t, s, tokB)
	defer cancelB()

	if s.Limiter.ActiveSlots("user-A") != 2 {
		t.Fatalf("expected 2 active slots for user-A, got %d", s.Limiter.ActiveSlots("user-A"))
	}
	if s.Limiter.ActiveSlots("user-B") != 1 {
		t.Fatalf("expected 1 active slot for user-B, got %d", s.Limiter.ActiveSlots("user-B"))
	}

	// Publish revocation for user A via bus
	err = b.PublishAccountEvent(context.Background(), &bus.AccountEvent{
		Action: "ACCOUNT_SUSPENDED",
		UserID: "user-A",
	})
	if err != nil {
		t.Fatalf("publish account event: %v", err)
	}

	// Both streams for user A must close
	select {
	case <-doneA1:
	case <-time.After(2 * time.Second):
		t.Fatal("stream A1 did not close after user-A revocation")
	}
	select {
	case <-doneA2:
	case <-time.After(2 * time.Second):
		t.Fatal("stream A2 did not close after user-A revocation")
	}

	// User B stream must remain active
	select {
	case <-doneB:
		t.Fatal("stream B closed unexpectedly after user-A revocation")
	case <-time.After(100 * time.Millisecond):
	}

	// Push a notification to user B to verify stream B is alive and well
	err = b.Publish(context.Background(), &models.Notification{
		ID:        "notif-b-1",
		UserID:    "user-B",
		Type:      "system",
		Title:     "Still Alive",
		Body:      "Message for B",
		CreatedAt: time.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}

	select {
	case frame := <-recB.bodyCh:
		if frame == "" {
			t.Fatal("empty frame on stream B")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for live notification on stream B")
	}

	if s.Limiter.ActiveSlots("user-A") != 0 {
		t.Fatalf("expected 0 slots for user-A, got %d", s.Limiter.ActiveSlots("user-A"))
	}
	if s.Limiter.ActiveSlots("user-B") != 1 {
		t.Fatalf("expected 1 slot for user-B, got %d", s.Limiter.ActiveSlots("user-B"))
	}

	// Clean up user B stream
	cancelB()
	<-doneB
}

// 2. single-sid revoke closes only that sid
func TestSSE_PublishRevokeSession_ClosesOnlyTargetSID(t *testing.T) {
	jwtutil.Init("test-jwt-secret-0123456789abcdef")
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer rdb.Close()
	jwtutil.SetRedisClient(rdb)
	defer jwtutil.SetRedisClient(nil)

	b := bus.NewRedisBus(rdb)
	s := New(store.NewMemoryStore(), b, "gw-secret", "internal-123")
	stopListener := s.StartAccountEventsListener(context.Background())
	defer stopListener()

	tok1, err := jwtutil.GenerateTokenWithSession("user-single", "user", "single@example.com", "sid-to-close")
	if err != nil {
		t.Fatal(err)
	}
	tok2, err := jwtutil.GenerateTokenWithSession("user-single", "user", "single@example.com", "sid-to-keep")
	if err != nil {
		t.Fatal(err)
	}

	_, cancel1, done1 := openTestStream(t, s, tok1)
	defer cancel1()
	rec2, cancel2, done2 := openTestStream(t, s, tok2)
	defer cancel2()

	if s.Limiter.ActiveSlots("user-single") != 2 {
		t.Fatalf("expected 2 active slots, got %d", s.Limiter.ActiveSlots("user-single"))
	}

	// Revoke sid-to-close via jwtutil.RevokeSession (which denylists and publishes to account:events)
	if err := jwtutil.RevokeSession("sid-to-close"); err != nil {
		t.Fatalf("RevokeSession failed: %v", err)
	}

	// Stream 1 must close
	select {
	case <-done1:
	case <-time.After(2 * time.Second):
		t.Fatal("stream 1 did not close after session revocation")
	}

	// Stream 2 must remain open
	select {
	case <-done2:
		t.Fatal("stream 2 closed unexpectedly")
	case <-time.After(100 * time.Millisecond):
	}

	// Stream 2 still receives notifications
	err = b.Publish(context.Background(), &models.Notification{
		ID:        "n2",
		UserID:    "user-single",
		Type:      "system",
		Title:     "Session 2 alive",
		CreatedAt: time.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case frame := <-rec2.bodyCh:
		if frame == "" {
			t.Fatal("empty frame on stream 2")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for notification on stream 2")
	}

	if s.Limiter.ActiveSlots("user-single") != 1 {
		t.Fatalf("expected 1 remaining slot, got %d", s.Limiter.ActiveSlots("user-single"))
	}

	// Clean up stream 2
	cancel2()
	<-done2
}

// 3a. Heartbeat re-check: (a) session revoked -> stream closes
func TestSSE_HeartbeatRecheck_SessionRevoked_ClosesStream(t *testing.T) {
	jwtutil.Init("test-jwt-secret-0123456789abcdef")
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer rdb.Close()
	jwtutil.SetRedisClient(rdb)
	defer jwtutil.SetRedisClient(nil)

	s := testServer()
	// Rapid heartbeat: 25ms, check on every tick
	s.HeartbeatInterval = 25 * time.Millisecond
	s.CheckEveryNth = 1

	sid := "sid-session-revoked"
	tok, err := jwtutil.GenerateTokenWithSession("user-sess-revoked", "user", "sess@example.com", sid)
	if err != nil {
		t.Fatal(err)
	}

	_, cancel, done := openTestStream(t, s, tok)
	defer cancel()

	// Silently set session revocation key in Redis
	err = rdb.Set(context.Background(), "jwt:sid:"+sid, "1", 24*time.Hour).Err()
	if err != nil {
		t.Fatalf("failed to set redis key: %v", err)
	}

	// The stream must close upon the next heartbeat check
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("stream did not close after session revocation")
	}

	if s.Limiter.ActiveSlots("user-sess-revoked") != 0 {
		t.Fatalf("expected 0 slots after heartbeat revocation, got %d", s.Limiter.ActiveSlots("user-sess-revoked"))
	}
}

// 3b. Heartbeat re-check: (b) token revoked -> stream closes
func TestSSE_HeartbeatRecheck_TokenRevoked_ClosesStream(t *testing.T) {
	jwtutil.Init("test-jwt-secret-0123456789abcdef")
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer rdb.Close()
	jwtutil.SetRedisClient(rdb)
	defer jwtutil.SetRedisClient(nil)

	s := testServer()
	s.HeartbeatInterval = 25 * time.Millisecond
	s.CheckEveryNth = 1

	userID := "user-token-revoked"
	tok, err := jwtutil.GenerateTokenWithSession(userID, "user", "tok@example.com", "sid-tok-revoked")
	if err != nil {
		t.Fatal(err)
	}

	_, cancel, done := openTestStream(t, s, tok)
	defer cancel()

	// Silently set user invalidation timestamp in Redis to future timestamp so issued token is revoked
	futureTs := time.Now().Add(1 * time.Hour).Unix()
	err = rdb.Set(context.Background(), "jwt:invalidated_before:"+userID, strconv.FormatInt(futureTs, 10), 24*time.Hour).Err()
	if err != nil {
		t.Fatalf("failed to set redis key: %v", err)
	}

	// The stream must close upon the next heartbeat check
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("stream did not close after token invalidation")
	}

	if s.Limiter.ActiveSlots(userID) != 0 {
		t.Fatalf("expected 0 slots after token revocation, got %d", s.Limiter.ActiveSlots(userID))
	}
}

// 3c. Heartbeat re-check: (c) transient Redis error -> stream stays open and is not closed
func TestSSE_HeartbeatRecheck_TransientRedisError_StaysOpen(t *testing.T) {
	jwtutil.Init("test-jwt-secret-0123456789abcdef")
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr(), MaxRetries: -1})
	defer rdb.Close()
	jwtutil.SetRedisClient(rdb)
	defer jwtutil.SetRedisClient(nil)

	s := testServer()
	s.HeartbeatInterval = 30 * time.Millisecond
	s.CheckEveryNth = 1

	sid := "sid-redis-transient"
	tok, err := jwtutil.GenerateTokenWithSession("user-redis-transient", "user", "transient@example.com", sid)
	if err != nil {
		t.Fatal(err)
	}

	rec, cancel, done := openTestStream(t, s, tok)
	defer func() {
		cancel()
		<-done
	}()

	// Inject transient Redis error
	mr.SetError("ERR mock transient redis error")

	// Wait across multiple heartbeat ticks
	// Stream must NOT close, and ping frames continue to arrive
	pingCount := 0
	deadline := time.After(300 * time.Millisecond)
loop:
	for {
		select {
		case chunk := <-rec.bodyCh:
			if chunk == ": ping\n\n" {
				pingCount++
				if pingCount >= 2 {
					break loop
				}
			}
		case <-done:
			t.Fatal("stream closed during transient Redis error; expected stream to stay open")
		case <-deadline:
			break loop
		}
	}

	if pingCount < 2 {
		t.Fatalf("expected at least 2 ping frames despite Redis error, got %d", pingCount)
	}

	if s.Limiter.ActiveSlots("user-redis-transient") != 1 {
		t.Fatalf("expected stream slot to remain active, got %d", s.Limiter.ActiveSlots("user-redis-transient"))
	}
}

// 3d. Heartbeat re-check: (d) after Redis recovers and a real revocation exists -> stream closes on the next check
func TestSSE_HeartbeatRecheck_RedisRecoversAndRevoked_ClosesStream(t *testing.T) {
	jwtutil.Init("test-jwt-secret-0123456789abcdef")
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr(), MaxRetries: -1})
	defer rdb.Close()
	jwtutil.SetRedisClient(rdb)
	defer jwtutil.SetRedisClient(nil)

	s := testServer()
	s.HeartbeatInterval = 30 * time.Millisecond
	s.CheckEveryNth = 1

	sid := "sid-recover-then-revoke"
	tok, err := jwtutil.GenerateTokenWithSession("user-recover", "user", "recover@example.com", sid)
	if err != nil {
		t.Fatal(err)
	}

	rec, cancel, done := openTestStream(t, s, tok)
	defer func() {
		cancel()
		<-done
	}()

	// 1. Inject transient Redis error
	mr.SetError("ERR mock transient redis error")

	// Verify stream survives across at least 1-2 heartbeat ticks
	pingReceived := false
	timer := time.After(200 * time.Millisecond)
loop:
	for {
		select {
		case chunk := <-rec.bodyCh:
			if chunk == ": ping\n\n" {
				pingReceived = true
				break loop
			}
		case <-done:
			t.Fatal("stream closed during transient Redis error")
		case <-timer:
			break loop
		}
	}
	if !pingReceived {
		t.Fatal("expected at least one ping frame while Redis returned error")
	}

	// 2. Clear Redis error and set revocation
	mr.SetError("")
	err = rdb.Set(context.Background(), "jwt:sid:"+sid, "1", 24*time.Hour).Err()
	if err != nil {
		t.Fatalf("failed to set redis revocation: %v", err)
	}

	// 3. Now Redis is healthy and session is revoked: stream must close on next check
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("stream did not close after Redis recovered and revocation was present")
	}

	if s.Limiter.ActiveSlots("user-recover") != 0 {
		t.Fatalf("expected 0 slots after revocation on recovered Redis, got %d", s.Limiter.ActiveSlots("user-recover"))
	}
}

// 5. No goroutine leak after close
func TestSSE_NoGoroutineLeakAfterClose(t *testing.T) {
	jwtutil.Init("test-jwt-secret-0123456789abcdef")
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer rdb.Close()
	jwtutil.SetRedisClient(rdb)
	defer jwtutil.SetRedisClient(nil)

	b := bus.NewRedisBus(rdb)
	s := New(store.NewMemoryStore(), b, "gw-secret", "internal-123")
	s.HeartbeatInterval = 50 * time.Millisecond
	stopListener := s.StartAccountEventsListener(context.Background())

	// Settle baseline goroutines
	time.Sleep(50 * time.Millisecond)
	baselineGoroutines := runtime.NumGoroutine()

	tok, err := jwtutil.GenerateTokenWithSession("user-leak", "user", "leak@example.com", "sid-leak")
	if err != nil {
		t.Fatal(err)
	}

	_, cancel, done := openTestStream(t, s, tok)
	defer cancel()

	// Close stream via session revocation
	if err := jwtutil.RevokeSession("sid-leak"); err != nil {
		t.Fatal(err)
	}

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("stream did not close on revocation")
	}

	// Stop listener to clean up its background subscription
	stopListener()

	// Wait up to 2 seconds for all background goroutines to finish
	var finalGoroutines int
	for i := 0; i < 40; i++ {
		time.Sleep(50 * time.Millisecond)
		finalGoroutines = runtime.NumGoroutine()
		if finalGoroutines <= baselineGoroutines {
			break
		}
	}

	if finalGoroutines > baselineGoroutines {
		t.Fatalf("goroutine leak detected: baseline=%d, final=%d", baselineGoroutines, finalGoroutines)
	}

	if s.Limiter.ActiveSlots("user-leak") != 0 {
		t.Fatalf("expected 0 slots, got %d", s.Limiter.ActiveSlots("user-leak"))
	}
}

// 6. Unchanged behaviour for non-revoked streams
func TestSSE_UnchangedBehaviour_NonRevokedStreams(t *testing.T) {
	jwtutil.Init("test-jwt-secret-0123456789abcdef")
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer rdb.Close()
	jwtutil.SetRedisClient(rdb)
	defer jwtutil.SetRedisClient(nil)

	s := testServer()
	s.HeartbeatInterval = 30 * time.Millisecond
	s.CheckEveryNth = 1

	tok, err := jwtutil.GenerateTokenWithSession("user-ok", "user", "ok@example.com", "sid-ok")
	if err != nil {
		t.Fatal(err)
	}

	rec, cancel, done := openTestStream(t, s, tok)
	defer cancel()

	// Stream survives multiple heartbeats without being closed
	pingCount := 0
	deadline := time.After(200 * time.Millisecond)
loop:
	for {
		select {
		case chunk := <-rec.bodyCh:
			if chunk == ": ping\n\n" {
				pingCount++
			}
		case <-done:
			t.Fatal("healthy stream closed unexpectedly")
		case <-deadline:
			break loop
		}
	}

	if pingCount < 2 {
		t.Fatalf("expected at least 2 ping frames, got %d", pingCount)
	}

	// Clean client disconnect
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("stream did not finish after client cancel")
	}
}

// 7. REQUIRE_DB test on real Redis
func TestSSE_RequireDB_RealRedisPubSub(t *testing.T) {
	redisURI := os.Getenv("REDIS_URI")
	if redisURI == "" {
		if os.Getenv("REQUIRE_DB") == "1" {
			t.Fatal("REQUIRE_DB=1 but REDIS_URI is unset")
		}
		t.Skip("skipping real Redis integration test: REDIS_URI unset")
	}

	jwtutil.Init("test-jwt-secret-0123456789abcdef")
	rdb, err := redis.ParseURL(redisURI)
	if err != nil {
		t.Fatalf("parse redis URI: %v", err)
	}
	client := redis.NewClient(rdb)
	defer client.Close()
	jwtutil.SetRedisClient(client)
	defer jwtutil.SetRedisClient(nil)

	b := bus.NewRedisBus(client)
	s := New(store.NewMemoryStore(), b, "gw-secret", "internal-123")
	stopListener := s.StartAccountEventsListener(context.Background())
	defer stopListener()

	sid := "sid-real-redis-1"
	tok, err := jwtutil.GenerateTokenWithSession("user-real-redis", "user", "real@example.com", sid)
	if err != nil {
		t.Fatal(err)
	}

	_, cancel, done := openTestStream(t, s, tok)
	defer cancel()

	// Revoke session via jwtutil
	if err := jwtutil.RevokeSession(sid); err != nil {
		t.Fatalf("RevokeSession failed: %v", err)
	}

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("stream did not close on real Redis session revocation")
	}

	if s.Limiter.ActiveSlots("user-real-redis") != 0 {
		t.Fatalf("expected 0 slots on real Redis, got %d", s.Limiter.ActiveSlots("user-real-redis"))
	}
}
