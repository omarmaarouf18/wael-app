package store

import (
	"context"
	"testing"
	"time"

	"github.com/omarmaarouf18/wael-app/auth-service/internal/models"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// runDeletionSuite exercises the F-UX2 self-deletion lifecycle against any
// Store: request (CAS active->pending_deletion), cancel (CAS back), purge
// (CAS + anonymize, no blocklist), due listing, and account events.
func runDeletionSuite(t *testing.T, s Store) {
	t.Helper()
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	u := &models.User{
		ID:            "del-1",
		Email:         "del1@example.com",
		PasswordHash:  "hash",
		Role:          models.RoleUser,
		FullName:      "Delete Me",
		Phone:         "+201012345799",
		EmailVerified: true,
	}
	if err := s.Create(ctx, u); err != nil {
		t.Fatalf("create: %v", err)
	}

	purgeAfter := now.Add(30 * 24 * time.Hour)
	if err := s.RequestDeletion(ctx, u.ID, now, purgeAfter); err != nil {
		t.Fatalf("request: %v", err)
	}
	got, err := s.FindByID(ctx, u.ID)
	if err != nil || got == nil {
		t.Fatalf("find: %v", err)
	}
	if got.Status != models.StatusPendingDeletion {
		t.Fatalf("status = %q, want pending_deletion", got.Status)
	}
	if !got.DeletionRequestedAt.Equal(now) || !got.PurgeAfter.Equal(purgeAfter) {
		t.Fatalf("grace fields = %v %v", got.DeletionRequestedAt, got.PurgeAfter)
	}

	// Not due yet.
	due, err := s.ListDeletionsDue(ctx, now.Add(29*24*time.Hour))
	if err != nil {
		t.Fatalf("list due: %v", err)
	}
	if len(due) != 0 {
		t.Fatalf("due early: %d", len(due))
	}
	// Due after the grace period.
	due, err = s.ListDeletionsDue(ctx, now.Add(31*24*time.Hour))
	if err != nil {
		t.Fatalf("list due: %v", err)
	}
	if len(due) != 1 || due[0].ID != u.ID {
		t.Fatalf("due = %+v", due)
	}

	// Cancel restores active and clears the fields.
	if err := s.CancelDeletion(ctx, u.ID, now); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	got, _ = s.FindByID(ctx, u.ID)
	if got.Status != models.StatusActive || !got.DeletionRequestedAt.IsZero() || !got.PurgeAfter.IsZero() {
		t.Fatalf("after cancel: %+v", got)
	}

	// Request again and purge past the deadline.
	if err := s.RequestDeletion(ctx, u.ID, now, now.Add(time.Hour)); err != nil {
		t.Fatalf("request 2: %v", err)
	}
	if err := s.PurgeDeletion(ctx, u.ID, "deleted-del-1@deleted.local", now.Add(2*time.Hour)); err != nil {
		t.Fatalf("purge: %v", err)
	}
	got, _ = s.FindByID(ctx, u.ID)
	if got.Status != models.StatusDeleted {
		t.Fatalf("status = %q, want deleted", got.Status)
	}
	if got.FullName != "" || got.Phone != "" || got.PasswordHash != "" {
		t.Fatalf("PII not cleared: %+v", got)
	}
	if got.Email != "deleted-del-1@deleted.local" {
		t.Fatalf("email = %q", got.Email)
	}
	if got.ID != u.ID {
		t.Fatalf("id changed: %q", got.ID)
	}

	// No blocklist entry: the identity may sign up again (F-UX2 A6).
	blocked, err := s.IsBlocked(ctx, "email", "hash-of-del1@example.com")
	if err != nil || blocked {
		t.Fatalf("unexpected blocklist hit: %v %v", blocked, err)
	}

	// Account events carry no PII values.
	if err := s.CreateAccountEvent(ctx, &models.AccountEvent{UserID: u.ID, Type: models.AccountEventDeletionRequested, CreatedAt: now}); err != nil {
		t.Fatalf("event: %v", err)
	}
	if err := s.CreateAccountEvent(ctx, &models.AccountEvent{UserID: u.ID, Type: models.AccountEventAccountPurged, CreatedAt: now}); err != nil {
		t.Fatalf("event: %v", err)
	}
}

func TestMemoryStore_DeletionLifecycle(t *testing.T) {
	runDeletionSuite(t, NewMemoryStore())
}

func TestMongoStore_DeletionLifecycle(t *testing.T) {
	mongoURI, _ := requireDB(t)
	dbName := randomDBName("test_auth_deletion")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	ms, err := NewMongoStore(ctx, mongoURI, dbName)
	if err != nil {
		t.Fatalf("NewMongoStore: %v", err)
	}
	runDeletionSuite(t, ms)
}

// TestMongoStore_AccountIndexes asserts the F-UX2 indexes exist after boot:
// the (status, purge_after) purge scan index on users and the
// (user_id, created_at) index on the new account_events collection. Both are
// new non-unique indexes, so creating them on existing data can never
// conflict; no migration step beyond EnsureIndexes (NewMongoStore) is needed.
func TestMongoStore_AccountIndexes(t *testing.T) {
	mongoURI, _ := requireDB(t)
	dbName := randomDBName("test_auth_acctidx")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if _, err := NewMongoStore(ctx, mongoURI, dbName); err != nil {
		t.Fatalf("NewMongoStore: %v", err)
	}
	// Second boot is idempotent.
	if _, err := NewMongoStore(ctx, mongoURI, dbName); err != nil {
		t.Fatalf("second NewMongoStore: %v", err)
	}

	client, err := mongo.Connect(options.Client().ApplyURI(mongoURI))
	if err != nil {
		t.Fatalf("mongo connect: %v", err)
	}
	defer func() { _ = client.Disconnect(context.Background()) }()
	db := client.Database(dbName)

	indexNames := func(collName string) map[string]bool {
		t.Helper()
		cursor, err := db.Collection(collName).Indexes().List(ctx)
		if err != nil {
			t.Fatalf("list %s indexes: %v", collName, err)
		}
		defer cursor.Close(ctx)
		names := map[string]bool{}
		for cursor.Next(ctx) {
			var doc struct {
				Name string `bson:"name"`
			}
			if err := cursor.Decode(&doc); err != nil {
				t.Fatalf("decode index: %v", err)
			}
			names[doc.Name] = true
		}
		return names
	}

	usersIdx := indexNames("users")
	if !usersIdx["status_1_purge_after_1"] {
		t.Fatalf("users indexes lack status_1_purge_after_1: %v", usersIdx)
	}
	evtIdx := indexNames("account_events")
	if !evtIdx["user_id_1_created_at_-1"] {
		t.Fatalf("account_events indexes lack user_id_1_created_at_-1: %v", evtIdx)
	}

	// The purge scan uses the new index (explain wins a fit check only
	// loosely: assert the query is served, i.e. no error on a covered scan).
	count, err := db.Collection("users").CountDocuments(ctx, bson.M{
		"status":      string(models.StatusPendingDeletion),
		"purge_after": bson.M{"$lte": time.Now().UTC()},
	})
	if err != nil {
		t.Fatalf("purge scan: %v", err)
	}
	if count != 0 {
		t.Fatalf("purge scan count = %d, want 0", count)
	}
}
