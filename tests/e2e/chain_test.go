// Package e2e holds the saved gateway chain test. It runs only against a
// live compose stack: set E2E_GATEWAY_URL (e.g. https://localhost:18080) and
// E2E_CA_CERT (path to infrastructure/certs/ca.crt). Everything else comes
// from env; no secrets live in the repo.
package e2e

import (
	"bufio"
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func requireOrSkip(t *testing.T, reason string) {
	t.Helper()
	if os.Getenv("E2E_REQUIRED") == "1" {
		t.Fatalf("%s", reason)
	}
	t.Skip(reason)
}

func requirePushOrSkip(t *testing.T, reason string) {
	t.Helper()
	if os.Getenv("E2E_PUSH_REQUIRED") == "1" {
		t.Fatalf("%s", reason)
	}
	t.Skip(reason)
}

type chainClient struct {
	base   string
	client *http.Client
}

func newChainClient(t *testing.T) *chainClient {
	t.Helper()
	base := os.Getenv("E2E_GATEWAY_URL")
	if base == "" {
		requireOrSkip(t, "E2E_GATEWAY_URL not set; start the compose stack to run the gateway chain (e.g. E2E_GATEWAY_URL=https://localhost:18080)")
		return nil
	}
	caPath := os.Getenv("E2E_CA_CERT")
	if caPath == "" {
		requireOrSkip(t, "E2E_CA_CERT not set; point it at infrastructure/certs/ca.crt so the local CA is trusted (never InsecureSkipVerify)")
		return nil
	}
	// #nosec G304 //nolint:gosec -- path comes from the test runner env, not the app
	caPEM, err := os.ReadFile(caPath)
	if err != nil {
		t.Fatalf("read CA: %v", err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(caPEM) {
		t.Fatal("bad CA PEM")
	}
	// If an external gateway cert exists alongside the CA cert, append it to the pool as well.
	extPath := filepath.Join(filepath.Dir(caPath), "api-gateway-external.crt")
	// #nosec G304 //nolint:gosec -- path comes from test certs directory
	if extPEM, err := os.ReadFile(extPath); err == nil {
		_ = pool.AppendCertsFromPEM(extPEM)
	}
	return &chainClient{
		base: strings.TrimSuffix(base, "/"),
		client: &http.Client{
			Timeout:   15 * time.Second,
			Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}},
		},
	}
}

func (c *chainClient) post(t *testing.T, path, token string, body any) (int, map[string]any) {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatal(err)
		}
	}
	req, err := http.NewRequest(http.MethodPost, c.base+path, &buf)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := c.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(resp.Body)
	var out map[string]any
	_ = json.Unmarshal(raw, &out)
	if out == nil {
		out = map[string]any{}
	}
	return resp.StatusCode, out
}

func (c *chainClient) get(t *testing.T, path, token string) (int, map[string]any) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, c.base+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := c.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(resp.Body)
	var out map[string]any
	_ = json.Unmarshal(raw, &out)
	if out == nil {
		out = map[string]any{}
	}
	return resp.StatusCode, out
}

// TestGatewayChain exercises signup -> OTP -> login -> refresh -> list ->
// mark-read -> internal push -> SSE frame through the live gateway.
func TestGatewayChain(t *testing.T) {
	c := newChainClient(t)
	if c == nil {
		return
	}

	email := fmt.Sprintf("e2e-%d@example.com", time.Now().UnixNano())
	var (
		phone       string
		otp         string
		access      string
		refresh     string
		notifID     string
		stageFailed bool
	)

	runStage := func(name string, fn func(subT *testing.T)) {
		ok := t.Run(name, func(subT *testing.T) {
			if stageFailed {
				subT.Skip("previous stage failed")
			}
			fn(subT)
		})
		if !ok {
			stageFailed = true
		}
	}

	runStage("signup", func(subT *testing.T) {
		phone = fmt.Sprintf("+2010%08d", (time.Now().UnixNano() % 100000000))
		code, body := c.post(subT, "/api/v1/auth/signup", "", map[string]string{
			"full_name": "E2E Student",
			"email":     email,
			"phone":     phone,
			"password":  "password123",
		})
		if code != http.StatusCreated {
			subT.Fatalf("signup = %d (%v)", code, body)
		}
		var ok bool
		otp, ok = body["dev_otp"].(string)
		if !ok || otp == "" {
			requireOrSkip(subT, "signup response has no dev_otp; backend is not in local/dev mode")
		}
	})
	if otp == "" {
		stageFailed = true
	}

	runStage("verify_otp", func(subT *testing.T) {
		code, body := c.post(subT, "/api/v1/auth/verify-otp", "", map[string]string{"email": email, "code": otp})
		if code != http.StatusOK {
			subT.Fatalf("verify-otp = %d (%v)", code, body)
		}
		var okAccess, okRefresh bool
		access, okAccess = body["access_token"].(string)
		refresh, okRefresh = body["refresh_token"].(string)
		if !okAccess || !okRefresh || access == "" || refresh == "" {
			subT.Fatal("verify-otp returned no tokens")
		}
	})
	if access == "" || refresh == "" {
		stageFailed = true
	}

	runStage("login", func(subT *testing.T) {
		code, body := c.post(subT, "/api/v1/auth/login", "", map[string]string{"email": email, "password": "password123"})
		if code != http.StatusOK {
			subT.Fatalf("login = %d (%v)", code, body)
		}
	})

	runStage("refresh", func(subT *testing.T) {
		code, body := c.post(subT, "/api/v1/auth/refresh", "", map[string]string{"refresh_token": refresh})
		if code != http.StatusOK {
			subT.Fatalf("refresh = %d (%v)", code, body)
		}
		if newAccess, _ := body["access_token"].(string); newAccess != "" {
			access = newAccess
		}
	})

	runStage("list", func(subT *testing.T) {
		code, body := c.get(subT, "/api/v1/notifications/list?page=1&limit=5", access)
		if code != http.StatusOK {
			subT.Fatalf("list = %d (%v)", code, body)
		}
		items, _ := body["notifications"].([]any)
		if len(items) == 0 {
			subT.Fatal("expected the welcome notification after OTP verification")
		}
		first, _ := items[0].(map[string]any)
		var ok bool
		notifID, ok = first["id"].(string)
		if !ok || notifID == "" {
			subT.Fatal("notification item has no id")
		}
	})
	if notifID == "" {
		stageFailed = true
	}

	runStage("mark_read", func(subT *testing.T) {
		code, body := c.post(subT, "/api/v1/notifications/read", access, map[string]string{"id": notifID})
		if code != http.StatusOK {
			subT.Fatalf("mark-read = %d (%v)", code, body)
		}
	})

	runStage("push_and_stream", func(subT *testing.T) {
		// Internal push step needs direct service credentials; skip without them.
		notifURL := os.Getenv("E2E_NOTIFICATION_URL")
		internalToken := os.Getenv("E2E_INTERNAL_TOKEN")
		certFile := os.Getenv("E2E_CLIENT_CERT")
		keyFile := os.Getenv("E2E_CLIENT_KEY")
		if notifURL == "" || internalToken == "" || certFile == "" || keyFile == "" {
			requirePushOrSkip(subT, "internal push step skipped: set E2E_NOTIFICATION_URL, E2E_INTERNAL_TOKEN, E2E_CLIENT_CERT and E2E_CLIENT_KEY to run it")
			return
		}

		code, me := c.get(subT, "/api/v1/auth/me", access)
		if code != http.StatusOK {
			subT.Fatalf("me = %d (%v)", code, me)
		}
		userID, _ := me["id"].(string)
		if userID == "" {
			subT.Fatal("auth/me returned no user id")
		}
		if me["full_name"] != "E2E Student" {
			subT.Errorf("auth/me full_name = %v, want 'E2E Student'", me["full_name"])
		}
		if me["phone"] != phone {
			subT.Errorf("auth/me phone = %v, want %q", me["phone"], phone)
		}

		// Open the SSE stream first (query-token auth; may change later), then
		// push directly to the service and await the live frame.
		streamReq, err := http.NewRequest(http.MethodGet, c.base+"/api/v1/notifications/stream?token="+access, nil)
		if err != nil {
			subT.Fatal(err)
		}
		streamResp, err := c.client.Do(streamReq)
		if err != nil {
			subT.Fatal(err)
		}
		defer func() { _ = streamResp.Body.Close() }()
		if streamResp.StatusCode != http.StatusOK {
			subT.Fatalf("stream status = %d", streamResp.StatusCode)
		}

		pushBody, _ := json.Marshal(map[string]string{"user_id": userID, "title": "E2E-PROBE", "body": "b", "type": "system"})
		cert, err := tls.LoadX509KeyPair(certFile, keyFile)
		if err != nil {
			subT.Fatalf("load client cert: %v", err)
		}
		// #nosec G304 //nolint:gosec -- paths come from the test runner env, not the app
		caPEM2, err := os.ReadFile(os.Getenv("E2E_CA_CERT"))
		if err != nil {
			subT.Fatalf("read CA: %v", err)
		}
		pool2 := x509.NewCertPool()
		if !pool2.AppendCertsFromPEM(caPEM2) {
			subT.Fatal("bad CA PEM")
		}
		direct := &http.Client{
			Timeout: 15 * time.Second,
			Transport: &http.Transport{TLSClientConfig: &tls.Config{
				Certificates: []tls.Certificate{cert}, RootCAs: pool2, MinVersion: tls.VersionTLS12,
			}},
		}
		pushReq, err := http.NewRequest(http.MethodPost, strings.TrimSuffix(notifURL, "/")+"/internal/push", bytes.NewReader(pushBody))
		if err != nil {
			subT.Fatal(err)
		}
		pushReq.Header.Set("Content-Type", "application/json")
		pushReq.Header.Set("X-Internal-Token", internalToken)
		pushResp, err := direct.Do(pushReq)
		if err != nil {
			subT.Fatal(err)
		}
		_ = pushResp.Body.Close()
		if pushResp.StatusCode != http.StatusCreated {
			subT.Fatalf("internal push = %d", pushResp.StatusCode)
		}

		lines := make(chan string, 64)
		scanErr := make(chan error, 1)
		go func() {
			scanner := bufio.NewScanner(streamResp.Body)
			scanner.Buffer(make([]byte, 64*1024), 64*1024)
			for scanner.Scan() {
				lines <- scanner.Text()
			}
			scanErr <- scanner.Err()
		}()
		timeout := time.After(25 * time.Second)
		for {
			select {
			case line := <-lines:
				if strings.Contains(line, "E2E-PROBE") {
					return
				}
			case err := <-scanErr:
				subT.Fatalf("stream closed before the probe frame arrived: %v", err)
			case <-timeout:
				subT.Fatal("timed out waiting for the SSE probe frame")
			}
		}
	})
}
