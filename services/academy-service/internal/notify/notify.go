// Package notify delivers best-effort internal pushes to notification-service
// for academy-service events (request accept, request reject, admin grant,
// admin revoke). Calls are skipped when no service URL is configured (tests,
// minimal dev) and never block admin workflows.
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
		Timeout: 5 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				Certificates: []tls.Certificate{cert},
				RootCAs:      pool,
				MinVersion:   tls.VersionTLS12,
			},
		},
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
		"user_id":      userID,
		"title":        title,
		"title_ar":     titleAr,
		"body":         body,
		"body_ar":      bodyAr,
		"type":         ntype,
		"target_route": route,
	})
	reqCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
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

// SubjectActivated notifies a student that a subject has been unlocked.
func SubjectActivated(ctx context.Context, baseURL, internalToken, userID, titleAr, titleEn string) error {
	nameEn := titleEn
	if nameEn == "" {
		nameEn = titleAr
	}
	return Push(ctx, baseURL, internalToken, userID,
		"Subject activated", "تم تفعيل المادة",
		fmt.Sprintf("Subject %s has been activated", nameEn),
		fmt.Sprintf("تم تفعيل مادة %s", titleAr),
		"system", "/main")
}

// SubjectRejected notifies a student that their access request was rejected with reason.
func SubjectRejected(ctx context.Context, baseURL, internalToken, userID, titleAr, titleEn, reason string) error {
	nameEn := titleEn
	if nameEn == "" {
		nameEn = titleAr
	}
	return Push(ctx, baseURL, internalToken, userID,
		"Subject request rejected", "تم رفض طلب المادة",
		fmt.Sprintf("Your request for %s was rejected: %s", nameEn, reason),
		fmt.Sprintf("تم رفض طلبك لمادة %s: %s", titleAr, reason),
		"system", "/main")
}

// SubjectRevoked notifies a student that access to a subject was revoked.
func SubjectRevoked(ctx context.Context, baseURL, internalToken, userID, titleAr, titleEn, reason string) error {
	nameEn := titleEn
	if nameEn == "" {
		nameEn = titleAr
	}
	bodyEn := fmt.Sprintf("Access to %s was revoked", nameEn)
	bodyAr := fmt.Sprintf("تم سحب مادة %s", titleAr)
	if reason != "" {
		bodyEn = fmt.Sprintf("Access to %s was revoked: %s", nameEn, reason)
		bodyAr = fmt.Sprintf("تم سحب مادة %s: %s", titleAr, reason)
	}
	return Push(ctx, baseURL, internalToken, userID,
		"Subject revoked", "تم سحب المادة",
		bodyEn, bodyAr,
		"security", "/main")
}
