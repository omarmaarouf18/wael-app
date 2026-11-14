package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/omarmaarouf18/wael-app/academy-service/internal/iap"
	"github.com/omarmaarouf18/wael-app/academy-service/internal/models"
)

// iapAuditFixture builds a server with one published subject in a package and
// returns a signed transaction for it (owner 2026-11-14: iap_purchase audit).
func iapAuditFixture(t *testing.T, txID, env string) (*Server, string, string, *models.IAPPackage) {
	t.Helper()
	s := newTestServer(false)
	ca := newIAPTestCA(t)
	s.IAPVerifier = ca.verifier
	s.IAPBundleID = "com.wael.app"
	s.IAPSandboxAllowAll = true

	studentID := "usr_stud_audit_" + txID
	future := time.Now().UTC().Add(30 * 24 * time.Hour).Truncate(time.Second)
	subj := createIAPTestSubject(t, s, "subj_audit_"+txID, "bachelor-y1", models.StatusPublished, future)
	pkg := &models.IAPPackage{
		ID:         "pkg_audit_" + txID,
		ProductID:  "com.wael.app.pkg.year2",
		TitleAr:    "الفرقة الثانية",
		TitleEn:    "Year 2",
		SubjectIDs: []string{subj.ID},
		Published:  true,
		Position:   1,
	}
	if err := s.Store.CreateIAPPackage(context.Background(), pkg); err != nil {
		t.Fatalf("CreateIAPPackage: %v", err)
	}
	ms := time.Now().UTC().UnixMilli()
	jws := ca.makeJWS(t, &iap.JWSTransactionPayload{
		TransactionID:         txID,
		OriginalTransactionID: txID,
		BundleID:              "com.wael.app",
		ProductID:             pkg.ProductID,
		PurchaseDate:          ms,
		OriginalPurchaseDate:  ms,
		SignedDate:            ms,
		Type:                  "Non-Renewing Subscription",
		Environment:           env,
		AppAccountToken:       models.DeterministicAppAccountToken(studentID),
	})
	return s, studentID, jws, pkg
}

func iapAuditEntries(t *testing.T, s *Server) []*models.AuditLog {
	t.Helper()
	logs, _, err := s.Store.ListAuditLogs(context.Background(), 1, 100)
	if err != nil {
		t.Fatalf("ListAuditLogs: %v", err)
	}
	var out []*models.AuditLog
	for _, l := range logs {
		if l.Action == "iap_purchase" {
			out = append(out, l)
		}
	}
	return out
}

func TestIAPTransaction_NewGrant_WritesExactlyOneAuditEntry(t *testing.T) {
	s, studentID, jws, pkg := iapAuditFixture(t, "tx_audit_new_1", "Sandbox")
	rec := postTransaction(t, s, makeStudentToken(t, studentID), "test-gateway-secret", map[string]string{"signed_transaction": jws})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	var resp IAPTransactionResponseDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil || resp.Status != "granted" {
		t.Fatalf("status = %q (err %v), want granted", resp.Status, err)
	}

	entries := iapAuditEntries(t, s)
	if len(entries) != 1 {
		t.Fatalf("iap_purchase entries = %d, want 1", len(entries))
	}
	e := entries[0]
	if e.ActorID != studentID || e.ActorName != studentID {
		t.Errorf("actor = %q/%q, want student id %q", e.ActorID, e.ActorName, studentID)
	}
	if e.TargetType != "iap_package" || e.TargetID != pkg.ID {
		t.Errorf("target = %s/%s, want iap_package/%s", e.TargetType, e.TargetID, pkg.ID)
	}
	want := "product=com.wael.app.pkg.year2 env=Sandbox tx=tx_audit_new_1 subjects=1"
	if e.Detail != want {
		t.Errorf("detail = %q, want %q", e.Detail, want)
	}
	if strings.Contains(e.Detail, jws[:20]) || strings.Contains(e.Detail, models.DeterministicAppAccountToken(studentID)) {
		t.Error("detail must not carry the JWS or the appAccountToken")
	}
}

func TestIAPTransaction_AlreadyProcessed_WritesNoAuditEntry(t *testing.T) {
	s, studentID, jws, _ := iapAuditFixture(t, "tx_audit_replay_1", "Production")
	tok := makeStudentToken(t, studentID)
	if rec := postTransaction(t, s, tok, "test-gateway-secret", map[string]string{"signed_transaction": jws}); rec.Code != http.StatusOK {
		t.Fatalf("first submit: %d (%s)", rec.Code, rec.Body.String())
	}
	// Restore re-submits the same transaction: already_processed, no new entry.
	for i := 0; i < 2; i++ {
		rec := postTransaction(t, s, tok, "test-gateway-secret", map[string]string{"signed_transaction": jws})
		var resp IAPTransactionResponseDTO
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil || resp.Status != "already_processed" {
			t.Fatalf("replay %d: status %q (code %d, err %v)", i, resp.Status, rec.Code, err)
		}
	}
	if n := len(iapAuditEntries(t, s)); n != 1 {
		t.Fatalf("iap_purchase entries after replays = %d, want 1 (only the original grant)", n)
	}
}

func TestIAPTransaction_AuditFailure_DoesNotFailPurchase(t *testing.T) {
	s, studentID, jws, _ := iapAuditFixture(t, "tx_audit_fail_1", "Production")
	s.Store = &failingAuditStore{Store: s.Store}
	rec := postTransaction(t, s, makeStudentToken(t, studentID), "test-gateway-secret", map[string]string{"signed_transaction": jws})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 despite audit failure, got %d (%s)", rec.Code, rec.Body.String())
	}
	var resp IAPTransactionResponseDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil || resp.Status != "granted" || len(resp.GrantedSubjectIDs) != 1 {
		t.Fatalf("resp = %+v (err %v), want granted with 1 subject", resp, err)
	}
}
