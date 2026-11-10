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

// runOptionalPhoneSuite exercises the optional phone (owner amendment
// 2026-11-03, App Store 5.1.1(v)) against any Store: two verified accounts
// without a phone coexist, a duplicate phone is still refused, a first phone
// can be added at once while a later change keeps the 30-day limit, and a
// replacement write without a phone leaves none behind.
func runOptionalPhoneSuite(t *testing.T, s Store) {
	t.Helper()
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	mk := func(id, email, phone string) *models.User {
		return &models.User{
			ID: id, Email: email, PasswordHash: "h", Role: models.RoleUser,
			FullName: "No Phone", Phone: phone, EmailVerified: true,
			Status: models.StatusActive, CreatedAt: now, UpdatedAt: now,
		}
	}
	if err := s.Create(ctx, mk("np-1", "np1@example.com", "")); err != nil {
		t.Fatalf("create first no-phone account: %v", err)
	}
	if err := s.Create(ctx, mk("np-2", "np2@example.com", "")); err != nil {
		t.Fatalf("create second no-phone account: %v", err)
	}
	if err := s.Create(ctx, mk("np-3", "np3@example.com", "+201012345900")); err != nil {
		t.Fatalf("create phone account: %v", err)
	}
	if err := s.Create(ctx, mk("np-4", "np4@example.com", "+201012345900")); !errors.Is(err, ErrDuplicate) {
		t.Fatalf("duplicate phone create = %v, want ErrDuplicate", err)
	}
	if u, err := s.FindByPhone(ctx, ""); err != nil || u != nil {
		t.Fatalf("FindByPhone(\"\") = %v, %v; want nil, nil", u, err)
	}

	// Adding a first phone is not limited, even right after a name change.
	name := "Has Name"
	if err := s.UpdateProfileFields(ctx, "np-1", ProfileFields{Name: &name}, now); err != nil {
		t.Fatalf("name change: %v", err)
	}
	taken := "+201012345900"
	if err := s.UpdateProfileFields(ctx, "np-1", ProfileFields{Phone: &taken}, now); !errors.Is(err, ErrDuplicate) {
		t.Fatalf("add taken phone = %v, want ErrDuplicate", err)
	}
	first := "+201012345901"
	if err := s.UpdateProfileFields(ctx, "np-1", ProfileFields{Phone: &first}, now); err != nil {
		t.Fatalf("add first phone: %v", err)
	}
	got, err := s.FindByID(ctx, "np-1")
	if err != nil || got == nil || got.Phone != first || got.PhoneChangedAt.IsZero() {
		t.Fatalf("after add: %+v, %v", got, err)
	}
	// Changing it now is a change: the 30-day limit applies.
	second := "+201012345902"
	if err := s.UpdateProfileFields(ctx, "np-1", ProfileFields{Phone: &second}, now.Add(time.Hour)); !errors.Is(err, ErrChangeTooSoon) {
		t.Fatalf("change right after add = %v, want ErrChangeTooSoon", err)
	}
	if err := s.UpdateProfileFields(ctx, "np-1", ProfileFields{Phone: &second}, now.Add(ProfileChangeCooldown+time.Hour)); err != nil {
		t.Fatalf("change after 30 days: %v", err)
	}

	// A replacement write without a phone keeps none and stays unique-safe.
	u, err := s.FindByID(ctx, "np-3")
	if err != nil || u == nil {
		t.Fatalf("find np-3: %v", err)
	}
	u.Phone = ""
	if err := s.Update(ctx, u); err != nil {
		t.Fatalf("update to no phone: %v", err)
	}
	if got, _ := s.FindByID(ctx, "np-3"); got == nil || got.Phone != "" {
		t.Fatalf("np-3 after update: %+v", got)
	}
}

func TestMemoryStore_OptionalPhone(t *testing.T) {
	runOptionalPhoneSuite(t, NewMemoryStore())
}

func TestMongoStore_OptionalPhone(t *testing.T) {
	mongoURI, _ := requireDB(t)
	dbName := randomDBName("test_auth_optphone")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	ms, err := NewMongoStore(ctx, mongoURI, dbName)
	if err != nil {
		t.Fatalf("NewMongoStore: %v", err)
	}
	runOptionalPhoneSuite(t, ms)

	// No phone is stored as an absent field, never "": neither by Create nor
	// by the replacement Update above.
	for _, id := range []string{"np-2", "np-3"} {
		n, err := ms.coll.CountDocuments(ctx, bson.M{"_id": id, "phone": bson.M{"$exists": true}})
		if err != nil {
			t.Fatalf("count %s: %v", id, err)
		}
		if n != 0 {
			t.Fatalf("%s has a stored phone field, want absent", id)
		}
	}
}

// TestMongoStore_PhoneIndexMigrationWithNoPhoneAccounts boots a store that
// already has the previous phone_1 index (status filter without
// pending_deletion) and data, including a legacy verified row with phone ""
// and one with no phone field. The boot must succeed, the rebuilt index must
// cover only documents that have a phone, and further accounts without a
// phone must insert while a duplicate phone is still refused.
func TestMongoStore_PhoneIndexMigrationWithNoPhoneAccounts(t *testing.T) {
	mongoURI, _ := requireDB(t)
	dbName := randomDBName("test_auth_optphonemig")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	first, err := NewMongoStore(ctx, mongoURI, dbName)
	if err != nil {
		t.Fatalf("first NewMongoStore: %v", err)
	}
	coll := first.coll
	_ = coll.Indexes().DropOne(ctx, "uniq_phone_verified_v2")
	_ = coll.Indexes().DropOne(ctx, "phone_1")
	if _, err := coll.Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys: bson.D{{Key: "phone", Value: 1}},
		Options: options.Index().
			SetUnique(true).
			SetPartialFilterExpression(bson.M{
				"phone":          bson.M{"$type": "string", "$gt": ""},
				"status":         bson.M{"$in": []string{"active", "suspended"}},
				"email_verified": true,
			}),
	}); err != nil {
		t.Fatalf("recreate old phone index: %v", err)
	}
	now := time.Now().UTC()
	if _, err := coll.InsertMany(ctx, []any{
		bson.M{"_id": "mig-p", "email": "migp@example.com", "phone": "+201012345910", "status": "active", "email_verified": true, "created_at": now},
		bson.M{"_id": "mig-empty", "email": "migempty@example.com", "phone": "", "status": "active", "email_verified": true, "created_at": now},
		bson.M{"_id": "mig-absent", "email": "migabsent@example.com", "status": "active", "email_verified": true, "created_at": now},
	}); err != nil {
		t.Fatalf("seed rows: %v", err)
	}

	second, err := NewMongoStore(ctx, mongoURI, dbName)
	if err != nil {
		t.Fatalf("second NewMongoStore (migration boot): %v", err)
	}

	cursor, err := coll.Indexes().List(ctx)
	if err != nil {
		t.Fatalf("list indexes: %v", err)
	}
	var phoneFilter bson.M
	for cursor.Next(ctx) {
		var doc struct {
			Name                    string `bson:"name"`
			PartialFilterExpression bson.M `bson:"partialFilterExpression"`
		}
		if err := cursor.Decode(&doc); err != nil {
			t.Fatalf("decode index: %v", err)
		}
		if doc.Name == "uniq_phone_verified_v2" || doc.Name == "phone_1" {
			phoneFilter = doc.PartialFilterExpression
		}
	}
	_ = cursor.Close(ctx)
	if phoneFilter == nil || phoneFilter["phone"] == nil || phoneFilter["status"] == nil {
		t.Fatalf("phone_1 partial filter = %v, want phone and status clauses", phoneFilter)
	}

	mk := func(id, email, phone string) *models.User {
		return &models.User{ID: id, Email: email, PasswordHash: "h", Role: models.RoleUser, FullName: "Mig", Phone: phone, EmailVerified: true, Status: models.StatusActive, CreatedAt: now, UpdatedAt: now}
	}
	if err := second.Create(ctx, mk("mig-new1", "mignew1@example.com", "")); err != nil {
		t.Fatalf("no-phone account after migration: %v", err)
	}
	if err := second.Create(ctx, mk("mig-new2", "mignew2@example.com", "")); err != nil {
		t.Fatalf("second no-phone account after migration: %v", err)
	}
	if err := second.Create(ctx, mk("mig-dup", "migdup@example.com", "+201012345910")); !errors.Is(err, ErrDuplicate) {
		t.Fatalf("duplicate phone after migration = %v, want ErrDuplicate", err)
	}
	// Pending deletion now reserves the phone too (current status filter kept).
	if _, err := coll.InsertOne(ctx, bson.M{"_id": "mig-pd", "email": "migpd@example.com", "phone": "+201012345911", "status": "pending_deletion", "email_verified": true, "created_at": now}); err != nil {
		t.Fatalf("insert pending_deletion row: %v", err)
	}
	if err := second.Create(ctx, mk("mig-pd2", "migpd2@example.com", "+201012345911")); !errors.Is(err, ErrDuplicate) {
		t.Fatalf("phone held by pending_deletion = %v, want ErrDuplicate", err)
	}
}
