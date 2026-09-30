package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
