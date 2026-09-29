package handlerutil

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWriteSafeError_SanitizesBodyKeepsStatus(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/users/jobs/track?requester_token=secret-jwt-value", nil)
	rec := httptest.NewRecorder()
	internal := json.Unmarshal([]byte("{bad"), &struct{}{})
	WriteSafeError(rec, req, http.StatusBadRequest, ErrCodeInvalidJSON, "invalid request body", internal)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status changed: got %d", rec.Code)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body["error"] != "invalid request body" {
		t.Errorf("error not generic: %v", body["error"])
	}
	if body["code"] != ErrCodeInvalidJSON {
		t.Errorf("code missing: %v", body)
	}
	rid, ok := body["request_id"].(string)
	if !ok || len(rid) != 16 {
		t.Errorf("request_id missing/malformed: %v", body)
	}
	raw := rec.Body.String()
	for _, leak := range []string{"secret-jwt-value", "unexpected end of JSON", "requester_token"} {
		if strings.Contains(raw, leak) {
			t.Errorf("internal detail leaked in body %q: contains %q", raw, leak)
		}
	}
}

func TestWriteSafeError_NilError(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	rec := httptest.NewRecorder()
	WriteSafeError(rec, req, http.StatusInternalServerError, ErrCodeInternal, "internal server error", nil)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status changed: got %d", rec.Code)
	}
}

func TestBearerToken_HeaderParsing(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/x?requester_token=query-jwt", nil)
	if got := BearerToken(req); got != "" {
		t.Errorf("expected empty without header, got %q", got)
	}
	req.Header.Set("Authorization", "Bearer header-jwt")
	if got := BearerToken(req); got != "header-jwt" {
		t.Errorf("expected header-jwt, got %q", got)
	}
	req.Header.Set("Authorization", "Basic abc")
	if got := BearerToken(req); got != "" {
		t.Errorf("expected empty for non-Bearer scheme, got %q", got)
	}
}
