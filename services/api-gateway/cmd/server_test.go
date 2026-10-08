package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestNewServer_DefaultTimeouts(t *testing.T) {
	srv := newServer(":0", http.NotFoundHandler(), defaultServerTimeouts)
	if srv.ReadHeaderTimeout != 5*time.Second || srv.ReadTimeout != 30*time.Second || srv.IdleTimeout != 120*time.Second {
		t.Fatalf("timeouts = header %v, read %v, idle %v; want 5s, 30s, 120s", srv.ReadHeaderTimeout, srv.ReadTimeout, srv.IdleTimeout)
	}
	if srv.WriteTimeout != 0 {
		t.Fatalf("WriteTimeout = %v, want 0 (it would cut SSE streams)", srv.WriteTimeout)
	}
}

// A client that never finishes its headers (slowloris) is disconnected once
// ReadHeaderTimeout passes, and the handler never runs.
func TestNewServer_SlowlorisHeaderReadCut(t *testing.T) {
	handled := make(chan struct{}, 1)
	srv := newServer("", http.HandlerFunc(func(http.ResponseWriter, *http.Request) { handled <- struct{}{} }),
		serverTimeouts{readHeader: 200 * time.Millisecond, read: 3 * time.Second, idle: 3 * time.Second})
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = srv.Serve(ln) }()
	defer func() { _ = srv.Close() }()

	conn, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	start := time.Now()
	// Partial request: the blank line that ends the headers never comes.
	if _, err := fmt.Fprint(conn, "GET /health HTTP/1.1\r\nHost: gateway\r\nX-Slow: 1\r\n"); err != nil {
		t.Fatal(err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	_, err = io.ReadAll(conn) // returns when the server closes the connection
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		t.Fatal("server kept the slow header connection open for 5s")
	}
	// Well under the 3s ReadTimeout fallback: ReadHeaderTimeout did the cut.
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("connection closed after %v, want about 200ms", elapsed)
	}
	select {
	case <-handled:
		t.Fatal("handler ran for an incomplete request")
	default:
	}
}

// ReadTimeout bounds reading the request only: a streaming response (SSE)
// keeps flowing past it, over HTTP/1.1 and HTTP/2.
func TestNewServer_StreamOutlivesReadTimeout(t *testing.T) {
	const ticks = 8
	stream := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		f := w.(http.Flusher)
		for i := 0; i < ticks; i++ {
			select {
			case <-r.Context().Done():
				return
			case <-time.After(100 * time.Millisecond):
			}
			_, _ = fmt.Fprintf(w, "data: %d\n\n", i)
			f.Flush()
		}
	})
	to := serverTimeouts{readHeader: 100 * time.Millisecond, read: 200 * time.Millisecond, idle: time.Second}

	for _, h2 := range []bool{false, true} {
		ts := httptest.NewUnstartedServer(stream)
		srv := newServer("", stream, to)
		ts.Config.ReadHeaderTimeout = srv.ReadHeaderTimeout
		ts.Config.ReadTimeout = srv.ReadTimeout
		ts.Config.IdleTimeout = srv.IdleTimeout
		ts.Config.WriteTimeout = srv.WriteTimeout
		if h2 {
			ts.EnableHTTP2 = true
			ts.StartTLS()
		} else {
			ts.Start()
		}
		resp, err := ts.Client().Get(ts.URL + "/api/v1/notifications/stream")
		if err != nil {
			ts.Close()
			t.Fatal(err)
		}
		if h2 && resp.ProtoMajor != 2 {
			t.Fatalf("proto = %s, want HTTP/2", resp.Proto)
		}
		got := 0
		sc := bufio.NewScanner(resp.Body)
		for sc.Scan() {
			if strings.HasPrefix(sc.Text(), "data: ") {
				got++
			}
		}
		_ = resp.Body.Close()
		ts.Close()
		if got != ticks {
			t.Fatalf("h2=%t: received %d of %d events over %v with ReadTimeout %v", h2, got, ticks, ticks*100*time.Millisecond, to.read)
		}
	}
}
