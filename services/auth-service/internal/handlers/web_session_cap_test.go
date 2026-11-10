package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/omarmaarouf18/wael-app/auth-service/internal/models"
	"github.com/omarmaarouf18/wael-app/auth-service/internal/otp"
	"golang.org/x/crypto/bcrypt"
)

// TestWebSessions_CappedAt5 tests that a user can have at most the newest 5 active
// web sessions. Creating a 6th web session ends the oldest active web session (Item 1.1 / M3).
func TestWebSessions_CappedAt5(t *testing.T) {
	s, _, cleanup := setupSessionTestServer(t)
	defer cleanup()
	ctx := context.Background()

	hash, _ := bcrypt.GenerateFromPassword([]byte("StrongPassword123!"), bcrypt.MinCost)
	u := &models.User{
		ID:            "u-web-cap-test",
		Email:         "webcap@example.com",
		PasswordHash:  string(hash),
		Role:          models.RoleUser,
		EmailVerified: true,
		FullName:      "Web Cap Student",
		Status:        models.StatusActive,
		CreatedAt:     time.Now().UTC(),
		UpdatedAt:     time.Now().UTC(),
	}
	if err := s.Store.Create(ctx, u); err != nil {
		t.Fatalf("create user: %v", err)
	}

	// Sign in 6 times on web
	for i := 1; i <= 6; i++ {
		body, _ := json.Marshal(map[string]string{
			"email":        "webcap@example.com",
			"password":     "StrongPassword123!",
			"device_label": fmt.Sprintf("Web Browser %d", i),
		})
		req := httptest.NewRequest(http.MethodPost, "/auth/login", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Client", "web")
		rec := httptest.NewRecorder()

		s.Login(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("login %d failed: %d: %s", i, rec.Code, rec.Body.String())
		}
		// Small delay to ensure strictly sequential created_at timestamps
		time.Sleep(5 * time.Millisecond)
	}

	active, err := s.Store.ListActiveSessions(ctx, u.ID)
	if err != nil {
		t.Fatalf("ListActiveSessions: %v", err)
	}

	var activeWebCount int
	for _, sess := range active {
		if sess.Client == "web" {
			activeWebCount++
		}
	}

	if activeWebCount > 5 {
		t.Fatalf("expected at most 5 active web sessions, got %d", activeWebCount)
	}
	if activeWebCount != 5 {
		t.Fatalf("expected exactly 5 active web sessions, got %d", activeWebCount)
	}
}

// TestWebSession_RefreshFailsWhenKeyGoneEndsSession tests that when an active web session's
// refresh key is gone from Redis, an attempt to refresh returns 401 AND ends the web session in DB.
func TestWebSession_RefreshFailsWhenKeyGoneEndsSession(t *testing.T) {
	s, _, cleanup := setupSessionTestServer(t)
	defer cleanup()
	ctx := context.Background()

	hash, _ := bcrypt.GenerateFromPassword([]byte("StrongPassword123!"), bcrypt.MinCost)
	u := &models.User{
		ID:            "u-web-keygone-test",
		Email:         "keygone@example.com",
		PasswordHash:  string(hash),
		Role:          models.RoleUser,
		EmailVerified: true,
		FullName:      "Web KeyGone Student",
		Status:        models.StatusActive,
		CreatedAt:     time.Now().UTC(),
		UpdatedAt:     time.Now().UTC(),
	}
	if err := s.Store.Create(ctx, u); err != nil {
		t.Fatalf("create user: %v", err)
	}

	// 1. Web login to create active web session and receive cookie
	body, _ := json.Marshal(map[string]string{
		"email":    "keygone@example.com",
		"password": "StrongPassword123!",
	})
	req := httptest.NewRequest(http.MethodPost, "/auth/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Client", "web")
	rec := httptest.NewRecorder()
	s.Login(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("login failed: %d", rec.Code)
	}

	var refreshCookie *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == "refresh_token" {
			refreshCookie = c
			break
		}
	}
	if refreshCookie == nil {
		t.Fatalf("no refresh cookie")
	}

	// Verify session is active in DB
	active, err := s.Store.ListActiveSessions(ctx, u.ID)
	if err != nil || len(active) != 1 {
		t.Fatalf("expected 1 active session, got: %v", active)
	}
	sessID := active[0].ID

	// 2. Simulate refresh key expiration or eviction in Redis: delete refresh:* key
	if err := s.Codes.Delete(ctx, "refresh:"+otp.HashToken(refreshCookie.Value)); err != nil {
		t.Fatalf("delete refresh key: %v", err)
	}

	// 3. Attempt refresh with the cookie
	refReq := httptest.NewRequest(http.MethodPost, "/auth/refresh", nil)
	refReq.Header.Set("X-Client", "web")
	refReq.AddCookie(refreshCookie)
	refRec := httptest.NewRecorder()
	s.Refresh(refRec, refReq)

	if refRec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 Unauthorized, got %d", refRec.Code)
	}

	// 4. The session in DB must now be ENDED!
	sess, err := s.Store.GetSession(ctx, sessID)
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if sess == nil {
		t.Fatalf("session %s not found", sessID)
	}
	if sess.EndedAt == nil {
		t.Fatalf("web session %s was NOT ended when refresh failed due to missing refresh key", sessID)
	}
}
