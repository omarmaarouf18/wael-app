// Package e2e holds the broadcast chain test (SPEC Phase 7). It runs only
// against a live compose stack with admin credentials: E2E_GATEWAY_URL and
// E2E_CA_CERT (as in chain_test.go) plus E2E_NOTIFICATION_ADMIN_URL,
// E2E_ACADEMY_ADMIN_URL, E2E_INTERNAL_TOKEN, E2E_ADMIN_TOKEN,
// E2E_CLIENT_CERT and E2E_CLIENT_KEY for the mTLS admin listeners. Without
// the admin credentials the test skips (set E2E_BROADCAST_REQUIRED=1 to fail
// instead). No secrets live in the repo.
package e2e

import (
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

func requireBroadcastOrSkip(t *testing.T, reason string) {
	t.Helper()
	if os.Getenv("E2E_BROADCAST_REQUIRED") == "1" {
		t.Fatalf("%s", reason)
	}
	t.Skip(reason)
}

type broadcastAdmin struct {
	notifURL   string
	academyURL string
	token      string
	adminToken string
	client     *http.Client
}

func newBroadcastAdmin(t *testing.T) *broadcastAdmin {
	t.Helper()
	notifURL := os.Getenv("E2E_NOTIFICATION_ADMIN_URL")
	academyURL := os.Getenv("E2E_ACADEMY_ADMIN_URL")
	internalToken := os.Getenv("E2E_INTERNAL_TOKEN")
	adminToken := os.Getenv("E2E_ADMIN_TOKEN")
	certFile := os.Getenv("E2E_CLIENT_CERT")
	keyFile := os.Getenv("E2E_CLIENT_KEY")
	if notifURL == "" || academyURL == "" || internalToken == "" || adminToken == "" || certFile == "" || keyFile == "" {
		requireBroadcastOrSkip(t, "broadcast admin step skipped: set E2E_NOTIFICATION_ADMIN_URL, E2E_ACADEMY_ADMIN_URL, E2E_INTERNAL_TOKEN, E2E_ADMIN_TOKEN, E2E_CLIENT_CERT and E2E_CLIENT_KEY to run it")
		return nil
	}
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		t.Fatalf("load client cert: %v", err)
	}
	// #nosec G304 //nolint:gosec -- paths come from the test runner env, not the app
	caPEM, err := os.ReadFile(os.Getenv("E2E_CA_CERT"))
	if err != nil {
		t.Fatalf("read CA: %v", err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(caPEM) {
		t.Fatal("bad CA PEM")
	}
	return &broadcastAdmin{
		notifURL:   strings.TrimSuffix(notifURL, "/"),
		academyURL: strings.TrimSuffix(academyURL, "/"),
		token:      internalToken,
		adminToken: adminToken,
		client: &http.Client{
			Timeout: 15 * time.Second,
			Transport: &http.Transport{TLSClientConfig: &tls.Config{
				Certificates: []tls.Certificate{cert}, RootCAs: pool, MinVersion: tls.VersionTLS12,
			}},
		},
	}
}

func (a *broadcastAdmin) do(t *testing.T, method, url string, body any) (int, map[string]any) {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatal(err)
		}
	}
	req, err := http.NewRequest(method, url, &buf)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Internal-Token", a.token)
	req.Header.Set("X-Admin-Token", a.adminToken)
	resp, err := a.client.Do(req)
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

// signupLogin creates a student through the gateway and returns user id and
// access token.
func signupLogin(t *testing.T, c *chainClient, name, email string) (string, string) {
	t.Helper()
	phone := fmt.Sprintf("+2010%08d", time.Now().UnixNano()%100000000)
	code, body := c.post(t, "/api/v1/auth/signup", "", map[string]string{
		"full_name": name,
		"email":     email,
		"phone":     phone,
		"password":  "password123",
	})
	if code != http.StatusCreated {
		t.Fatalf("signup %s = %d (%v)", email, code, body)
	}
	otp, _ := body["dev_otp"].(string)
	pendingID, _ := body["pending_id"].(string)
	if otp == "" || pendingID == "" {
		t.Fatalf("signup %s has no dev_otp/pending_id; backend is not in local/dev mode", email)
	}
	code, body = c.post(t, "/api/v1/auth/verify-otp", "", map[string]string{
		"email":      email,
		"code":       otp,
		"pending_id": pendingID,
		"device_id":  "11111111-1111-4111-8111-111111111111",
	})
	if code != http.StatusOK {
		t.Fatalf("verify-otp %s = %d (%v)", email, code, body)
	}
	access, _ := body["access_token"].(string)
	if access == "" {
		t.Fatalf("verify-otp %s returned no access token", email)
	}
	code, me := c.get(t, "/api/v1/auth/me", access)
	if code != http.StatusOK {
		t.Fatalf("me %s = %d (%v)", email, code, me)
	}
	userID, _ := me["id"].(string)
	if userID == "" {
		t.Fatalf("me %s returned no user id", email)
	}
	return userID, access
}

// TestBroadcastChain exercises the Phase 7 flow on a live stack: two
// students sign up, the admin grants one of them a subject and broadcasts to
// subject:<id>, the owner sees the row in GET /notifications and the other
// student does not.
func TestBroadcastChain(t *testing.T) {
	c := newChainClient(t)
	if c == nil {
		return
	}
	a := newBroadcastAdmin(t)
	if a == nil {
		return
	}

	stamp := time.Now().UnixNano()
	ownerEmail := fmt.Sprintf("e2e-bc-owner-%d@example.com", stamp)
	otherEmail := fmt.Sprintf("e2e-bc-other-%d@example.com", stamp)
	stageFailed := false
	var ownerID, ownerAccess, otherAccess, subjectID, broadcastID string

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

	runStage("students", func(subT *testing.T) {
		var otherID string
		ownerID, ownerAccess = signupLogin(subT, c, "E2E BC Owner", ownerEmail)
		otherID, otherAccess = signupLogin(subT, c, "E2E BC Other", otherEmail)
		if otherID == "" || otherAccess == "" {
			subT.Fatal("second student signup failed")
		}
	})

	runStage("subject_and_grant", func(subT *testing.T) {
		code, level := a.do(subT, http.MethodPost, a.academyURL+"/internal/admin/levels", map[string]string{
			"study_type": "diploma",
			"name_ar":    fmt.Sprintf("دبلومة البث %d", stamp),
		})
		if code != http.StatusCreated {
			subT.Fatalf("create level = %d (%v)", code, level)
		}
		levelKey, _ := level["key"].(string)
		if levelKey == "" {
			subT.Fatalf("create level returned no key: %v", level)
		}
		code, subject := a.do(subT, http.MethodPost, a.academyURL+"/internal/admin/subjects", map[string]any{
			"level_id":          levelKey,
			"title_ar":          fmt.Sprintf("مادة البث %d", stamp),
			"term":              "first",
			"price":             0,
			"access_expires_at": "2027-06-01T21:59:59Z",
		})
		if code != http.StatusCreated {
			subT.Fatalf("create subject = %d (%v)", code, subject)
		}
		var ok bool
		subjectID, ok = subject["id"].(string)
		if !ok || subjectID == "" {
			subT.Fatalf("create subject returned no id: %v", subject)
		}
		code, grant := a.do(subT, http.MethodPost, a.academyURL+"/internal/admin/entitlements", map[string]string{
			"user_id":    ownerID,
			"subject_id": subjectID,
		})
		if code != http.StatusCreated && code != http.StatusOK {
			subT.Fatalf("grant = %d (%v)", code, grant)
		}
	})

	runStage("broadcast", func(subT *testing.T) {
		audience := "subject:" + subjectID
		code, preview := a.do(subT, http.MethodPost, a.notifURL+"/internal/admin/broadcasts/preview", map[string]string{
			"audience": audience,
		})
		if code != http.StatusOK {
			subT.Fatalf("preview = %d (%v)", code, preview)
		}
		if count, _ := preview["recipient_count"].(float64); count < 1 {
			subT.Fatalf("preview count = %v, want >= 1", preview["recipient_count"])
		}
		title := fmt.Sprintf("بث E2E %d", stamp)
		code, created := a.do(subT, http.MethodPost, a.notifURL+"/internal/admin/broadcasts", map[string]string{
			"audience": audience,
			"title_ar": title,
			"body_ar":  "رسالة اختبار E2E",
		})
		if code != http.StatusCreated {
			subT.Fatalf("create broadcast = %d (%v)", code, created)
		}
		var ok bool
		broadcastID, ok = created["id"].(string)
		if !ok || broadcastID == "" {
			subT.Fatalf("create broadcast returned no id: %v", created)
		}
	})

	hasSubjectRow := func(subT *testing.T, access string) bool {
		code, body := c.get(subT, "/api/v1/notifications/list?page=1&limit=20", access)
		if code != http.StatusOK {
			subT.Fatalf("list = %d (%v)", code, body)
		}
		items, _ := body["notifications"].([]any)
		for _, item := range items {
			row, _ := item.(map[string]any)
			if row["subject_id"] == subjectID {
				if row["type"] != "event" || row["title_ar"] == "" || row["body_ar"] == "" {
					subT.Fatalf("broadcast row has wrong shape: %v", row)
				}
				return true
			}
		}
		return false
	}

	runStage("owner_sees_row", func(subT *testing.T) {
		deadline := time.Now().Add(30 * time.Second)
		for {
			if hasSubjectRow(subT, ownerAccess) {
				return
			}
			if time.Now().After(deadline) {
				subT.Fatalf("owner never saw the broadcast row for subject %s", subjectID)
			}
			time.Sleep(2 * time.Second)
		}
	})

	runStage("other_sees_nothing", func(subT *testing.T) {
		// Give fan-out time to finish first: the broadcast must be terminal
		// before a missing row proves exclusion rather than delay.
		deadline := time.Now().Add(30 * time.Second)
		for {
			code, b := a.do(subT, http.MethodGet, a.notifURL+"/internal/admin/broadcasts/"+broadcastID, nil)
			if code != http.StatusOK {
				subT.Fatalf("get broadcast = %d (%v)", code, b)
			}
			if status, _ := b["status"].(string); status == "done" || status == "partial" || status == "failed" {
				break
			}
			if time.Now().After(deadline) {
				subT.Fatalf("broadcast %s never finished: %v", broadcastID, b)
			}
			time.Sleep(2 * time.Second)
		}
		if hasSubjectRow(subT, otherAccess) {
			subT.Fatalf("non-owner saw the broadcast row for subject %s", subjectID)
		}
	})
}
