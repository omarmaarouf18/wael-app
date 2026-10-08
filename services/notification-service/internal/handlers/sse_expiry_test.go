package handlers

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/omarmaarouf18/wael-app/shared/infra/jwtutil"
)

// signTestToken signs an HS256 access token with the test secret and the
// given expiry (jwtutil only issues tokens of at least 5 minutes).
func signTestToken(t *testing.T, userID string, exp *jwt.NumericDate) string {
	t.Helper()
	claims := jwtutil.Claims{UserID: userID, Role: "user", RegisteredClaims: jwt.RegisteredClaims{
		ExpiresAt: exp,
		IssuedAt:  jwt.NewNumericDate(time.Now().Add(-time.Minute)),
		ID:        userID + "-jti",
	}}
	tok, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte("test-jwt-secret-0123456789abcdef"))
	if err != nil {
		t.Fatal(err)
	}
	return tok
}

// A stream closes (plain end of body, which the app reconnects from) when
// its access token expires, even with no revocation and slow heartbeats.
func TestSSE_ClosesAtTokenExpiry(t *testing.T) {
	s := testServer()
	jwtutil.SetRedisClient(nil)
	s.HeartbeatInterval = time.Hour // expiry, not a heartbeat, must end it

	userID := "user-short-token"
	// exp has whole-second precision; pick a boundary 1-2s ahead.
	exp := time.Unix(time.Now().Unix()+2, 0)
	_, cancel, done := openTestStream(t, s, signTestToken(t, userID, jwt.NewNumericDate(exp)))
	defer cancel()

	select {
	case <-done:
		if early := time.Until(exp); early > 50*time.Millisecond {
			t.Fatalf("stream closed %v before token expiry", early)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("stream outlived its access token")
	}
	if got := s.Limiter.ActiveSlots(userID); got != 0 {
		t.Fatalf("active slots after expiry = %d, want 0", got)
	}
}

// A token without exp cannot open a stream.
func TestSSE_TokenWithoutExpRefused(t *testing.T) {
	s := testServer()
	jwtutil.SetRedisClient(nil)
	req := httptest.NewRequest(http.MethodGet, "/notifications/stream", nil)
	req.Header.Set("X-Gateway-Secret", "gw-secret")
	req.Header.Set("Authorization", "Bearer "+signTestToken(t, "user-no-exp", nil))
	rec := httptest.NewRecorder()
	s.GatewayAuth(http.HandlerFunc(s.Stream)).ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}
