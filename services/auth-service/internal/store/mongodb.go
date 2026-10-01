// MongoStore is a MongoDB-backed Store for shared server/CLI persistence.
package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/omarmaarouf18/wael-app/auth-service/internal/models"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// MongoStore persists users in the "users" collection and blocklist entries in "blocklist".
type MongoStore struct {
	coll      *mongo.Collection
	blockColl *mongo.Collection
}

// NewMongoStore connects to MongoDB, ensures unique indexes on email, phone (partial for active/suspended),
// and blocklist (kind, hash), and returns a Store shared by the server and ops CLI tools.
func NewMongoStore(ctx context.Context, mongoURI, dbName string) (*MongoStore, error) {
	if mongoURI == "" {
		return nil, fmt.Errorf("store: MONGO_URI is empty")
	}
	if dbName == "" {
		dbName = "auth_db"
	}
	client, err := mongo.Connect(options.Client().ApplyURI(mongoURI))
	if err != nil {
		return nil, fmt.Errorf("store: mongo connect: %w", err)
	}
	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := client.Ping(pingCtx, nil); err != nil {
		return nil, fmt.Errorf("store: mongo ping: %w", err)
	}
	db := client.Database(dbName)
	coll := db.Collection("users")

	// Ensure unique index on email
	_, err = coll.Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys:    bson.D{{Key: "email", Value: 1}},
		Options: options.Index().SetUnique(true),
	})
	if err != nil {
		return nil, fmt.Errorf("store: ensure email index: %w", err)
	}

	// Ensure partial unique index on phone (P-6)
	_, err = coll.Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys: bson.D{{Key: "phone", Value: 1}},
		Options: options.Index().
			SetUnique(true).
			SetPartialFilterExpression(bson.M{
				"phone":  bson.M{"$type": "string", "$gt": ""},
				"status": bson.M{"$in": []string{"active", "suspended"}},
			}),
	})
	if err != nil {
		return nil, fmt.Errorf("store: ensure phone index: %w", err)
	}

	// Ensure unique compound index on blocklist (kind, hash)
	blockColl := db.Collection("blocklist")
	_, err = blockColl.Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys: bson.D{
			{Key: "kind", Value: 1},
			{Key: "hash", Value: 1},
		},
		Options: options.Index().SetUnique(true),
	})
	if err != nil {
		return nil, fmt.Errorf("store: ensure blocklist index: %w", err)
	}

	return &MongoStore{coll: coll, blockColl: blockColl}, nil
}

// Create inserts a new user. Status defaults to "active" explicitly for new users.
func (s *MongoStore) Create(ctx context.Context, u *models.User) error {
	if u.CreatedAt.IsZero() {
		u.CreatedAt = time.Now().UTC()
	}
	u.UpdatedAt = time.Now().UTC()
	if u.Status == "" {
		u.Status = models.StatusActive
	}
	_, err := s.coll.InsertOne(ctx, u)
	if err != nil {
		if mongo.IsDuplicateKeyError(err) {
			return ErrDuplicate
		}
		return fmt.Errorf("store: insert: %w", err)
	}
	return nil
}

// FindByEmail returns the user with the given email, or nil when absent.
func (s *MongoStore) FindByEmail(ctx context.Context, email string) (*models.User, error) {
	var u models.User
	err := s.coll.FindOne(ctx, bson.M{"email": email}).Decode(&u)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, nil
		}
		return nil, fmt.Errorf("store: find by email: %w", err)
	}
	return &u, nil
}

// FindByID returns the user with the given id, or nil when absent.
func (s *MongoStore) FindByID(ctx context.Context, id string) (*models.User, error) {
	var u models.User
	err := s.coll.FindOne(ctx, bson.M{"_id": id}).Decode(&u)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, nil
		}
		return nil, fmt.Errorf("store: find by id: %w", err)
	}
	return &u, nil
}

// FindByPhone returns an active or suspended user with the given phone, or nil when absent.
func (s *MongoStore) FindByPhone(ctx context.Context, phone string) (*models.User, error) {
	if phone == "" {
		return nil, nil
	}
	filter := bson.M{
		"phone":  phone,
		"status": bson.M{"$in": []string{string(models.StatusActive), string(models.StatusSuspended)}},
	}
	var u models.User
	err := s.coll.FindOne(ctx, filter).Decode(&u)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, nil
		}
		return nil, fmt.Errorf("store: find by phone: %w", err)
	}
	return &u, nil
}

// Update replaces non-status fields of the stored user record via $set.
// Status fields are never modified by Update to prevent lost updates (finding P-1).
func (s *MongoStore) Update(ctx context.Context, u *models.User) error {
	now := time.Now().UTC()
	u.UpdatedAt = now
	update := bson.M{
		"$set": bson.M{
			"email":                  u.Email,
			"password_hash":          u.PasswordHash,
			"role":                   u.Role,
			"email_verified":         u.EmailVerified,
			"full_name":              u.FullName,
			"phone":                  u.Phone,
			"otp_hash":               u.OTPHash,
			"otp_expires_at":         u.OTPExpiresAt,
			"reset_token_hash":       u.ResetTokenHash,
			"reset_token_expires_at": u.ResetTokenExpiresAt,
			"updated_at":             now,
		},
	}
	res, err := s.coll.UpdateOne(ctx, bson.M{"_id": u.ID}, update)
	if err != nil {
		if mongo.IsDuplicateKeyError(err) {
			return ErrDuplicate
		}
		return fmt.Errorf("store: update: %w", err)
	}
	if res.MatchedCount == 0 {
		return ErrUserNotFound
	}
	return nil
}

// SetStatus performs an atomic compare-and-set of the user's status.
// Rejects any `to` status that is not active|suspended|deleted with ErrInvalidStatus.
// When from is "active", documents with a missing or empty status field also match.
// Returns ErrStatusConflict if the current status does not match `from`.
func (s *MongoStore) SetStatus(ctx context.Context, userID, from, to, reason string, at time.Time) error {
	switch models.UserStatus(to) {
	case models.StatusActive, models.StatusSuspended, models.StatusDeleted:
	default:
		return ErrInvalidStatus
	}

	if at.IsZero() {
		at = time.Now().UTC()
	}

	var filter bson.M
	if from == string(models.StatusActive) || from == "" {
		filter = bson.M{
			"_id": userID,
			"$or": []bson.M{
				{"status": string(models.StatusActive)},
				{"status": models.StatusActive},
				{"status": ""},
				{"status": bson.M{"$exists": false}},
			},
		}
	} else {
		filter = bson.M{
			"_id":    userID,
			"status": from,
		}
	}

	setFields := bson.M{
		"status":        models.UserStatus(to),
		"status_reason": reason,
		"updated_at":    at,
	}
	switch models.UserStatus(to) {
	case models.StatusSuspended:
		setFields["suspended_at"] = at
	case models.StatusActive:
		setFields["reactivated_at"] = at
	case models.StatusDeleted:
		setFields["deleted_at"] = at
	}

	update := bson.M{"$set": setFields}

	res, err := s.coll.UpdateOne(ctx, filter, update)
	if err != nil {
		if mongo.IsDuplicateKeyError(err) {
			return ErrDuplicate
		}
		return fmt.Errorf("store: set status: %w", err)
	}
	if res.MatchedCount == 0 {
		// Differentiate between non-existent user and status conflict
		existing, findErr := s.FindByID(ctx, userID)
		if findErr != nil {
			return fmt.Errorf("store: set status: %w", findErr)
		}
		if existing == nil {
			return ErrUserNotFound
		}
		return ErrStatusConflict
	}
	return nil
}

// Count returns the number of stored users.
func (s *MongoStore) Count(ctx context.Context) (int, error) {
	n, err := s.coll.CountDocuments(ctx, bson.M{})
	if err != nil {
		return 0, fmt.Errorf("store: count: %w", err)
	}
	return int(n), nil
}

// IsBlocked checks whether the given identity hash (kind: "email"|"phone") is blocklisted.
func (s *MongoStore) IsBlocked(ctx context.Context, kind, hash string) (bool, error) {
	if kind == "" || hash == "" {
		return false, nil
	}
	filter := bson.M{"kind": kind, "hash": hash}
	count, err := s.blockColl.CountDocuments(ctx, filter)
	if err != nil {
		return false, fmt.Errorf("store: check blocklist: %w", err)
	}
	return count > 0, nil
}

// AddToBlocklist records a blocked identity hash.
func (s *MongoStore) AddToBlocklist(ctx context.Context, kind, hash, reason string, at time.Time) error {
	if kind == "" || hash == "" {
		return errors.New("store: kind and hash are required")
	}
	if at.IsZero() {
		at = time.Now().UTC()
	}
	doc := bson.M{
		"kind":       kind,
		"hash":       hash,
		"reason":     reason,
		"created_at": at,
	}
	_, err := s.blockColl.InsertOne(ctx, doc)
	if err != nil {
		if mongo.IsDuplicateKeyError(err) {
			return nil
		}
		return fmt.Errorf("store: add to blocklist: %w", err)
	}
	return nil
}
