// Package notify delivers best-effort internal pushes to notification-service
// (welcome after OTP verification, password-changed after reset). Calls are
// skipped when no service URL is configured (tests, minimal dev) and never
// block authentication flows.
package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

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
	resp, err := http.DefaultClient.Do(req)
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
