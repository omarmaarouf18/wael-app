// MongoStore is a MongoDB-backed Store for shared server/CLI persistence.
package store

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/omarmaarouf18/wael-app/auth-service/internal/models"
	"github.com/omarmaarouf18/wael-app/shared/infra/jwtutil"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// MongoStore persists users in the "users" collection, blocklist entries in "blocklist",
// admins in "admins", and audit logs in "admin_audit_log".
type MongoStore struct {
	coll           *mongo.Collection
	blockColl      *mongo.Collection
	adminColl      *mongo.Collection
	adminAuditColl *mongo.Collection
}

// NewMongoStore connects to MongoDB, ensures unique indexes on email, phone (partial for active/suspended),
// blocklist (kind, hash), and admins (token_hash), and returns a Store shared by the server and ops CLI tools.
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

	// Ensure unique index on admins (token_hash)
	adminColl := db.Collection("admins")
	_, err = adminColl.Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys:    bson.D{{Key: "token_hash", Value: 1}},
		Options: options.Index().SetUnique(true),
	})
	if err != nil {
		return nil, fmt.Errorf("store: ensure admin token_hash index: %w", err)
	}

	// Ensure indexes on admin_audit_log: (actor_id, created_at) and (target_type, target_id)
	adminAuditColl := db.Collection("admin_audit_log")
	_, err = adminAuditColl.Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys: bson.D{
			{Key: "actor_id", Value: 1},
			{Key: "created_at", Value: -1},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("store: ensure admin_audit_log actor_id_created_at index: %w", err)
	}

	_, err = adminAuditColl.Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys: bson.D{
			{Key: "target_type", Value: 1},
			{Key: "target_id", Value: 1},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("store: ensure admin_audit_log target_type_target_id index: %w", err)
	}

	return &MongoStore{
		coll:           coll,
		blockColl:      blockColl,
		adminColl:      adminColl,
		adminAuditColl: adminAuditColl,
	}, nil
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
	if from == "active|suspended" {
		filter = bson.M{
			"_id": userID,
			"$or": []bson.M{
				{"status": string(models.StatusActive)},
				{"status": models.StatusActive},
				{"status": ""},
				{"status": bson.M{"$exists": false}},
				{"status": string(models.StatusSuspended)},
				{"status": models.StatusSuspended},
			},
		}
	} else if from == string(models.StatusActive) || from == "" {
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

// CreateAdmin inserts a new admin document. Returns ErrDuplicate on duplicate token_hash or _id.
func (s *MongoStore) CreateAdmin(ctx context.Context, a *models.Admin) error {
	if a.CreatedAt.IsZero() {
		a.CreatedAt = time.Now().UTC()
	}
	_, err := s.adminColl.InsertOne(ctx, a)
	if err != nil {
		if mongo.IsDuplicateKeyError(err) {
			return ErrDuplicate
		}
		return fmt.Errorf("store: insert admin: %w", err)
	}
	return nil
}

// FindAdminByTokenHash retrieves an admin by SHA-256 token hash, or nil if absent.
func (s *MongoStore) FindAdminByTokenHash(ctx context.Context, tokenHash string) (*models.Admin, error) {
	var a models.Admin
	err := s.adminColl.FindOne(ctx, bson.M{"token_hash": tokenHash}).Decode(&a)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, nil
		}
		return nil, fmt.Errorf("store: find admin by hash: %w", err)
	}
	return &a, nil
}

// FindAdminByID retrieves an admin by _id, or nil if absent.
func (s *MongoStore) FindAdminByID(ctx context.Context, id string) (*models.Admin, error) {
	var a models.Admin
	err := s.adminColl.FindOne(ctx, bson.M{"_id": id}).Decode(&a)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, nil
		}
		return nil, fmt.Errorf("store: find admin by id: %w", err)
	}
	return &a, nil
}

// RevokeAdmin sets revoked_at for the admin with given id. Idempotent.
func (s *MongoStore) RevokeAdmin(ctx context.Context, id string, at time.Time) error {
	res, err := s.adminColl.UpdateOne(ctx, bson.M{"_id": id}, bson.M{"$set": bson.M{"revoked_at": at}})
	if err != nil {
		return fmt.Errorf("store: revoke admin: %w", err)
	}
	if res.MatchedCount == 0 {
		return ErrAdminNotFound
	}
	return nil
}

// ListUsers returns paginated users matching filter criteria.
func (s *MongoStore) ListUsers(ctx context.Context, filter UserFilter) ([]*models.User, int, error) {
	page := filter.Page
	if page < 1 {
		page = 1
	}
	limit := filter.Limit
	if limit <= 0 {
		limit = 20
	} else if limit > 100 {
		limit = 100
	}

	var andConditions []bson.M

	if filter.Status != "" {
		if filter.Status == string(models.StatusActive) {
			andConditions = append(andConditions, bson.M{
				"$or": []bson.M{
					{"status": string(models.StatusActive)},
					{"status": models.StatusActive},
					{"status": ""},
					{"status": bson.M{"$exists": false}},
				},
			})
		} else {
			andConditions = append(andConditions, bson.M{"status": filter.Status})
		}
	}

	if filter.Search != "" {
		escaped := regexp.QuoteMeta(filter.Search)
		searchOr := []bson.M{
			{"_id": filter.Search},
			{"full_name": bson.M{"$regex": escaped, "$options": "i"}},
			{"email": bson.M{"$regex": escaped, "$options": "i"}},
			{"phone": bson.M{"$regex": escaped, "$options": "i"}},
		}
		if filter.NormalizedPhone != "" && filter.NormalizedPhone != filter.Search {
			searchOr = append(searchOr, bson.M{"phone": filter.NormalizedPhone})
		}
		andConditions = append(andConditions, bson.M{"$or": searchOr})
	}

	query := bson.M{}
	if len(andConditions) == 1 {
		query = andConditions[0]
	} else if len(andConditions) > 1 {
		query = bson.M{"$and": andConditions}
	}

	total, err := s.coll.CountDocuments(ctx, query)
	if err != nil {
		return nil, 0, fmt.Errorf("store: count users: %w", err)
	}

	skip := int64((page - 1) * limit)
	opts := options.Find().
		SetSort(bson.D{{Key: "created_at", Value: -1}}).
		SetSkip(skip).
		SetLimit(int64(limit))

	cursor, err := s.coll.Find(ctx, query, opts)
	if err != nil {
		return nil, 0, fmt.Errorf("store: find users: %w", err)
	}
	defer cursor.Close(ctx)

	var users []*models.User
	if err := cursor.All(ctx, &users); err != nil {
		return nil, 0, fmt.Errorf("store: decode users: %w", err)
	}
	if users == nil {
		users = []*models.User{}
	}
	return users, int(total), nil
}

// CreateAuditLog records an admin action in admin_audit_log.
func (s *MongoStore) CreateAuditLog(ctx context.Context, entry *models.AuditLog) error {
	if entry.ID == "" {
		id, err := jwtutil.GenerateUUID()
		if err != nil {
			return err
		}
		entry.ID = id
	}
	if entry.CreatedAt.IsZero() {
		entry.CreatedAt = time.Now().UTC()
	}
	_, err := s.adminAuditColl.InsertOne(ctx, entry)
	if err != nil {
		return fmt.Errorf("store: insert audit log: %w", err)
	}
	return nil
}

// ListAuditLogs returns paginated audit log entries, newest first.
func (s *MongoStore) ListAuditLogs(ctx context.Context, page, limit int) ([]*models.AuditLog, int, error) {
	if page < 1 {
		page = 1
	}
	if limit <= 0 {
		limit = 20
	} else if limit > 100 {
		limit = 100
	}

	total, err := s.adminAuditColl.CountDocuments(ctx, bson.M{})
	if err != nil {
		return nil, 0, fmt.Errorf("store: count audit logs: %w", err)
	}

	skip := int64((page - 1) * limit)
	opts := options.Find().
		SetSort(bson.D{{Key: "created_at", Value: -1}}).
		SetSkip(skip).
		SetLimit(int64(limit))

	cursor, err := s.adminAuditColl.Find(ctx, bson.M{}, opts)
	if err != nil {
		return nil, 0, fmt.Errorf("store: find audit logs: %w", err)
	}
	defer cursor.Close(ctx)

	var logs []*models.AuditLog
	if err := cursor.All(ctx, &logs); err != nil {
		return nil, 0, fmt.Errorf("store: decode audit logs: %w", err)
	}
	if logs == nil {
		logs = []*models.AuditLog{}
	}
	return logs, int(total), nil
}
