package store

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/omarmaarouf18/wael-app/notification-service/internal/models"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// MongoStore persists notifications in the "notifications" collection.
type MongoStore struct {
	coll      *mongo.Collection
	broadcast *mongo.Collection
	targets   *mongo.Collection
	audit     *mongo.Collection
}

// NewMongoStore connects to MongoDB and ensures a user_id index.
func NewMongoStore(ctx context.Context, mongoURI, dbName string) (*MongoStore, error) {
	if mongoURI == "" {
		return nil, fmt.Errorf("store: MONGO_URI is empty")
	}
	if dbName == "" {
		dbName = "notification_db"
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
	coll := db.Collection("notifications")
	_, err = coll.Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys: bson.D{{Key: "user_id", Value: 1}, {Key: "created_at", Value: -1}},
	})
	if err != nil {
		return nil, fmt.Errorf("store: ensure index: %w", err)
	}
	targets := db.Collection("broadcast_targets")
	_, err = targets.Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys:    bson.D{{Key: "broadcast_id", Value: 1}, {Key: "status", Value: 1}, {Key: "user_id", Value: 1}},
		Options: options.Index().SetName("idx_broadcast_targets_lookup"),
	})
	if err != nil {
		return nil, fmt.Errorf("store: ensure broadcast_targets index: %w", err)
	}
	audit := db.Collection("admin_audit_log")
	_, err = audit.Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys:    bson.D{{Key: "created_at", Value: -1}},
		Options: options.Index().SetName("idx_admin_audit_log_created"),
	})
	if err != nil {
		return nil, fmt.Errorf("store: ensure admin_audit_log index: %w", err)
	}
	return &MongoStore{
		coll:      coll,
		broadcast: db.Collection("broadcasts"),
		targets:   targets,
		audit:     audit,
	}, nil
}

// Create inserts a notification.
func (s *MongoStore) Create(ctx context.Context, n *models.Notification) error {
	if n.CreatedAt.IsZero() {
		n.CreatedAt = time.Now()
	}
	_, err := s.coll.InsertOne(ctx, n)
	if err != nil {
		if mongo.IsDuplicateKeyError(err) {
			return fmt.Errorf("store: notification already exists")
		}
		return fmt.Errorf("store: insert: %w", err)
	}
	return nil
}

// List returns the user's notifications newest-first, paginated (page from 1).
func (s *MongoStore) List(ctx context.Context, userID string, page, limit int) ([]*models.Notification, error) {
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 100 {
		limit = 20
	}
	opts := options.Find().
		SetSort(bson.D{{Key: "created_at", Value: -1}, {Key: "_id", Value: -1}}).
		SetSkip(int64((page - 1) * limit)).
		SetLimit(int64(limit))
	cur, err := s.coll.Find(ctx, bson.M{"user_id": userID}, opts)
	if err != nil {
		return nil, fmt.Errorf("store: list: %w", err)
	}
	defer func() { _ = cur.Close(ctx) }()
	var out []*models.Notification
	if err := cur.All(ctx, &out); err != nil {
		return nil, fmt.Errorf("store: decode: %w", err)
	}
	if out == nil {
		out = []*models.Notification{}
	}
	return out, nil
}

// MarkRead flags the user's notification as read.
func (s *MongoStore) MarkRead(ctx context.Context, userID, id string) error {
	res, err := s.coll.UpdateOne(ctx,
		bson.M{"_id": id, "user_id": userID},
		bson.M{"$set": bson.M{"read": true}},
	)
	if err != nil {
		return fmt.Errorf("store: mark read: %w", err)
	}
	if res.MatchedCount == 0 {
		return fmt.Errorf("store: notification not found")
	}
	return nil
}

// CreateBroadcast inserts a broadcast record.
func (s *MongoStore) CreateBroadcast(ctx context.Context, b *models.Broadcast) error {
	if b.CreatedAt.IsZero() {
		b.CreatedAt = time.Now()
	}
	_, err := s.broadcast.InsertOne(ctx, b)
	if err != nil {
		if mongo.IsDuplicateKeyError(err) {
			return fmt.Errorf("store: broadcast already exists")
		}
		return fmt.Errorf("store: insert broadcast: %w", err)
	}
	return nil
}

// GetBroadcast returns one broadcast by id, or an error when missing.
func (s *MongoStore) GetBroadcast(ctx context.Context, id string) (*models.Broadcast, error) {
	var b models.Broadcast
	if err := s.broadcast.FindOne(ctx, bson.M{"_id": id}).Decode(&b); err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, fmt.Errorf("store: broadcast not found")
		}
		return nil, fmt.Errorf("store: get broadcast: %w", err)
	}
	return &b, nil
}

// ListBroadcasts returns broadcasts newest-first, paginated (page from 1),
// plus the total count.
func (s *MongoStore) ListBroadcasts(ctx context.Context, page, limit int) ([]*models.Broadcast, int, error) {
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 100 {
		limit = 20
	}
	total64, err := s.broadcast.CountDocuments(ctx, bson.M{})
	if err != nil {
		return nil, 0, fmt.Errorf("store: count broadcasts: %w", err)
	}
	opts := options.Find().
		SetSort(bson.D{{Key: "created_at", Value: -1}, {Key: "_id", Value: -1}}).
		SetSkip(int64((page - 1) * limit)).
		SetLimit(int64(limit))
	cur, err := s.broadcast.Find(ctx, bson.M{}, opts)
	if err != nil {
		return nil, 0, fmt.Errorf("store: list broadcasts: %w", err)
	}
	defer func() { _ = cur.Close(ctx) }()
	var out []*models.Broadcast
	if err := cur.All(ctx, &out); err != nil {
		return nil, 0, fmt.Errorf("store: decode broadcasts: %w", err)
	}
	if out == nil {
		out = []*models.Broadcast{}
	}
	return out, int(total64), nil
}

// UpdateBroadcast replaces the stored broadcast record.
func (s *MongoStore) UpdateBroadcast(ctx context.Context, b *models.Broadcast) error {
	res, err := s.broadcast.ReplaceOne(ctx, bson.M{"_id": b.ID}, b)
	if err != nil {
		return fmt.Errorf("store: update broadcast: %w", err)
	}
	if res.MatchedCount == 0 {
		return fmt.Errorf("store: broadcast not found")
	}
	return nil
}

// UpsertBroadcastTarget inserts a pending target; an existing row is left
// untouched so resolution and retries stay idempotent. It reports whether
// the row was inserted.
func (s *MongoStore) UpsertBroadcastTarget(ctx context.Context, t *models.BroadcastTarget) (bool, error) {
	if t.UpdatedAt.IsZero() {
		t.UpdatedAt = time.Now()
	}
	res, err := s.targets.UpdateOne(ctx,
		bson.M{"_id": t.ID},
		bson.M{"$setOnInsert": bson.M{
			"broadcast_id": t.BroadcastID,
			"user_id":      t.UserID,
			"status":       t.Status,
			"attempts":     t.Attempts,
			"updated_at":   t.UpdatedAt,
		}},
		options.UpdateOne().SetUpsert(true),
	)
	if err != nil {
		return false, fmt.Errorf("store: upsert target: %w", err)
	}
	return res.UpsertedCount == 1, nil
}

// ListPendingTargets returns up to limit pending targets for a broadcast,
// ordered by user id for stable paging.
func (s *MongoStore) ListPendingTargets(ctx context.Context, broadcastID string, limit int) ([]*models.BroadcastTarget, error) {
	if limit < 1 || limit > 1000 {
		limit = 100
	}
	opts := options.Find().
		SetSort(bson.D{{Key: "user_id", Value: 1}}).
		SetLimit(int64(limit))
	cur, err := s.targets.Find(ctx,
		bson.M{"broadcast_id": broadcastID, "status": models.TargetPending}, opts)
	if err != nil {
		return nil, fmt.Errorf("store: list pending targets: %w", err)
	}
	defer func() { _ = cur.Close(ctx) }()
	var out []*models.BroadcastTarget
	if err := cur.All(ctx, &out); err != nil {
		return nil, fmt.Errorf("store: decode targets: %w", err)
	}
	if out == nil {
		out = []*models.BroadcastTarget{}
	}
	return out, nil
}

// MarkBroadcastTarget sets a target's status (sent or failed).
func (s *MongoStore) MarkBroadcastTarget(ctx context.Context, broadcastID, userID, status, lastErr string) error {
	res, err := s.targets.UpdateOne(ctx,
		bson.M{"_id": TargetID(broadcastID, userID)},
		bson.M{"$set": bson.M{
			"status":     status,
			"last_error": lastErr,
			"updated_at": time.Now(),
		}, "$inc": bson.M{"attempts": 1}},
	)
	if err != nil {
		return fmt.Errorf("store: mark target: %w", err)
	}
	if res.MatchedCount == 0 {
		return fmt.Errorf("store: broadcast target not found")
	}
	return nil
}

// ResetFailedTargets moves every failed target of a broadcast back to
// pending and reports how many were reset.
func (s *MongoStore) ResetFailedTargets(ctx context.Context, broadcastID string) (int, error) {
	res, err := s.targets.UpdateMany(ctx,
		bson.M{"broadcast_id": broadcastID, "status": models.TargetFailed},
		bson.M{"$set": bson.M{"status": models.TargetPending, "last_error": "", "updated_at": time.Now()}},
	)
	if err != nil {
		return 0, fmt.Errorf("store: reset failed targets: %w", err)
	}
	return int(res.ModifiedCount), nil
}

// CountTargets counts a broadcast's targets in one status.
func (s *MongoStore) CountTargets(ctx context.Context, broadcastID, status string) (int, error) {
	n, err := s.targets.CountDocuments(ctx, bson.M{"broadcast_id": broadcastID, "status": status})
	if err != nil {
		return 0, fmt.Errorf("store: count targets: %w", err)
	}
	return int(n), nil
}

// CreateAuditLog records an admin action in admin_audit_log. It assigns an
// id and timestamp when empty, mirroring auth-service semantics.
func (s *MongoStore) CreateAuditLog(ctx context.Context, entry *models.AuditLog) error {
	if entry.ID == "" {
		var b [16]byte
		if _, err := rand.Read(b[:]); err != nil {
			return fmt.Errorf("store: audit id: %w", err)
		}
		entry.ID = hex.EncodeToString(b[:])
	}
	if entry.CreatedAt.IsZero() {
		entry.CreatedAt = time.Now().UTC()
	}
	_, err := s.audit.InsertOne(ctx, entry)
	if err != nil {
		return fmt.Errorf("store: insert audit: %w", err)
	}
	return nil
}

// ListAuditLogs returns audit entries newest-first, paginated (page from 1),
// plus the total count.
func (s *MongoStore) ListAuditLogs(ctx context.Context, page, limit int) ([]*models.AuditLog, int, error) {
	if page < 1 {
		page = 1
	} else if page > 10000 {
		page = 10000
	}
	if limit <= 0 {
		limit = 20
	} else if limit > 100 {
		limit = 100
	}
	total64, err := s.audit.CountDocuments(ctx, bson.M{})
	if err != nil {
		return nil, 0, fmt.Errorf("store: count audit: %w", err)
	}
	opts := options.Find().
		SetSort(bson.D{{Key: "created_at", Value: -1}, {Key: "_id", Value: -1}}).
		SetSkip(int64((page - 1) * limit)).
		SetLimit(int64(limit))
	cur, err := s.audit.Find(ctx, bson.M{}, opts)
	if err != nil {
		return nil, 0, fmt.Errorf("store: list audit: %w", err)
	}
	defer func() { _ = cur.Close(ctx) }()
	var out []*models.AuditLog
	if err := cur.All(ctx, &out); err != nil {
		return nil, 0, fmt.Errorf("store: decode audit: %w", err)
	}
	if out == nil {
		out = []*models.AuditLog{}
	}
	return out, int(total64), nil
}
