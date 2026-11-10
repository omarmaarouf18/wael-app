package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/omarmaarouf18/wael-app/auth-service/internal/models"
	"github.com/omarmaarouf18/wael-app/auth-service/internal/otp"
	"golang.org/x/crypto/bcrypt"
)

// TestWebRefresh_CookieOnlyAndUndecodableBodyIs400 verifies:
// 1. When X-Client: web sends an undecodable body, Refresh returns 400.
// 2. When X-Client: web sends a cookie AND a body refresh_token, the body is ignored and cookie is used.
func TestWebRefresh_CookieOnlyAndUndecodableBodyIs400(t *testing.T) {
	s, _, cleanup := setupSessionTestServer(t)
	defer cleanup()
	ctx := context.Background()

	hash, _ := bcrypt.GenerateFromPassword([]byte("StrongPassword123!"), bcrypt.MinCost)
	u := &models.User{
		ID:            "u-web-body-test",
		Email:         "webbody@example.com",
		PasswordHash:  string(hash),
		Role:          models.RoleUser,
		EmailVerified: true,
		FullName:      "Web Body Student",
		Status:        models.StatusActive,
		CreatedAt:     time.Now().UTC(),
		UpdatedAt:     time.Now().UTC(),
	}
	if err := s.Store.Create(ctx, u); err != nil {
		t.Fatalf("create user: %v", err)
	}

	// 1. Undecodable body with X-Client: web must return 400 Bad Request
	badReq := httptest.NewRequest(http.MethodPost, "/auth/refresh", bytes.NewReader([]byte("{invalid-json")))
	badReq.Header.Set("Content-Type", "application/json")
	badReq.Header.Set("X-Client", "web")
	badReq.AddCookie(&http.Cookie{Name: "refresh_token", Value: "some-cookie"})
	badRec := httptest.NewRecorder()

	s.Refresh(badRec, badReq)
	if badRec.Code != http.StatusBadRequest {
		t.Fatalf("undecodable body expected 400 Bad Request, got %d: %s", badRec.Code, badRec.Body.String())
	}

	// 2. Cookie only: Body refresh_token must be ignored.
	// Seed a valid refresh key for the cookie token
	cookieToken := "cookie-refresh-valid-token-12345"
	bodyToken := "body-refresh-different-token-67890"

	// Create session for user
	sess := &models.Session{
		ID:          "sid-web-cookie-test",
		UserID:      u.ID,
		DeviceID:    "dev-cookie-test",
		Client:      "web",
		RefreshHash: otp.HashToken(cookieToken),
		CreatedAt:   time.Now().UTC(),
		LastUsedAt:  time.Now().UTC(),
	}
	if _, err := s.Store.CreateOrReplaceSession(ctx, sess); err != nil {
		t.Fatalf("create session: %v", err)
	}
	// Store cookie token in Redis
	if err := s.Codes.Set(ctx, "refresh:"+otp.HashToken(cookieToken), u.ID+":"+sess.ID, time.Hour); err != nil {
		t.Fatalf("set cookie token: %v", err)
	}
	// Store body token in Redis pointing to a different dummy user
	if err := s.Codes.Set(ctx, "refresh:"+otp.HashToken(bodyToken), "wrong-user:wrong-sess", time.Hour); err != nil {
		t.Fatalf("set body token: %v", err)
	}

	// Request with cookie=cookieToken, and body={"refresh_token": bodyToken}
	bodyJSON, _ := json.Marshal(map[string]string{
		"refresh_token": bodyToken,
	})
	req := httptest.NewRequest(http.MethodPost, "/auth/refresh", bytes.NewReader(bodyJSON))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Client", "web")
	req.AddCookie(&http.Cookie{Name: "refresh_token", Value: cookieToken})
	rec := httptest.NewRecorder()

	s.Refresh(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("refresh with valid cookie expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	// Verify the body token was NOT consumed from Redis (it was ignored!)
	val, err := s.Codes.Get(ctx, "refresh:"+otp.HashToken(bodyToken))
	if err != nil || val == "" {
		t.Fatalf("body refresh token was consumed or used! It must be ignored when X-Client: web")
	}
}
