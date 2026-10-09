package handlers

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

// dlOwner3 grants the fixture subject to a third student.
func dlOwner3(t *testing.T, f *downloadFixture) string {
	t.Helper()
	const id = "user-dl-owner-3"
	if ok, _ := f.s.Store.HasActiveEntitlement(context.Background(), id, f.subj.ID); !ok {
		grantForTest(t, f, id)
	}
	return id
}

func waitNoDownloadsInFlight(t *testing.T, s *Server) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for s.downloadsInFlight() != 0 {
		if time.Now().After(deadline) {
			t.Fatalf("%d per-student entries left in flight", s.downloadsInFlight())
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestDownloadFile_OnePerStudent(t *testing.T) {
	f := newDownloadFixture(t) // default cap 3
	g := &gateStorage{Storage: f.spy.Storage, gate: make(chan struct{}), entered: make(chan struct{}, 10)}
	f.s.Files = g
	path := downloadPath(f.subj.ID, f.file.ID)

	held := f.getAsync(t, path, dlOwner)
	<-g.entered // the owner's first download is in flight
	if n := f.s.downloadsInFlight(); n != 1 {
		t.Fatalf("in flight = %d, want 1", n)
	}

	// The same student again: refused with the busy shape, nothing taken.
	assertBusy429(t, f.get(t, path, dlOwner))
	if n := f.s.downloadsInFlight(); n != 1 {
		t.Fatalf("a refused second download changed the entries: %d", n)
	}

	// Another student is not affected: a free slot is still there.
	other := f.getAsync(t, path, dlOwner2(t, f))
	<-g.entered
	close(g.gate)
	for _, ch := range []<-chan *httptest.ResponseRecorder{held, other} {
		if rec := <-ch; rec.Code != http.StatusOK {
			t.Fatalf("held download: %d", rec.Code)
		}
	}
	waitNoDownloadsInFlight(t, f.s)
	// Released: the same student can download again.
	if rec := f.get(t, path, dlOwner); rec.Code != http.StatusOK {
		t.Fatalf("after release: %d", rec.Code)
	}
	waitNoDownloadsInFlight(t, f.s)
}

func TestDownloadFile_StudentEntryRemovedOnErrorAndAbort(t *testing.T) {
	f := newDownloadFixture(t)

	t.Run("storage_error", func(t *testing.T) {
		missing := storeFile(t, f.s, f.spy.Storage, f.subj.ID, "a7", pdfBytes(100))
		_ = f.spy.Storage.Delete(context.Background(), missing.StorageKey)
		if rec := f.get(t, downloadPath(f.subj.ID, missing.ID), dlOwner); rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("missing object: %d", rec.Code)
		}
		waitNoDownloadsInFlight(t, f.s)
		if rec := f.get(t, downloadPath(f.subj.ID, f.file.ID), dlOwner); rec.Code != http.StatusOK {
			t.Fatalf("same student after a storage error: %d", rec.Code)
		}
	})

	t.Run("client_abort", func(t *testing.T) {
		big := storeFile(t, f.s, f.spy.Storage, f.subj.ID, "b8", pdfBytes(20<<20))
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
		_, _ = resp.Body.Read(buf)
		cancel()
		_ = resp.Body.Close()
		waitNoDownloadsInFlight(t, f.s)
		if rec := f.get(t, downloadPath(f.subj.ID, f.file.ID), dlOwner); rec.Code != http.StatusOK {
			t.Fatalf("same student after an abort: %d", rec.Code)
		}
	})
}

// TestDownloadFile_NoPerStudentLeak: many concurrent downloads by a few
// students, half of them failing in storage, leave no entry and every slot
// free afterwards.
func TestDownloadFile_NoPerStudentLeak(t *testing.T) {
	f := newDownloadFixture(t)
	f.s.MaxConcurrentDownloads = 2
	missing := storeFile(t, f.s, f.spy.Storage, f.subj.ID, "c9", pdfBytes(100))
	_ = f.spy.Storage.Delete(context.Background(), missing.StorageKey)
	users := []string{dlOwner}
	for i := 0; i < 5; i++ {
		id := fmt.Sprintf("user-dl-many-%d", i)
		grantForTest(t, f, id)
		users = append(users, id)
	}
	toks := map[string]string{}
	for _, u := range users {
		toks[u] = makeStudentToken(t, u)
	}

	var wg sync.WaitGroup
	var mu sync.Mutex
	codes := map[int]int{}
	for i := 0; i < 200; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			u := users[i%len(users)]
			fileID := f.file.ID
			if i%2 == 1 {
				fileID = missing.ID
			}
			req := httptest.NewRequest(http.MethodGet, downloadPath(f.subj.ID, fileID), nil)
			req.Header.Set("X-Gateway-Secret", "test-gateway-secret")
			req.Header.Set("Authorization", "Bearer "+toks[u])
			rec := httptest.NewRecorder()
			f.h.ServeHTTP(rec, req)
			mu.Lock()
			codes[rec.Code]++
			mu.Unlock()
		}(i)
	}
	wg.Wait()
	for code := range codes {
		if code != http.StatusOK && code != http.StatusServiceUnavailable && code != http.StatusTooManyRequests {
			t.Fatalf("unexpected status %d in %v", code, codes)
		}
	}
	waitNoDownloadsInFlight(t, f.s)
	// Both slots are free: two different students acquire at once.
	r1, ok1 := f.s.acquireDownloadSlot("probe-1")
	r2, ok2 := f.s.acquireDownloadSlot("probe-2")
	if !ok1 || !ok2 {
		t.Fatalf("slots leaked after the run (codes %v)", codes)
	}
	r1()
	r2()
	t.Logf("200 downloads by %d students: %v", len(users), codes)
}
