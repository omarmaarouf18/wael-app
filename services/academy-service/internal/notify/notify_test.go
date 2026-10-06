package notify

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// capturePush runs fn against a stub notification-service and returns the
// decoded push payload.
func capturePush(t *testing.T, fn func(baseURL string) error) map[string]string {
	t.Helper()
	var got map[string]string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/internal/push" {
			t.Errorf("push path = %s, want /internal/push", r.URL.Path)
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("content type = %q", r.Header.Get("Content-Type"))
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatalf("failed to decode push payload: %v", err)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	if err := fn(server.URL); err != nil {
		t.Fatalf("push failed: %v", err)
	}
	return got
}

// TestNotify_SubjectIDs asserts each of the four academy notices carries the
// subject it is about, and nothing else new: the revoke reason stays
// internal and no payment wording appears.
func TestNotify_SubjectIDs(t *testing.T) {
	ctx := context.Background()
	token := "test-internal-token"

	// 1. Request approved (admin_requests accept path).
	p := capturePush(t, func(baseURL string) error {
		return SubjectActivated(ctx, baseURL, token, "student-1", "subj-accept-1", "قانون", "Law")
	})
	if p["subject_id"] != "subj-accept-1" || p["user_id"] != "student-1" {
		t.Fatalf("accept payload = %v", p)
	}

	// 2. Request rejected (admin_requests reject path).
	p = capturePush(t, func(baseURL string) error {
		return SubjectRejected(ctx, baseURL, token, "student-2", "subj-reject-2", "قانون", "Law", "بيانات ناقصة")
	})
	if p["subject_id"] != "subj-reject-2" || p["user_id"] != "student-2" {
		t.Fatalf("reject payload = %v", p)
	}

	// 3. Access granted (admin_entitlements grant path).
	p = capturePush(t, func(baseURL string) error {
		return SubjectActivated(ctx, baseURL, token, "student-3", "subj-grant-3", "قانون", "Law")
	})
	if p["subject_id"] != "subj-grant-3" || p["user_id"] != "student-3" {
		t.Fatalf("grant payload = %v", p)
	}

	// 4. Access revoked (admin_entitlements revoke path).
	p = capturePush(t, func(baseURL string) error {
		return SubjectRevoked(ctx, baseURL, token, "student-4", "subj-revoke-4", "قانون", "Law")
	})
	if p["subject_id"] != "subj-revoke-4" || p["user_id"] != "student-4" {
		t.Fatalf("revoke payload = %v", p)
	}
	if _, ok := p["reason"]; ok {
		t.Fatalf("revoke payload leaks a reason: %v", p)
	}
	if _, ok := p["revoke_reason"]; ok {
		t.Fatalf("revoke payload leaks revoke_reason: %v", p)
	}
}
