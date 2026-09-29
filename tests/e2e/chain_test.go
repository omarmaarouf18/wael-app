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
	"strings"
	"testing"
	"time"
)

type chainClient struct {
	t      *testing.T
	base   string
	client *http.Client
}

func newChainClient(t *testing.T) *chainClient {
	t.Helper()
	base := os.Getenv("E2E_GATEWAY_URL")
	if base == "" {
		t.Skip("E2E_GATEWAY_URL not set; start the compose stack to run the gateway chain (e.g. E2E_GATEWAY_URL=https://localhost:18080)")
	}
	caPath := os.Getenv("E2E_CA_CERT")
	if caPath == "" {
		t.Skip("E2E_CA_CERT not set; point it at infrastructure/certs/ca.crt so the local CA is trusted (never InsecureSkipVerify)")
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
	return &chainClient{
		t:    t,
		base: strings.TrimSuffix(base, "/"),
		client: &http.Client{
			Timeout:   15 * time.Second,
			Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}},
		},
	}
}

func (c *chainClient) post(path, token string, body any) (int, map[string]any) {
	c.t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			c.t.Fatal(err)
		}
	}
	req, err := http.NewRequest(http.MethodPost, c.base+path, &buf)
	if err != nil {
		c.t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := c.client.Do(req)
	if err != nil {
		c.t.Fatal(err)
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

func (c *chainClient) get(path, token string) (int, map[string]any) {
	c.t.Helper()
	req, err := http.NewRequest(http.MethodGet, c.base+path, nil)
	if err != nil {
		c.t.Fatal(err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := c.client.Do(req)
	if err != nil {
		c.t.Fatal(err)
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
	email := fmt.Sprintf("e2e-%d@example.com", time.Now().UnixNano())

	code, body := c.post("/api/v1/auth/signup", "", map[string]string{"email": email, "password": "password123"})
	if code != http.StatusCreated {
		t.Fatalf("signup = %d (%v)", code, body)
	}
	otp, _ := body["dev_otp"].(string)
	if otp == "" {
		t.Skip("signup response has no dev_otp; backend is not in local/dev mode")
	}

	code, body = c.post("/api/v1/auth/verify-otp", "", map[string]string{"email": email, "code": otp})
	if code != http.StatusOK {
		t.Fatalf("verify-otp = %d (%v)", code, body)
	}
	access, _ := body["access_token"].(string)
	refresh, _ := body["refresh_token"].(string)
	if access == "" || refresh == "" {
		t.Fatal("verify-otp returned no tokens")
	}

	code, body = c.post("/api/v1/auth/login", "", map[string]string{"email": email, "password": "password123"})
	if code != http.StatusOK {
		t.Fatalf("login = %d (%v)", code, body)
	}

	code, body = c.post("/api/v1/auth/refresh", "", map[string]string{"refresh_token": refresh})
	if code != http.StatusOK {
		t.Fatalf("refresh = %d (%v)", code, body)
	}
	if newAccess, _ := body["access_token"].(string); newAccess != "" {
		access = newAccess
	}

	code, body = c.get("/api/v1/notifications/list?page=1&limit=5", access)
	if code != http.StatusOK {
		t.Fatalf("list = %d (%v)", code, body)
	}
	items, _ := body["notifications"].([]any)
	if len(items) == 0 {
		t.Fatal("expected the welcome notification after OTP verification")
	}
	first, _ := items[0].(map[string]any)
	id, _ := first["id"].(string)

	code, body = c.post("/api/v1/notifications/read", access, map[string]string{"id": id})
	if code != http.StatusOK {
		t.Fatalf("mark-read = %d (%v)", code, body)
	}

	// Internal push step needs direct service credentials; skip without them.
	notifURL := os.Getenv("E2E_NOTIFICATION_URL")
	internalToken := os.Getenv("E2E_INTERNAL_TOKEN")
	certFile := os.Getenv("E2E_CLIENT_CERT")
	keyFile := os.Getenv("E2E_CLIENT_KEY")
	if notifURL == "" || internalToken == "" || certFile == "" || keyFile == "" {
		t.Skip("internal push step skipped: set E2E_NOTIFICATION_URL, E2E_INTERNAL_TOKEN, E2E_CLIENT_CERT and E2E_CLIENT_KEY to run it")
	}

	code, me := c.get("/api/v1/auth/me", access)
	if code != http.StatusOK {
		t.Fatalf("me = %d (%v)", code, me)
	}
	userID, _ := me["id"].(string)

	// Open the SSE stream first (query-token auth; may change later), then
	// push directly to the service and await the live frame.
	streamReq, err := http.NewRequest(http.MethodGet, c.base+"/api/v1/notifications/stream?token="+access, nil)
	if err != nil {
		t.Fatal(err)
	}
	streamResp, err := c.client.Do(streamReq)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = streamResp.Body.Close() }()
	if streamResp.StatusCode != http.StatusOK {
		t.Fatalf("stream status = %d", streamResp.StatusCode)
	}

	pushBody, _ := json.Marshal(map[string]string{"user_id": userID, "title": "E2E-PROBE", "body": "b", "type": "system"})
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		t.Fatalf("load client cert: %v", err)
	}
	// #nosec G304 //nolint:gosec -- paths come from the test runner env, not the app
	caPEM2, err := os.ReadFile(os.Getenv("E2E_CA_CERT"))
	if err != nil {
		t.Fatalf("read CA: %v", err)
	}
	pool2 := x509.NewCertPool()
	if !pool2.AppendCertsFromPEM(caPEM2) {
		t.Fatal("bad CA PEM")
	}
	direct := &http.Client{
		Timeout: 15 * time.Second,
		Transport: &http.Transport{TLSClientConfig: &tls.Config{
			Certificates: []tls.Certificate{cert}, RootCAs: pool2, MinVersion: tls.VersionTLS12,
		}},
	}
	pushReq, err := http.NewRequest(http.MethodPost, strings.TrimSuffix(notifURL, "/")+"/internal/push", bytes.NewReader(pushBody))
	if err != nil {
		t.Fatal(err)
	}
	pushReq.Header.Set("Content-Type", "application/json")
	pushReq.Header.Set("X-Internal-Token", internalToken)
	pushResp, err := direct.Do(pushReq)
	if err != nil {
		t.Fatal(err)
	}
	_ = pushResp.Body.Close()
	if pushResp.StatusCode != http.StatusCreated {
		t.Fatalf("internal push = %d", pushResp.StatusCode)
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
			t.Fatalf("stream closed before the probe frame arrived: %v", err)
		case <-timeout:
			t.Fatal("timed out waiting for the SSE probe frame")
		}
	}
}
