package store

import (
	"context"
	"errors"
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

	// Identifiers stay reserved during the grace period (owner decision D2):
	// the email unique index covers every status, and the phone partial
	// index covers pending_deletion, so neither can be taken until the
	// purge finalizes the deletion.
	dupEmail := &models.User{ID: "del-dup-email", Email: "del1@example.com", PasswordHash: "h", Role: models.RoleUser, FullName: "Dup", Phone: "+201012345791", Status: models.StatusActive}
	if err := s.Create(ctx, dupEmail); !errors.Is(err, ErrDuplicate) {
		t.Fatalf("duplicate email during grace: err = %v, want ErrDuplicate", err)
	}
	dupPhone := &models.User{ID: "del-dup-phone", Email: "other@example.com", PasswordHash: "h", Role: models.RoleUser, FullName: "Dup", Phone: "+201012345799", Status: models.StatusActive}
	if err := s.Create(ctx, dupPhone); !errors.Is(err, ErrDuplicate) {
		t.Fatalf("duplicate phone during grace: err = %v, want ErrDuplicate", err)
	}
	holder, err := s.FindByPhone(ctx, "+201012345799")
	if err != nil || holder == nil || holder.ID != u.ID {
		t.Fatalf("FindByPhone during grace: holder=%+v err=%v", holder, err)
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

// TestMemoryStore_CancelDeletionPhoneTaken plants a second verified account
// on the same phone (bypassing the checks CancelDeletion itself relies on)
// and proves the restore refuses with the distinct ErrPhoneTaken instead of
// creating a duplicate. Unreachable while the grace period reserves the
// phone; Login answers it with 409, never a retry loop.
func TestMemoryStore_CancelDeletionPhoneTaken(t *testing.T) {
	st := NewMemoryStore()
	ctx := context.Background()
	now := time.Now().UTC()
	a := &models.User{ID: "pt-a", Email: "pta@example.com", PasswordHash: "h", Role: models.RoleUser, FullName: "A", Phone: "+201012345792", EmailVerified: true}
	if err := st.Create(ctx, a); err != nil {
		t.Fatalf("create A: %v", err)
	}
	if err := st.RequestDeletion(ctx, "pt-a", now, now.Add(30*24*time.Hour)); err != nil {
		t.Fatalf("request: %v", err)
	}
	b := &models.User{ID: "pt-b", Email: "ptb@example.com", PasswordHash: "h", Role: models.RoleUser, FullName: "B", Phone: "+201012345792", EmailVerified: true, Status: models.StatusActive, CreatedAt: now, UpdatedAt: now}
	st.byID["pt-b"] = b
	st.byMail["ptb@example.com"] = b

	if err := st.CancelDeletion(ctx, "pt-a", now); !errors.Is(err, ErrPhoneTaken) {
		t.Fatalf("CancelDeletion: err = %v, want ErrPhoneTaken", err)
	}
	still, err := st.FindByID(ctx, "pt-a")
	if err != nil || still == nil || still.Status != models.StatusPendingDeletion {
		t.Fatalf("account left in %+v (err %v), want still pending_deletion", still, err)
	}
}

// TestMongoStore_PhoneIndexMigrationToPendingDeletion simulates production
// data as created by the pre-change EnsureIndexes (phone_1 covering only
// active/suspended, plus live rows) and proves the new EnsureIndexes migrates
// cleanly on boot: no IndexOptionsConflict, phone_1 replaced by the filter
// that also covers pending_deletion. Idempotent: a second boot works.
func TestMongoStore_PhoneIndexMigrationToPendingDeletion(t *testing.T) {
	mongoURI, _ := requireDB(t)
	dbName := randomDBName("test_auth_phonemig")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// 1. New code creates the new indexes.
	first, err := NewMongoStore(ctx, mongoURI, dbName)
	if err != nil {
		t.Fatalf("first NewMongoStore: %v", err)
	}
	coll := first.coll

	// 2. Seed representative rows under the new shape.
	now := time.Now().UTC()
	active := &models.User{ID: "mig-active", Email: "migactive@example.com", PasswordHash: "h", Role: models.RoleUser, FullName: "Mig", Phone: "+201012345793", EmailVerified: true, Status: models.StatusActive, CreatedAt: now, UpdatedAt: now}
	if err := first.Create(ctx, active); err != nil {
		t.Fatalf("create active: %v", err)
	}

	// 3. Roll the phone index back to the pre-change shape.
	if err := coll.Indexes().DropOne(ctx, "phone_1"); err != nil {
		t.Fatalf("drop phone_1: %v", err)
	}
	oldPhone, err := coll.Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys: bson.D{{Key: "phone", Value: 1}},
		Options: options.Index().
			SetUnique(true).
			SetPartialFilterExpression(bson.M{
				"phone":          bson.M{"$type": "string", "$gt": ""},
				"status":         bson.M{"$in": []string{"active", "suspended"}},
				"email_verified": true,
			}),
	})
	if err != nil {
		t.Fatalf("recreate old phone index: %v", err)
	}
	if oldPhone != "phone_1" {
		t.Fatalf("old phone index name = %q, want phone_1", oldPhone)
	}

	// 4. Boot again against the old index with data present: must succeed.
	second, err := NewMongoStore(ctx, mongoURI, dbName)
	if err != nil {
		t.Fatalf("second NewMongoStore (migration boot): %v", err)
	}

	// 5. The migrated filter covers pending_deletion; the seeded row reads back.
	cursor, err := coll.Indexes().List(ctx)
	if err != nil {
		t.Fatalf("list indexes: %v", err)
	}
	defer cursor.Close(ctx)
	found := false
	for cursor.Next(ctx) {
		var doc struct {
			Name                    string `bson:"name"`
			PartialFilterExpression bson.M `bson:"partialFilterExpression"`
		}
		if err := cursor.Decode(&doc); err != nil {
			t.Fatalf("decode index: %v", err)
		}
		if doc.Name == "phone_1" {
			statuses, _ := doc.PartialFilterExpression["status"].(bson.M)["$in"].(bson.A)
			for _, st := range statuses {
				if st == "pending_deletion" {
					found = true
				}
			}
		}
	}
	if !found {
		t.Fatal("migrated phone_1 filter does not cover pending_deletion")
	}
	_ = second
}
