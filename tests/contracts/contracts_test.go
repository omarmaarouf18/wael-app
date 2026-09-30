package contracts

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/omarmaarouf18/wael-app/shared/infra/handlerutil"
	"github.com/omarmaarouf18/wael-app/shared/infra/jwtutil"
)

// 1. Gateway contract: strips X-Internal-Token from client requests so no client-supplied
// internal token reaches downstream services, and never injects an internal token to backends.
func TestContract_GatewayStripsInternalToken(t *testing.T) {
	cmd := exec.Command("go", "test", "-v", "-count=1", "-run", "^(TestNew_RemovesClientInternalToken|TestNew_DoesNotInjectInternalToken)$", "github.com/omarmaarouf18/wael-app/api-gateway/internal/proxy")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("gateway internal token contract verification failed: %v\nOutput:\n%s", err, string(out))
	}
	outStr := string(out)
	if !strings.Contains(outStr, "PASS: TestNew_RemovesClientInternalToken") {
		t.Errorf("contract verification missing TestNew_RemovesClientInternalToken pass:\n%s", outStr)
	}
	if !strings.Contains(outStr, "PASS: TestNew_DoesNotInjectInternalToken") {
		t.Errorf("contract verification missing TestNew_DoesNotInjectInternalToken pass:\n%s", outStr)
	}
}

// 2. Error body contract: standard SafeError JSON structure.
func TestContract_SafeErrorBodyShape(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/test/path", nil)
	rec := httptest.NewRecorder()

	internalErr := errors.New("sensitive database connection timeout to 10.0.0.5:27017")
	safeMessage := "An unexpected error occurred. Please try again."
	code := handlerutil.ErrCodeInternal

	handlerutil.WriteSafeError(rec, req, http.StatusInternalServerError, code, safeMessage, internalErr)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected status 500, got %d", rec.Code)
	}

	contentType := rec.Header().Get("Content-Type")
	if !strings.Contains(contentType, "application/json") {
		t.Fatalf("expected application/json content type, got: %q", contentType)
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &parsed); err != nil {
		t.Fatalf("failed to parse JSON response: %v", err)
	}

	// Must contain exactly error, code, request_id
	expectedKeys := map[string]bool{"error": true, "code": true, "request_id": true}
	for k := range parsed {
		if !expectedKeys[k] {
			t.Errorf("unexpected key in SafeError response: %q", k)
		}
	}
	for k := range expectedKeys {
		if _, ok := parsed[k]; !ok {
			t.Errorf("missing expected key in SafeError response: %q", k)
		}
	}

	if parsed["error"] != safeMessage {
		t.Errorf("expected error message %q, got: %v", safeMessage, parsed["error"])
	}
	if parsed["code"] != code {
		t.Errorf("expected error code %q, got: %v", code, parsed["code"])
	}
	rid, ok := parsed["request_id"].(string)
	if !ok || len(rid) == 0 {
		t.Errorf("expected non-empty request_id string, got: %v", parsed["request_id"])
	}

	// Confidential backend error must never leak to client
	if strings.Contains(rec.Body.String(), "10.0.0.5") || strings.Contains(rec.Body.String(), "sensitive database") {
		t.Fatalf("vulnerability: internal error details leaked in response body: %s", rec.Body.String())
	}
}

// 3. JWT claims contract: token structure and mandatory claim set.
func TestContract_JWTClaimSet(t *testing.T) {
	secret := "test-contract-secret-at-least-32-bytes-long!"
	jwtutil.Init(secret)

	userID := "user-uuid-12345"
	role := "student"
	email := "student@academy.test"
	amr := []string{"pwd", "otp"}

	tokenStr, err := jwtutil.GenerateToken(userID, role, email, amr)
	if err != nil {
		t.Fatalf("GenerateToken failed: %v", err)
	}

	// 1. Validate through jwtutil Claims struct
	claims, err := jwtutil.ValidateToken(tokenStr)
	if err != nil {
		t.Fatalf("ValidateToken failed: %v", err)
	}

	if claims.UserID != userID {
		t.Errorf("UserID claim mismatch: got %q, want %q", claims.UserID, userID)
	}
	if claims.Role != role {
		t.Errorf("Role claim mismatch: got %q, want %q", claims.Role, role)
	}
	if claims.Email != email {
		t.Errorf("Email claim mismatch: got %q, want %q", claims.Email, email)
	}
	if len(claims.AMR) != 2 || claims.AMR[0] != "pwd" || claims.AMR[1] != "otp" {
		t.Errorf("AMR claim mismatch: got %v, want %v", claims.AMR, amr)
	}

	if claims.ID == "" {
		t.Errorf("expected non-empty JTI claim (ID)")
	}
	if claims.ExpiresAt == nil || claims.ExpiresAt.Time.Before(time.Now()) {
		t.Errorf("expected future ExpiresAt claim, got: %v", claims.ExpiresAt)
	}
	if claims.IssuedAt == nil || claims.IssuedAt.Time.After(time.Now().Add(1*time.Minute)) {
		t.Errorf("expected valid IssuedAt claim, got: %v", claims.IssuedAt)
	}
	if claims.NotBefore == nil {
		t.Errorf("expected non-nil NotBefore claim")
	}

	// 2. Validate raw payload JSON keys directly to enforce wire contract
	parts := strings.Split(tokenStr, ".")
	if len(parts) != 3 {
		t.Fatalf("expected 3-part JWT, got %d parts", len(parts))
	}

	payloadJSON, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatalf("failed to decode JWT payload base64: %v", err)
	}

	var rawPayload map[string]interface{}
	if err := json.Unmarshal(payloadJSON, &rawPayload); err != nil {
		t.Fatalf("failed to unmarshal JWT payload JSON: %v", err)
	}

	mandatoryClaims := []string{"user_id", "role", "email", "amr", "exp", "iat", "nbf", "jti"}
	for _, mc := range mandatoryClaims {
		if _, ok := rawPayload[mc]; !ok {
			t.Errorf("mandatory JWT claim %q missing from wire payload JSON: %v", mc, rawPayload)
		}
	}
}
