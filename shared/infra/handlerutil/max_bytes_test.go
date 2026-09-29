package handlerutil

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMaxBytesMiddleware_RejectsOversizedPayload(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/test/api", func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "body too large: "+err.Error(), http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(string(rune(len(body)))))
	})

	handler := MaxBytesMiddleware(1024)(mux) // 1 KB limit

	// 1. Within limit: 500 bytes -> 200 OK
	smallBody := strings.Repeat("A", 500)
	reqSmall := httptest.NewRequest(http.MethodPost, "/test/api", strings.NewReader(smallBody))
	recSmall := httptest.NewRecorder()
	handler.ServeHTTP(recSmall, reqSmall)
	if recSmall.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for 500 byte payload, got %d: %s", recSmall.Code, recSmall.Body.String())
	}

	// 2. Exceeds limit: 2000 bytes -> 400 Bad Request
	largeBody := strings.Repeat("B", 2000)
	reqLarge := httptest.NewRequest(http.MethodPost, "/test/api", strings.NewReader(largeBody))
	recLarge := httptest.NewRecorder()
	handler.ServeHTTP(recLarge, reqLarge)
	if recLarge.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 Bad Request for oversized payload, got %d", recLarge.Code)
	}
}

func TestMaxBytesMiddleware_UploadRouteHigherLimit(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/documents/upload", func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "body too large: "+err.Error(), http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(string(rune(len(body)))))
	})
	mux.HandleFunc("/chat/tickets/123/attachment", func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "body too large: "+err.Error(), http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(string(rune(len(body)))))
	})

	// Default limit 1KB, but upload route should allow up to 10MB
	handler := MaxBytesMiddleware(1024)(mux)

	// 50KB payload on upload route -> 200 OK
	uploadBody := strings.Repeat("C", 50*1024)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/documents/upload", strings.NewReader(uploadBody))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for upload payload on upload route, got %d: %s", rec.Code, rec.Body.String())
	}

	// 50KB payload on attachment route -> 200 OK
	reqAttach := httptest.NewRequest(http.MethodPost, "/chat/tickets/123/attachment", strings.NewReader(uploadBody))
	recAttach := httptest.NewRecorder()
	handler.ServeHTTP(recAttach, reqAttach)
	if recAttach.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for attachment payload on attachment route, got %d: %s", recAttach.Code, recAttach.Body.String())
	}
}
