package handlers

// Accept claims the request before it grants anything (SPEC Section 7 R4,
// amended 2026-11-09). These tests pin the races and partial failures the
// old order (grant, record payment, then compare-and-set) got wrong.

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/omarmaarouf18/wael-app/academy-service/internal/models"
	"github.com/omarmaarouf18/wael-app/academy-service/internal/store"
)

// claimTestStore lets a test act at a chosen point inside acceptRequestCore.
type claimTestStore struct {
	*store.MemoryStore
	// beforeClaim runs once, on the first GetSubjectByID: after the
	// handler's pending check, before its compare-and-set.
	beforeClaim func()
	// duringGrant runs once inside Grant, after the claim.
	duringGrant func()
	// failPayment makes CreatePaymentRecord fail with a store error.
	failPayment bool
}

func (c *claimTestStore) GetSubjectByID(ctx context.Context, id string) (*models.Subject, error) {
	if f := c.beforeClaim; f != nil {
		c.beforeClaim = nil
		f()
	}
	return c.MemoryStore.GetSubjectByID(ctx, id)
}

func (c *claimTestStore) Grant(ctx context.Context, e *models.Entitlement) error {
	if f := c.duringGrant; f != nil {
		c.duringGrant = nil
		f()
	}
	return c.MemoryStore.Grant(ctx, e)
}

func (c *claimTestStore) CreatePaymentRecord(ctx context.Context, rec *models.PaymentRecord) error {
	if c.failPayment {
		return errors.New("payment store down")
	}
	return c.MemoryStore.CreatePaymentRecord(ctx, rec)
}

func newClaimTestServer(t *testing.T) (*Server, *claimTestStore) {
	t.Helper()
	mem := store.NewMemoryStore()
	_ = mem.SeedLevels(context.Background())
	cs := &claimTestStore{MemoryStore: mem}
	return New(cs, "test", "gw", "tok", "", false, ""), cs
}

var claimAdmin = &AdminIdentity{ID: "adm-1", Name: "Admin One"}

func rejectNow(t *testing.T, mem *store.MemoryStore, reqID string) bool {
	t.Helper()
	ok, err := mem.DecideRequest(context.Background(), reqID, models.RequestStatusPending, models.RequestStatusRejected, "adm-2", time.Now().UTC(), "wrong transfer")
	if err != nil {
		t.Fatalf("reject: %v", err)
	}
	return ok
}

func assertAccessState(t *testing.T, mem *store.MemoryStore, userID, subjectID string, wantOwned bool, wantPayments int) {
	t.Helper()
	ctx := context.Background()
	owned, err := mem.HasActiveEntitlement(ctx, userID, subjectID)
	if err != nil {
		t.Fatalf("HasActiveEntitlement: %v", err)
	}
	if owned != wantOwned {
		t.Errorf("active entitlement = %v, want %v", owned, wantOwned)
	}
	pays, err := mem.ListPaymentRecordsByUser(ctx, userID)
	if err != nil {
		t.Fatalf("ListPaymentRecordsByUser: %v", err)
	}
	if len(pays) != wantPayments {
		t.Errorf("payment records = %d, want %d", len(pays), wantPayments)
	}
}

// A reject that wins between the pending check and the claim leaves the
// accept with nothing written: no entitlement, no payment record.
func TestAcceptClaim_RejectWinsFirst_GrantsNothing(t *testing.T) {
	s, cs := newClaimTestServer(t)
	subj := createTestSubject(t, s, "bachelor-y1", 300, time.Now().Add(30*24*time.Hour))
	pr := createTestRequest(t, s, "stu-1", subj.ID)
	cs.beforeClaim = func() {
		if !rejectNow(t, cs.MemoryStore, pr.ID) {
			t.Fatal("reject did not apply")
		}
	}

	res := s.acceptRequestCore(context.Background(), claimAdmin, pr.ID)
	if res.OK || res.Code != "request_not_pending" {
		t.Fatalf("accept = ok %v code %q, want request_not_pending", res.OK, res.Code)
	}
	got, _ := cs.GetRequestByID(context.Background(), pr.ID)
	if got.Status != models.RequestStatusRejected {
		t.Fatalf("status = %s, want rejected", got.Status)
	}
	assertAccessState(t, cs.MemoryStore, "stu-1", subj.ID, false, 0)
}

// A reject that arrives while the accept is granting finds the request
// already claimed and changes nothing; the accept completes.
func TestAcceptClaim_RejectDuringGrant_Loses(t *testing.T) {
	s, cs := newClaimTestServer(t)
	subj := createTestSubject(t, s, "bachelor-y1", 300, time.Now().Add(30*24*time.Hour))
	pr := createTestRequest(t, s, "stu-1", subj.ID)
	rejected := true
	cs.duringGrant = func() { rejected = rejectNow(t, cs.MemoryStore, pr.ID) }

	res := s.acceptRequestCore(context.Background(), claimAdmin, pr.ID)
	if !res.OK {
		t.Fatalf("accept failed: %s", res.Code)
	}
	if rejected {
		t.Fatal("reject applied to a request the accept had already claimed")
	}
	got, _ := cs.GetRequestByID(context.Background(), pr.ID)
	if got.Status != models.RequestStatusAccepted {
		t.Fatalf("status = %s, want accepted", got.Status)
	}
	assertAccessState(t, cs.MemoryStore, "stu-1", subj.ID, true, 1)
}

// A payment-record failure after a fresh grant revokes that grant and puts
// the request back to pending; a reject afterwards leaves no access, and a
// retried accept completes with exactly one payment record.
func TestAcceptClaim_PaymentFailure_UndoesClaimAndGrant(t *testing.T) {
	s, cs := newClaimTestServer(t)
	subj := createTestSubject(t, s, "bachelor-y1", 300, time.Now().Add(30*24*time.Hour))
	pr := createTestRequest(t, s, "stu-1", subj.ID)
	cs.failPayment = true

	res := s.acceptRequestCore(context.Background(), claimAdmin, pr.ID)
	if res.OK || res.Code != "service_unavailable" {
		t.Fatalf("accept = ok %v code %q, want service_unavailable", res.OK, res.Code)
	}
	got, _ := cs.GetRequestByID(context.Background(), pr.ID)
	if got.Status != models.RequestStatusPending || got.DecidedAt != nil || got.DecidedBy != "" {
		t.Fatalf("after undo: status %s decided_at %v decided_by %q, want a clean pending row", got.Status, got.DecidedAt, got.DecidedBy)
	}
	assertAccessState(t, cs.MemoryStore, "stu-1", subj.ID, false, 0)

	cs.failPayment = false
	res = s.acceptRequestCore(context.Background(), claimAdmin, pr.ID)
	if !res.OK {
		t.Fatalf("retried accept failed: %s", res.Code)
	}
	assertAccessState(t, cs.MemoryStore, "stu-1", subj.ID, true, 1)
}

// A refused grant (subject expired between the check and the grant) puts
// the request back to pending.
func TestAcceptClaim_GrantRefused_RevertsToPending(t *testing.T) {
	s, cs := newClaimTestServer(t)
	subj := createTestSubject(t, s, "bachelor-y1", 300, time.Now().Add(30*24*time.Hour))
	pr := createTestRequest(t, s, "stu-1", subj.ID)
	cs.duringGrant = func() {
		subj.AccessExpiresAt = time.Now().Add(-time.Minute)
		if err := cs.UpdateSubject(context.Background(), subj); err != nil {
			t.Fatalf("UpdateSubject: %v", err)
		}
	}

	res := s.acceptRequestCore(context.Background(), claimAdmin, pr.ID)
	if res.OK || res.Code != "subject_expired" {
		t.Fatalf("accept = ok %v code %q, want subject_expired", res.OK, res.Code)
	}
	got, _ := cs.GetRequestByID(context.Background(), pr.ID)
	if got.Status != models.RequestStatusPending {
		t.Fatalf("status = %s, want pending", got.Status)
	}
	assertAccessState(t, cs.MemoryStore, "stu-1", subj.ID, false, 0)
}
