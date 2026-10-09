package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/omarmaarouf18/wael-app/shared/infra/storage"
)

// gateStorage blocks OpenFile until the gate is closed, so downloads can be
// held in flight; entered reports each OpenFile that started.
type gateStorage struct {
	storage.Storage
	gate    chan struct{}
	entered chan struct{}
}

func (g *gateStorage) OpenFile(key string) (io.ReadCloser, error) {
	g.entered <- struct{}{}
	<-g.gate
	return g.Storage.OpenFile(key)
}

func (f *downloadFixture) getAsync(t *testing.T, path, userID string) <-chan *httptest.ResponseRecorder {
	t.Helper()
	tok := makeStudentToken(t, userID)
	out := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("X-Gateway-Secret", "test-gateway-secret")
		req.Header.Set("Authorization", "Bearer "+tok)
		rec := httptest.NewRecorder()
		f.h.ServeHTTP(rec, req)
		out <- rec
	}()
	return out
}

func assertBusy429(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429 with every slot busy, got %d (%s)", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Retry-After"); got != "5" {
		t.Fatalf("Retry-After = %q, want 5", got)
	}
	if got := rec.Header().Get("Cache-Control"); got != "private, no-store" {
		t.Fatalf("Cache-Control = %q, want private, no-store", got)
	}
	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || body["code"] != "rate_limited" || body["error"] == "" {
		t.Fatalf("429 body %q is not the rate limiter's shape", rec.Body.String())
	}
	if rec.Header().Get("Content-Type") == "application/pdf" {
		t.Fatal("429 sent PDF headers")
	}
}

func TestDownloadFile_ConcurrencyCapEnforced(t *testing.T) {
	f := newDownloadFixture(t)
	f.s.MaxConcurrentDownloads = 2
	g := &gateStorage{Storage: f.spy.Storage, gate: make(chan struct{}), entered: make(chan struct{}, 10)}
	f.s.Files = g
	path := downloadPath(f.subj.ID, f.file.ID)

	first := f.getAsync(t, path, dlOwner)
	second := f.getAsync(t, path, dlOwner2(t, f))
	<-g.entered
	<-g.entered // both slots are held inside OpenFile

	assertBusy429(t, f.get(t, path, dlOwner3(t, f))) // a third student: no slot left
	// A refusal decided before storage takes no slot: still 403, not 429.
	if rec := f.get(t, path, dlStudent); rec.Code != http.StatusForbidden {
		t.Fatalf("non-owner with full slots: expected 403, got %d", rec.Code)
	}

	close(g.gate)
	for _, ch := range []<-chan *httptest.ResponseRecorder{first, second} {
		if rec := <-ch; rec.Code != http.StatusOK || rec.Body.Len() != len(f.content) {
			t.Fatalf("held download: %d, %d bytes", rec.Code, rec.Body.Len())
		}
	}
	// Both slots came back on success.
	for i := 0; i < 2; i++ {
		if rec := f.get(t, path, dlOwner); rec.Code != http.StatusOK {
			t.Fatalf("download %d after release: expected 200, got %d", i+1, rec.Code)
		}
	}
}

func TestDownloadFile_SlotReleasedOnStorageError(t *testing.T) {
	f := newDownloadFixture(t)
	f.s.MaxConcurrentDownloads = 1
	missing := storeFile(t, f.s, f.spy.Storage, f.subj.ID, "d4", pdfBytes(100))
	if err := f.spy.Storage.Delete(context.Background(), missing.StorageKey); err != nil {
		t.Fatalf("delete object: %v", err)
	}
	for i := 0; i < 3; i++ {
		if rec := f.get(t, downloadPath(f.subj.ID, missing.ID), dlOwner); rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("missing object %d: expected 503, got %d", i+1, rec.Code)
		}
	}
	if rec := f.get(t, downloadPath(f.subj.ID, f.file.ID), dlOwner); rec.Code != http.StatusOK {
		t.Fatalf("after storage errors the only slot must be free: got %d", rec.Code)
	}
}

func TestDownloadFile_SlotReleasedOnClientAbort(t *testing.T) {
	f := newDownloadFixture(t)
	f.s.MaxConcurrentDownloads = 1
	big := storeFile(t, f.s, f.spy.Storage, f.subj.ID, "e5", pdfBytes(20<<20))
	srv := httptest.NewServer(f.h)
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+downloadPath(f.subj.ID, big.ID), nil)
	req.Header.Set("X-Gateway-Secret", "test-gateway-secret")
	req.Header.Set("Authorization", "Bearer "+makeStudentToken(t, dlOwner))
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	buf := make([]byte, 1024)
	if _, err := io.ReadFull(resp.Body, buf); err != nil {
		t.Fatalf("first bytes: %v", err)
	}
	cancel() // the client goes away mid-body
	_ = resp.Body.Close()

	// The handler notices the broken connection and gives the slot back.
	deadline := time.Now().Add(5 * time.Second)
	for {
		rec := f.get(t, downloadPath(f.subj.ID, f.file.ID), dlOwner)
		if rec.Code == http.StatusOK {
			return
		}
		if rec.Code != http.StatusTooManyRequests || time.Now().After(deadline) {
			t.Fatalf("after a client abort: got %d, slot not released", rec.Code)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestAcquireDownloadSlot_DefaultAndRelease(t *testing.T) {
	s := newTestServer(false) // MaxConcurrentDownloads unset: the default 3, never unlimited
	var releases []func()
	for i := 0; i < 3; i++ {
		release, ok := s.acquireDownloadSlot(fmt.Sprintf("user-%d", i))
		if !ok {
			t.Fatalf("slot %d refused under the default cap", i+1)
		}
		releases = append(releases, release)
	}
	if _, ok := s.acquireDownloadSlot("user-3"); ok {
		t.Fatal("a fourth slot was granted; default cap is 3")
	}
	releases[0]()
	releases[0]() // a second call must not free another slot
	if _, ok := s.acquireDownloadSlot("user-4"); !ok {
		t.Fatal("released slot not available")
	}
	if _, ok := s.acquireDownloadSlot("user-5"); ok {
		t.Fatal("a double release freed two slots")
	}

	// Many goroutines never get more than the cap at once.
	s2 := newTestServer(false)
	s2.MaxConcurrentDownloads = 2
	var mu sync.Mutex
	inFlight, peak := 0, 0
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			release, ok := s2.acquireDownloadSlot(fmt.Sprintf("user-%d", i))
			if !ok {
				return
			}
			mu.Lock()
			inFlight++
			if inFlight > peak {
				peak = inFlight
			}
			mu.Unlock()
			time.Sleep(time.Millisecond)
			mu.Lock()
			inFlight--
			mu.Unlock()
			release()
		}(i)
	}
	wg.Wait()
	if peak > 2 {
		t.Fatalf("peak in flight = %d, cap is 2", peak)
	}
}
