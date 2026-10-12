package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/omarmaarouf18/wael-app/academy-service/internal/models"
)

func grantTestEntitlement(t *testing.T, s *Server, id, userID, subjectID string) {
	t.Helper()
	e := &models.Entitlement{
		ID:        id,
		UserID:    userID,
		SubjectID: subjectID,
		ExpiresAt: time.Now().Add(48 * time.Hour).UTC(),
		GrantedAt: time.Now().UTC(),
		Source:    models.EntitlementSourceAdminGrant,
		GrantedBy: "adm-1",
		Active:    true,
	}
	if err := s.Store.Grant(context.Background(), e); err != nil {
		t.Fatalf("Grant %s: %v", id, err)
	}
}

type ownerIDsResponse struct {
	SubjectID  string   `json:"subject_id"`
	UserIDs    []string `json:"user_ids"`
	NextCursor string   `json:"next_cursor"`
	Total      int      `json:"total"`
}

func getOwnerIDs(t *testing.T, s *Server, path string) (int, ownerIDsResponse) {
	t.Helper()
	rec := doAdmin(t, s, http.MethodGet, path, "test-internal-token", "some-admin-token", "")
	var v ownerIDsResponse
	if rec.Code == http.StatusOK {
		if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
	}
	return rec.Code, v
}

func TestAdminBroadcastAudience_ActiveOnly(t *testing.T) {
	s, _ := newAdminTestServer(t, okVerify)
	future := time.Now().Add(48 * time.Hour).UTC()
	subj := createTestSubject(t, s, "bachelor-y1", 1500, future)

	grantTestEntitlement(t, s, "ent-active", "user-active", subj.ID)
	grantTestEntitlement(t, s, "ent-revoked", "user-revoked", subj.ID)
	// Grant normalizes active/expiry from the subject; revocation is applied
	// through the store so the row keeps its revoked marker.
	if _, err := s.Store.RevokeEntitlement(context.Background(), "ent-revoked", "adm-1", "test", time.Now().UTC()); err != nil {
		t.Fatalf("RevokeEntitlement: %v", err)
	}
	// Expired, inactive and unrevoked-marker rows are covered white-box at
	// the store level (Grant would normalize them away here).

	code, v := getOwnerIDs(t, s, "/internal/admin/subjects/"+subj.ID+"/owner-ids")
	if code != http.StatusOK {
		t.Fatalf("expected 200, got %d", code)
	}
	if v.SubjectID != subj.ID || v.Total != 1 || len(v.UserIDs) != 1 || v.UserIDs[0] != "user-active" {
		t.Fatalf("response = %+v, want only user-active", v)
	}
	if v.NextCursor != "" {
		t.Fatalf("single page must have empty next_cursor, got %q", v.NextCursor)
	}
}

func TestAdminBroadcastAudience_Paging(t *testing.T) {
	s, _ := newAdminTestServer(t, okVerify)
	future := time.Now().Add(48 * time.Hour).UTC()
	subj := createTestSubject(t, s, "bachelor-y1", 1500, future)

	for i, uid := range []string{"u-a", "u-b", "u-c"} {
		grantTestEntitlement(t, s, "ent-pg-"+string(rune('0'+i)), uid, subj.ID)
	}
	code, v := getOwnerIDs(t, s, "/internal/admin/subjects/"+subj.ID+"/owner-ids?limit=2")
	if code != http.StatusOK || v.Total != 3 || len(v.UserIDs) != 2 {
		t.Fatalf("page1 = %d %+v", code, v)
	}
	if v.UserIDs[0] != "u-a" || v.UserIDs[1] != "u-b" || v.NextCursor != "u-b" {
		t.Fatalf("page1 = %+v, want [u-a u-b] cursor u-b", v)
	}
	code, v = getOwnerIDs(t, s, "/internal/admin/subjects/"+subj.ID+"/owner-ids?limit=2&cursor=u-b")
	if code != http.StatusOK || len(v.UserIDs) != 1 || v.UserIDs[0] != "u-c" || v.NextCursor != "" {
		t.Fatalf("page2 = %d %+v", code, v)
	}
}

func TestAdminBroadcastAudience_NotFoundAndGuards(t *testing.T) {
	s, _ := newAdminTestServer(t, okVerify)

	rec := doAdmin(t, s, http.MethodGet, "/internal/admin/subjects/no-such-subject/owner-ids", "test-internal-token", "some-admin-token", "")
	if rec.Code != http.StatusNotFound || adminCode(t, rec) != "subject_not_found" {
		t.Fatalf("unknown subject = %d %s, want 404 subject_not_found", rec.Code, rec.Body.String())
	}
	rec = doAdmin(t, s, http.MethodPost, "/internal/admin/subjects/no-such-subject/owner-ids", "test-internal-token", "some-admin-token", "")
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST = %d, want 405", rec.Code)
	}
	rec = doAdmin(t, s, http.MethodGet, "/internal/admin/subjects/bad!id/owner-ids", "test-internal-token", "some-admin-token", "")
	if rec.Code != http.StatusBadRequest || adminCode(t, rec) != "invalid_subject_id" {
		t.Fatalf("malformed id = %d %s, want 400 invalid_subject_id", rec.Code, rec.Body.String())
	}
	rec = doAdmin(t, s, http.MethodGet, "/internal/admin/subjects/abc/owner-ids", "", "some-admin-token", "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("missing internal token = %d, want 401", rec.Code)
	}
}
