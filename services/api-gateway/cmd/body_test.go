package main

import (
	"bufio"
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/omarmaarouf18/wael-app/api-gateway/internal/config"
)

// gatewayFor serves the real gateway mux (reverse proxy + per-upstream
// resilience round trippers) with academy and notifications pointing at
// the given upstream.
func gatewayFor(t *testing.T, upstream *httptest.Server) *httptest.Server {
	t.Helper()
	cfg := &config.Config{
		GatewaySecret: "gw-secret",
		Routes: []config.ServiceRoute{
			{Name: "notification", Prefix: "/api/v1/notifications/", Target: upstream.URL, StripPrefix: "/api/v1"},
			{Name: "academy", Prefix: "/api/v1/academy/", Target: upstream.URL, StripPrefix: "/api/v1"},
		},
	}
	mux, err := newMux(cfg, http.DefaultTransport)
	if err != nil {
		t.Fatal(err)
	}
	return httptest.NewServer(mux)
}

// A 25 MiB download (larger than the old 10 MiB silent cap and the planned
// 20 MB MAX_PDF_BYTES) passes the gateway byte for byte.
func TestGateway_LargeDownloadIntact(t *testing.T) {
	const size = 25 << 20
	chunk := make([]byte, 64<<10)
	for i := range chunk {
		chunk[i] = byte(i*7 + i/97)
	}
	want := sha256.New()
	for left := size; left > 0; left -= len(chunk) {
		want.Write(chunk[:min(left, len(chunk))])
	}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/pdf")
		for left := size; left > 0; left -= len(chunk) {
			if _, err := w.Write(chunk[:min(left, len(chunk))]); err != nil {
				return
			}
		}
	}))
	defer upstream.Close()
	gw := gatewayFor(t, upstream)
	defer gw.Close()

	resp, err := http.Get(gw.URL + "/api/v1/academy/files/1/download")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	got := sha256.New()
	n, err := io.Copy(got, resp.Body)
	if err != nil || n != size {
		t.Fatalf("status %d, read %d bytes, err %v; want %d bytes", resp.StatusCode, n, err, size)
	}
	if string(got.Sum(nil)) != string(want.Sum(nil)) {
		t.Fatal("sha256 mismatch through the gateway")
	}
}

// The notification SSE stream keeps flowing through the gateway past 11 MiB.
func TestGateway_SSEPast11MiB(t *testing.T) {
	const target = 11<<20 + 512<<10
	payload := strings.Repeat("y", 32<<10)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
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
	defer upstream.Close()
	gw := gatewayFor(t, upstream)
	defer gw.Close()

	resp, err := http.Get(gw.URL + "/api/v1/notifications/stream")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 64<<10), 64<<10)
	seen, sawEnd := 0, false
	for sc.Scan() {
		seen += len(sc.Bytes()) + 1
		if sc.Text() == "event: end" {
			sawEnd = true
		}
	}
	if sc.Err() != nil || !sawEnd || seen < target {
		t.Fatalf("stream ended after %d bytes (final event: %t, err %v), want > %d", seen, sawEnd, sc.Err(), target)
	}
}
