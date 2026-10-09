package handlers

import (
	"bytes"
	"context"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/omarmaarouf18/wael-app/academy-service/internal/models"
)

// lockedBuffer collects log output from several goroutines.
type lockedBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *lockedBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

func captureLog(t *testing.T) *lockedBuffer {
	t.Helper()
	buf := &lockedBuffer{}
	prev, flags := log.Writer(), log.Flags()
	log.SetOutput(buf)
	t.Cleanup(func() {
		log.SetOutput(prev)
		log.SetFlags(flags)
	})
	return buf
}

// TestDownloadFile_StalledClientFreesSlot: a client that reads the headers
// and then stops reading loses the response at the stall timeout, and its
// slot comes back. Runs on a real listener, where write deadlines work.
func TestDownloadFile_StalledClientFreesSlot(t *testing.T) {
	logs := captureLog(t)
	f := newDownloadFixture(t)
	f.s.MaxConcurrentDownloads = 1
	f.s.DownloadStallTimeout = 300 * time.Millisecond
	big := storeFile(t, f.s, f.spy.Storage, f.subj.ID, "f6", pdfBytes(20<<20))
	srv := httptest.NewServer(f.h)
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+downloadPath(f.subj.ID, big.ID), nil)
	req.Header.Set("X-Gateway-Secret", "test-gateway-secret")
	req.Header.Set("Authorization", "Bearer "+makeStudentToken(t, dlOwner))
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}
	stalledAt := time.Now()
	// Not reading the body from here on: the server's writes block once the
	// socket buffers are full, and the per-chunk deadline must cut it.

	// The only slot is taken while the transfer is stuck...
	if rec := f.get(t, downloadPath(f.subj.ID, f.file.ID), dlStudent); rec.Code != http.StatusForbidden {
		t.Fatalf("non-owner check while stalled: %d", rec.Code) // refusal, no slot needed
	}
	// ...and free again within the timeout plus a margin.
	// Polled through the real listener too, so every response in this test
	// is written by a writer that supports deadlines.
	other := dlOwner2(t, f)
	poll := func() int {
		r, _ := http.NewRequest(http.MethodGet, srv.URL+downloadPath(f.subj.ID, f.file.ID), nil)
		r.Header.Set("X-Gateway-Secret", "test-gateway-secret")
		r.Header.Set("Authorization", "Bearer "+makeStudentToken(t, other))
		pr, err := http.DefaultTransport.RoundTrip(r)
		if err != nil {
			t.Fatalf("poll: %v", err)
		}
		var b bytes.Buffer
		_, _ = b.ReadFrom(pr.Body)
		_ = pr.Body.Close()
		return pr.StatusCode
	}
	deadline := stalledAt.Add(3 * time.Second)
	for {
		code := poll()
		if code == http.StatusOK {
			break
		}
		if code != http.StatusTooManyRequests || time.Now().After(deadline) {
			t.Fatalf("slot not released after the stall timeout: %d after %v", code, time.Since(stalledAt))
		}
		time.Sleep(20 * time.Millisecond)
	}
	if freed := time.Since(stalledAt); freed < 250*time.Millisecond {
		t.Fatalf("slot freed after %v, before the 300 ms stall timeout could have fired", freed)
	}
	out := logs.String()
	if !strings.Contains(out, "file_download incomplete") || !strings.Contains(out, "i/o timeout") {
		t.Fatalf("expected an incomplete download cut by the write deadline in the log:\n%s", out)
	}
	if strings.Contains(out, "no write deadline support") {
		t.Fatal("the real listener reported no write deadline support")
	}
	if strings.Contains(out, big.StorageKey) {
		t.Fatal("log carries the storage key")
	}
}

// dlOwner2 grants the fixture subject to a second student, so the follow-up
// download is not refused by any per-student rule.
func dlOwner2(t *testing.T, f *downloadFixture) string {
	t.Helper()
	const id = "user-dl-owner-2"
	if ok, _ := f.s.Store.HasActiveEntitlement(context.Background(), id, f.subj.ID); !ok {
		grantForTest(t, f, id)
	}
	return id
}

// TestDownloadFile_DeadlineClearedAfterDownload: a normal download on a real
// listener completes, and keep-alive reuse after it is not cut by a leftover
// deadline.
func TestDownloadFile_DeadlineClearedAfterDownload(t *testing.T) {
	logs := captureLog(t)
	f := newDownloadFixture(t)
	f.s.DownloadStallTimeout = 200 * time.Millisecond
	srv := httptest.NewServer(f.h)
	defer srv.Close()
	client := srv.Client()
	get := func() int {
		req, _ := http.NewRequest(http.MethodGet, srv.URL+downloadPath(f.subj.ID, f.file.ID), nil)
		req.Header.Set("X-Gateway-Secret", "test-gateway-secret")
		req.Header.Set("Authorization", "Bearer "+makeStudentToken(t, dlOwner))
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("request: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()
		var b bytes.Buffer
		if _, err := b.ReadFrom(resp.Body); err != nil {
			t.Fatalf("body: %v", err)
		}
		if b.Len() != len(f.content) {
			t.Fatalf("body %d bytes, want %d", b.Len(), len(f.content))
		}
		return resp.StatusCode
	}
	if code := get(); code != http.StatusOK {
		t.Fatalf("first: %d", code)
	}
	time.Sleep(400 * time.Millisecond) // longer than the stall timeout, same connection
	if code := get(); code != http.StatusOK {
		t.Fatalf("second on the kept-alive connection: %d", code)
	}
	if strings.Contains(logs.String(), "incomplete") {
		t.Fatalf("a complete download was logged as incomplete:\n%s", logs.String())
	}
}

// TestDownloadFile_NoDeadlineSupportLoggedOnce: a writer without deadline
// support still gets the whole file, and the warning is logged once.
func TestDownloadFile_NoDeadlineSupportLoggedOnce(t *testing.T) {
	logs := captureLog(t)
	f := newDownloadFixture(t)
	for i := 0; i < 3; i++ {
		rec := f.get(t, downloadPath(f.subj.ID, f.file.ID), dlOwner) // ResponseRecorder: no deadlines
		if rec.Code != http.StatusOK || rec.Body.Len() != len(f.content) {
			t.Fatalf("download %d: %d, %d bytes", i+1, rec.Code, rec.Body.Len())
		}
	}
	if n := strings.Count(logs.String(), "no write deadline support"); n != 1 {
		t.Fatalf("warning logged %d times, want once", n)
	}
}

func grantForTest(t *testing.T, f *downloadFixture, userID string) {
	t.Helper()
	if err := f.s.Store.Grant(context.Background(), &models.Entitlement{UserID: userID, SubjectID: f.subj.ID}); err != nil {
		t.Fatalf("Grant: %v", err)
	}
}
