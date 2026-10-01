package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/omarmaarouf18/wael-app/notification-service/internal/bus"
	"github.com/omarmaarouf18/wael-app/notification-service/internal/store"
	"github.com/omarmaarouf18/wael-app/shared/infra/jwtutil"
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

func TestStream_ConcurrentCap_Exceeded(t *testing.T) {
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

	// Alice stream 3 (cap+1) -> 429
	req3 := httptest.NewRequest(http.MethodGet, "/notifications/stream", nil)
	req3.Header.Set("X-Gateway-Secret", "gw-secret")
	req3.Header.Set("Authorization", "Bearer "+tokenAlice)
	rec3 := newSyncRecorder()
	s.GatewayAuth(http.HandlerFunc(s.Stream)).ServeHTTP(rec3, req3)

	if rec3.Code() != http.StatusTooManyRequests {
		t.Fatalf("expected 429 for stream cap+1, got %d (%s)", rec3.Code(), rec3.String())
	}
	var errBody map[string]any
	if err := json.Unmarshal([]byte(rec3.String()), &errBody); err == nil {
		if errBody["code"] != "stream_cap_exceeded" {
			t.Errorf("expected code stream_cap_exceeded, got %v", errBody["code"])
		}
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

	// Release Alice stream 1 -> slot freed
	cancel1()
	<-done1
	if s.Limiter.ActiveSlots("alice") != 1 {
		t.Fatalf("expected 1 active slot for alice after disconnect, got %d", s.Limiter.ActiveSlots("alice"))
	}

	// Alice can now open another stream -> succeeds
	ctx4, cancel4 := context.WithCancel(context.Background())
	defer cancel4()
	req4 := httptest.NewRequest(http.MethodGet, "/notifications/stream", nil).WithContext(ctx4)
	req4.Header.Set("X-Gateway-Secret", "gw-secret")
	req4.Header.Set("Authorization", "Bearer "+tokenAlice)
	rec4 := newSyncRecorder()
	done4 := make(chan struct{})
	go func() {
		defer close(done4)
		s.GatewayAuth(http.HandlerFunc(s.Stream)).ServeHTTP(rec4, req4)
	}()
	waitForBody(t, rec4, ": connected")

	// Disconnect all
	cancel2()
	<-done2
	cancel4()
	<-done4
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

	// 5. Test double-release safety on acquireStreamSlot
	release1, ok := limiter.acquireStreamSlot("user1")
	if !ok {
		t.Fatal("expected slot acquisition to succeed")
	}
	if limiter.ActiveSlots("user1") != 1 {
		t.Fatalf("expected 1 active slot, got %d", limiter.ActiveSlots("user1"))
	}
	// Calling release twice must not decrement below 0 (sync.Once protection)
	release1()
	release1()
	if limiter.ActiveSlots("user1") != 0 {
		t.Fatalf("expected 0 active slots, got %d", limiter.ActiveSlots("user1"))
	}
}
