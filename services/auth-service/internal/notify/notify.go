// Package notify delivers best-effort internal pushes to notification-service
// (welcome after OTP verification, password-changed after reset). Calls are
// skipped when no service URL is configured (tests, minimal dev) and never
// block authentication flows.
package notify

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"sync"
	"time"
)

var (
	mu     sync.RWMutex
	client = http.DefaultClient
)

// InitClient configures the push client with mTLS certs for notification-service.
// Skip it in localhost HTTP dev (empty paths keep http.DefaultClient).
func InitClient(certFile, keyFile, caFile string) error {
	if certFile == "" || keyFile == "" || caFile == "" {
		return nil
	}
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return fmt.Errorf("notify: load key pair: %w", err)
	}
	caPEM, err := os.ReadFile(caFile) // #nosec G304 //nolint:gosec -- path comes from bootstrap env config, not user input
	if err != nil {
		return fmt.Errorf("notify: read CA: %w", err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(caPEM) {
		return fmt.Errorf("notify: bad CA PEM")
	}
	mu.Lock()
	defer mu.Unlock()
	client = &http.Client{
		Timeout:   10 * time.Second,
		Transport: &http.Transport{TLSClientConfig: &tls.Config{Certificates: []tls.Certificate{cert}, RootCAs: pool, MinVersion: tls.VersionTLS12}},
	}
	return nil
}

func httpClient() *http.Client {
	mu.RLock()
	defer mu.RUnlock()
	return client
}

// Push posts one notification; empty baseURL is a no-op success.
func Push(ctx context.Context, baseURL, internalToken, userID, title, titleAr, body, bodyAr, ntype, route string) error {
	if baseURL == "" {
		return nil
	}
	payload, _ := json.Marshal(map[string]string{
		"user_id": userID, "title": title, "title_ar": titleAr,
		"body": body, "body_ar": bodyAr, "type": ntype, "target_route": route,
	})
	reqCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, baseURL+"/internal/push", bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("notify: new request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Internal-Token", internalToken)
	resp, err := httpClient().Do(req)
	if err != nil {
		return fmt.Errorf("notify: push: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("notify: push status %d", resp.StatusCode)
	}
	return nil
}

// Welcome notifies a newly verified account.
func Welcome(ctx context.Context, baseURL, internalToken, userID string) {
	_ = Push(ctx, baseURL, internalToken, userID,
		"Welcome", "مرحباً بك",
		"Your email is verified. You are signed in.",
		"تم تأكيد بريدك الإلكتروني. تم تسجيل دخولك.",
		"system", "/main")
}

// PasswordChanged notifies after a completed password reset.
func PasswordChanged(ctx context.Context, baseURL, internalToken, userID string) {
	_ = Push(ctx, baseURL, internalToken, userID,
		"Password updated", "تم تحديث كلمة المرور",
		"Your password was just changed. Contact support if this was not you.",
		"تم تغيير كلمة المرور للتو. تواصل مع الدعم إذا لم تكن أنت.",
		"security", "/settings")
}

// AccountSuspended notifies an account when suspended (generic text, no reason exposed).
func AccountSuspended(ctx context.Context, baseURL, internalToken, userID string) error {
	return Push(ctx, baseURL, internalToken, userID,
		"Account suspended", "تم تعليق الحساب",
		"Your account has been suspended. Please contact support.",
		"تم تعليق حسابك. يرجى التواصل مع الدعم.",
		"security", "/support")
}

// AccountReactivated notifies an account when reactivated.
func AccountReactivated(ctx context.Context, baseURL, internalToken, userID string) error {
	return Push(ctx, baseURL, internalToken, userID,
		"Account reactivated", "تم إعادة تفعيل الحساب",
		"Your account has been reactivated. You may now sign in.",
		"تم إعادة تفعيل حسابك. يمكنك الآن تسجيل الدخول.",
		"security", "/login")
}

// AccountDeleted notifies an account when deleted.
func AccountDeleted(ctx context.Context, baseURL, internalToken, userID string) error {
	return Push(ctx, baseURL, internalToken, userID,
		"Account deleted", "تم حذف الحساب",
		"Your account has been deleted.",
		"تم حذف حسابك.",
		"security", "/login")
}
