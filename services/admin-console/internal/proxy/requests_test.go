package proxy

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
)

// requestsRoutes returns the table entries for the requests and entitlements routes.
func requestsRoutes() []route {
	return []route{
		{
			name: "requests.list", handler: func(p *Proxy) http.HandlerFunc { return p.RequestsList },
			method: http.MethodGet, target: "/api/requests?status=pending&subject_id=sub-1&page=2&limit=10",
			upstreamMethod: http.MethodGet, upstreamPath: "/internal/admin/requests",
			upstreamQuery:  "limit=10&page=2&status=pending&subject_id=sub-1",
			upstreamStatus: 200, upstreamReply: `{"items":[],"total":0,"page":2,"limit":10,"pending_count":0}`,
		},
		{
			name: "requests.accept", handler: func(p *Proxy) http.HandlerFunc { return p.RequestsAccept },
			method: http.MethodPost, target: "/api/requests/accept",
			body:           `{"id":"req-1"}`,
			upstreamMethod: http.MethodPost, upstreamPath: "/internal/admin/requests/req-1/accept",
			upstreamStatus: 200, upstreamReply: `{"id":"req-1","status":"accepted"}`,
		},
		{
			name: "requests.reject", handler: func(p *Proxy) http.HandlerFunc { return p.RequestsReject },
			method: http.MethodPost, target: "/api/requests/reject",
			body:           `{"id":"req-1","reason":"payment not received"}`,
			upstreamMethod: http.MethodPost, upstreamPath: "/internal/admin/requests/req-1/reject",
			upstreamBody:   `{"reason":"payment not received"}`,
			upstreamStatus: 200, upstreamReply: `{"ok":true}`,
		},
		{
			name: "entitlements.list", handler: func(p *Proxy) http.HandlerFunc { return p.EntitlementsList },
			method: http.MethodGet, target: "/api/entitlements?user_id=" + testID,
			upstreamMethod: http.MethodGet, upstreamPath: "/internal/admin/entitlements",
			upstreamQuery:  "user_id=" + testID,
			upstreamStatus: 200, upstreamReply: `{"items":[]}`,
		},
		{
			name: "entitlements.grant", handler: func(p *Proxy) http.HandlerFunc { return p.EntitlementsGrant },
			method: http.MethodPost, target: "/api/entitlements/grant",
			body:           `{"user_id":"` + testID + `","subject_id":"sub-1"}`,
			upstreamMethod: http.MethodPost, upstreamPath: "/internal/admin/entitlements",
			upstreamBody:   `{"user_id":"` + testID + `","subject_id":"sub-1"}`,
			upstreamStatus: 201, upstreamReply: `{"id":"ent-1"}`,
			wantStatus: 201,
		},
		{
			name: "entitlements.revoke", handler: func(p *Proxy) http.HandlerFunc { return p.EntitlementsRevoke },
			method: http.MethodPost, target: "/api/entitlements/revoke",
			body:           `{"id":"ent-1","reason":"refund requested"}`,
			upstreamMethod: http.MethodDelete, upstreamPath: "/internal/admin/entitlements/ent-1",
			upstreamBody:   `{"reason":"refund requested"}`,
			upstreamStatus: 200, upstreamReply: `{"ok":true}`,
		},
	}
}

func TestRequests_Validation(t *testing.T) {
	up := newUpstream(t, 200, `{"items":[]}`)
	p := newProxy(t, up)

	// RequestsList invalid status
	w := do(p.RequestsList, http.MethodGet, "/api/requests?status=bogus", "", withToken())
	assertSafeError(t, w, http.StatusBadRequest, "bad_request")

	// RequestsList invalid subject_id
	w = do(p.RequestsList, http.MethodGet, "/api/requests?subject_id=bad$id", "", withToken())
	assertSafeError(t, w, http.StatusBadRequest, "bad_request")

	// RequestsAccept invalid id
	w = do(p.RequestsAccept, http.MethodPost, "/api/requests/accept", `{"id":"bad$id"}`, withToken())
	assertSafeError(t, w, http.StatusBadRequest, "bad_request")

	// RequestsReject missing reason
	w = do(p.RequestsReject, http.MethodPost, "/api/requests/reject", `{"id":"req-1"}`, withToken())
	assertSafeError(t, w, http.StatusBadRequest, "bad_request")

	// RequestsReject empty reason
	w = do(p.RequestsReject, http.MethodPost, "/api/requests/reject", `{"id":"req-1","reason":""}`, withToken())
	assertSafeError(t, w, http.StatusBadRequest, "bad_request")

	// RequestsReject too long reason (> 1000 runes)
	longReason := strings.Repeat("x", 1001)
	w = do(p.RequestsReject, http.MethodPost, "/api/requests/reject", `{"id":"req-1","reason":"`+longReason+`"}`, withToken())
	assertSafeError(t, w, http.StatusBadRequest, "bad_request")
}

func TestEntitlements_Validation(t *testing.T) {
	up := newUpstream(t, 200, `{"items":[]}`)
	p := newProxy(t, up)

	// EntitlementsList missing user_id
	w := do(p.EntitlementsList, http.MethodGet, "/api/entitlements", "", withToken())
	assertSafeError(t, w, http.StatusBadRequest, "bad_request")

	// EntitlementsList invalid user_id
	w = do(p.EntitlementsList, http.MethodGet, "/api/entitlements?user_id=not-a-uuid", "", withToken())
	assertSafeError(t, w, http.StatusBadRequest, "bad_request")

	// EntitlementsGrant invalid user_id
	w = do(p.EntitlementsGrant, http.MethodPost, "/api/entitlements/grant", `{"user_id":"bad-id","subject_id":"sub-1"}`, withToken())
	assertSafeError(t, w, http.StatusBadRequest, "bad_request")

	// EntitlementsGrant invalid subject_id
	w = do(p.EntitlementsGrant, http.MethodPost, "/api/entitlements/grant", `{"user_id":"`+testID+`","subject_id":"bad$subj"}`, withToken())
	assertSafeError(t, w, http.StatusBadRequest, "bad_request")

	// EntitlementsRevoke invalid id
	w = do(p.EntitlementsRevoke, http.MethodPost, "/api/entitlements/revoke", `{"id":"bad$id","reason":"valid reason"}`, withToken())
	assertSafeError(t, w, http.StatusBadRequest, "bad_request")

	// EntitlementsRevoke invalid reason
	w = do(p.EntitlementsRevoke, http.MethodPost, "/api/entitlements/revoke", `{"id":"ent-1","reason":""}`, withToken())
	assertSafeError(t, w, http.StatusBadRequest, "bad_request")
}

func TestAccounts_FilterByIDs(t *testing.T) {
	up := newUpstream(t, 200, `{"items":[],"total":0}`)
	p := newProxy(t, up)

	// Valid single ID
	w := do(p.Accounts, http.MethodGet, "/api/accounts?ids="+testID, "", withToken())
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	calls := up.calls()
	if len(calls) != 1 || calls[0].RawQuery != "ids="+testID {
		t.Fatalf("unexpected calls: %+v", calls)
	}

	// Valid multiple IDs and deduplication
	id2 := "22222222-2222-2222-2222-222222222222"
	w = do(p.Accounts, http.MethodGet, fmt.Sprintf("/api/accounts?ids=%s,%s,%s", testID, id2, testID), "", withToken())
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}

	// Invalid ID format -> 400
	w = do(p.Accounts, http.MethodGet, "/api/accounts?ids=not-a-uuid", "", withToken())
	assertSafeError(t, w, http.StatusBadRequest, "bad_request")

	// More than 100 IDs -> 400
	var manyIDs []string
	for i := 0; i < 101; i++ {
		manyIDs = append(manyIDs, fmt.Sprintf("00000000-0000-0000-0000-%012d", i))
	}
	w = do(p.Accounts, http.MethodGet, "/api/accounts?ids="+strings.Join(manyIDs, ","), "", withToken())
	assertSafeError(t, w, http.StatusBadRequest, "bad_request")
}

func TestRequests_RelayUpstreamErrors(t *testing.T) {
	cases := []struct {
		name       string
		handler    func(*Proxy) http.HandlerFunc
		method     string
		target     string
		body       string
		upStatus   int
		upBody     string
		wantStatus int
		wantCode   string
	}{
		{
			name: "accept request_not_pending", handler: func(p *Proxy) http.HandlerFunc { return p.RequestsAccept },
			method: http.MethodPost, target: "/api/requests/accept", body: `{"id":"req-1"}`,
			upStatus: 409, upBody: `{"code":"request_not_pending","error":"conflict"}`,
			wantStatus: 409, wantCode: "request_not_pending",
		},
		{
			name: "accept subject_expired", handler: func(p *Proxy) http.HandlerFunc { return p.RequestsAccept },
			method: http.MethodPost, target: "/api/requests/accept", body: `{"id":"req-1"}`,
			upStatus: 409, upBody: `{"code":"subject_expired","error":"conflict"}`,
			wantStatus: 409, wantCode: "subject_expired",
		},
		{
			name: "grant already_owned", handler: func(p *Proxy) http.HandlerFunc { return p.EntitlementsGrant },
			method: http.MethodPost, target: "/api/entitlements/grant", body: `{"user_id":"` + testID + `","subject_id":"sub-1"}`,
			upStatus: 409, upBody: `{"code":"already_owned","error":"conflict"}`,
			wantStatus: 409, wantCode: "already_owned",
		},
		{
			name: "grant subject_expired", handler: func(p *Proxy) http.HandlerFunc { return p.EntitlementsGrant },
			method: http.MethodPost, target: "/api/entitlements/grant", body: `{"user_id":"` + testID + `","subject_id":"sub-1"}`,
			upStatus: 409, upBody: `{"code":"subject_expired","error":"conflict"}`,
			wantStatus: 409, wantCode: "subject_expired",
		},
		{
			name: "revoke entitlement_not_found", handler: func(p *Proxy) http.HandlerFunc { return p.EntitlementsRevoke },
			method: http.MethodPost, target: "/api/entitlements/revoke", body: `{"id":"ent-1","reason":"valid reason"}`,
			upStatus: 404, upBody: `{"code":"entitlement_not_found","error":"not found"}`,
			wantStatus: 404, wantCode: "entitlement_not_found",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			up := newUpstream(t, tc.upStatus, tc.upBody)
			p := newProxy(t, up)
			w := do(tc.handler(p), tc.method, tc.target, tc.body, withToken())
			assertSafeError(t, w, tc.wantStatus, tc.wantCode)
		})
	}
}

func TestRequests_ReachAcademyNotAuth(t *testing.T) {
	for _, rt := range requestsRoutes() {
		t.Run(rt.name, func(t *testing.T) {
			authUp := newUpstream(t, 200, `{"items":[]}`)
			academyUp := newUpstream(t, rt.upstreamStatus, rt.upstreamReply)
			p := newSplitProxy(t, authUp, academyUp)
			w := do(rt.handler(p), rt.method, rt.target, rt.body, withToken())
			if w.Code != http.StatusOK && w.Code != http.StatusCreated {
				t.Fatalf("status = %d body=%q", w.Code, w.Body.String())
			}
			if n := len(authUp.calls()); n != 0 {
				t.Fatalf("auth upstream was called %d times", n)
			}
			if n := len(academyUp.calls()); n != 1 {
				t.Fatalf("academy upstream calls = %d, want 1", n)
			}
		})
	}
}
