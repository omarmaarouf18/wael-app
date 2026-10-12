package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/omarmaarouf18/wael-app/notification-service/internal/bus"
	"github.com/omarmaarouf18/wael-app/notification-service/internal/models"
	"github.com/omarmaarouf18/wael-app/notification-service/internal/store"
)

// fakeUpstream stubs auth-service (verify + accounts) and academy-service
// (owner-ids) for broadcast tests.
type fakeUpstream struct {
	mu       sync.Mutex
	accounts []string
	owners   map[string][]string
	down     bool
}

func (f *fakeUpstream) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/internal/admin/verify", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		down := f.down
		f.mu.Unlock()
		if down {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		if r.Header.Get("X-Internal-Token") != "internal-123" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch r.Header.Get("X-Admin-Token") {
		case "admin-token":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"admin_id":"admin-1","name":"Sara"}`))
		case "locked-token":
			w.WriteHeader(http.StatusTooManyRequests)
		default:
			w.WriteHeader(http.StatusUnauthorized)
		}
	})
	mux.HandleFunc("/internal/admin/accounts", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Internal-Token") != "internal-123" || r.Header.Get("X-Admin-Token") != "admin-token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if r.URL.Query().Get("status") != "active" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		f.mu.Lock()
		ids := append([]string{}, f.accounts...)
		f.mu.Unlock()
		page, limit := 1, 100
		if p := r.URL.Query().Get("page"); p != "" {
			n := 0
			for _, c := range p {
				if c < '0' || c > '9' {
					n = 0
					break
				}
				n = n*10 + int(c-'0')
			}
			if n >= 1 {
				page = n
			}
		}
		start := (page - 1) * limit
		if start > len(ids) {
			start = len(ids)
		}
		end := start + limit
		if end > len(ids) {
			end = len(ids)
		}
		items := make([]map[string]string, 0, len(ids[start:end]))
		for _, id := range ids[start:end] {
			items = append(items, map[string]string{"id": id})
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"items": items, "total": len(ids), "page": page, "limit": limit,
		})
	})
	mux.HandleFunc("/internal/admin/subjects/", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Internal-Token") != "internal-123" || r.Header.Get("X-Admin-Token") != "admin-token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		rest := strings.TrimPrefix(r.URL.Path, "/internal/admin/subjects/")
		parts := strings.Split(strings.Trim(rest, "/"), "/")
		if len(parts) != 2 || parts[1] != "owner-ids" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		f.mu.Lock()
		ids, ok := f.owners[parts[0]]
		f.mu.Unlock()
		if !ok {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":"subject not found","code":"subject_not_found"}`))
			return
		}
		cursor := r.URL.Query().Get("cursor")
		out := []string{}
		for _, id := range ids {
			if cursor == "" || id > cursor {
				out = append(out, id)
			}
		}
		next := ""
		if len(out) > 100 {
			out = out[:100]
			next = out[len(out)-1]
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"subject_id": parts[0], "user_ids": out, "next_cursor": next, "total": len(ids),
		})
	})
	return mux
}

func testAdminServer(upstream string, st store.Store, b bus.Bus, limit SendLimiter) *AdminServer {
	if st == nil {
		st = store.NewMemoryStore()
	}
	if b == nil {
		b = bus.NewMemoryBus()
	}
	if limit == nil {
		limit = NewMemorySendLimiter(1000, time.Hour)
	}
	return &AdminServer{
		Store:           st,
		Bus:             b,
		InternalToken:   "internal-123",
		AuthAdminURL:    upstream,
		AcademyAdminURL: upstream,
		Limiter:         limit,
	}
}

func doAdmin(t *testing.T, a *AdminServer, method, path, adminToken string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatal(err)
		}
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("X-Internal-Token", "internal-123")
	if adminToken != "" {
		req.Header.Set("X-Admin-Token", adminToken)
	}
	rec := httptest.NewRecorder()
	a.AdminHandler().ServeHTTP(rec, req)
	return rec
}

func adminCode(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var v map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&v); err != nil {
		t.Fatalf("decode error body: %v", err)
	}
	code, _ := v["code"].(string)
	return code
}

func waitBroadcast(t *testing.T, st store.Store, id string) *models.Broadcast {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		b, err := st.GetBroadcast(context.Background(), id)
		if err != nil {
			t.Fatalf("GetBroadcast: %v", err)
		}
		if b.Status == models.BroadcastDone || b.Status == models.BroadcastPartial || b.Status == models.BroadcastFailed {
			return b
		}
		if time.Now().After(deadline) {
			t.Fatalf("broadcast %s stuck in %s", id, b.Status)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestBroadcast_AuthGuards(t *testing.T) {
	up := httptest.NewServer((&fakeUpstream{}).handler())
	defer up.Close()
	a := testAdminServer(up.URL, nil, nil, nil)

	body := map[string]string{"audience": "all"}
	// Missing internal token.
	req := httptest.NewRequest(http.MethodPost, "/internal/admin/broadcasts/preview", strings.NewReader(`{"audience":"all"}`))
	req.Header.Set("X-Admin-Token", "admin-token")
	rec := httptest.NewRecorder()
	a.AdminHandler().ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("missing internal token = %d, want 401", rec.Code)
	}
	// Wrong internal token.
	req = httptest.NewRequest(http.MethodPost, "/internal/admin/broadcasts/preview", strings.NewReader(`{"audience":"all"}`))
	req.Header.Set("X-Internal-Token", "wrong")
	req.Header.Set("X-Admin-Token", "admin-token")
	rec = httptest.NewRecorder()
	a.AdminHandler().ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("wrong internal token = %d, want 401", rec.Code)
	}
	// Missing admin token.
	rec = doAdmin(t, a, http.MethodPost, "/internal/admin/broadcasts/preview", "", body)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("missing admin token = %d, want 401", rec.Code)
	}
	// Unknown admin token (verify 401).
	rec = doAdmin(t, a, http.MethodPost, "/internal/admin/broadcasts/preview", "nope", body)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("bad admin token = %d, want 401", rec.Code)
	}
	// Locked admin token (verify 429 relays).
	rec = doAdmin(t, a, http.MethodPost, "/internal/admin/broadcasts/preview", "locked-token", body)
	if rec.Code != http.StatusTooManyRequests || adminCode(t, rec) != "locked_out" {
		t.Fatalf("locked token = %d %s, want 429 locked_out", rec.Code, rec.Body.String())
	}
}

func TestBroadcast_VerifyDownFailsClosed(t *testing.T) {
	fu := &fakeUpstream{}
	up := httptest.NewServer(fu.handler())
	a := testAdminServer(up.URL, nil, nil, nil)
	up.Close()
	fu.mu.Lock()
	fu.down = true
	fu.mu.Unlock()
	rec := doAdmin(t, a, http.MethodPost, "/internal/admin/broadcasts/preview", "admin-token", map[string]string{"audience": "all"})
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("verify down = %d, want 503", rec.Code)
	}
}

func TestBroadcast_Validation(t *testing.T) {
	fu := &fakeUpstream{accounts: []string{"u1"}}
	up := httptest.NewServer(fu.handler())
	defer up.Close()
	a := testAdminServer(up.URL, nil, nil, nil)

	valid := map[string]string{
		"audience": "all", "title_ar": "عنوان", "title_en": "Title", "body_ar": "نص الرسالة", "body_en": "Body",
	}
	cases := []struct {
		name string
		body map[string]string
		code string
	}{
		{"bad audience", map[string]string{"audience": "everyone", "title_ar": "ع", "body_ar": "ن"}, "invalid_audience"},
		{"malformed subject id", map[string]string{"audience": "subject:bad id!", "title_ar": "ع", "body_ar": "ن"}, "invalid_audience"},
		{"missing title", map[string]string{"audience": "all", "body_ar": "ن"}, "invalid_title"},
		{"long title", map[string]string{"audience": "all", "title_ar": strings.Repeat("ع", 81), "body_ar": "ن"}, "invalid_title"},
		{"missing body", map[string]string{"audience": "all", "title_ar": "ع"}, "invalid_body"},
		{"long body", map[string]string{"audience": "all", "title_ar": "ع", "body_ar": strings.Repeat("ن", 501)}, "invalid_body"},
		{"control chars", map[string]string{"audience": "all", "title_ar": "ع", "body_ar": "ن\x00ص"}, "invalid_body"},
		{"forbidden ar", map[string]string{"audience": "all", "title_ar": "ادفع الآن", "body_ar": "ن"}, "broadcast_forbidden_word"},
		{"forbidden en", map[string]string{"audience": "all", "title_ar": "ع", "title_en": "pay now", "body_ar": "ن"}, "broadcast_forbidden_word"},
	}
	for _, tc := range cases {
		rec := doAdmin(t, a, http.MethodPost, "/internal/admin/broadcasts", "admin-token", tc.body)
		if rec.Code != http.StatusBadRequest || adminCode(t, rec) != tc.code {
			t.Errorf("%s = %d %s, want 400 %s", tc.name, rec.Code, rec.Body.String(), tc.code)
		}
	}
	// The valid shape passes validation (201; fan-out runs async).
	rec := doAdmin(t, a, http.MethodPost, "/internal/admin/broadcasts", "admin-token", valid)
	if rec.Code != http.StatusCreated {
		t.Fatalf("valid create = %d %s, want 201", rec.Code, rec.Body.String())
	}
}

func TestBroadcast_Preview(t *testing.T) {
	fu := &fakeUpstream{accounts: []string{"u1", "u2", "u3"}, owners: map[string][]string{"s1": {"u1", "u2"}}}
	up := httptest.NewServer(fu.handler())
	defer up.Close()
	a := testAdminServer(up.URL, nil, nil, nil)

	rec := doAdmin(t, a, http.MethodPost, "/internal/admin/broadcasts/preview", "admin-token", map[string]string{"audience": "all"})
	if rec.Code != http.StatusOK {
		t.Fatalf("preview all = %d %s", rec.Code, rec.Body.String())
	}
	var pv map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&pv); err != nil {
		t.Fatal(err)
	}
	if pv["recipient_count"] != float64(3) {
		t.Fatalf("preview all count = %v, want 3", pv["recipient_count"])
	}
	rec = doAdmin(t, a, http.MethodPost, "/internal/admin/broadcasts/preview", "admin-token", map[string]string{"audience": "subject:s1"})
	if rec.Code != http.StatusOK {
		t.Fatalf("preview subject = %d %s", rec.Code, rec.Body.String())
	}
	pv = nil
	if err := json.NewDecoder(rec.Body).Decode(&pv); err != nil {
		t.Fatal(err)
	}
	if pv["recipient_count"] != float64(2) {
		t.Fatalf("preview subject count = %v, want 2", pv["recipient_count"])
	}
	rec = doAdmin(t, a, http.MethodPost, "/internal/admin/broadcasts/preview", "admin-token", map[string]string{"audience": "subject:missing"})
	if rec.Code != http.StatusNotFound || adminCode(t, rec) != "subject_not_found" {
		t.Fatalf("preview missing subject = %d %s, want 404 subject_not_found", rec.Code, rec.Body.String())
	}
	rec = doAdmin(t, a, http.MethodPost, "/internal/admin/broadcasts/preview", "admin-token", map[string]string{"audience": "bogus"})
	if rec.Code != http.StatusBadRequest || adminCode(t, rec) != "invalid_audience" {
		t.Fatalf("preview bad audience = %d %s", rec.Code, rec.Body.String())
	}
}

func TestBroadcast_CreateFanoutDone(t *testing.T) {
	fu := &fakeUpstream{accounts: []string{"u1", "u2", "u3"}}
	up := httptest.NewServer(fu.handler())
	defer up.Close()
	st := store.NewMemoryStore()
	mb := bus.NewMemoryBus()
	a := testAdminServer(up.URL, st, mb, nil)

	// Subscribe one user before the send to prove live SSE delivery.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch, unsub, err := mb.Subscribe(ctx, "u1")
	if err != nil {
		t.Fatal(err)
	}
	defer unsub()

	rec := doAdmin(t, a, http.MethodPost, "/internal/admin/broadcasts", "admin-token", map[string]string{
		"audience": "all", "title_ar": "عنوان", "title_en": "Title", "body_ar": "نص", "body_en": "Body",
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create = %d %s", rec.Code, rec.Body.String())
	}
	var created map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}
	id, _ := created["id"].(string)
	if id == "" {
		t.Fatalf("create returned no id: %v", created)
	}
	b := waitBroadcast(t, st, id)
	if b.Status != models.BroadcastDone || b.Total != 3 || b.Delivered != 3 || b.Failed != 0 {
		t.Fatalf("broadcast = %+v", b)
	}

	// Live bus delivery for the subscribed user.
	select {
	case n := <-ch:
		if n.Title != "Title" || n.TitleAr != "عنوان" || n.BodyAr != "نص" || n.Type != "event" {
			t.Fatalf("live row = %+v", n)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no live bus message for u1")
	}

	// Stored rows: one per user, app-contract shape.
	for _, uid := range []string{"u1", "u2", "u3"} {
		rows, err := st.List(context.Background(), uid, 1, 10)
		if err != nil || len(rows) != 1 {
			t.Fatalf("rows for %s = %+v, %v", uid, rows, err)
		}
		r := rows[0]
		if r.Type != "event" || r.Title != "Title" || r.TitleAr != "عنوان" || r.Body != "Body" || r.BodyAr != "نص" || r.Read {
			t.Fatalf("row for %s = %+v", uid, r)
		}
		if r.SubjectID != "" {
			t.Fatalf("all-audience row carries subject_id %q", r.SubjectID)
		}
	}
}

func TestBroadcast_ArabicOnlyFillsBase(t *testing.T) {
	fu := &fakeUpstream{accounts: []string{"u1"}}
	up := httptest.NewServer(fu.handler())
	defer up.Close()
	st := store.NewMemoryStore()
	a := testAdminServer(up.URL, st, nil, nil)

	rec := doAdmin(t, a, http.MethodPost, "/internal/admin/broadcasts", "admin-token", map[string]string{
		"audience": "all", "title_ar": "عنوان فقط", "body_ar": "نص فقط",
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create = %d %s", rec.Code, rec.Body.String())
	}
	var created map[string]any
	_ = json.NewDecoder(rec.Body).Decode(&created)
	b := waitBroadcast(t, st, created["id"].(string))
	if b.Status != models.BroadcastDone {
		t.Fatalf("broadcast = %+v", b)
	}
	rows, _ := st.List(context.Background(), "u1", 1, 10)
	if len(rows) != 1 || rows[0].Title != "عنوان فقط" || rows[0].Body != "نص فقط" {
		t.Fatalf("arabic-only row = %+v", rows)
	}
}

func TestBroadcast_EmptyAudience(t *testing.T) {
	fu := &fakeUpstream{accounts: []string{}, owners: map[string][]string{"empty-subject": {}}}
	up := httptest.NewServer(fu.handler())
	defer up.Close()
	st := store.NewMemoryStore()
	a := testAdminServer(up.URL, st, nil, nil)

	mk := map[string]string{"title_ar": "ع", "body_ar": "ن"}
	mk["audience"] = "all"
	rec := doAdmin(t, a, http.MethodPost, "/internal/admin/broadcasts", "admin-token", mk)
	if rec.Code != http.StatusBadRequest || adminCode(t, rec) != "empty_audience" {
		t.Fatalf("empty all = %d %s, want 400 empty_audience", rec.Code, rec.Body.String())
	}
	mk["audience"] = "subject:empty-subject"
	rec = doAdmin(t, a, http.MethodPost, "/internal/admin/broadcasts", "admin-token", mk)
	if rec.Code != http.StatusBadRequest || adminCode(t, rec) != "empty_audience" {
		t.Fatalf("empty subject = %d %s, want 400 empty_audience", rec.Code, rec.Body.String())
	}
	items, total, err := st.ListBroadcasts(context.Background(), 1, 10)
	if err != nil || total != 0 || len(items) != 0 {
		t.Fatalf("nothing must be stored: total=%d err=%v", total, err)
	}
}

func TestBroadcast_SubjectAudienceAndUnknownSubject(t *testing.T) {
	fu := &fakeUpstream{owners: map[string][]string{"s1": {"u1", "u2"}}}
	up := httptest.NewServer(fu.handler())
	defer up.Close()
	st := store.NewMemoryStore()
	a := testAdminServer(up.URL, st, nil, nil)

	rec := doAdmin(t, a, http.MethodPost, "/internal/admin/broadcasts", "admin-token", map[string]string{
		"audience": "subject:s1", "title_ar": "مادة", "body_ar": "درس جديد",
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create subject = %d %s", rec.Code, rec.Body.String())
	}
	var created map[string]any
	_ = json.NewDecoder(rec.Body).Decode(&created)
	b := waitBroadcast(t, st, created["id"].(string))
	if b.Status != models.BroadcastDone || b.Total != 2 || b.Delivered != 2 {
		t.Fatalf("broadcast = %+v", b)
	}
	rows, _ := st.List(context.Background(), "u1", 1, 10)
	if len(rows) != 1 || rows[0].SubjectID != "s1" {
		t.Fatalf("subject row = %+v", rows)
	}

	rec = doAdmin(t, a, http.MethodPost, "/internal/admin/broadcasts", "admin-token", map[string]string{
		"audience": "subject:nope", "title_ar": "ع", "body_ar": "ن",
	})
	if rec.Code != http.StatusNotFound || adminCode(t, rec) != "subject_not_found" {
		t.Fatalf("unknown subject = %d %s, want 404 subject_not_found", rec.Code, rec.Body.String())
	}
}

// failBus fails Publish for configured users.
type failBus struct {
	bus.Bus
	mu   sync.Mutex
	fail map[string]bool
}

func (f *failBus) Publish(ctx context.Context, n *models.Notification) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail[n.UserID] {
		return errors.New("bus down for " + n.UserID)
	}
	return f.Bus.Publish(ctx, n)
}

func (f *failBus) setFail(uid string, fail bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.fail[uid] = fail
}

func TestBroadcast_PartialFailureThenRetry(t *testing.T) {
	fu := &fakeUpstream{accounts: []string{"u1", "u2"}}
	up := httptest.NewServer(fu.handler())
	defer up.Close()
	st := store.NewMemoryStore()
	fb := &failBus{Bus: bus.NewMemoryBus(), fail: map[string]bool{"u2": true}}
	a := testAdminServer(up.URL, st, fb, nil)

	rec := doAdmin(t, a, http.MethodPost, "/internal/admin/broadcasts", "admin-token", map[string]string{
		"audience": "all", "title_ar": "ع", "body_ar": "ن",
	})
	var created map[string]any
	_ = json.NewDecoder(rec.Body).Decode(&created)
	id := created["id"].(string)
	b := waitBroadcast(t, st, id)
	if b.Status != models.BroadcastPartial || b.Delivered != 1 || b.Failed != 1 {
		t.Fatalf("broadcast = %+v, want partial 1/1", b)
	}
	// One failure never stops the broadcast: u1 still got exactly one row.
	if rows, _ := st.List(context.Background(), "u1", 1, 10); len(rows) != 1 {
		t.Fatalf("u1 rows = %d, want 1", len(rows))
	}

	// Retry while the bus still fails: still partial, no duplicate for u1.
	rec = doAdmin(t, a, http.MethodPost, "/internal/admin/broadcasts/"+id+"/retry", "admin-token", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("retry = %d %s", rec.Code, rec.Body.String())
	}
	b = waitBroadcast(t, st, id)
	if b.Status != models.BroadcastPartial || b.Failed != 1 {
		t.Fatalf("broadcast after failed retry = %+v", b)
	}
	if rows, _ := st.List(context.Background(), "u1", 1, 10); len(rows) != 1 {
		t.Fatalf("u1 rows after retry = %d, want 1", len(rows))
	}

	// Heal the bus and retry: done, u2 has exactly one row.
	fb.setFail("u2", false)
	rec = doAdmin(t, a, http.MethodPost, "/internal/admin/broadcasts/"+id+"/retry", "admin-token", nil)
	var rbody map[string]any
	_ = json.NewDecoder(rec.Body).Decode(&rbody)
	if rec.Code != http.StatusOK || rbody["retried"] != float64(1) {
		t.Fatalf("retry healed = %d %s", rec.Code, rec.Body.String())
	}
	b = waitBroadcast(t, st, id)
	if b.Status != models.BroadcastDone || b.Delivered != 2 || b.Failed != 0 {
		t.Fatalf("broadcast after healed retry = %+v", b)
	}
	if rows, _ := st.List(context.Background(), "u2", 1, 10); len(rows) != 1 {
		t.Fatalf("u2 rows = %d, want exactly 1", len(rows))
	}
}

func TestBroadcast_RetryWhileSendingConflicts(t *testing.T) {
	up := httptest.NewServer((&fakeUpstream{}).handler())
	defer up.Close()
	st := store.NewMemoryStore()
	a := testAdminServer(up.URL, st, nil, nil)
	_ = st.CreateBroadcast(context.Background(), &models.Broadcast{ID: "bc-run", Audience: "all", Status: models.BroadcastSending})
	a.mu.Lock()
	a.running = map[string]bool{"bc-run": true}
	a.mu.Unlock()
	rec := doAdmin(t, a, http.MethodPost, "/internal/admin/broadcasts/bc-run/retry", "admin-token", nil)
	if rec.Code != http.StatusConflict || adminCode(t, rec) != "broadcast_in_progress" {
		t.Fatalf("retry while sending = %d %s, want 409", rec.Code, rec.Body.String())
	}
	rec = doAdmin(t, a, http.MethodPost, "/internal/admin/broadcasts/missing/retry", "admin-token", nil)
	if rec.Code != http.StatusNotFound || adminCode(t, rec) != "broadcast_not_found" {
		t.Fatalf("retry missing = %d %s", rec.Code, rec.Body.String())
	}
}

func TestBroadcast_RestartResumeDeliversOnce(t *testing.T) {
	up := httptest.NewServer((&fakeUpstream{}).handler())
	defer up.Close()
	st := store.NewMemoryStore()
	// A broadcast left in sending with resolved pending targets (as after a
	// crash between runs).
	_ = st.CreateBroadcast(context.Background(), &models.Broadcast{
		ID: "bc-crash", Audience: "all", Title: "ع", TitleAr: "ع", Body: "ن", BodyAr: "ن",
		CreatedBy: "admin-1", Status: models.BroadcastSending, Total: 2, ResolveDone: true,
	})
	for _, uid := range []string{"u1", "u2"} {
		_, _ = st.UpsertBroadcastTarget(context.Background(), &models.BroadcastTarget{
			ID: store.TargetID("bc-crash", uid), BroadcastID: "bc-crash", UserID: uid, Status: models.TargetPending,
		})
	}
	// A fresh server on the same store resumes without an admin token.
	a2 := testAdminServer(up.URL, st, bus.NewMemoryBus(), nil)
	a2.runBroadcast(context.Background(), "", "bc-crash")
	b, _ := st.GetBroadcast(context.Background(), "bc-crash")
	if b.Status != models.BroadcastDone || b.Delivered != 2 {
		t.Fatalf("resumed broadcast = %+v", b)
	}
	for _, uid := range []string{"u1", "u2"} {
		if rows, _ := st.List(context.Background(), uid, 1, 10); len(rows) != 1 {
			t.Fatalf("%s rows = %d, want exactly 1", uid, len(rows))
		}
	}
	// A second run (double start) changes nothing.
	a2.runBroadcast(context.Background(), "", "bc-crash")
	for _, uid := range []string{"u1", "u2"} {
		if rows, _ := st.List(context.Background(), uid, 1, 10); len(rows) != 1 {
			t.Fatalf("%s rows after rerun = %d, want 1", uid, len(rows))
		}
	}
}

func TestBroadcast_RateLimit(t *testing.T) {
	fu := &fakeUpstream{accounts: []string{"u1"}}
	up := httptest.NewServer(fu.handler())
	defer up.Close()
	st := store.NewMemoryStore()
	a := testAdminServer(up.URL, st, nil, NewMemorySendLimiter(2, time.Hour))

	mk := map[string]string{"audience": "all", "title_ar": "ع", "body_ar": "ن"}
	for i := 0; i < 2; i++ {
		if rec := doAdmin(t, a, http.MethodPost, "/internal/admin/broadcasts", "admin-token", mk); rec.Code != http.StatusCreated {
			t.Fatalf("send %d = %d %s", i, rec.Code, rec.Body.String())
		}
	}
	rec := doAdmin(t, a, http.MethodPost, "/internal/admin/broadcasts", "admin-token", mk)
	if rec.Code != http.StatusTooManyRequests || adminCode(t, rec) != "rate_limited" {
		t.Fatalf("over budget = %d %s, want 429 rate_limited", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("Retry-After") == "" {
		t.Fatal("429 without Retry-After")
	}
}

func TestBroadcast_AuditCarriesNoMessageText(t *testing.T) {
	fu := &fakeUpstream{accounts: []string{"u1"}}
	up := httptest.NewServer(fu.handler())
	defer up.Close()
	st := store.NewMemoryStore()
	a := testAdminServer(up.URL, st, nil, nil)

	const secretTitle = "عنوان سري للغاية"
	rec := doAdmin(t, a, http.MethodPost, "/internal/admin/broadcasts", "admin-token", map[string]string{
		"audience": "all", "title_ar": secretTitle, "body_ar": "نص سري",
	})
	var created map[string]any
	_ = json.NewDecoder(rec.Body).Decode(&created)
	waitBroadcast(t, st, created["id"].(string))

	logs, total, err := st.ListAuditLogs(context.Background(), 1, 10)
	if err != nil || total != 1 || len(logs) != 1 {
		t.Fatalf("audit = %+v total=%d err=%v", logs, total, err)
	}
	e := logs[0]
	if e.Action != "broadcast_send" || e.ActorID != "admin-1" || e.ActorName != "Sara" || e.TargetType != "broadcast" || e.TargetID != created["id"] {
		t.Fatalf("audit entry = %+v", e)
	}
	if strings.Contains(e.Detail, secretTitle) || strings.Contains(e.Detail, "نص سري") {
		t.Fatalf("audit detail leaks message text: %q", e.Detail)
	}
	if !strings.Contains(e.Detail, "audience=all") || !strings.Contains(e.Detail, "total=1") {
		t.Fatalf("audit detail misses counts: %q", e.Detail)
	}

	// The audit-log route serves the same entry.
	rec = doAdmin(t, a, http.MethodGet, "/internal/admin/audit-log", "admin-token", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("audit-log route = %d", rec.Code)
	}
	var v struct {
		Items []map[string]any `json:"items"`
		Total int              `json:"total"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&v); err != nil {
		t.Fatal(err)
	}
	if v.Total != 1 || len(v.Items) != 1 || v.Items[0]["action"] != "broadcast_send" {
		t.Fatalf("audit-log body = %s", rec.Body.String())
	}
}

func TestBroadcast_HistoryAndGet(t *testing.T) {
	fu := &fakeUpstream{accounts: []string{"u1"}}
	up := httptest.NewServer(fu.handler())
	defer up.Close()
	st := store.NewMemoryStore()
	a := testAdminServer(up.URL, st, nil, nil)

	var ids []string
	for _, title := range []string{"أول", "ثان"} {
		rec := doAdmin(t, a, http.MethodPost, "/internal/admin/broadcasts", "admin-token", map[string]string{
			"audience": "all", "title_ar": title, "body_ar": "ن",
		})
		var created map[string]any
		_ = json.NewDecoder(rec.Body).Decode(&created)
		ids = append(ids, created["id"].(string))
	}
	for _, id := range ids {
		waitBroadcast(t, st, id)
	}
	rec := doAdmin(t, a, http.MethodGet, "/internal/admin/broadcasts?page=1&limit=10", "admin-token", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("history = %d", rec.Code)
	}
	var v struct {
		Items []map[string]any `json:"items"`
		Total int              `json:"total"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&v); err != nil {
		t.Fatal(err)
	}
	if v.Total != 2 || len(v.Items) != 2 {
		t.Fatalf("history = %s", rec.Body.String())
	}
	for _, it := range v.Items {
		for _, k := range []string{"id", "audience", "title", "title_ar", "body", "body_ar", "created_by", "created_at", "status", "total", "delivered", "failed"} {
			if _, ok := it[k]; !ok {
				t.Fatalf("history item misses %s: %v", k, it)
			}
		}
		if _, ok := it["resolve_cursor"]; ok {
			t.Fatalf("history leaks resolve_cursor: %v", it)
		}
	}

	rec = doAdmin(t, a, http.MethodGet, "/internal/admin/broadcasts/"+ids[0], "admin-token", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("get = %d", rec.Code)
	}
	rec = doAdmin(t, a, http.MethodGet, "/internal/admin/broadcasts/missing", "admin-token", nil)
	if rec.Code != http.StatusNotFound || adminCode(t, rec) != "broadcast_not_found" {
		t.Fatalf("get missing = %d %s", rec.Code, rec.Body.String())
	}
}

// TestBroadcast_RowMatchesAppContract pins the stored row shape the app
// parses (see tests/contracts): every field the repository reads is present
// with the app's names, and absent optionals stay absent.
func TestBroadcast_RowMatchesAppContract(t *testing.T) {
	fu := &fakeUpstream{owners: map[string][]string{"s9": {"u1"}}}
	up := httptest.NewServer(fu.handler())
	defer up.Close()
	st := store.NewMemoryStore()
	a := testAdminServer(up.URL, st, nil, nil)

	rec := doAdmin(t, a, http.MethodPost, "/internal/admin/broadcasts", "admin-token", map[string]string{
		"audience": "subject:s9", "title_ar": "ع", "body_ar": "ن",
	})
	var created map[string]any
	_ = json.NewDecoder(rec.Body).Decode(&created)
	waitBroadcast(t, st, created["id"].(string))

	rows, err := st.List(context.Background(), "u1", 1, 10)
	if err != nil || len(rows) != 1 {
		t.Fatalf("rows = %+v, %v", rows, err)
	}
	raw, _ := json.Marshal(rows[0])
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"id", "user_id", "title", "title_ar", "body", "body_ar", "type", "subject_id", "read", "created_at"} {
		if _, ok := got[k]; !ok {
			t.Fatalf("row misses %s: %s", k, raw)
		}
	}
	if got["type"] != "event" || got["subject_id"] != "s9" || got["read"] != false {
		t.Fatalf("row = %s", raw)
	}
	if _, ok := got["target_route"]; ok {
		t.Fatalf("row must omit empty target_route: %s", raw)
	}
}
