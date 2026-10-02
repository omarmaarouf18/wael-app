package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
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

func testServer() *Server {
	jwtutil.Init("test-jwt-secret-0123456789abcdef")
	return New(store.NewMemoryStore(), bus.NewMemoryBus(), "gw-secret", "internal-123")
}

func userToken(t *testing.T, userID string) string {
	t.Helper()
	token, err := jwtutil.GenerateToken(userID, "user", userID+"@example.com")
	if err != nil {
		t.Fatal(err)
	}
	return token
}

func doUser(t *testing.T, s *Server, method, path, token string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatal(err)
		}
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("X-Gateway-Secret", "gw-secret")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	var h http.HandlerFunc
	switch {
	case method == http.MethodGet && strings.HasPrefix(path, "/notifications/list"):
		h = s.List
	case method == http.MethodPost && strings.HasPrefix(path, "/notifications/read"):
		h = s.MarkRead
	default:
		t.Fatalf("unknown route %s %s", method, path)
	}
	s.GatewayAuth(h).ServeHTTP(rec, req)
	return rec
}

func doPush(t *testing.T, s *Server, internalToken string, body map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(body); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/internal/push", &buf)
	if internalToken != "" {
		req.Header.Set("X-Internal-Token", internalToken)
	}
	rec := httptest.NewRecorder()
	s.GatewayAuth(s.InternalAuth(http.HandlerFunc(s.Push))).ServeHTTP(rec, req)
	return rec
}

func pushNotif(t *testing.T, s *Server, userID, title string) string {
	t.Helper()
	rec := doPush(t, s, "internal-123", map[string]string{"user_id": userID, "title": title, "body": "b", "type": "system"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("push status = %d (%s)", rec.Code, rec.Body.String())
	}
	var out map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	return out["id"]
}

func TestPush_AuthGuards(t *testing.T) {
	s := testServer()
	rec := doPush(t, s, "wrong-token", map[string]string{"user_id": "u1", "title": "t"})
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("wrong internal token = %d, want 401", rec.Code)
	}
	rec = doPush(t, s, "", map[string]string{"user_id": "u1", "title": "t"})
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("missing internal token = %d, want 401", rec.Code)
	}
	// User routes require the gateway secret.
	req := httptest.NewRequest(http.MethodGet, "/notifications/list", nil)
	rec2 := httptest.NewRecorder()
	s.GatewayAuth(http.HandlerFunc(s.List)).ServeHTTP(rec2, req)
	if rec2.Code != http.StatusUnauthorized {
		t.Fatalf("missing gateway secret = %d, want 401", rec2.Code)
	}
}

func TestList_PaginationAndIsolation(t *testing.T) {
	s := testServer()
	pushNotif(t, s, "alice", "first")
	time.Sleep(5 * time.Millisecond)
	pushNotif(t, s, "alice", "second")
	time.Sleep(5 * time.Millisecond)
	pushNotif(t, s, "alice", "third")
	pushNotif(t, s, "bob", "other")

	token := userToken(t, "alice")
	rec := doUser(t, s, http.MethodGet, "/notifications/list?page=1&limit=2", token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("list = %d (%s)", rec.Code, rec.Body.String())
	}
	var out struct {
		Notifications []map[string]any `json:"notifications"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if len(out.Notifications) != 2 || out.Notifications[0]["title"] != "third" {
		t.Fatalf("page1 = %v", out.Notifications)
	}
	rec = doUser(t, s, http.MethodGet, "/notifications/list?page=2&limit=2", token, nil)
	var out2 struct {
		Notifications []map[string]any `json:"notifications"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&out2); err != nil {
		t.Fatal(err)
	}
	if len(out2.Notifications) != 1 || out2.Notifications[0]["title"] != "first" {
		t.Fatalf("page2 = %v", out2.Notifications)
	}
}

func TestMarkRead_OwnerScope(t *testing.T) {
	s := testServer()
	id := pushNotif(t, s, "alice", "hello")
	alice := userToken(t, "alice")
	rec := doUser(t, s, http.MethodPost, "/notifications/read", alice, map[string]string{"id": id})
	if rec.Code != http.StatusOK {
		t.Fatalf("read = %d (%s)", rec.Code, rec.Body.String())
	}
	bob := userToken(t, "bob")
	rec = doUser(t, s, http.MethodPost, "/notifications/read", bob, map[string]string{"id": id})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("cross-user read = %d, want 404", rec.Code)
	}
}

type syncRecorder struct {
	mu     sync.Mutex
	header http.Header
	buf    bytes.Buffer
	code   int
}

func newSyncRecorder() *syncRecorder {
	return &syncRecorder{header: make(http.Header)}
}

func (r *syncRecorder) Header() http.Header {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.header
}

func (r *syncRecorder) WriteHeader(code int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.code == 0 {
		r.code = code
	}
}

func (r *syncRecorder) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.code == 0 {
		r.code = http.StatusOK
	}
	return r.buf.Write(p)
}

func (r *syncRecorder) Flush() {}

func (r *syncRecorder) Code() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.code
}

func (r *syncRecorder) String() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.buf.String()
}

func waitForBody(t *testing.T, rec *syncRecorder, substr string) {
	t.Helper()
	for i := 0; i < 40; i++ {
		if strings.Contains(rec.String(), substr) {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %q (body %q)", substr, rec.String())
}

func TestStream_BearerAndQueryToken(t *testing.T) {
	for _, useQuery := range []bool{false, true} {
		s := testServer()
		token := userToken(t, "alice")
		ctx, cancel := context.WithCancel(context.Background())
		target := "/notifications/stream"
		if useQuery {
			target += "?token=" + token
		}
		req := httptest.NewRequest(http.MethodGet, target, nil).WithContext(ctx)
		req.Header.Set("X-Gateway-Secret", "gw-secret")
		if !useQuery {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		rec := newSyncRecorder()
		done := make(chan struct{})
		go func() {
			defer close(done)
			s.GatewayAuth(http.HandlerFunc(s.Stream)).ServeHTTP(rec, req)
		}()
		waitForBody(t, rec, ": connected")

		pushRec := doPush(t, s, "internal-123", map[string]string{"user_id": "alice", "title": "hello-live", "body": "b"})
		if pushRec.Code != http.StatusCreated {
			cancel()
			<-done
			t.Fatalf("push = %d", pushRec.Code)
		}
		waitForBody(t, rec, "hello-live")
		cancel()
		<-done
	}
}

func TestStream_Unauthenticated(t *testing.T) {
	s := testServer()
	req := httptest.NewRequest(http.MethodGet, "/notifications/stream", nil)
	req.Header.Set("X-Gateway-Secret", "gw-secret")
	rec := newSyncRecorder()
	s.GatewayAuth(http.HandlerFunc(s.Stream)).ServeHTTP(rec, req)
	if rec.Code() != http.StatusUnauthorized {
		t.Fatalf("no token = %d, want 401", rec.Code())
	}
}

func TestStream_ConcurrentCap_NewestWinsOldestEvicted(t *testing.T) {
	s := testServer()
	s.Limiter = NewStreamLimiter(2, 50, time.Minute)

	tokenAlice := userToken(t, "alice")
	tokenBob := userToken(t, "bob")

	// Alice stream 1
	ctx1, cancel1 := context.WithCancel(context.Background())
	defer cancel1()
	req1 := httptest.NewRequest(http.MethodGet, "/notifications/stream", nil).WithContext(ctx1)
	req1.Header.Set("X-Gateway-Secret", "gw-secret")
	req1.Header.Set("Authorization", "Bearer "+tokenAlice)
	rec1 := newSyncRecorder()
	done1 := make(chan struct{})
	go func() {
		defer close(done1)
		s.GatewayAuth(http.HandlerFunc(s.Stream)).ServeHTTP(rec1, req1)
	}()
	waitForBody(t, rec1, ": connected")

	// Alice stream 2
	ctx2, cancel2 := context.WithCancel(context.Background())
	defer cancel2()
	req2 := httptest.NewRequest(http.MethodGet, "/notifications/stream", nil).WithContext(ctx2)
	req2.Header.Set("X-Gateway-Secret", "gw-secret")
	req2.Header.Set("Authorization", "Bearer "+tokenAlice)
	rec2 := newSyncRecorder()
	done2 := make(chan struct{})
	go func() {
		defer close(done2)
		s.GatewayAuth(http.HandlerFunc(s.Stream)).ServeHTTP(rec2, req2)
	}()
	waitForBody(t, rec2, ": connected")

	if s.Limiter.ActiveSlots("alice") != 2 {
		t.Fatalf("expected 2 active slots for alice, got %d", s.Limiter.ActiveSlots("alice"))
	}

	// Alice stream 3 (cap+1) -> under "newest wins", evicts stream 1, accepts stream 3
	ctx3, cancel3 := context.WithCancel(context.Background())
	defer cancel3()
	req3 := httptest.NewRequest(http.MethodGet, "/notifications/stream", nil).WithContext(ctx3)
	req3.Header.Set("X-Gateway-Secret", "gw-secret")
	req3.Header.Set("Authorization", "Bearer "+tokenAlice)
	rec3 := newSyncRecorder()
	done3 := make(chan struct{})
	go func() {
		defer close(done3)
		s.GatewayAuth(http.HandlerFunc(s.Stream)).ServeHTTP(rec3, req3)
	}()

	// 1. Oldest stream's handler (done1) returns due to eviction
	select {
	case <-done1:
		// Stream 1 returned!
	case <-time.After(2 * time.Second):
		t.Fatal("expected oldest stream 1 handler to return upon eviction")
	}

	// 2. Stream 3 connects successfully
	waitForBody(t, rec3, ": connected")

	// 3. Slot count remains at cap (2), release after eviction does not double free
	if s.Limiter.ActiveSlots("alice") != 2 {
		t.Fatalf("expected active slots for alice to equal cap (2), got %d", s.Limiter.ActiveSlots("alice"))
	}

	// Bob stream 1 -> succeeds (per-account isolation)
	ctxBob, cancelBob := context.WithCancel(context.Background())
	defer cancelBob()
	reqBob := httptest.NewRequest(http.MethodGet, "/notifications/stream", nil).WithContext(ctxBob)
	reqBob.Header.Set("X-Gateway-Secret", "gw-secret")
	reqBob.Header.Set("Authorization", "Bearer "+tokenBob)
	recBob := newSyncRecorder()
	doneBob := make(chan struct{})
	go func() {
		defer close(doneBob)
		s.GatewayAuth(http.HandlerFunc(s.Stream)).ServeHTTP(recBob, reqBob)
	}()
	waitForBody(t, recBob, ": connected")
	if s.Limiter.ActiveSlots("bob") != 1 {
		t.Fatalf("expected 1 active slot for bob, got %d", s.Limiter.ActiveSlots("bob"))
	}

	// Disconnect all remaining active streams
	cancel2()
	<-done2
	cancel3()
	<-done3
	cancelBob()
	<-doneBob

	if s.Limiter.TotalActiveSlots() != 0 {
		t.Fatalf("expected 0 total active slots after all disconnect, got %d", s.Limiter.TotalActiveSlots())
	}
}

func TestStream_RateLimit_WithRetryAfter(t *testing.T) {
	s := testServer()
	// Rate limit: 3 opens per minute, cap 10
	s.Limiter = NewStreamLimiter(10, 3, time.Minute)

	tokenAlice := userToken(t, "alice")
	tokenBob := userToken(t, "bob")

	// Alice opens 3 times sequentially -> all 3 succeed
	for i := 0; i < 3; i++ {
		ctx, cancel := context.WithCancel(context.Background())
		req := httptest.NewRequest(http.MethodGet, "/notifications/stream", nil).WithContext(ctx)
		req.Header.Set("X-Gateway-Secret", "gw-secret")
		req.Header.Set("Authorization", "Bearer "+tokenAlice)
		rec := newSyncRecorder()
		done := make(chan struct{})
		go func() {
			defer close(done)
			s.GatewayAuth(http.HandlerFunc(s.Stream)).ServeHTTP(rec, req)
		}()
		waitForBody(t, rec, ": connected")
		cancel()
		<-done
	}

	// Alice 4th attempt -> 429 with Retry-After header
	req4 := httptest.NewRequest(http.MethodGet, "/notifications/stream", nil)
	req4.Header.Set("X-Gateway-Secret", "gw-secret")
	req4.Header.Set("Authorization", "Bearer "+tokenAlice)
	rec4 := newSyncRecorder()
	s.GatewayAuth(http.HandlerFunc(s.Stream)).ServeHTTP(rec4, req4)

	if rec4.Code() != http.StatusTooManyRequests {
		t.Fatalf("expected 429 for rate limit exceeded, got %d (%s)", rec4.Code(), rec4.String())
	}
	retryAfterStr := rec4.Header().Get("Retry-After")
	if retryAfterStr == "" {
		t.Fatal("expected Retry-After header on 429 rate limit response")
	}
	retryAfterSec, err := strconv.Atoi(retryAfterStr)
	if err != nil || retryAfterSec <= 0 {
		t.Fatalf("expected positive integer Retry-After, got %q (err: %v)", retryAfterStr, err)
	}

	// Bob can still connect (per-account rate limit, keyed on JWT user id, not IP)
	ctxBob, cancelBob := context.WithCancel(context.Background())
	reqBob := httptest.NewRequest(http.MethodGet, "/notifications/stream", nil).WithContext(ctxBob)
	reqBob.Header.Set("X-Gateway-Secret", "gw-secret")
	reqBob.Header.Set("Authorization", "Bearer "+tokenBob)
	recBob := newSyncRecorder()
	doneBob := make(chan struct{})
	go func() {
		defer close(doneBob)
		s.GatewayAuth(http.HandlerFunc(s.Stream)).ServeHTTP(recBob, reqBob)
	}()
	waitForBody(t, recBob, ": connected")
	cancelBob()
	<-doneBob
}

func TestStream_ReleaseOnContextCancel(t *testing.T) {
	s := testServer()
	token := userToken(t, "charlie")

	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodGet, "/notifications/stream", nil).WithContext(ctx)
	req.Header.Set("X-Gateway-Secret", "gw-secret")
	req.Header.Set("Authorization", "Bearer "+token)
	rec := newSyncRecorder()
	done := make(chan struct{})
	go func() {
		defer close(done)
		s.GatewayAuth(http.HandlerFunc(s.Stream)).ServeHTTP(rec, req)
	}()
	waitForBody(t, rec, ": connected")

	if s.Limiter.ActiveSlots("charlie") != 1 {
		t.Fatalf("expected 1 active slot, got %d", s.Limiter.ActiveSlots("charlie"))
	}

	cancel()
	<-done

	if s.Limiter.ActiveSlots("charlie") != 0 {
		t.Fatalf("expected 0 active slots after context cancel, got %d", s.Limiter.ActiveSlots("charlie"))
	}
	if s.Limiter.TotalActiveSlots() != 0 {
		t.Fatalf("expected 0 total active slots, got %d", s.Limiter.TotalActiveSlots())
	}
}

func TestStream_NOpens_NDisconnects_ZeroSlots_UnderConcurrency(t *testing.T) {
	s := testServer()
	// High capacity for concurrent open test
	s.Limiter = NewStreamLimiter(50, 100, time.Minute)

	const n = 20
	var wg sync.WaitGroup
	wg.Add(n)

	for i := 0; i < n; i++ {
		userID := fmt.Sprintf("concurrent-user-%d", i%5) // 5 users, 4 streams each
		go func(uid string) {
			defer wg.Done()
			token := userToken(t, uid)
			ctx, cancel := context.WithCancel(context.Background())
			req := httptest.NewRequest(http.MethodGet, "/notifications/stream", nil).WithContext(ctx)
			req.Header.Set("X-Gateway-Secret", "gw-secret")
			req.Header.Set("Authorization", "Bearer "+token)
			rec := newSyncRecorder()
			done := make(chan struct{})
			go func() {
				defer close(done)
				s.GatewayAuth(http.HandlerFunc(s.Stream)).ServeHTTP(rec, req)
			}()
			waitForBody(t, rec, ": connected")
			// Disconnect after connection verified
			cancel()
			<-done
		}(userID)
	}

	wg.Wait()

	if s.Limiter.TotalActiveSlots() != 0 {
		t.Fatalf("expected 0 total active slots after %d opens and disconnects, got %d", n, s.Limiter.TotalActiveSlots())
	}
}

func TestStreamLimiter_DeterministicSlidingWindow(t *testing.T) {
	limiter := NewStreamLimiter(2, 3, time.Minute)
	now := time.Now()

	// 1. Initial attempts at t=0
	if allowed, _ := limiter.allowStreamOpenAt("user1", now); !allowed {
		t.Fatal("expected attempt 1 to be allowed")
	}
	if allowed, _ := limiter.allowStreamOpenAt("user1", now.Add(10*time.Second)); !allowed {
		t.Fatal("expected attempt 2 to be allowed")
	}
	if allowed, _ := limiter.allowStreamOpenAt("user1", now.Add(20*time.Second)); !allowed {
		t.Fatal("expected attempt 3 to be allowed")
	}

	// 2. 4th attempt at t=30s -> rejected with retryAfter ~30s
	allowed, retryAfter := limiter.allowStreamOpenAt("user1", now.Add(30*time.Second))
	if allowed {
		t.Fatal("expected attempt 4 to be rejected")
	}
	if retryAfter < 29 || retryAfter > 31 {
		t.Fatalf("expected retryAfter ~30s, got %d", retryAfter)
	}

	// 3. Different user at t=30s -> allowed (isolation)
	if allowed, _ := limiter.allowStreamOpenAt("user2", now.Add(30*time.Second)); !allowed {
		t.Fatal("expected user2 attempt to be allowed")
	}

	// 4. After window passes for attempt 1 (t=61s) -> user1 allowed again
	allowed, _ = limiter.allowStreamOpenAt("user1", now.Add(61*time.Second))
	if !allowed {
		t.Fatal("expected attempt at t=61s to be allowed as first attempt expired")
	}

	// 5. Test newest-wins eviction and double-release safety on acquireStreamSlot
	ctx1, release1 := limiter.acquireStreamSlot(context.Background(), "user1")
	if limiter.ActiveSlots("user1") != 1 {
		t.Fatalf("expected 1 active slot, got %d", limiter.ActiveSlots("user1"))
	}
	_, release2 := limiter.acquireStreamSlot(context.Background(), "user1")
	if limiter.ActiveSlots("user1") != 2 {
		t.Fatalf("expected 2 active slots, got %d", limiter.ActiveSlots("user1"))
	}
	// 3rd stream for user1 (cap is 2) -> evicts stream 1
	_, release3 := limiter.acquireStreamSlot(context.Background(), "user1")
	if limiter.ActiveSlots("user1") != 2 {
		t.Fatalf("expected slot count to remain at cap (2), got %d", limiter.ActiveSlots("user1"))
	}
	select {
	case <-ctx1.Done():
		// Stream 1 context canceled by eviction
	default:
		t.Fatal("expected evicted stream 1 context to be canceled")
	}

	// Calling release on evicted stream must not double free
	release1()
	release1()
	if limiter.ActiveSlots("user1") != 2 {
		t.Fatalf("expected slot count to remain 2 after calling release on evicted stream, got %d", limiter.ActiveSlots("user1"))
	}

	// Releasing active streams decrements to 0
	release2()
	release3()
	if limiter.ActiveSlots("user1") != 0 {
		t.Fatalf("expected 0 active slots, got %d", limiter.ActiveSlots("user1"))
	}
}

type failResponseWriter struct {
	mu         sync.Mutex
	header     http.Header
	failOnCall int
	callCount  int
}

func newFailResponseWriter(failOnCall int) *failResponseWriter {
	return &failResponseWriter{
		header:     make(http.Header),
		failOnCall: failOnCall,
	}
}

func (w *failResponseWriter) Header() http.Header {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.header
}

func (w *failResponseWriter) WriteHeader(statusCode int) {}

func (w *failResponseWriter) Write(b []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.callCount++
	if w.failOnCall > 0 && w.callCount >= w.failOnCall {
		return 0, errors.New("simulated broken pipe")
	}
	return len(b), nil
}

func (w *failResponseWriter) Flush() {}

func TestStream_WriteError_ReturnsAndReleasesSlot(t *testing.T) {
	s := testServer()
	token := userToken(t, "user-write-fail")

	// 1. Initial write failure (fail on call 1 = ": connected\n\n")
	w1 := newFailResponseWriter(1)
	req1 := httptest.NewRequest(http.MethodGet, "/notifications/stream", nil)
	req1.Header.Set("X-Gateway-Secret", "gw-secret")
	req1.Header.Set("Authorization", "Bearer "+token)

	s.GatewayAuth(http.HandlerFunc(s.Stream)).ServeHTTP(w1, req1)
	if s.Limiter.ActiveSlots("user-write-fail") != 0 {
		t.Fatalf("expected 0 active slots after initial write error, got %d", s.Limiter.ActiveSlots("user-write-fail"))
	}

	// 2. Subsequent write failure during event push (fail on call 2 = data event)
	w2 := newFailResponseWriter(2)
	req2 := httptest.NewRequest(http.MethodGet, "/notifications/stream", nil)
	req2.Header.Set("X-Gateway-Secret", "gw-secret")
	req2.Header.Set("Authorization", "Bearer "+token)

	done2 := make(chan struct{})
	go func() {
		defer close(done2)
		s.GatewayAuth(http.HandlerFunc(s.Stream)).ServeHTTP(w2, req2)
	}()

	// Wait for slot to be acquired
	for i := 0; i < 50; i++ {
		if s.Limiter.ActiveSlots("user-write-fail") == 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if s.Limiter.ActiveSlots("user-write-fail") != 1 {
		t.Fatalf("expected 1 active slot, got %d", s.Limiter.ActiveSlots("user-write-fail"))
	}

	// Push an event; write call 2 fails with error
	_ = s.Bus.Publish(context.Background(), &models.Notification{
		ID:        "n1",
		UserID:    "user-write-fail",
		Type:      "test",
		Title:     "test",
		Body:      "test",
		CreatedAt: time.Now(),
	})

	select {
	case <-done2:
		// Handler returned on write error
	case <-time.After(2 * time.Second):
		t.Fatal("handler did not return after event write error")
	}

	if s.Limiter.ActiveSlots("user-write-fail") != 0 {
		t.Fatalf("expected 0 active slots after event write error, got %d", s.Limiter.ActiveSlots("user-write-fail"))
	}
}

func TestNotifications_RevokedSessionTokenRejected(t *testing.T) {
	jwtutil.Init("test-jwt-secret-0123456789abcdef")
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer rdb.Close()
	jwtutil.SetRedisClient(rdb)
	defer jwtutil.SetRedisClient(nil)

	s := testServer()
	sid := "test-session-sid-456"
	token, err := jwtutil.GenerateTokenWithSession("user-revoked", "user", "revoked@example.com", sid)
	if err != nil {
		t.Fatalf("generate token: %v", err)
	}

	// Active token succeeds
	rec := doUser(t, s, http.MethodGet, "/notifications/list", token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 on active session token, got %d", rec.Code)
	}

	// Revoke the session sid in Redis
	if err := jwtutil.RevokeSession(sid); err != nil {
		t.Fatalf("revoke session: %v", err)
	}

	// List with revoked session token fails with 401
	rec = doUser(t, s, http.MethodGet, "/notifications/list", token, nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 on revoked session token, got %d", rec.Code)
	}
}
