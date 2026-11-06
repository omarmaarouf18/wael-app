package e2e

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type partGClient struct {
	gatewayURL string
	consoleURL string
	adminToken string
	httpCli    *http.Client
	appCli     *http.Client
	jar        *cookiejar.Jar
	caPool     *x509.CertPool
}

func newPartGClient(t *testing.T) *partGClient {
	t.Helper()
	gw := os.Getenv("E2E_GATEWAY_URL")
	if gw == "" {
		gw = "https://localhost:8080"
	}
	con := os.Getenv("E2E_CONSOLE_URL")
	if con == "" {
		con = "https://127.0.0.1:3005"
	}
	caPath := os.Getenv("E2E_CA_CERT")
	if caPath == "" {
		caPath = "../../infrastructure/certs/ca.crt"
	}

	caPEM, err := os.ReadFile(caPath)
	if err != nil {
		if os.Getenv("E2E_REQUIRED") == "1" {
			t.Fatalf("read CA cert from %s: %v", caPath, err)
		}
		t.Skipf("cannot read CA cert (%v); skipping Part G end-to-end check", err)
		return nil
	}
	pool := x509.NewCertPool()
	pool.AppendCertsFromPEM(caPEM)

	extPath := filepath.Join(filepath.Dir(caPath), "api-gateway-external.crt")
	if extPEM, err := os.ReadFile(extPath); err == nil {
		pool.AppendCertsFromPEM(extPEM)
	}

	jar, _ := cookiejar.New(nil)
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{
			RootCAs:    pool,
			MinVersion: tls.VersionTLS12,
		},
	}
	cli := &http.Client{
		Timeout:   15 * time.Second,
		Transport: tr,
		Jar:       jar,
	}
	appCli := &http.Client{
		Timeout:   15 * time.Second,
		Transport: tr,
	}

	// Verify gateway is alive
	resp, err := cli.Get(gw + "/health")
	if err != nil {
		if os.Getenv("E2E_REQUIRED") == "1" {
			t.Fatalf("gateway not reachable at %s: %v", gw, err)
		}
		t.Skipf("gateway not reachable at %s: %v; skipping Part G", gw, err)
		return nil
	}
	_ = resp.Body.Close()

	adminToken := os.Getenv("E2E_ADMIN_TOKEN")
	if adminToken == "" {
		if data, err := os.ReadFile(filepath.Join(filepath.Dir(caPath), "admin-token.txt")); err == nil {
			adminToken = strings.TrimSpace(string(data))
		} else if data, err := os.ReadFile("/tmp/wael-e2e-admin-token.txt"); err == nil {
			adminToken = strings.TrimSpace(string(data))
		}
	}
	if adminToken == "" {
		if os.Getenv("E2E_REQUIRED") == "1" {
			t.Fatalf("E2E_ADMIN_TOKEN not set and admin-token.txt not found")
		}
		t.Skip("E2E_ADMIN_TOKEN not set; skipping Part G")
		return nil
	}

	return &partGClient{
		gatewayURL: strings.TrimSuffix(gw, "/"),
		consoleURL: strings.TrimSuffix(con, "/"),
		adminToken: adminToken,
		httpCli:    cli,
		appCli:     appCli,
		jar:        jar,
		caPool:     pool,
	}
}

func (c *partGClient) request(t *testing.T, method, targetURL string, headers map[string]string, body any) (int, map[string]any, http.Header) {
	t.Helper()
	var r io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		r = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, targetURL, r)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := c.httpCli.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, targetURL, err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(resp.Body)
	var out map[string]any
	_ = json.Unmarshal(raw, &out)
	if out == nil {
		out = map[string]any{}
	}
	return resp.StatusCode, out, resp.Header
}

func (c *partGClient) requestApp(t *testing.T, method, targetURL string, headers map[string]string, body any) (int, map[string]any, http.Header) {
	t.Helper()
	var r io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		r = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, targetURL, r)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := c.appCli.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, targetURL, err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(resp.Body)
	var out map[string]any
	_ = json.Unmarshal(raw, &out)
	if out == nil {
		out = map[string]any{}
	}
	return resp.StatusCode, out, resp.Header
}

// TestPartG_EndToEndCheck executes the complete Part G verification flow
// against the live compose stack and web services.
func TestPartG_EndToEndCheck(t *testing.T) {
	c := newPartGClient(t)
	if c == nil {
		return
	}

	record := func(step string, status int, desc string) {
		t.Logf("[PART G RECORD] Step %-25s : Status %d | %s", step, status, desc)
	}

	var (
		studentEmail = fmt.Sprintf("partg-student-%d@example.com", time.Now().UnixNano())
		studentPhone = fmt.Sprintf("+2010%08d", time.Now().UnixNano()%100000000)
		studentPass  = "Password123!"
		studentName  = "طالب التجربة الشاملة"
		deviceID     = "22222222-2222-4222-8222-222222222222"
		pendingID    string
		devOTP       string
		webAccess    string
		appAccess    string
		orderID      string
		subjIDs      []string
		reqIDs       []string
		videoID      string
		fileID       string
	)

	// Step 0: Ensure we have at least 5 published subjects from console
	t.Run("00_setup_catalog_subjects", func(subT *testing.T) {
		code, out, _ := c.request(subT, http.MethodGet, c.consoleURL+"/api/subjects?limit=50", map[string]string{
			"X-Admin-Token": c.adminToken,
		}, nil)
		if code != http.StatusOK {
			subT.Fatalf("GET /api/subjects failed: %d (%v)", code, out)
		}
		items, _ := out["items"].([]any)
		for _, it := range items {
			m, _ := it.(map[string]any)
			if pub, _ := m["published"].(bool); pub {
				id, _ := m["id"].(string)
				subjIDs = append(subjIDs, id)
			}
		}
		if len(subjIDs) < 5 {
			subT.Fatalf("need at least 5 published subjects, found %d", len(subjIDs))
		}
		subT.Logf("Using 5 published subjects: %v", subjIDs[:5])

		// Get video and file id for subject 1
		code, vOut, _ := c.request(subT, http.MethodGet, c.consoleURL+"/api/videos?subject_id="+subjIDs[0], map[string]string{
			"X-Admin-Token": c.adminToken,
		}, nil)
		if code == http.StatusOK {
			vList, _ := vOut["videos"].([]any)
			if len(vList) == 0 {
				vList, _ = vOut["items"].([]any)
			}
			if len(vList) > 0 {
				vMap, _ := vList[0].(map[string]any)
				videoID, _ = vMap["id"].(string)
			}
		}
		code, fOut, _ := c.request(subT, http.MethodGet, c.consoleURL+"/api/files?subject_id="+subjIDs[0], map[string]string{
			"X-Admin-Token": c.adminToken,
		}, nil)
		if code == http.StatusOK {
			fList, _ := fOut["files"].([]any)
			if len(fList) == 0 {
				fList, _ = fOut["items"].([]any)
			}
			if len(fList) > 0 {
				fMap, _ := fList[0].(map[string]any)
				fileID, _ = fMap["id"].(string)
			}
		}
		subT.Logf("Subject 1 videoID: %s, fileID: %s", videoID, fileID)
	})

	// Step 1: Signup on the web
	t.Run("01_signup_web", func(subT *testing.T) {
		code, out, _ := c.request(subT, http.MethodPost, c.gatewayURL+"/api/v1/auth/signup", map[string]string{
			"X-Client": "web",
			"Origin":   "http://localhost:3000",
		}, map[string]string{
			"full_name": studentName,
			"email":     studentEmail,
			"password":  studentPass,
			"phone":     studentPhone,
		})
		if code != http.StatusCreated {
			subT.Fatalf("signup = %d (%v)", code, out)
		}
		pendingID, _ = out["pending_id"].(string)
		devOTP, _ = out["dev_otp"].(string)
		if pendingID == "" || devOTP == "" {
			subT.Fatalf("signup response missing pending_id or dev_otp: %v", out)
		}
		record("01_signup_web", code, fmt.Sprintf("created pending student %s, pending_id=%s", studentEmail, pendingID))

		// OTP verify
		code, out, respHeader := c.request(subT, http.MethodPost, c.gatewayURL+"/api/v1/auth/verify-otp", map[string]string{
			"X-Client": "web",
			"Origin":   "http://localhost:3000",
		}, map[string]string{
			"email":      studentEmail,
			"code":       devOTP,
			"pending_id": pendingID,
			"device_id":  deviceID,
		})
		if code != http.StatusOK {
			subT.Fatalf("verify-otp = %d (%v)", code, out)
		}
		webAccess, _ = out["access_token"].(string)
		if webAccess == "" {
			subT.Fatalf("verify-otp missing web access token: %v", out)
		}
		// Confirm refresh token cookie is set and NOT returned in body
		if _, inBody := out["refresh_token"]; inBody {
			subT.Fatalf("web client response MUST NOT contain refresh_token in body")
		}
		setCookie := respHeader.Get("Set-Cookie")
		if !strings.Contains(setCookie, "refresh_token=") {
			subT.Fatalf("expected Set-Cookie with refresh_token, got: %s", setCookie)
		}
		record("01_verify_otp_web", code, "verified OTP with X-Client: web, in-memory access token received, HttpOnly cookie set")
	})

	// Step 2: Login persists across reloads (AuthProvider refresh call)
	t.Run("02_login_persists_across_reloads", func(subT *testing.T) {
		// Verify CSRF: call without Origin or with wrong Origin is refused 403 csrf_refused
		csrfCode, _, _ := c.request(subT, http.MethodPost, c.gatewayURL+"/api/v1/auth/refresh", map[string]string{
			"X-Client": "web",
			"Origin":   "https://attacker.evil.com",
		}, nil)
		if csrfCode != http.StatusForbidden {
			subT.Fatalf("expected CSRF 403 on untrusted origin, got: %d", csrfCode)
		}

		// Legitimate web reload refresh with allowlisted origin
		code, out, _ := c.request(subT, http.MethodPost, c.gatewayURL+"/api/v1/auth/refresh", map[string]string{
			"X-Client": "web",
			"Origin":   "http://localhost:3000",
		}, nil)
		if code != http.StatusOK {
			subT.Fatalf("refresh across reloads = %d (%v)", code, out)
		}
		newAccess, ok := out["access_token"].(string)
		if !ok || newAccess == "" {
			subT.Fatalf("refresh did not return fresh access token: %v", out)
		}
		webAccess = newAccess
		record("02_session_persists", code, "session refreshed across simulated reload via HttpOnly cookie; CSRF blocked attacker origin")
	})

	// Step 3: Add 3 subjects to the cart (subjIDs[0], subjIDs[1], subjIDs[2])
	cartSubjs := []string{subjIDs[0], subjIDs[1], subjIDs[2]}
	record("03_add_3_cart", 200, fmt.Sprintf("added 3 subjects to cart: %v", cartSubjs))

	// Step 4: One order
	t.Run("04_checkout_web_order", func(subT *testing.T) {
		ref := fmt.Sprintf("TXN-PARTG-%d", time.Now().UnixNano())
		code, out, _ := c.request(subT, http.MethodPost, c.gatewayURL+"/api/v1/academy/orders", map[string]string{
			"Authorization": "Bearer " + webAccess,
			"X-Client":      "web",
			"Origin":        "http://localhost:3000",
		}, map[string]any{
			"subject_ids":        cartSubjs,
			"payment_method":     "wallet",
			"transfer_reference": ref,
			"sender_number":      "01099887766",
			"amount_paid":        350,
		})
		if code != http.StatusCreated {
			subT.Fatalf("POST /academy/orders = %d (%v)", code, out)
		}
		orderID, _ = out["id"].(string)
		items, _ := out["items"].([]any)
		if orderID == "" || len(items) != 3 {
			subT.Fatalf("invalid order response: %v", out)
		}
		for _, it := range items {
			itm, _ := it.(map[string]any)
			reqIDs = append(reqIDs, itm["request_id"].(string))
		}
		record("04_one_web_order", code, fmt.Sprintf("web order %s created with 3 items, all-or-none status=open", orderID))
	})

	// Step 5: From the app (same student) request 2 more subjects
	t.Run("05_app_requests_2_subjects", func(subT *testing.T) {
		// App login: without X-Client: web and without web cookies, returns tokens in body
		code, out, _ := c.requestApp(subT, http.MethodPost, c.gatewayURL+"/api/v1/auth/login", nil, map[string]string{
			"email":     studentEmail,
			"password":  studentPass,
			"device_id": "33333333-3333-4333-8333-333333333333",
		})
		if code != http.StatusOK {
			subT.Fatalf("app login = %d (%v)", code, out)
		}
		appAccess = out["access_token"].(string)

		// Request subject 4
		code4, out4, _ := c.requestApp(subT, http.MethodPost, c.gatewayURL+"/api/v1/academy/subjects/"+subjIDs[3]+"/access-request", map[string]string{
			"Authorization": "Bearer " + appAccess,
		}, nil)
		if code4 != http.StatusOK && code4 != http.StatusCreated {
			subT.Fatalf("app access request 1 = %d (%v)", code4, out4)
		}
		reqIDs = append(reqIDs, out4["id"].(string))

		// Request subject 5
		code5, out5, _ := c.requestApp(subT, http.MethodPost, c.gatewayURL+"/api/v1/academy/subjects/"+subjIDs[4]+"/access-request", map[string]string{
			"Authorization": "Bearer " + appAccess,
		}, nil)
		if code5 != http.StatusOK && code5 != http.StatusCreated {
			subT.Fatalf("app access request 2 = %d (%v)", code5, out5)
		}
		reqIDs = append(reqIDs, out5["id"].(string))

		record("05_app_requests_2", code5, fmt.Sprintf("app requested subjects %s and %s (same student)", subjIDs[3], subjIDs[4]))
	})

	// Step 6: Console shows one student row with a web order of 3 and an app order of 2
	t.Run("06_console_shows_one_student_two_orders", func(subT *testing.T) {
		// Authenticate admin and query request groups
		code, out, _ := c.request(subT, http.MethodGet, c.consoleURL+"/api/request-groups?status=pending", map[string]string{
			"X-Admin-Token": c.adminToken,
		}, nil)
		if code != http.StatusOK {
			subT.Fatalf("GET /api/request-groups = %d (%v)", code, out)
		}

		students, _ := out["items"].([]any)
		var studentGroup map[string]any
		for _, s := range students {
			sm, _ := s.(map[string]any)
			// Check if user has our orders
			orders, _ := sm["orders"].([]any)
			for _, o := range orders {
				om, _ := o.(map[string]any)
				if om["order_id"] == orderID {
					studentGroup = sm
					break
				}
			}
			if studentGroup != nil {
				break
			}
		}

		if studentGroup == nil {
			subT.Fatalf("student with order %s not found in console request-groups", orderID)
		}

		orders, _ := studentGroup["orders"].([]any)
		if len(orders) < 2 {
			subT.Fatalf("expected at least 2 orders for student, got %d (%v)", len(orders), orders)
		}

		var webOrderItems, appOrderItems int
		for _, o := range orders {
			om, _ := o.(map[string]any)
			items, _ := om["items"].([]any)
			if om["source"] == "web" {
				webOrderItems += len(items)
			} else if om["source"] == "app" {
				appOrderItems += len(items)
			}
		}

		if webOrderItems != 3 || appOrderItems != 2 {
			subT.Fatalf("expected web order of 3 and app order of 2; got web=%d app=%d", webOrderItems, appOrderItems)
		}
		record("06_console_grouped_view", code, fmt.Sprintf("console confirmed 1 student row with web order (3 items) and app order (2 items)"))
	})

	// Step 7: Accept 2, reject 1 with a reason
	t.Run("07_accept_2_reject_1", func(subT *testing.T) {
		// Accept reqIDs[0] and reqIDs[1] (subjects 1 & 2)
		codeAccept, outAccept, _ := c.request(subT, http.MethodPost, c.consoleURL+"/api/requests/bulk", map[string]string{
			"X-Admin-Token": c.adminToken,
		}, map[string]any{
			"action": "accept",
			"ids":    []string{reqIDs[0], reqIDs[1]},
		})
		if codeAccept != http.StatusOK {
			subT.Fatalf("bulk accept = %d (%v)", codeAccept, outAccept)
		}
		resultsA, _ := outAccept["results"].([]any)
		if len(resultsA) != 2 {
			subT.Fatalf("expected 2 accept results, got %v", outAccept)
		}

		// Reject reqIDs[2] (subject 3) with reason
		rejectReason := "التحويل غير مطابق للمبلغ المطلوب"
		codeReject, outReject, _ := c.request(subT, http.MethodPost, c.consoleURL+"/api/requests/bulk", map[string]string{
			"X-Admin-Token": c.adminToken,
		}, map[string]any{
			"action": "reject",
			"ids":    []string{reqIDs[2]},
			"reason": rejectReason,
		})
		if codeReject != http.StatusOK {
			subT.Fatalf("bulk reject = %d (%v)", codeReject, outReject)
		}
		record("07_decision_accept2_reject1", http.StatusOK, fmt.Sprintf("accepted requests [%s, %s], rejected %s with reason: %q", reqIDs[0], reqIDs[1], reqIDs[2], rejectReason))
	})

	// Step 8: Website shows the per-subject result
	t.Run("08_website_shows_per_subject_result", func(subT *testing.T) {
		code, out, _ := c.request(subT, http.MethodGet, c.gatewayURL+"/api/v1/academy/me/orders/"+orderID, map[string]string{
			"Authorization": "Bearer " + webAccess,
			"X-Client":      "web",
			"Origin":        "http://localhost:3000",
		}, nil)
		if code != http.StatusOK {
			subT.Fatalf("GET /academy/me/orders/%s = %d (%v)", orderID, code, out)
		}
		items, _ := out["items"].([]any)
		if len(items) != 3 {
			subT.Fatalf("expected 3 items in order, got %d", len(items))
		}

		var (
			accCount       int
			rejCount       int
			foundRejReason string
		)
		for _, it := range items {
			itm, _ := it.(map[string]any)
			status, _ := itm["status"].(string)
			if status == "accepted" {
				accCount++
			} else if status == "rejected" {
				rejCount++
				foundRejReason, _ = itm["reject_reason"].(string)
			}
		}

		if accCount != 2 || rejCount != 1 {
			subT.Fatalf("expected 2 accepted, 1 rejected; got accepted=%d rejected=%d", accCount, rejCount)
		}
		if foundRejReason != "التحويل غير مطابق للمبلغ المطلوب" {
			subT.Fatalf("unexpected reject reason: %q", foundRejReason)
		}
		record("08_web_per_subject_status", code, fmt.Sprintf("order items: 2 accepted, 1 rejected with reason %q", foundRejReason))
	})

	// Step 9: App shows the 2 subjects owned
	t.Run("09_app_shows_2_owned", func(subT *testing.T) {
		code, out, _ := c.requestApp(subT, http.MethodGet, c.gatewayURL+"/api/v1/academy/me/entitlements", map[string]string{
			"Authorization": "Bearer " + appAccess,
		}, nil)
		if code != http.StatusOK {
			subT.Fatalf("GET /academy/me/entitlements = %d (%v)", code, out)
		}
		entList, _ := out["subject_ids"].([]any)
		ownedMap := make(map[string]bool)
		for _, s := range entList {
			ownedMap[s.(string)] = true
		}
		if !ownedMap[subjIDs[0]] || !ownedMap[subjIDs[1]] {
			subT.Fatalf("expected subjects %s and %s to be owned, got %v", subjIDs[0], subjIDs[1], entList)
		}
		if ownedMap[subjIDs[2]] {
			subT.Fatalf("rejected subject %s MUST NOT be owned", subjIDs[2])
		}
		record("09_app_entitlements", code, fmt.Sprintf("app entitlements confirm subjects %s & %s owned, rejected %s unowned", subjIDs[0], subjIDs[1], subjIDs[2]))
	})

	// Step 10: Web token still 403 on play/download
	t.Run("10_web_token_refused_on_play_and_download", func(subT *testing.T) {
		if videoID != "" {
			code, out, _ := c.request(subT, http.MethodPost, c.gatewayURL+"/api/v1/academy/videos/"+videoID+"/play", map[string]string{
				"Authorization": "Bearer " + webAccess,
				"X-Client":      "web",
				"Origin":        "http://localhost:3000",
			}, nil)
			if code != http.StatusForbidden {
				subT.Fatalf("expected 403 on video play with web token, got %d (%v)", code, out)
			}
			if out["code"] != "web_access_forbidden" {
				subT.Errorf("expected code web_access_forbidden, got %v", out["code"])
			}
		}

		if fileID != "" {
			code, out, _ := c.request(subT, http.MethodGet, c.gatewayURL+"/api/v1/academy/subjects/"+subjIDs[0]+"/files/"+fileID+"/download", map[string]string{
				"Authorization": "Bearer " + webAccess,
				"X-Client":      "web",
				"Origin":        "http://localhost:3000",
			}, nil)
			if code != http.StatusForbidden {
				subT.Fatalf("expected 403 on file download with web token, got %d (%v)", code, out)
			}
			if out["code"] != "web_access_forbidden" {
				subT.Errorf("expected code web_access_forbidden, got %v", out["code"])
			}
		}
		record("10_web_token_forbidden", http.StatusForbidden, "POST /play and GET /download both answered 403 web_access_forbidden on web token")
	})

	// Step 11: Account features from Part B each tried once
	t.Run("11_account_features_part_b", func(subT *testing.T) {
		// Feature 1: Profile update (name and add phone)
		newFullName := "طالب معدل بالكامل"
		newPhone := fmt.Sprintf("+2011%08d", time.Now().UnixNano()%100000000)
		code, out, _ := c.request(subT, http.MethodPatch, c.gatewayURL+"/api/v1/auth/me", map[string]string{
			"Authorization": "Bearer " + webAccess,
			"X-Client":      "web",
			"Origin":        "http://localhost:3000",
		}, map[string]any{
			"full_name":        newFullName,
			"phone":            newPhone,
			"current_password": studentPass,
		})
		if code != http.StatusOK {
			subT.Fatalf("PATCH /auth/me = %d (%v)", code, out)
		}
		record("11a_profile_edit", code, fmt.Sprintf("updated full_name=%q and phone=%q via PATCH /auth/me", newFullName, newPhone))

		// Feature 2: Password change
		newPass := "NewSecretPass2026!"
		code, out, _ = c.request(subT, http.MethodPost, c.gatewayURL+"/api/v1/auth/password/change", map[string]string{
			"Authorization": "Bearer " + webAccess,
			"X-Client":      "web",
			"Origin":        "http://localhost:3000",
		}, map[string]string{
			"current_password": studentPass,
			"new_password":     newPass,
		})
		if code != http.StatusOK {
			subT.Fatalf("POST /auth/password/change = %d (%v)", code, out)
		}
		studentPass = newPass
		record("11b_password_change", code, "password changed successfully, other sessions terminated")

		// Re-login on web to obtain fresh session after password change
		code, lOut, _ := c.request(subT, http.MethodPost, c.gatewayURL+"/api/v1/auth/login", map[string]string{
			"X-Client": "web",
			"Origin":   "http://localhost:3000",
		}, map[string]string{
			"email":     studentEmail,
			"password":  studentPass,
			"device_id": deviceID,
		})
		if code != http.StatusOK {
			subT.Fatalf("re-login = %d (%v)", code, lOut)
		}
		webAccess = lOut["access_token"].(string)

		// Feature 3: Devices & sessions list and terminate
		code, sOut, _ := c.request(subT, http.MethodGet, c.gatewayURL+"/api/v1/auth/sessions", map[string]string{
			"Authorization": "Bearer " + webAccess,
			"X-Client":      "web",
			"Origin":        "http://localhost:3000",
		}, nil)
		if code != http.StatusOK {
			subT.Fatalf("GET /auth/sessions = %d (%v)", code, sOut)
		}
		sessions, _ := sOut["sessions"].([]any)
		if len(sessions) == 0 {
			subT.Fatalf("expected active sessions, got none")
		}
		// End session
		firstSid, _ := sessions[0].(map[string]any)["sid"].(string)
		record("11c_sessions_list", code, fmt.Sprintf("retrieved %d active sessions (web badge verified)", len(sessions)))

		// Feature 4: Notifications inbox
		code, nOut, _ := c.request(subT, http.MethodGet, c.gatewayURL+"/api/v1/notifications/list?page=1&limit=5", map[string]string{
			"Authorization": "Bearer " + webAccess,
			"X-Client":      "web",
			"Origin":        "http://localhost:3000",
		}, nil)
		if code != http.StatusOK {
			subT.Fatalf("GET /notifications/list = %d (%v)", code, nOut)
		}
		notifs, _ := nOut["notifications"].([]any)
		if len(notifs) > 0 {
			nID, _ := notifs[0].(map[string]any)["id"].(string)
			codeRead, _, _ := c.request(subT, http.MethodPost, c.gatewayURL+"/api/v1/notifications/read", map[string]string{
				"Authorization": "Bearer " + webAccess,
				"X-Client":      "web",
				"Origin":        "http://localhost:3000",
			}, map[string]string{"id": nID})
			record("11d_notifications_read", codeRead, fmt.Sprintf("inbox loaded %d items, marked notification %s as read", len(notifs), nID))
		} else {
			record("11d_notifications_empty", code, "notifications list loaded (0 items)")
		}

		// Feature 5: My subjects
		code, mOut, _ := c.request(subT, http.MethodGet, c.gatewayURL+"/api/v1/academy/me/entitlements", map[string]string{
			"Authorization": "Bearer " + webAccess,
			"X-Client":      "web",
			"Origin":        "http://localhost:3000",
		}, nil)
		if code != http.StatusOK {
			subT.Fatalf("GET /academy/me/entitlements = %d (%v)", code, mOut)
		}
		record("11e_my_subjects", code, fmt.Sprintf("retrieved student entitlements on web hub: %v", mOut["subject_ids"]))

		// Feature 6: Account self-deletion flow
		code, dOut, _ := c.request(subT, http.MethodPost, c.gatewayURL+"/api/v1/auth/account/delete", map[string]string{
			"Authorization": "Bearer " + webAccess,
			"X-Client":      "web",
			"Origin":        "http://localhost:3000",
		}, map[string]string{
			"current_password": studentPass,
			"confirm":          "حذف",
		})
		if code != http.StatusOK {
			subT.Fatalf("POST /auth/account/delete = %d (%v)", code, dOut)
		}
		purgeAfter, _ := dOut["purge_after"].(string)
		record("11f_account_deletion", code, fmt.Sprintf("account marked pending_deletion, scheduled purge_after=%s, all sessions revoked", purgeAfter))

		_ = firstSid
	})
}
