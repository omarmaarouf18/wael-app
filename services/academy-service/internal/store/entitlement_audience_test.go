package store

import (
	"context"
	"testing"
	"time"

	"github.com/omarmaarouf18/wael-app/academy-service/internal/models"
	"go.mongodb.org/mongo-driver/v2/bson"
)

// TestMemoryStore_ActiveOwnerIDs inserts raw rows (bypassing Grant, which
// normalizes active/expiry) and asserts SPEC R1 filtering: only active,
// unexpired, unrevoked owners are returned, ordered, paged and counted.
func TestMemoryStore_ActiveOwnerIDs(t *testing.T) {
	s := NewMemoryStore()
	now := time.Now()
	past := now.Add(-time.Hour)
	future := now.Add(48 * time.Hour)
	revokedAt := now.Add(-time.Minute)
	rows := []*models.Entitlement{
		{ID: "e-active", UserID: "u-active", SubjectID: "s1", Active: true, ExpiresAt: future},
		{ID: "e-expired", UserID: "u-expired", SubjectID: "s1", Active: true, ExpiresAt: past},
		{ID: "e-revoked", UserID: "u-revoked", SubjectID: "s1", Active: true, ExpiresAt: future, RevokedAt: &revokedAt},
		{ID: "e-inactive", UserID: "u-inactive", SubjectID: "s1", Active: false, ExpiresAt: future},
		{ID: "e-other", UserID: "u-other", SubjectID: "s2", Active: true, ExpiresAt: future},
		{ID: "e-b", UserID: "u-b", SubjectID: "s1", Active: true, ExpiresAt: future},
		{ID: "e-c", UserID: "u-c", SubjectID: "s1", Active: true, ExpiresAt: future},
	}
	s.mu.Lock()
	s.entitlements = append(s.entitlements, rows...)
	s.mu.Unlock()

	ctx := context.Background()
	ids, err := s.ListActiveOwnerIDsBySubject(ctx, "s1", "", 100)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	want := []string{"u-active", "u-b", "u-c"}
	if len(ids) != len(want) {
		t.Fatalf("ids = %v, want %v", ids, want)
	}
	for i := range want {
		if ids[i] != want[i] {
			t.Fatalf("ids = %v, want %v", ids, want)
		}
	}
	if n, err := s.CountActiveOwnersBySubject(ctx, "s1"); err != nil || n != 3 {
		t.Fatalf("count s1 = %d, %v; want 3", n, err)
	}
	if n, err := s.CountActiveOwnersBySubject(ctx, "s2"); err != nil || n != 1 {
		t.Fatalf("count s2 = %d, %v; want 1", n, err)
	}
	if n, err := s.CountActiveOwnersBySubject(ctx, "missing"); err != nil || n != 0 {
		t.Fatalf("count missing = %d, %v; want 0", n, err)
	}

	ids, err = s.ListActiveOwnerIDsBySubject(ctx, "s1", "", 2)
	if err != nil || len(ids) != 2 || ids[0] != "u-active" || ids[1] != "u-b" {
		t.Fatalf("page1 = %v, %v", ids, err)
	}
	ids, err = s.ListActiveOwnerIDsBySubject(ctx, "s1", "u-b", 2)
	if err != nil || len(ids) != 1 || ids[0] != "u-c" {
		t.Fatalf("page2 = %v, %v", ids, err)
	}
	ids, err = s.ListActiveOwnerIDsBySubject(ctx, "s1", "u-c", 2)
	if err != nil || len(ids) != 0 {
		t.Fatalf("page3 = %v, %v; want empty", ids, err)
	}
}

// TestMongoStore_ActiveOwnerIDs runs the same R1 filtering against MongoDB
// (skips cleanly without a database).
func TestMongoStore_ActiveOwnerIDs(t *testing.T) {
	mongoURI := requireDB(t)
	dbName := randomDBName("test_academy_audience")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	s, err := NewMongoStore(ctx, mongoURI, dbName)
	if err != nil {
		t.Fatalf("NewMongoStore failed: %v", err)
	}
	t.Cleanup(func() {
		dropCtx, dropCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer dropCancel()
		_ = s.Database().Drop(dropCtx)
		_ = s.Close(dropCtx)
	})

	now := time.Now()
	past := now.Add(-time.Hour)
	future := now.Add(48 * time.Hour)
	revokedAt := now.Add(-time.Minute)
	docs := []any{
		bson.M{"_id": "e-active", "user_id": "u-active", "subject_id": "s1", "active": true, "expires_at": future},
		bson.M{"_id": "e-expired", "user_id": "u-expired", "subject_id": "s1", "active": true, "expires_at": past},
		bson.M{"_id": "e-revoked", "user_id": "u-revoked", "subject_id": "s1", "active": true, "expires_at": future, "revoked_at": revokedAt},
		bson.M{"_id": "e-inactive", "user_id": "u-inactive", "subject_id": "s1", "active": false, "expires_at": future},
		bson.M{"_id": "e-other", "user_id": "u-other", "subject_id": "s2", "active": true, "expires_at": future},
		bson.M{"_id": "e-b", "user_id": "u-b", "subject_id": "s1", "active": true, "expires_at": future},
		bson.M{"_id": "e-c", "user_id": "u-c", "subject_id": "s1", "active": true, "expires_at": future},
	}
	if _, err := s.db.Collection("entitlements").InsertMany(ctx, docs); err != nil {
		t.Fatalf("insert: %v", err)
	}

	ids, err := s.ListActiveOwnerIDsBySubject(ctx, "s1", "", 100)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	want := []string{"u-active", "u-b", "u-c"}
	if len(ids) != len(want) {
		t.Fatalf("ids = %v, want %v", ids, want)
	}
	for i := range want {
		if ids[i] != want[i] {
			t.Fatalf("ids = %v, want %v", ids, want)
		}
	}
	if n, err := s.CountActiveOwnersBySubject(ctx, "s1"); err != nil || n != 3 {
		t.Fatalf("count s1 = %d, %v; want 3", n, err)
	}
	ids, err = s.ListActiveOwnerIDsBySubject(ctx, "s1", "", 2)
	if err != nil || len(ids) != 2 || ids[0] != "u-active" || ids[1] != "u-b" {
		t.Fatalf("page1 = %v, %v", ids, err)
	}
	ids, err = s.ListActiveOwnerIDsBySubject(ctx, "s1", "u-b", 2)
	if err != nil || len(ids) != 1 || ids[0] != "u-c" {
		t.Fatalf("page2 = %v, %v", ids, err)
	}
}
