package resilience

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"
	"time"
)

// patternChunk is the 64 KiB building block of the large test bodies.
var patternChunk = func() []byte {
	b := make([]byte, 64<<10)
	for i := range b {
		b[i] = byte(i*31 + i/251)
	}
	return b
}()

// largeBodyServer streams size bytes of the pattern in 64 KiB flushed chunks
// and returns the expected sha256.
func largeBodyServer(t *testing.T, size int) (*httptest.Server, [32]byte) {
	t.Helper()
	h := sha256.New()
	for left := size; left > 0; left -= len(patternChunk) {
		h.Write(patternChunk[:min(left, len(patternChunk))])
	}
	var want [32]byte
	copy(want[:], h.Sum(nil))
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/pdf")
		f := w.(http.Flusher)
		for left := size; left > 0; left -= len(patternChunk) {
			if _, err := w.Write(patternChunk[:min(left, len(patternChunk))]); err != nil {
				return
			}
			f.Flush()
		}
	}))
	return ts, want
}

// A 25 MiB body passes the round tripper intact (no silent cap at 10 MiB),
// streamed: allocations stay far below the body size.
func TestRoundTripper_LargeBodyIntactAndStreamed(t *testing.T) {
	const size = 25 << 20
	ts, want := largeBodyServer(t, size)
	defer ts.Close()

	rt := NewRoundTripper(http.DefaultTransport, "test-large-body-rt", 0, 2*time.Second)
	req, _ := http.NewRequest(http.MethodGet, ts.URL, nil)

	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	resp, err := rt.RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	h := sha256.New()
	n, err := io.Copy(h, resp.Body)
	_ = resp.Body.Close()
	runtime.ReadMemStats(&after)
	if err != nil {
		t.Fatalf("read after %d bytes: %v", n, err)
	}
	if n != size {
		t.Fatalf("read %d bytes, want %d", n, size)
	}
	var got [32]byte
	copy(got[:], h.Sum(nil))
	if got != want {
		t.Fatal("sha256 mismatch: body altered in transit")
	}
	// Server and client in one process: both sides' allocations count. A
	// buffered body would allocate at least its 25 MiB size.
	alloc := after.TotalAlloc - before.TotalAlloc
	t.Logf("allocated %d bytes while streaming %d bytes", alloc, size)
	if alloc > 8<<20 {
		t.Fatalf("allocated %d bytes for a %d byte body: not streamed", alloc, size)
	}
}

// An SSE stream keeps flowing past 11 MiB of events and ends only with the
// upstream's own final event.
func TestRoundTripper_SSEPast11MiB(t *testing.T) {
	const target = 11<<20 + 512<<10
	payload := strings.Repeat("x", 32<<10)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		f := w.(http.Flusher)
		for sent := 0; sent < target; {
			n, err := fmt.Fprintf(w, "data: %s\n\n", payload)
			if err != nil {
				return
			}
			sent += n
			f.Flush()
		}
		_, _ = fmt.Fprint(w, "event: end\ndata: done\n\n")
		f.Flush()
	}))
	defer ts.Close()

	rt := NewRoundTripper(http.DefaultTransport, "test-sse-11mib-rt", 0, 2*time.Second)
	req, _ := http.NewRequest(http.MethodGet, ts.URL, nil)
	resp, err := rt.RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 64<<10), 64<<10)
	var bytesSeen int
	sawEnd := false
	for sc.Scan() {
		bytesSeen += len(sc.Bytes()) + 1
		if sc.Text() == "event: end" {
			sawEnd = true
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("stream error after %d bytes: %v", bytesSeen, err)
	}
	if !sawEnd || bytesSeen < target {
		t.Fatalf("stream ended after %d bytes (final event seen: %t), want > %d and the final event", bytesSeen, sawEnd, target)
	}
}

// An explicit client cap fails loudly: reading past it returns
// ErrResponseTooLarge (never a clean EOF), a body exactly at the cap reads
// fine, and a client without a cap reads everything.
func TestResilienceClient_MaxResponseBytes(t *testing.T) {
	const capBytes = 1 << 20
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		size := capBytes
		if r.URL.Path == "/over" {
			size = capBytes + 1
		}
		_, _ = w.Write(bytes.Repeat([]byte("a"), size))
	}))
	defer ts.Close()

	capped := NewClient(http.DefaultClient, "test-capped-client", 0, 2*time.Second).WithMaxResponseBytes(capBytes)
	read := func(c *ResilienceClient, path string) (int, error) {
		req, _ := http.NewRequest(http.MethodGet, ts.URL+path, nil)
		resp, err := c.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		b, err := io.ReadAll(resp.Body)
		return len(b), err
	}

	if n, err := read(capped, "/over"); !errors.Is(err, ErrResponseTooLarge) {
		t.Fatalf("over cap: read %d bytes, err %v; want ErrResponseTooLarge", n, err)
	} else if n != capBytes {
		t.Fatalf("over cap: returned %d bytes before the error, want %d", n, capBytes)
	}
	if n, err := read(capped, "/exact"); err != nil || n != capBytes {
		t.Fatalf("at cap: read %d bytes, err %v; want %d, nil", n, err, capBytes)
	}
	uncapped := NewClient(http.DefaultClient, "test-uncapped-client", 0, 2*time.Second)
	if n, err := read(uncapped, "/over"); err != nil || n != capBytes+1 {
		t.Fatalf("no cap: read %d bytes, err %v; want %d, nil", n, err, capBytes+1)
	}
}

// Every read after the cap was passed keeps failing.
func TestCancelReadCloser_ErrorIsSticky(t *testing.T) {
	c := newCancelReadCloser(io.NopCloser(strings.NewReader("abcdef")), func() {}, 3)
	buf := make([]byte, 10)
	n, err := c.Read(buf)
	if n != 3 || !errors.Is(err, ErrResponseTooLarge) || string(buf[:n]) != "abc" {
		t.Fatalf("first read = %d %q %v", n, buf[:n], err)
	}
	if n, err := c.Read(buf); n != 0 || !errors.Is(err, ErrResponseTooLarge) {
		t.Fatalf("second read = %d %v, want 0, ErrResponseTooLarge", n, err)
	}
}
