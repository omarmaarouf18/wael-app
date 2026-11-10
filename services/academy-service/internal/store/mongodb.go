// MongoStore is a MongoDB-backed Store for academy-service persistence.
package store

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/omarmaarouf18/wael-app/academy-service/internal/models"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// MongoStore persists academy entities in MongoDB.
type MongoStore struct {
	client                         *mongo.Client
	db                             *mongo.Database
	settingsMu                     sync.RWMutex
	cachedSettings                 *models.AppSettings
	cachedSettingsAt               time.Time
	beforeAppOrderInsert           func(orderID string)
	beforeMigrateAppRequestsAttach func(ids []string)
}

// NewMongoStore connects to MongoDB, verifies the connection, ensures indexes,
// and returns an initialized *MongoStore.
func NewMongoStore(ctx context.Context, mongoURI, dbName string) (*MongoStore, error) {
	if mongoURI == "" {
		return nil, fmt.Errorf("store: MONGO_URI is empty")
	}
	if dbName == "" {
		dbName = "academy_db"
	}
	client, err := mongo.Connect(options.Client().ApplyURI(mongoURI))
	if err != nil {
		return nil, fmt.Errorf("store: mongo connect: %w", err)
	}
	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := client.Ping(pingCtx, nil); err != nil {
		_ = client.Disconnect(ctx)
		return nil, fmt.Errorf("store: mongo ping: %w", err)
	}

	db := client.Database(dbName)
	ms := &MongoStore{
		client: client,
		db:     db,
	}

	if err := ms.EnsureIndexes(ctx); err != nil {
		_ = client.Disconnect(ctx)
		return nil, fmt.Errorf("store: ensure indexes: %w", err)
	}

	if err := ms.MigrateSubjectPriceVisibility(ctx); err != nil {
		_ = client.Disconnect(ctx)
		return nil, fmt.Errorf("store: migrate subject price visibility: %w", err)
	}

	// Group existing pending app/mobile requests into one open app order per
	// student (2026-11-05 orders brief). Idempotent: requests that already
	// have an order_id are skipped on every later start.
	if err := ms.MigrateLegacyAppRequests(ctx); err != nil {
		_ = client.Disconnect(ctx)
		return nil, fmt.Errorf("store: migrate legacy app requests: %w", err)
	}

	// Wrap legacy pending web requests into a web order of one each, so the
	// grouped console view never loses them. Idempotent, history untouched.
	if err := ms.MigrateLegacyWebRequests(ctx); err != nil {
		_ = client.Disconnect(ctx)
		return nil, fmt.Errorf("store: migrate legacy web requests: %w", err)
	}

	return ms, nil
}

// EnsureIndexes creates required collection indexes.
func (s *MongoStore) EnsureIndexes(ctx context.Context) error {
	// Unique index on levels(key)
	_, err := s.db.Collection("levels").Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys:    bson.D{{Key: "key", Value: 1}},
		Options: options.Index().SetUnique(true).SetName("uniq_level_key"),
	})
	if err != nil {
		return fmt.Errorf("store: ensure levels key index: %w", err)
	}

	// Compound index on subjects(level_key, status)
	_, err = s.db.Collection("subjects").Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys: bson.D{
			{Key: "level_key", Value: 1},
			{Key: "status", Value: 1},
		},
		Options: options.Index().SetName("idx_subjects_level_status"),
	})
	if err != nil {
		return fmt.Errorf("store: ensure subjects level_status index: %w", err)
	}

	// Non-unique index on videos(subject_id, position)
	_, err = s.db.Collection("videos").Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys: bson.D{
			{Key: "subject_id", Value: 1},
			{Key: "position", Value: 1},
		},
		Options: options.Index().SetName("idx_videos_subject_position"),
	})
	if err != nil {
		return fmt.Errorf("store: ensure videos subject_position index: %w", err)
	}

	// Index on subject_files(subject_id)
	_, err = s.db.Collection("subject_files").Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys:    bson.D{{Key: "subject_id", Value: 1}},
		Options: options.Index().SetName("idx_subject_files_subject_id"),
	})
	if err != nil {
		return fmt.Errorf("store: ensure subject_files subject_id index: %w", err)
	}

	// Partial unique index on entitlements(user_id, subject_id) where active = true
	_, err = s.db.Collection("entitlements").Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys: bson.D{
			{Key: "user_id", Value: 1},
			{Key: "subject_id", Value: 1},
		},
		Options: options.Index().
			SetUnique(true).
			SetPartialFilterExpression(bson.M{"active": true}).
			SetName("uniq_active_user_subject_entitlement"),
	})
	if err != nil {
		return fmt.Errorf("store: ensure entitlements active unique index: %w", err)
	}

	// Non-unique compound index on entitlements(user_id, subject_id, expires_at)
	_, err = s.db.Collection("entitlements").Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys: bson.D{
			{Key: "user_id", Value: 1},
			{Key: "subject_id", Value: 1},
			{Key: "expires_at", Value: 1},
		},
		Options: options.Index().SetName("idx_entitlements_user_subject_expires"),
	})
	if err != nil {
		return fmt.Errorf("store: ensure entitlements user_subject_expires index: %w", err)
	}

	// Index on entitlements(user_id)
	_, err = s.db.Collection("entitlements").Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys:    bson.D{{Key: "user_id", Value: 1}},
		Options: options.Index().SetName("idx_entitlements_user_id"),
	})
	if err != nil {
		return fmt.Errorf("store: ensure entitlements user_id index: %w", err)
	}

	// Partial unique index on purchase_requests(user_id, subject_id) where status = pending (R5)
	_, err = s.db.Collection("purchase_requests").Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys: bson.D{
			{Key: "user_id", Value: 1},
			{Key: "subject_id", Value: 1},
		},
		Options: options.Index().
			SetUnique(true).
			SetPartialFilterExpression(bson.M{"status": models.RequestStatusPending}).
			SetName("uniq_pending_user_subject_request"),
	})
	if err != nil {
		return fmt.Errorf("store: ensure purchase_requests pending unique index: %w", err)
	}

	// Index on purchase_requests(status)
	_, err = s.db.Collection("purchase_requests").Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys:    bson.D{{Key: "status", Value: 1}},
		Options: options.Index().SetName("idx_purchase_requests_status"),
	})
	if err != nil {
		return fmt.Errorf("store: ensure purchase_requests status index: %w", err)
	}

	// Index on purchase_requests(normalized_reference) for reference lookups.
	// In the orders architecture (2026-11-05), orders enforce reference uniqueness
	// via uniq_orders_normalized_reference. Multi-subject cart orders share the
	// reference across items, so purchase_requests index must not be unique.
	// We drop the legacy unique index if present.
	if err := s.db.Collection("purchase_requests").Indexes().DropOne(ctx, "uniq_purchase_requests_normalized_reference"); err == nil {
		log.Printf("[ACADEMY] dropped legacy unique index uniq_purchase_requests_normalized_reference on purchase_requests")
	} else if !isIndexNotFound(err) {
		return fmt.Errorf("store: drop legacy purchase_requests index: %w", err)
	}
	_, err = s.db.Collection("purchase_requests").Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys: bson.D{
			{Key: "normalized_reference", Value: 1},
		},
		Options: options.Index().SetName("idx_purchase_requests_normalized_reference"),
	})
	if err != nil {
		return fmt.Errorf("store: ensure purchase_requests normalized_reference index: %w", err)
	}

	// Orders (2026-11-05 orders brief, ADR-0014 W4/C2).
	// One open app order per student: unique partial on user_id where source=app
	// and status=open. This is what makes parallel app requests converge on one
	// open order (the loser re-reads the winner's order).
	_, err = s.db.Collection("orders").Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys: bson.D{{Key: "user_id", Value: 1}},
		Options: options.Index().
			SetUnique(true).
			SetPartialFilterExpression(bson.M{"source": models.OrderSourceApp, "status": models.OrderStatusOpen}).
			SetName("uniq_open_app_order_user"),
	})
	if err != nil {
		return fmt.Errorf("store: ensure orders open app unique index: %w", err)
	}

	// Unique partial index on orders(normalized_reference) where non-empty.
	_, err = s.db.Collection("orders").Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys: bson.D{{Key: "normalized_reference", Value: 1}},
		Options: options.Index().
			SetUnique(true).
			SetPartialFilterExpression(bson.M{"normalized_reference": bson.M{"$type": "string", "$gt": ""}}).
			SetName("uniq_orders_normalized_reference"),
	})
	if err != nil {
		return fmt.Errorf("store: ensure orders normalized_reference unique index: %w", err)
	}

	// Index on orders(user_id, created_at) for the student order list.
	_, err = s.db.Collection("orders").Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys: bson.D{
			{Key: "user_id", Value: 1},
			{Key: "created_at", Value: -1},
		},
		Options: options.Index().SetName("idx_orders_user_created"),
	})
	if err != nil {
		return fmt.Errorf("store: ensure orders user_created index: %w", err)
	}

	// Index on purchase_requests(order_id) for order item reads.
	_, err = s.db.Collection("purchase_requests").Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys:    bson.D{{Key: "order_id", Value: 1}},
		Options: options.Index().SetName("idx_purchase_requests_order_id"),
	})
	if err != nil {
		return fmt.Errorf("store: ensure purchase_requests order_id index: %w", err)
	}

	// Indexes on video_plays: (user_id, played_at) and (video_id, played_at)
	_, err = s.db.Collection("video_plays").Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys: bson.D{
			{Key: "user_id", Value: 1},
			{Key: "played_at", Value: 1},
		},
		Options: options.Index().SetName("idx_video_plays_user_played"),
	})
	if err != nil {
		return fmt.Errorf("store: ensure video_plays user_played index: %w", err)
	}

	_, err = s.db.Collection("video_plays").Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys: bson.D{
			{Key: "video_id", Value: 1},
			{Key: "played_at", Value: 1},
		},
		Options: options.Index().SetName("idx_video_plays_video_played"),
	})
	if err != nil {
		return fmt.Errorf("store: ensure video_plays video_played index: %w", err)
	}

	// Indexes on admin_audit_log (Phase 4.1, per-service ownership per
	// ADR-0008 Section 9): (actor_id, created_at) and (target_type, target_id).
	_, err = s.db.Collection("admin_audit_log").Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys: bson.D{
			{Key: "actor_id", Value: 1},
			{Key: "created_at", Value: -1},
		},
		Options: options.Index().SetName("idx_admin_audit_log_actor_created"),
	})
	if err != nil {
		return fmt.Errorf("store: ensure admin_audit_log actor_created index: %w", err)
	}

	_, err = s.db.Collection("admin_audit_log").Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys: bson.D{
			{Key: "target_type", Value: 1},
			{Key: "target_id", Value: 1},
		},
		Options: options.Index().SetName("idx_admin_audit_log_target"),
	})
	if err != nil {
		return fmt.Errorf("store: ensure admin_audit_log target index: %w", err)
	}

	// Indexes on payment_records (Phase 4.5, decision 19):
	// unique on entitlement_id, plus (subject_id, recorded_at) and (user_id, recorded_at).
	_, err = s.db.Collection("payment_records").Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys:    bson.D{{Key: "entitlement_id", Value: 1}},
		Options: options.Index().SetUnique(true).SetName("uniq_payment_records_entitlement_id"),
	})
	if err != nil {
		return fmt.Errorf("store: ensure payment_records entitlement_id unique index: %w", err)
	}

	_, err = s.db.Collection("payment_records").Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys: bson.D{
			{Key: "subject_id", Value: 1},
			{Key: "recorded_at", Value: -1},
		},
		Options: options.Index().SetName("idx_payment_records_subject_recorded"),
	})
	if err != nil {
		return fmt.Errorf("store: ensure payment_records subject_recorded index: %w", err)
	}

	_, err = s.db.Collection("payment_records").Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys: bson.D{
			{Key: "user_id", Value: 1},
			{Key: "recorded_at", Value: -1},
		},
		Options: options.Index().SetName("idx_payment_records_user_recorded"),
	})
	if err != nil {
		return fmt.Errorf("store: ensure payment_records user_recorded index: %w", err)
	}

	return nil
}

// SeedLevels idempotently initializes the 5 fixed academic levels using $setOnInsert.
// Levels seeded before the published flag existed are backfilled to
// published=true (explicitly unpublished levels are left alone).
func (s *MongoStore) SeedLevels(ctx context.Context) error {
	for _, lvl := range models.SeededLevels {
		filter := bson.M{"key": lvl.Key}
		update := bson.M{
			"$setOnInsert": bson.M{
				"key":        lvl.Key,
				"study_type": lvl.StudyType,
				"title_ar":   lvl.TitleAr,
				"title_en":   lvl.TitleEn,
				"position":   lvl.Position,
				"published":  true,
			},
		}
		opts := options.UpdateOne().SetUpsert(true)
		if _, err := s.db.Collection("levels").UpdateOne(ctx, filter, update, opts); err != nil {
			return fmt.Errorf("store: seed level %s: %w", lvl.Key, err)
		}
		backfill := bson.M{"key": lvl.Key, "published": bson.M{"$exists": false}}
		if _, err := s.db.Collection("levels").UpdateOne(ctx, backfill, bson.M{"$set": bson.M{"published": true}}); err != nil {
			return fmt.Errorf("store: backfill level %s published: %w", lvl.Key, err)
		}
	}
	return nil
}

// ListLevels returns levels sorted by position. If onlyWithPublished is true,
// levels with no published subjects are omitted.
func (s *MongoStore) ListLevels(ctx context.Context, onlyWithPublished bool) ([]*models.Level, error) {
	var filter bson.M
	if onlyWithPublished {
		dr := s.db.Collection("subjects").Distinct(ctx, "level_key", bson.M{"status": models.StatusPublished})
		var distinctKeys []string
		if err := dr.Decode(&distinctKeys); err != nil {
			if !errors.Is(err, mongo.ErrNoDocuments) {
				return nil, fmt.Errorf("store: distinct published subjects: %w", err)
			}
			distinctKeys = []string{}
		}
		filter = bson.M{"key": bson.M{"$in": distinctKeys}}
	} else {
		filter = bson.M{}
	}

	opts := options.Find().SetSort(bson.D{{Key: "position", Value: 1}, {Key: "key", Value: 1}})
	cursor, err := s.db.Collection("levels").Find(ctx, filter, opts)
	if err != nil {
		return nil, fmt.Errorf("store: find levels: %w", err)
	}
	defer cursor.Close(ctx)

	var result []*models.Level
	if err := cursor.All(ctx, &result); err != nil {
		return nil, fmt.Errorf("store: decode levels: %w", err)
	}
	if result == nil {
		result = []*models.Level{}
	}
	return result, nil
}

// GetLevelByKey retrieves a single level by its key, or nil if missing.
func (s *MongoStore) GetLevelByKey(ctx context.Context, key string) (*models.Level, error) {
	var lvl models.Level
	err := s.db.Collection("levels").FindOne(ctx, bson.M{"key": key}).Decode(&lvl)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, nil
		}
		return nil, fmt.Errorf("store: find level by key: %w", err)
	}
	return &lvl, nil
}

// CreateLevel inserts a new level. Duplicate keys return ErrDuplicate.
func (s *MongoStore) CreateLevel(ctx context.Context, lvl *models.Level) error {
	_, err := s.db.Collection("levels").InsertOne(ctx, lvl)
	if err != nil {
		if mongo.IsDuplicateKeyError(err) {
			return ErrDuplicate
		}
		return fmt.Errorf("store: insert level: %w", err)
	}
	return nil
}

// UpdateLevel replaces an existing level matched by key. Missing keys return
// ErrNotFound.
func (s *MongoStore) UpdateLevel(ctx context.Context, lvl *models.Level) error {
	res, err := s.db.Collection("levels").ReplaceOne(ctx, bson.M{"key": lvl.Key}, lvl)
	if err != nil {
		return fmt.Errorf("store: update level: %w", err)
	}
	if res.MatchedCount == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteLevel removes a level by key. Missing keys return ErrNotFound.
func (s *MongoStore) DeleteLevel(ctx context.Context, key string) error {
	res, err := s.db.Collection("levels").DeleteOne(ctx, bson.M{"key": key})
	if err != nil {
		return fmt.Errorf("store: delete level: %w", err)
	}
	if res.DeletedCount == 0 {
		return ErrNotFound
	}
	return nil
}

// CountSubjectsByLevel counts subjects in a level, optionally filtered by
// status (empty status counts all).
func (s *MongoStore) CountSubjectsByLevel(ctx context.Context, levelKey, status string) (int, error) {
	filter := bson.M{"level_key": levelKey}
	if status != "" {
		filter["status"] = status
	}
	n, err := s.db.Collection("subjects").CountDocuments(ctx, filter)
	if err != nil {
		return 0, fmt.Errorf("store: count subjects by level: %w", err)
	}
	return int(n), nil
}

// CreateSubject inserts a new subject into the subjects collection.
func (s *MongoStore) CreateSubject(ctx context.Context, subj *models.Subject) error {
	if subj.PriceVisibility == "" {
		subj.PriceVisibility = models.PriceVisibilityFollowGlobal
	}
	_, err := s.db.Collection("subjects").InsertOne(ctx, subj)
	if err != nil {
		if mongo.IsDuplicateKeyError(err) {
			return ErrDuplicate
		}
		return fmt.Errorf("store: insert subject: %w", err)
	}
	return nil
}

// MigrateSubjectPriceVisibility updates all existing subjects that lack a price_visibility
// field (or have it set to empty/null) to have price_visibility = "follow_global".
func (s *MongoStore) MigrateSubjectPriceVisibility(ctx context.Context) error {
	filter := bson.M{
		"$or": []bson.M{
			{"price_visibility": bson.M{"$exists": false}},
			{"price_visibility": ""},
			{"price_visibility": nil},
		},
	}
	update := bson.M{
		"$set": bson.M{
			"price_visibility": models.PriceVisibilityFollowGlobal,
		},
	}
	_, err := s.db.Collection("subjects").UpdateMany(ctx, filter, update)
	if err != nil {
		return fmt.Errorf("store: migrate subject price visibility: %w", err)
	}
	return nil
}

// UpdateSubject updates an existing subject in MongoDB.
func (s *MongoStore) UpdateSubject(ctx context.Context, subj *models.Subject) error {
	res, err := s.db.Collection("subjects").ReplaceOne(ctx, bson.M{"_id": subj.ID}, subj)
	if err != nil {
		return fmt.Errorf("store: update subject: %w", err)
	}
	if res.MatchedCount == 0 {
		return ErrNotFound
	}
	return nil
}

// ListSubjects queries subjects based on filter criteria.
func (s *MongoStore) ListSubjects(ctx context.Context, filter SubjectFilter) ([]*models.Subject, int, error) {
	filterDoc := bson.M{}
	if filter.LevelKey != "" {
		filterDoc["level_key"] = filter.LevelKey
	}
	if filter.Term != "" {
		filterDoc["term"] = filter.Term
	}
	if filter.Status != "" {
		filterDoc["status"] = filter.Status
	}

	total, err := s.db.Collection("subjects").CountDocuments(ctx, filterDoc)
	if err != nil {
		return nil, 0, fmt.Errorf("store: count subjects: %w", err)
	}

	const maxPage = 10000
	page := filter.Page
	if page < 1 {
		page = 1
	} else if page > maxPage {
		page = maxPage
	}
	limit := filter.Limit
	if limit <= 0 {
		limit = 20
	} else if limit > 100 {
		limit = 100
	}

	var skip int64
	if page > 1 {
		skip = int64(page-1) * int64(limit)
	}
	if skip < 0 {
		skip = 0
	}
	opts := options.Find().
		SetSort(bson.D{{Key: "order", Value: 1}, {Key: "created_at", Value: 1}}).
		SetSkip(skip).
		SetLimit(int64(limit))

	cursor, err := s.db.Collection("subjects").Find(ctx, filterDoc, opts)
	if err != nil {
		return nil, 0, fmt.Errorf("store: find subjects: %w", err)
	}
	defer cursor.Close(ctx)

	var result []*models.Subject
	if err := cursor.All(ctx, &result); err != nil {
		return nil, 0, fmt.Errorf("store: decode subjects: %w", err)
	}
	if result == nil {
		result = []*models.Subject{}
	}
	return result, int(total), nil
}

// GetSubjectByID retrieves a single subject by its ID.
func (s *MongoStore) GetSubjectByID(ctx context.Context, id string) (*models.Subject, error) {
	var subj models.Subject
	err := s.db.Collection("subjects").FindOne(ctx, bson.M{"_id": id}).Decode(&subj)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, nil
		}
		return nil, fmt.Errorf("store: find subject by id: %w", err)
	}
	return &subj, nil
}

// GetSubjectCounts returns the counts of published videos, books, and notes.
func (s *MongoStore) GetSubjectCounts(ctx context.Context, subjectID string) (models.SubjectCountsDTO, error) {
	var counts models.SubjectCountsDTO

	vCount, err := s.db.Collection("videos").CountDocuments(ctx, bson.M{"subject_id": subjectID, "published": true, "deleted": bson.M{"$ne": true}})
	if err != nil {
		return counts, fmt.Errorf("store: count videos: %w", err)
	}
	counts.Videos = int(vCount)

	bCount, err := s.db.Collection("subject_files").CountDocuments(ctx, bson.M{"subject_id": subjectID, "kind": "book"})
	if err != nil {
		return counts, fmt.Errorf("store: count books: %w", err)
	}
	counts.Books = int(bCount)

	nCount, err := s.db.Collection("subject_files").CountDocuments(ctx, bson.M{"subject_id": subjectID, "kind": "note"})
	if err != nil {
		return counts, fmt.Errorf("store: count notes: %w", err)
	}
	counts.Notes = int(nCount)

	return counts, nil
}

// CreateVideo inserts a video into MongoDB.
func (s *MongoStore) CreateVideo(ctx context.Context, v *models.Video) error {
	_, err := s.db.Collection("videos").InsertOne(ctx, v)
	if err != nil {
		if mongo.IsDuplicateKeyError(err) {
			return ErrDuplicate
		}
		return fmt.Errorf("store: insert video: %w", err)
	}
	return nil
}

// GetVideoByID retrieves a single video by its ID.
func (s *MongoStore) GetVideoByID(ctx context.Context, id string) (*models.Video, error) {
	var v models.Video
	err := s.db.Collection("videos").FindOne(ctx, bson.M{"_id": id}).Decode(&v)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, nil
		}
		return nil, fmt.Errorf("store: find video by id: %w", err)
	}
	return &v, nil
}

// UpdateVideo replaces an existing video. Missing ids return ErrNotFound.
func (s *MongoStore) UpdateVideo(ctx context.Context, v *models.Video) error {
	res, err := s.db.Collection("videos").ReplaceOne(ctx, bson.M{"_id": v.ID}, v)
	if err != nil {
		return fmt.Errorf("store: update video: %w", err)
	}
	if res.MatchedCount == 0 {
		return ErrNotFound
	}
	return nil
}

// ListVideosBySubject returns non-deleted videos for a subject sorted by position.
func (s *MongoStore) ListVideosBySubject(ctx context.Context, subjectID string, onlyPublished bool) ([]*models.Video, error) {
	filter := bson.M{"subject_id": subjectID, "deleted": bson.M{"$ne": true}}
	if onlyPublished {
		filter["published"] = true
	}

	opts := options.Find().SetSort(bson.D{{Key: "position", Value: 1}})
	cursor, err := s.db.Collection("videos").Find(ctx, filter, opts)
	if err != nil {
		return nil, fmt.Errorf("store: find videos: %w", err)
	}
	defer cursor.Close(ctx)

	var result []*models.Video
	if err := cursor.All(ctx, &result); err != nil {
		return nil, fmt.Errorf("store: decode videos: %w", err)
	}
	if result == nil {
		result = []*models.Video{}
	}
	return result, nil
}

// CreateFile inserts a file record into MongoDB.
func (s *MongoStore) CreateFile(ctx context.Context, f *models.SubjectFile) error {
	_, err := s.db.Collection("subject_files").InsertOne(ctx, f)
	if err != nil {
		if mongo.IsDuplicateKeyError(err) {
			return ErrDuplicate
		}
		return fmt.Errorf("store: insert file: %w", err)
	}
	return nil
}

// ListFilesBySubject returns files for a subject sorted by creation date.
func (s *MongoStore) ListFilesBySubject(ctx context.Context, subjectID string) ([]*models.SubjectFile, error) {
	opts := options.Find().SetSort(bson.D{{Key: "created_at", Value: 1}})
	cursor, err := s.db.Collection("subject_files").Find(ctx, bson.M{"subject_id": subjectID}, opts)
	if err != nil {
		return nil, fmt.Errorf("store: find files: %w", err)
	}
	defer cursor.Close(ctx)

	var result []*models.SubjectFile
	if err := cursor.All(ctx, &result); err != nil {
		return nil, fmt.Errorf("store: decode files: %w", err)
	}
	if result == nil {
		result = []*models.SubjectFile{}
	}
	return result, nil
}

// GetFile returns the file when it belongs to subjectID; otherwise (nil, nil).
func (s *MongoStore) GetFile(ctx context.Context, subjectID, fileID string) (*models.SubjectFile, error) {
	var f models.SubjectFile
	err := s.db.Collection("subject_files").FindOne(ctx, bson.M{"_id": fileID, "subject_id": subjectID}).Decode(&f)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, nil
		}
		return nil, fmt.Errorf("store: find file: %w", err)
	}
	return &f, nil
}

// DeleteFile removes the file row when it belongs to subjectID; no match is ErrNotFound.
func (s *MongoStore) DeleteFile(ctx context.Context, subjectID, fileID string) error {
	res, err := s.db.Collection("subject_files").DeleteOne(ctx, bson.M{"_id": fileID, "subject_id": subjectID})
	if err != nil {
		return fmt.Errorf("store: delete file: %w", err)
	}
	if res.DeletedCount == 0 {
		return ErrNotFound
	}
	return nil
}

// subjectReferenced reports whether any entitlement, purchase request or
// payment record (any state) references the subject.
func (s *MongoStore) subjectReferenced(ctx context.Context, id string) (bool, error) {
	for _, coll := range []string{"entitlements", "purchase_requests", "payment_records"} {
		n, err := s.db.Collection(coll).CountDocuments(ctx, bson.M{"subject_id": id}, options.Count().SetLimit(1))
		if err != nil {
			return false, fmt.Errorf("store: count %s for subject: %w", coll, err)
		}
		if n > 0 {
			return true, nil
		}
	}
	return false, nil
}

// DeleteDraftSubject removes a draft subject nobody references, then its
// videos and file rows. MongoDB runs standalone here (no transactions), so
// the subject row goes first with a draft-only filter (a concurrent publish
// makes it match nothing), and the references are checked again afterwards:
// a reference that appeared in between puts the subject back and the call
// fails with ErrSubjectHasStudents. Children are removed only after that;
// a failure there is logged, not returned.
func (s *MongoStore) DeleteDraftSubject(ctx context.Context, id string) ([]*models.SubjectFile, error) {
	subj, err := s.GetSubjectByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if subj == nil {
		return nil, ErrNotFound
	}
	if subj.Status == models.StatusPublished {
		return nil, ErrSubjectPublished
	}
	referenced, err := s.subjectReferenced(ctx, id)
	if err != nil {
		return nil, err
	}
	if referenced {
		return nil, ErrSubjectHasStudents
	}

	files, err := s.ListFilesBySubject(ctx, id)
	if err != nil {
		return nil, err
	}

	res, err := s.db.Collection("subjects").DeleteOne(ctx, bson.M{"_id": id, "status": bson.M{"$ne": models.StatusPublished}})
	if err != nil {
		return nil, fmt.Errorf("store: delete subject: %w", err)
	}
	if res.DeletedCount == 0 {
		again, getErr := s.GetSubjectByID(ctx, id)
		if getErr != nil {
			return nil, getErr
		}
		if again == nil {
			return nil, ErrNotFound
		}
		return nil, ErrSubjectPublished
	}

	referenced, err = s.subjectReferenced(ctx, id)
	if err != nil || referenced {
		if _, insErr := s.db.Collection("subjects").InsertOne(ctx, subj); insErr != nil {
			return nil, fmt.Errorf("store: restore subject after reference race: %w", insErr)
		}
		if err != nil {
			return nil, err
		}
		return nil, ErrSubjectHasStudents
	}

	// The subject is gone; children left behind by a failure here are
	// unreachable (every read goes through the subject), so the failure is
	// logged and the delete still succeeds.
	if _, err := s.db.Collection("videos").DeleteMany(ctx, bson.M{"subject_id": id}); err != nil {
		log.Printf("[ERROR] delete videos of deleted subject %s: %v", id, err)
	}
	if _, err := s.db.Collection("subject_files").DeleteMany(ctx, bson.M{"subject_id": id}); err != nil {
		log.Printf("[ERROR] delete file rows of deleted subject %s: %v", id, err)
	}
	return files, nil
}

// Ping verifies MongoDB connectivity.
func (s *MongoStore) Ping(ctx context.Context) error {
	return s.client.Ping(ctx, nil)
}

// Close disconnects from MongoDB.
func (s *MongoStore) Close(ctx context.Context) error {
	return s.client.Disconnect(ctx)
}

// Database returns the underlying mongo.Database handle (for testing/cleanup).
func (s *MongoStore) Database() *mongo.Database {
	return s.db
}

// Grant grants an entitlement to a user for a subject.
// Enforces:
// 1. Subject must exist and not be expired (D20: ErrSubjectExpired).
// 2. Copies expires_at from subject's access_expires_at (D21).
// 3. Deactivates any expired entitlements for this (user_id, subject_id).
// 4. Guarantees at most ONE unexpired entitlement per (user_id, subject_id).
func (s *MongoStore) Grant(ctx context.Context, e *models.Entitlement) error {
	subj, err := s.GetSubjectByID(ctx, e.SubjectID)
	if err != nil {
		return fmt.Errorf("store: get subject: %w", err)
	}
	if subj == nil {
		return ErrNotFound
	}

	now := time.Now()
	if !subj.AccessExpiresAt.After(now) {
		return ErrSubjectExpired
	}

	e.ExpiresAt = subj.AccessExpiresAt
	if e.GrantedAt.IsZero() {
		e.GrantedAt = now
	}
	if e.ID == "" {
		e.ID = generateID()
	}
	if e.Source == "" {
		e.Source = models.EntitlementSourceAdminGrant
	}

	// Deactivate any expired entitlements for this user and subject
	filterExpired := bson.M{
		"user_id":    e.UserID,
		"subject_id": e.SubjectID,
		"active":     true,
		"expires_at": bson.M{"$lte": now},
	}
	_, err = s.db.Collection("entitlements").UpdateMany(ctx, filterExpired, bson.M{
		"$set": bson.M{"active": false},
	})
	if err != nil {
		return fmt.Errorf("store: deactivate expired entitlements: %w", err)
	}

	e.Active = true
	_, err = s.db.Collection("entitlements").InsertOne(ctx, e)
	if err != nil {
		if mongo.IsDuplicateKeyError(err) {
			return ErrDuplicate
		}
		return fmt.Errorf("store: insert entitlement: %w", err)
	}
	return nil
}

// HasActiveEntitlement reports whether user owns the subject with expires_at > now.
func (s *MongoStore) HasActiveEntitlement(ctx context.Context, userID, subjectID string) (bool, error) {
	now := time.Now()
	filter := bson.M{
		"user_id":    userID,
		"subject_id": subjectID,
		"active":     true,
		"expires_at": bson.M{"$gt": now},
		"$or": []bson.M{
			{"revoked_at": nil},
			{"revoked_at": bson.M{"$exists": false}},
		},
	}
	err := s.db.Collection("entitlements").FindOne(ctx, filter, options.FindOne().SetProjection(bson.M{"_id": 1})).Err()
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return false, nil
		}
		return false, fmt.Errorf("store: find active entitlement: %w", err)
	}
	return true, nil
}

// GetActiveEntitlement returns the active unexpired entitlement for (userID, subjectID), or nil if none.
func (s *MongoStore) GetActiveEntitlement(ctx context.Context, userID, subjectID string) (*models.Entitlement, error) {
	now := time.Now()
	filter := bson.M{
		"user_id":    userID,
		"subject_id": subjectID,
		"active":     true,
		"expires_at": bson.M{"$gt": now},
		"$or": []bson.M{
			{"revoked_at": nil},
			{"revoked_at": bson.M{"$exists": false}},
		},
	}
	var ent models.Entitlement
	err := s.db.Collection("entitlements").FindOne(ctx, filter).Decode(&ent)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, nil
		}
		return nil, fmt.Errorf("store: find active entitlement: %w", err)
	}
	return &ent, nil
}

// GetActiveEntitlements returns a map of subjectID -> active unexpired entitlement for the user.
func (s *MongoStore) GetActiveEntitlements(ctx context.Context, userID string) (map[string]*models.Entitlement, error) {
	now := time.Now()
	filter := bson.M{
		"user_id":    userID,
		"active":     true,
		"expires_at": bson.M{"$gt": now},
		"$or": []bson.M{
			{"revoked_at": nil},
			{"revoked_at": bson.M{"$exists": false}},
		},
	}
	cursor, err := s.db.Collection("entitlements").Find(ctx, filter)
	if err != nil {
		return nil, fmt.Errorf("store: find active entitlements: %w", err)
	}
	defer cursor.Close(ctx)

	var docs []models.Entitlement
	if err := cursor.All(ctx, &docs); err != nil {
		return nil, fmt.Errorf("store: decode active entitlements: %w", err)
	}

	result := make(map[string]*models.Entitlement, len(docs))
	for _, d := range docs {
		cp := d
		result[d.SubjectID] = &cp
	}
	return result, nil
}

// GetActiveEntitlementSubjectIDs returns a set of subject IDs owned by user with expires_at > now.
func (s *MongoStore) GetActiveEntitlementSubjectIDs(ctx context.Context, userID string) (map[string]bool, error) {
	now := time.Now()
	filter := bson.M{
		"user_id":    userID,
		"active":     true,
		"expires_at": bson.M{"$gt": now},
		"$or": []bson.M{
			{"revoked_at": nil},
			{"revoked_at": bson.M{"$exists": false}},
		},
	}
	opts := options.Find().SetProjection(bson.M{"subject_id": 1})
	cursor, err := s.db.Collection("entitlements").Find(ctx, filter, opts)
	if err != nil {
		return nil, fmt.Errorf("store: find active entitlements: %w", err)
	}
	defer cursor.Close(ctx)

	type subjectIDDoc struct {
		SubjectID string `bson:"subject_id"`
	}
	var docs []subjectIDDoc
	if err := cursor.All(ctx, &docs); err != nil {
		return nil, fmt.Errorf("store: decode active entitlements: %w", err)
	}

	result := make(map[string]bool, len(docs))
	for _, d := range docs {
		result[d.SubjectID] = true
	}
	return result, nil
}

// ListEntitlementsByUser returns all entitlements for a user (including expired history).
func (s *MongoStore) ListEntitlementsByUser(ctx context.Context, userID string) ([]*models.Entitlement, error) {
	opts := options.Find().SetSort(bson.D{{Key: "granted_at", Value: -1}})
	cursor, err := s.db.Collection("entitlements").Find(ctx, bson.M{"user_id": userID}, opts)
	if err != nil {
		return nil, fmt.Errorf("store: find user entitlements: %w", err)
	}
	defer cursor.Close(ctx)

	var result []*models.Entitlement
	if err := cursor.All(ctx, &result); err != nil {
		return nil, fmt.Errorf("store: decode user entitlements: %w", err)
	}
	if result == nil {
		result = []*models.Entitlement{}
	}
	return result, nil
}

// GetEntitlementByID retrieves a single entitlement by its ID.
func (s *MongoStore) GetEntitlementByID(ctx context.Context, id string) (*models.Entitlement, error) {
	var ent models.Entitlement
	err := s.db.Collection("entitlements").FindOne(ctx, bson.M{"_id": id}).Decode(&ent)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, nil
		}
		return nil, fmt.Errorf("store: get entitlement: %w", err)
	}
	return &ent, nil
}

// RevokeEntitlement ends access now (active=false plus revoke fields).
// It reports whether the row changed; an already-revoked row is a
// no-op success (false, nil), a missing row is ErrNotFound.
func (s *MongoStore) RevokeEntitlement(ctx context.Context, id, revokedBy, reason string, now time.Time) (bool, error) {
	col := s.db.Collection("entitlements")
	filter := bson.M{
		"_id": id,
		"$or": []bson.M{
			{"revoked_at": nil},
			{"revoked_at": bson.M{"$exists": false}},
		},
	}
	update := bson.M{
		"$set": bson.M{
			"active":        false,
			"revoked_at":    now,
			"revoked_by":    revokedBy,
			"revoke_reason": reason,
		},
	}
	res, err := col.UpdateOne(ctx, filter, update)
	if err != nil {
		return false, fmt.Errorf("store: revoke entitlement: %w", err)
	}
	if res.ModifiedCount > 0 {
		return true, nil
	}
	// Either already revoked or not found
	var dummy bson.M
	err = col.FindOne(ctx, bson.M{"_id": id}, options.FindOne().SetProjection(bson.M{"_id": 1})).Decode(&dummy)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return false, ErrNotFound
	}
	return false, nil
}

// CreateOrGetPendingRequest creates a pending request or retrieves the existing pending request (R5).
// Enforces partial unique index on (user_id, subject_id) where status = "pending".
// Concurrent callers are guaranteed to receive the same pending request.
func (s *MongoStore) CreateOrGetPendingRequest(ctx context.Context, req *models.PurchaseRequest) (*models.PurchaseRequest, bool, error) {
	if req.ID == "" {
		req.ID = generateID()
	}
	if req.Status == "" {
		req.Status = models.RequestStatusPending
	}
	if req.Source == "" {
		req.Source = "mobile"
	}
	if req.CreatedAt.IsZero() {
		req.CreatedAt = time.Now().UTC()
	}

	col := s.db.Collection("purchase_requests")
	filter := bson.M{
		"user_id":    req.UserID,
		"subject_id": req.SubjectID,
		"status":     models.RequestStatusPending,
	}

	// 1. Try to find existing pending request first
	var existing models.PurchaseRequest
	err := col.FindOne(ctx, filter).Decode(&existing)
	if err == nil {
		return &existing, false, nil
	}
	if !errors.Is(err, mongo.ErrNoDocuments) {
		return nil, false, fmt.Errorf("store: find pending purchase request: %w", err)
	}

	// 2. Not found, attempt insert
	_, err = col.InsertOne(ctx, req)
	if err == nil {
		return req, true, nil
	}

	// 3. If duplicate key, concurrent insert won: fetch the existing row
	if mongo.IsDuplicateKeyError(err) {
		err = col.FindOne(ctx, filter).Decode(&existing)
		if err == nil {
			return &existing, false, nil
		}
		return nil, false, fmt.Errorf("store: find pending purchase request after duplicate: %w", err)
	}

	return nil, false, fmt.Errorf("store: insert purchase request: %w", err)
}

// HasPurchaseRequestWithReference checks if any request exists with the normalized transfer reference.
func (s *MongoStore) HasPurchaseRequestWithReference(ctx context.Context, normalizedRef string) (bool, error) {
	if normalizedRef == "" {
		return false, nil
	}
	n, err := s.db.Collection("purchase_requests").CountDocuments(ctx, bson.M{
		"normalized_reference": normalizedRef,
	}, options.Count().SetLimit(1))
	if err != nil {
		return false, fmt.Errorf("store: check purchase request reference: %w", err)
	}
	return n > 0, nil
}

// CreatePurchaseRequest inserts a new purchase request enforcing uniqueness of pending requests per user+subject and normalized reference.
func (s *MongoStore) CreatePurchaseRequest(ctx context.Context, req *models.PurchaseRequest) error {
	if req.ID == "" {
		req.ID = generateID()
	}
	if req.Status == "" {
		req.Status = models.RequestStatusPending
	}
	if req.Source == "" {
		req.Source = "web"
	}
	if req.CreatedAt.IsZero() {
		req.CreatedAt = time.Now().UTC()
	}

	_, err := s.db.Collection("purchase_requests").InsertOne(ctx, req)
	if err != nil {
		if mongo.IsDuplicateKeyError(err) {
			return mapDuplicateKeyError(err)
		}
		return fmt.Errorf("store: insert purchase request: %w", err)
	}
	return nil
}

// GetPendingRequest returns the pending request for a user and subject, or nil if none exists.
func (s *MongoStore) GetPendingRequest(ctx context.Context, userID, subjectID string) (*models.PurchaseRequest, error) {
	filter := bson.M{
		"user_id":    userID,
		"subject_id": subjectID,
		"status":     models.RequestStatusPending,
	}
	var pr models.PurchaseRequest
	err := s.db.Collection("purchase_requests").FindOne(ctx, filter).Decode(&pr)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, nil
		}
		return nil, fmt.Errorf("store: get pending purchase request: %w", err)
	}
	return &pr, nil
}

// ListRequestsByUser returns all requests for a user ordered by CreatedAt ascending.
func (s *MongoStore) ListRequestsByUser(ctx context.Context, userID string) ([]*models.PurchaseRequest, error) {
	filter := bson.M{"user_id": userID}
	opts := options.Find().SetSort(bson.D{{Key: "created_at", Value: 1}})
	cursor, err := s.db.Collection("purchase_requests").Find(ctx, filter, opts)
	if err != nil {
		return nil, fmt.Errorf("store: list purchase requests: %w", err)
	}
	defer cursor.Close(ctx)

	var results []*models.PurchaseRequest
	if err := cursor.All(ctx, &results); err != nil {
		return nil, fmt.Errorf("store: decode purchase requests: %w", err)
	}
	if results == nil {
		results = []*models.PurchaseRequest{}
	}
	return results, nil
}

// GetRequestByID returns a purchase request by its primary key ID.
func (s *MongoStore) GetRequestByID(ctx context.Context, id string) (*models.PurchaseRequest, error) {
	var pr models.PurchaseRequest
	err := s.db.Collection("purchase_requests").FindOne(ctx, bson.M{"_id": id}).Decode(&pr)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, nil
		}
		return nil, fmt.Errorf("store: get purchase request: %w", err)
	}
	return &pr, nil
}

// ListRequests returns filtered purchase requests, newest first.
func (s *MongoStore) ListRequests(ctx context.Context, filter RequestFilter) ([]*models.PurchaseRequest, int, error) {
	q := bson.M{}
	if filter.Status != "" {
		q["status"] = filter.Status
	}
	if filter.SubjectID != "" {
		q["subject_id"] = filter.SubjectID
	}

	col := s.db.Collection("purchase_requests")
	total, err := col.CountDocuments(ctx, q)
	if err != nil {
		return nil, 0, fmt.Errorf("store: count requests: %w", err)
	}

	const maxPage = 10000
	page := filter.Page
	if page < 1 {
		page = 1
	} else if page > maxPage {
		page = maxPage
	}
	limit := filter.Limit
	if limit <= 0 {
		limit = 20
	} else if limit > 100 {
		limit = 100
	}

	var skip int64
	if page > 1 {
		skip = int64(page-1) * int64(limit)
	}
	if skip < 0 {
		skip = 0
	}
	opts := options.Find().
		SetSort(bson.D{{Key: "created_at", Value: -1}}).
		SetSkip(skip).
		SetLimit(int64(limit))

	cursor, err := col.Find(ctx, q, opts)
	if err != nil {
		return nil, 0, fmt.Errorf("store: find requests: %w", err)
	}
	defer cursor.Close(ctx)

	var results []*models.PurchaseRequest
	if err := cursor.All(ctx, &results); err != nil {
		return nil, 0, fmt.Errorf("store: decode requests: %w", err)
	}
	if results == nil {
		results = []*models.PurchaseRequest{}
	}
	return results, int(total), nil
}

// CountPendingRequests returns the count of requests currently pending.
func (s *MongoStore) CountPendingRequests(ctx context.Context) (int, error) {
	n, err := s.db.Collection("purchase_requests").CountDocuments(ctx, bson.M{"status": models.RequestStatusPending})
	if err != nil {
		return 0, fmt.Errorf("store: count pending requests: %w", err)
	}
	return int(n), nil
}

// DecideRequest compare-and-sets a request from fromStatus to toStatus.
// It reports whether the row changed; a row in another status is a
// no-op (false, nil), a missing row is ErrNotFound.
func (s *MongoStore) DecideRequest(ctx context.Context, id, fromStatus, toStatus, decidedBy string, decidedAt time.Time, reason string) (bool, error) {
	col := s.db.Collection("purchase_requests")
	setFields := bson.M{
		"status":     toStatus,
		"decided_at": decidedAt,
		"decided_by": decidedBy,
	}
	if reason != "" {
		setFields["reject_reason"] = reason
	}
	res, err := col.UpdateOne(ctx,
		bson.M{"_id": id, "status": fromStatus},
		bson.M{"$set": setFields},
	)
	if err != nil {
		return false, fmt.Errorf("store: decide request: %w", err)
	}
	if res.ModifiedCount > 0 {
		return true, nil
	}
	// Check whether the row exists in another status or is missing
	var dummy bson.M
	err = col.FindOne(ctx, bson.M{"_id": id}, options.FindOne().SetProjection(bson.M{"_id": 1})).Decode(&dummy)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return false, ErrNotFound
	}
	return false, nil
}

// UndoAccept reverts this accept call's claim (accepted -> pending). The
// filter matches only the accept made by decidedBy at decidedAt, so it can
// never undo another admin's decision. decidedAt is truncated to the
// millisecond, the precision BSON dates store.
func (s *MongoStore) UndoAccept(ctx context.Context, id, decidedBy string, decidedAt time.Time) (bool, error) {
	res, err := s.db.Collection("purchase_requests").UpdateOne(ctx,
		bson.M{
			"_id":        id,
			"status":     models.RequestStatusAccepted,
			"decided_by": decidedBy,
			"decided_at": decidedAt.Truncate(time.Millisecond),
		},
		bson.M{
			"$set":   bson.M{"status": models.RequestStatusPending},
			"$unset": bson.M{"decided_at": "", "decided_by": ""},
		},
	)
	if err != nil {
		return false, fmt.Errorf("store: undo accept: %w", err)
	}
	return res.ModifiedCount > 0, nil
}

// CreateOrderWithRequests creates one order and its requests all-or-none.
// MongoDB is standalone here (SPEC R4), so there is no transaction: the order
// is inserted first, and any failure removes the partial requests and the
// order (compensating delete).
func (s *MongoStore) CreateOrderWithRequests(ctx context.Context, order *models.Order, reqs []*models.PurchaseRequest) error {
	if order == nil {
		return errors.New("store: nil order")
	}
	if order.ID == "" {
		order.ID = generateID()
	}
	if order.Status == "" {
		order.Status = models.OrderStatusOpen
	}
	if order.Source == "" {
		order.Source = models.OrderSourceWeb
	}
	if order.CreatedAt.IsZero() {
		order.CreatedAt = time.Now().UTC()
	}

	if order.NormalizedReference != "" {
		has, err := s.HasOrderWithNormalizedReference(ctx, order.NormalizedReference)
		if err != nil {
			return err
		}
		if has {
			return ErrDuplicate
		}
		hasReq, err := s.HasPurchaseRequestWithReference(ctx, order.NormalizedReference)
		if err != nil {
			return err
		}
		if hasReq {
			return ErrDuplicate
		}
	}

	if _, err := s.db.Collection("orders").InsertOne(ctx, order); err != nil {
		if mongo.IsDuplicateKeyError(err) {
			return mapDuplicateKeyError(err)
		}
		return fmt.Errorf("store: insert order: %w", err)
	}

	if len(reqs) == 0 {
		return nil
	}

	docs := make([]any, 0, len(reqs))
	for _, req := range reqs {
		if req == nil {
			s.deleteOrderWithRequests(ctx, order.ID)
			return errors.New("store: nil request")
		}
		if req.ID == "" {
			req.ID = generateID()
		}
		if req.Status == "" {
			req.Status = models.RequestStatusPending
		}
		if req.Source == "" {
			req.Source = order.Source
		}
		if req.CreatedAt.IsZero() {
			req.CreatedAt = order.CreatedAt
		}
		req.OrderID = order.ID
		docs = append(docs, req)
	}

	if _, err := s.db.Collection("purchase_requests").InsertMany(ctx, docs, options.InsertMany().SetOrdered(true)); err != nil {
		s.deleteOrderWithRequests(ctx, order.ID)
		if mongo.IsDuplicateKeyError(err) {
			return mapDuplicateKeyError(err)
		}
		return fmt.Errorf("store: insert order requests: %w", err)
	}
	return nil
}

// mapDuplicateKeyError inspects a MongoDB duplicate key error to distinguish between
// a duplicate transfer reference and a duplicate pending request per subject.
func mapDuplicateKeyError(err error) error {
	if !mongo.IsDuplicateKeyError(err) {
		return err
	}
	msg := err.Error()
	var we mongo.WriteException
	if errors.As(err, &we) {
		for _, wErr := range we.WriteErrors {
			msg += " " + wErr.Message
		}
	}
	var bwe mongo.BulkWriteException
	if errors.As(err, &bwe) {
		for _, wErr := range bwe.WriteErrors {
			msg += " " + wErr.Message
		}
	}
	if strings.Contains(msg, "uniq_orders_normalized_reference") ||
		strings.Contains(msg, "uniq_purchase_requests_normalized_reference") ||
		strings.Contains(msg, "normalized_reference") {
		return ErrDuplicateReference
	}
	if strings.Contains(msg, "uniq_pending_user_subject_request") {
		return ErrPendingRequestExists
	}
	return ErrDuplicate
}

// deleteOrderWithRequests removes an order and any request rows already
// written for it (compensating delete after a partial create). It runs
// under a detached context so an expired parent context cannot skip it,
// and logs any deletion failures.
func (s *MongoStore) deleteOrderWithRequests(ctx context.Context, orderID string) {
	compCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	cleanID := strings.ReplaceAll(strings.ReplaceAll(orderID, "\r", ""), "\n", "")
	if _, err := s.db.Collection("purchase_requests").DeleteMany(compCtx, bson.M{"order_id": orderID}); err != nil {
		// #nosec G706 -- cleanID sanitized of CR/LF
		log.Printf("[ERROR] checkout compensation failed to delete purchase_requests for order %s: %v", cleanID, err)
	}
	if _, err := s.db.Collection("orders").DeleteOne(compCtx, bson.M{"_id": orderID}); err != nil {
		// #nosec G706 -- cleanID sanitized of CR/LF
		log.Printf("[ERROR] checkout compensation failed to delete order %s: %v", cleanID, err)
	}
}

// HasOrderWithNormalizedReference checks if any order uses the normalized transfer reference.
func (s *MongoStore) HasOrderWithNormalizedReference(ctx context.Context, normalizedRef string) (bool, error) {
	if normalizedRef == "" {
		return false, nil
	}
	n, err := s.db.Collection("orders").CountDocuments(ctx, bson.M{
		"normalized_reference": normalizedRef,
	}, options.Count().SetLimit(1))
	if err != nil {
		return false, fmt.Errorf("store: check order reference: %w", err)
	}
	return n > 0, nil
}

// GetOrderByID returns an order by id, or nil when it does not exist.
func (s *MongoStore) GetOrderByID(ctx context.Context, id string) (*models.Order, error) {
	var order models.Order
	err := s.db.Collection("orders").FindOne(ctx, bson.M{"_id": id}).Decode(&order)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, nil
		}
		return nil, fmt.Errorf("store: get order: %w", err)
	}
	return &order, nil
}

// ListOrdersByUser returns a user's orders, newest first.
func (s *MongoStore) ListOrdersByUser(ctx context.Context, userID string) ([]*models.Order, error) {
	opts := options.Find().SetSort(bson.D{{Key: "created_at", Value: -1}})
	cursor, err := s.db.Collection("orders").Find(ctx, bson.M{"user_id": userID}, opts)
	if err != nil {
		return nil, fmt.Errorf("store: list orders: %w", err)
	}
	defer cursor.Close(ctx)

	var results []*models.Order
	if err := cursor.All(ctx, &results); err != nil {
		return nil, fmt.Errorf("store: decode orders: %w", err)
	}
	if results == nil {
		results = []*models.Order{}
	}
	return results, nil
}

// ListRequestsByOrder returns an order's requests in creation order.
func (s *MongoStore) ListRequestsByOrder(ctx context.Context, orderID string) ([]*models.PurchaseRequest, error) {
	opts := options.Find().SetSort(bson.D{{Key: "created_at", Value: 1}})
	cursor, err := s.db.Collection("purchase_requests").Find(ctx, bson.M{"order_id": orderID}, opts)
	if err != nil {
		return nil, fmt.Errorf("store: list order requests: %w", err)
	}
	defer cursor.Close(ctx)

	var results []*models.PurchaseRequest
	if err := cursor.All(ctx, &results); err != nil {
		return nil, fmt.Errorf("store: decode order requests: %w", err)
	}
	if results == nil {
		results = []*models.PurchaseRequest{}
	}
	return results, nil
}

// CreateOrGetPendingAppRequest attaches an app request to the student's open
// app order, creating the order when none exists. The partial unique index on
// orders(user_id) where source=app and status=open makes concurrent first
// requests converge on one order; the loser re-reads the winner's order.
func (s *MongoStore) CreateOrGetPendingAppRequest(ctx context.Context, req *models.PurchaseRequest) (*models.PurchaseRequest, bool, error) {
	if req == nil {
		return nil, false, errors.New("store: nil request")
	}
	if req.ID == "" {
		req.ID = generateID()
	}
	if req.Status == "" {
		req.Status = models.RequestStatusPending
	}
	if req.Source == "" {
		req.Source = "mobile"
	}
	if req.CreatedAt.IsZero() {
		req.CreatedAt = time.Now().UTC()
	}

	col := s.db.Collection("purchase_requests")
	filter := bson.M{
		"user_id":    req.UserID,
		"subject_id": req.SubjectID,
		"status":     models.RequestStatusPending,
	}

	var existing models.PurchaseRequest
	err := col.FindOne(ctx, filter).Decode(&existing)
	if err == nil {
		return &existing, false, nil
	}
	if !errors.Is(err, mongo.ErrNoDocuments) {
		return nil, false, fmt.Errorf("store: find pending app request: %w", err)
	}

	for attempt := 0; attempt < 2; attempt++ {
		orderID, err := s.ensureOpenAppOrder(ctx, req.UserID, req.CreatedAt)
		if err != nil {
			return nil, false, err
		}
		if s.beforeAppOrderInsert != nil {
			s.beforeAppOrderInsert(orderID)
		}

		// Re-check that the order is still open. If closed concurrently, retry with a fresh order.
		updateRes, err := s.db.Collection("orders").UpdateOne(ctx,
			bson.M{"_id": orderID, "status": models.OrderStatusOpen},
			bson.M{"$set": bson.M{"updated_at": time.Now().UTC()}},
		)
		if err != nil {
			return nil, false, fmt.Errorf("store: re-check open app order: %w", err)
		}
		if updateRes.MatchedCount == 0 {
			// Matched nothing: order was closed concurrently. Retry once to open a fresh order.
			continue
		}

		req.OrderID = orderID

		if _, err := col.InsertOne(ctx, req); err == nil {
			return req, true, nil
		} else if mongo.IsDuplicateKeyError(err) {
			// Another concurrent call for the same subject won: return its row.
			var existing models.PurchaseRequest
			if err := col.FindOne(ctx, filter).Decode(&existing); err == nil {
				return &existing, false, nil
			}
			return nil, false, fmt.Errorf("store: find pending app request after duplicate: %w", err)
		} else {
			return nil, false, fmt.Errorf("store: insert app request: %w", err)
		}
	}
	return nil, false, errors.New("store: failed to attach app request to open order after retry")
}

// ensureOpenAppOrder returns the student's open app order id, creating the
// order when it does not exist. A duplicate-key race re-reads the winner.
func (s *MongoStore) ensureOpenAppOrder(ctx context.Context, userID string, createdAt time.Time) (string, error) {
	col := s.db.Collection("orders")
	filter := bson.M{
		"user_id": userID,
		"source":  models.OrderSourceApp,
		"status":  models.OrderStatusOpen,
	}

	var existing models.Order
	err := col.FindOne(ctx, filter).Decode(&existing)
	if err == nil {
		return existing.ID, nil
	}
	if !errors.Is(err, mongo.ErrNoDocuments) {
		return "", fmt.Errorf("store: find open app order: %w", err)
	}

	order := &models.Order{
		ID:        generateID(),
		UserID:    userID,
		Source:    models.OrderSourceApp,
		Status:    models.OrderStatusOpen,
		CreatedAt: createdAt,
	}
	if _, err := col.InsertOne(ctx, order); err == nil {
		return order.ID, nil
	} else if mongo.IsDuplicateKeyError(err) {
		var existing models.Order
		if err := col.FindOne(ctx, filter).Decode(&existing); err == nil {
			return existing.ID, nil
		}
		return "", fmt.Errorf("store: find open app order after duplicate: %w", err)
	} else {
		return "", fmt.Errorf("store: insert open app order: %w", err)
	}
}

// CloseOrderIfNoPending closes the order when it has no pending request.
func (s *MongoStore) CloseOrderIfNoPending(ctx context.Context, orderID string, closedAt time.Time) (bool, error) {
	if orderID == "" {
		return false, nil
	}
	n, err := s.db.Collection("purchase_requests").CountDocuments(ctx, bson.M{
		"order_id": orderID,
		"status":   models.RequestStatusPending,
	})
	if err != nil {
		return false, fmt.Errorf("store: count pending order requests: %w", err)
	}
	if n > 0 {
		return false, nil
	}
	res, err := s.db.Collection("orders").UpdateOne(ctx,
		bson.M{"_id": orderID, "status": models.OrderStatusOpen},
		bson.M{"$set": bson.M{"status": models.OrderStatusClosed, "closed_at": closedAt}},
	)
	if err != nil {
		return false, fmt.Errorf("store: close order: %w", err)
	}
	return res.ModifiedCount > 0, nil
}

// ListRequestsForGroups returns every matching request for the console read model, newest first.
func (s *MongoStore) ListRequestsForGroups(ctx context.Context, filter OrderRequestFilter) ([]*models.PurchaseRequest, error) {
	status := filter.Status
	if status == "" {
		status = models.RequestStatusPending
	}
	q := bson.M{"status": status}
	if filter.UserID != "" {
		q["user_id"] = filter.UserID
	}

	opts := options.Find().SetSort(bson.D{{Key: "created_at", Value: -1}})
	cursor, err := s.db.Collection("purchase_requests").Find(ctx, q, opts)
	if err != nil {
		return nil, fmt.Errorf("store: list request groups: %w", err)
	}
	defer cursor.Close(ctx)

	var results []*models.PurchaseRequest
	if err := cursor.All(ctx, &results); err != nil {
		return nil, fmt.Errorf("store: decode request groups: %w", err)
	}
	if results == nil {
		results = []*models.PurchaseRequest{}
	}
	return results, nil
}

// DeleteOrphanWebOrders removes old web orders that have no request rows.
// Only source=web is considered; app orders are never touched. The age
// threshold keeps an in-flight checkout (order inserted, requests not yet)
// out of reach.
func (s *MongoStore) DeleteOrphanWebOrders(ctx context.Context, before time.Time) (int, error) {
	ordersCol := s.db.Collection("orders")
	requestsCol := s.db.Collection("purchase_requests")

	opts := options.Find().SetProjection(bson.M{"_id": 1})
	cursor, err := ordersCol.Find(ctx, bson.M{
		"source":     models.OrderSourceWeb,
		"created_at": bson.M{"$lt": before},
	}, opts)
	if err != nil {
		return 0, fmt.Errorf("store: list orphan web order candidates: %w", err)
	}
	defer cursor.Close(ctx)

	removed := 0
	for cursor.Next(ctx) {
		var row struct {
			ID string `bson:"_id"`
		}
		if err := cursor.Decode(&row); err != nil {
			return removed, fmt.Errorf("store: decode orphan web order candidate: %w", err)
		}
		n, err := requestsCol.CountDocuments(ctx, bson.M{"order_id": row.ID}, options.Count().SetLimit(1))
		if err != nil {
			return removed, fmt.Errorf("store: count order requests: %w", err)
		}
		if n > 0 {
			continue
		}
		res, err := ordersCol.DeleteOne(ctx, bson.M{"_id": row.ID, "source": models.OrderSourceWeb})
		if err != nil {
			return removed, fmt.Errorf("store: delete orphan web order: %w", err)
		}
		removed += int(res.DeletedCount)
	}
	if err := cursor.Err(); err != nil {
		return removed, fmt.Errorf("store: iterate orphan web orders: %w", err)
	}
	return removed, nil
}

// MigrateLegacyWebRequests wraps every pending web request that has no order
// into its own web order of one, copying its proof fields (payment method,
// transfer reference, sender number, amount). Accepted/rejected history is
// never touched. It is idempotent: a request that already carries an order_id
// is skipped, so a second start creates nothing.
//
// A legacy row whose transfer reference is already used by an order is left
// without an order and logged: the unique index on orders(normalized_reference)
// must not turn a startup into a failure.
func (s *MongoStore) MigrateLegacyWebRequests(ctx context.Context) error {
	filter := bson.M{
		"status": models.RequestStatusPending,
		"source": models.OrderSourceWeb,
		"$or": bson.A{
			bson.M{"order_id": bson.M{"$exists": false}},
			bson.M{"order_id": ""},
		},
	}

	cursor, err := s.db.Collection("purchase_requests").Find(ctx, filter)
	if err != nil {
		return fmt.Errorf("store: find legacy web requests: %w", err)
	}
	defer cursor.Close(ctx)

	var legacy []*models.PurchaseRequest
	if err := cursor.All(ctx, &legacy); err != nil {
		return fmt.Errorf("store: decode legacy web requests: %w", err)
	}

	ordersCol := s.db.Collection("orders")
	requestsCol := s.db.Collection("purchase_requests")
	migrated := 0
	for _, req := range legacy {
		if req.NormalizedReference != "" {
			has, err := s.HasOrderWithNormalizedReference(ctx, req.NormalizedReference)
			if err != nil {
				return err
			}
			if has {
				log.Printf("[WARN] orders migration: skipped legacy web request %s: transfer reference %q is already used by an order", req.ID, req.NormalizedReference)
				continue
			}
		}

		order := &models.Order{
			ID:                  generateID(),
			UserID:              req.UserID,
			Source:              models.OrderSourceWeb,
			Status:              models.OrderStatusOpen,
			PaymentMethod:       req.PaymentMethod,
			TransferReference:   req.TransferReference,
			NormalizedReference: req.NormalizedReference,
			SenderNumber:        req.SenderNumber,
			AmountPaid:          req.Amount,
			CreatedAt:           req.CreatedAt,
		}
		if _, err := ordersCol.InsertOne(ctx, order); err != nil {
			if mongo.IsDuplicateKeyError(err) {
				log.Printf("[WARN] orders migration: skipped legacy web request %s: its transfer reference conflicts with an existing order", req.ID)
				continue
			}
			return fmt.Errorf("store: insert legacy web order: %w", err)
		}

		res, err := requestsCol.UpdateOne(ctx,
			bson.M{
				"_id":    req.ID,
				"status": models.RequestStatusPending,
				"$or": bson.A{
					bson.M{"order_id": bson.M{"$exists": false}},
					bson.M{"order_id": ""},
				},
			},
			bson.M{"$set": bson.M{"order_id": order.ID}},
		)
		if err != nil {
			// Compensate: never leave an order without its request.
			_, _ = ordersCol.DeleteOne(ctx, bson.M{"_id": order.ID})
			return fmt.Errorf("store: attach legacy web request: %w", err)
		}
		if res.MatchedCount == 0 {
			// Another writer attached it first: drop the order we created.
			_, _ = ordersCol.DeleteOne(ctx, bson.M{"_id": order.ID})
			continue
		}
		migrated++
	}
	if migrated > 0 {
		log.Printf("[ACADEMY] orders migration: wrapped %d legacy pending web request(s) into web orders of one", migrated)
	}
	return nil
}

// MigrateLegacyAppRequests groups existing pending app/mobile requests that
// have no order_id into one open app order per student. Accepted/rejected
// history keeps no order on purpose (see docs/changes/2026-11-05-orders.md).
// It is idempotent: rows that already carry an order_id are skipped.
func (s *MongoStore) MigrateLegacyAppRequests(ctx context.Context) error {
	filter := bson.M{
		"status": models.RequestStatusPending,
		"$and": bson.A{
			bson.M{"$or": bson.A{
				bson.M{"order_id": bson.M{"$exists": false}},
				bson.M{"order_id": ""},
			}},
			bson.M{"$or": bson.A{
				bson.M{"source": bson.M{"$exists": false}},
				bson.M{"source": ""},
				bson.M{"source": "mobile"},
			}},
		},
	}

	cursor, err := s.db.Collection("purchase_requests").Find(ctx, filter)
	if err != nil {
		return fmt.Errorf("store: find legacy app requests: %w", err)
	}
	defer cursor.Close(ctx)

	var legacy []*models.PurchaseRequest
	if err := cursor.All(ctx, &legacy); err != nil {
		return fmt.Errorf("store: decode legacy app requests: %w", err)
	}
	if len(legacy) == 0 {
		return nil
	}

	byUser := make(map[string][]*models.PurchaseRequest)
	earliest := make(map[string]time.Time)
	for _, req := range legacy {
		if req.UserID == "" {
			continue
		}
		byUser[req.UserID] = append(byUser[req.UserID], req)
		if t, ok := earliest[req.UserID]; !ok || req.CreatedAt.Before(t) {
			earliest[req.UserID] = req.CreatedAt
		}
	}

	for userID, reqs := range byUser {
		orderID, err := s.ensureOpenAppOrder(ctx, userID, earliest[userID])
		if err != nil {
			return err
		}
		ids := make([]string, 0, len(reqs))
		for _, req := range reqs {
			ids = append(ids, req.ID)
		}
		if len(ids) == 0 {
			continue
		}
		if s.beforeMigrateAppRequestsAttach != nil {
			s.beforeMigrateAppRequestsAttach(ids)
		}
		if _, err := s.db.Collection("purchase_requests").UpdateMany(ctx,
			bson.M{
				"_id":    bson.M{"$in": ids},
				"status": models.RequestStatusPending,
				"$or": bson.A{
					bson.M{"order_id": bson.M{"$exists": false}},
					bson.M{"order_id": ""},
				},
			},
			bson.M{"$set": bson.M{"order_id": orderID}},
		); err != nil {
			return fmt.Errorf("store: attach legacy app requests: %w", err)
		}
	}
	log.Printf("[ACADEMY] orders migration: attached %d legacy pending app request(s) for %d student(s) to open app orders", len(legacy), len(byUser))
	return nil
}

// CreatePaymentRecord inserts an append-only financial row. A duplicate entitlement_id is ErrDuplicate.
func (s *MongoStore) CreatePaymentRecord(ctx context.Context, rec *models.PaymentRecord) error {
	if rec.ID == "" {
		rec.ID = generateID()
	}
	if rec.RecordedAt.IsZero() {
		rec.RecordedAt = time.Now().UTC()
	}

	_, err := s.db.Collection("payment_records").InsertOne(ctx, rec)
	if err != nil {
		if mongo.IsDuplicateKeyError(err) {
			return ErrDuplicate
		}
		return fmt.Errorf("store: insert payment record: %w", err)
	}
	return nil
}

// ListPaymentRecordsByUser returns payment records for a user, newest first.
func (s *MongoStore) ListPaymentRecordsByUser(ctx context.Context, userID string) ([]*models.PaymentRecord, error) {
	opts := options.Find().SetSort(bson.D{{Key: "recorded_at", Value: -1}})
	cursor, err := s.db.Collection("payment_records").Find(ctx, bson.M{"user_id": userID}, opts)
	if err != nil {
		return nil, fmt.Errorf("store: list payment records: %w", err)
	}
	defer cursor.Close(ctx)

	var result []*models.PaymentRecord
	if err := cursor.All(ctx, &result); err != nil {
		return nil, fmt.Errorf("store: decode payment records: %w", err)
	}
	if result == nil {
		result = []*models.PaymentRecord{}
	}
	return result, nil
}

// RecordVideoPlay writes an append-only video playback event log.
// Deliberately contains no IP address.
func (s *MongoStore) RecordVideoPlay(ctx context.Context, play *models.VideoPlay) error {
	if play.ID == "" {
		play.ID = generateID()
	}
	if play.PlayedAt.IsZero() {
		play.PlayedAt = time.Now().UTC()
	}
	_, err := s.db.Collection("video_plays").InsertOne(ctx, play)
	if err != nil {
		return fmt.Errorf("store: record video play: %w", err)
	}
	return nil
}

// ListVideoPlaysByVideo lists playback events for a video.
func (s *MongoStore) ListVideoPlaysByVideo(ctx context.Context, videoID string) ([]*models.VideoPlay, error) {
	filter := bson.M{"video_id": videoID}
	opts := options.Find().SetSort(bson.D{{Key: "played_at", Value: -1}})
	cursor, err := s.db.Collection("video_plays").Find(ctx, filter, opts)
	if err != nil {
		return nil, fmt.Errorf("store: list video plays by video: %w", err)
	}
	defer cursor.Close(ctx)

	var plays []*models.VideoPlay
	if err := cursor.All(ctx, &plays); err != nil {
		return nil, fmt.Errorf("store: decode video plays: %w", err)
	}
	if plays == nil {
		plays = []*models.VideoPlay{}
	}
	return plays, nil
}

// ListVideoPlaysByUser lists playback events for a user.
func (s *MongoStore) ListVideoPlaysByUser(ctx context.Context, userID string) ([]*models.VideoPlay, error) {
	filter := bson.M{"user_id": userID}
	opts := options.Find().SetSort(bson.D{{Key: "played_at", Value: -1}})
	cursor, err := s.db.Collection("video_plays").Find(ctx, filter, opts)
	if err != nil {
		return nil, fmt.Errorf("store: list video plays by user: %w", err)
	}
	defer cursor.Close(ctx)

	var plays []*models.VideoPlay
	if err := cursor.All(ctx, &plays); err != nil {
		return nil, fmt.Errorf("store: decode video plays: %w", err)
	}
	if plays == nil {
		plays = []*models.VideoPlay{}
	}
	return plays, nil
}

// CreateAuditLog records an admin action in admin_audit_log.
func (s *MongoStore) CreateAuditLog(ctx context.Context, entry *models.AuditLog) error {
	if entry.ID == "" {
		entry.ID = generateID()
	}
	if entry.CreatedAt.IsZero() {
		entry.CreatedAt = time.Now().UTC()
	}
	_, err := s.db.Collection("admin_audit_log").InsertOne(ctx, entry)
	if err != nil {
		return fmt.Errorf("store: insert audit log: %w", err)
	}
	return nil
}

// ListAuditLogs returns paginated audit log entries, newest first.
func (s *MongoStore) ListAuditLogs(ctx context.Context, page, limit int) ([]*models.AuditLog, int, error) {
	const maxPage = 10000
	if page < 1 {
		page = 1
	} else if page > maxPage {
		page = maxPage
	}
	if limit <= 0 {
		limit = 20
	} else if limit > 100 {
		limit = 100
	}

	total, err := s.db.Collection("admin_audit_log").CountDocuments(ctx, bson.M{})
	if err != nil {
		return nil, 0, fmt.Errorf("store: count audit logs: %w", err)
	}

	var skip int64
	if page > 1 {
		skip = int64(page-1) * int64(limit)
	}
	if skip < 0 {
		skip = 0
	}
	opts := options.Find().
		SetSort(bson.D{{Key: "created_at", Value: -1}}).
		SetSkip(skip).
		SetLimit(int64(limit))

	cursor, err := s.db.Collection("admin_audit_log").Find(ctx, bson.M{}, opts)
	if err != nil {
		return nil, 0, fmt.Errorf("store: find audit logs: %w", err)
	}
	defer cursor.Close(ctx)

	var result []*models.AuditLog
	if err := cursor.All(ctx, &result); err != nil {
		return nil, 0, fmt.Errorf("store: decode audit logs: %w", err)
	}
	if result == nil {
		result = []*models.AuditLog{}
	}
	return result, int(total), nil
}

// GetAppSettings returns the stored app settings for MongoStore (empty
// settings when none were saved). Cached in-process for 60s (A1). Env
// defaults are applied by the caller (handlers), not here.
func (s *MongoStore) GetAppSettings(ctx context.Context) (*models.AppSettings, error) {
	s.settingsMu.RLock()
	if s.cachedSettings != nil && time.Since(s.cachedSettingsAt) < settingsCacheTTL {
		val := s.cachedSettings.Clone()
		s.settingsMu.RUnlock()
		return val, nil
	}
	s.settingsMu.RUnlock()

	s.settingsMu.Lock()
	defer s.settingsMu.Unlock()
	if s.cachedSettings != nil && time.Since(s.cachedSettingsAt) < settingsCacheTTL {
		return s.cachedSettings.Clone(), nil
	}

	var raw models.AppSettings
	err := s.db.Collection("app_settings").FindOne(ctx, bson.M{"_id": models.AppSettingsID}).Decode(&raw)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			empty := &models.AppSettings{ID: models.AppSettingsID}
			s.cachedSettings = empty.Clone()
			s.cachedSettingsAt = time.Now()
			return empty, nil
		}
		return nil, fmt.Errorf("store: find app_settings: %w", err)
	}

	raw.ID = models.AppSettingsID
	s.cachedSettings = raw.Clone()
	s.cachedSettingsAt = time.Now()
	return raw.Clone(), nil
}

// SaveAppSettings replaces the single app_settings document (_id: "app")
// and invalidates the in-process cache (A1).
func (s *MongoStore) SaveAppSettings(ctx context.Context, settings *models.AppSettings) error {
	if settings == nil {
		return errors.New("store: nil app settings")
	}
	toSave := settings.Clone()
	toSave.ID = models.AppSettingsID

	opts := options.Replace().SetUpsert(true)
	_, err := s.db.Collection("app_settings").ReplaceOne(ctx, bson.M{"_id": models.AppSettingsID}, toSave, opts)
	if err != nil {
		return fmt.Errorf("store: save app_settings: %w", err)
	}

	s.settingsMu.Lock()
	s.cachedSettings = nil
	s.cachedSettingsAt = time.Time{}
	s.settingsMu.Unlock()

	return nil
}

// isIndexNotFound reports whether err represents MongoDB error code 27 (IndexNotFound).
func isIndexNotFound(err error) bool {
	if err == nil {
		return false
	}
	var cmdErr mongo.CommandError
	if errors.As(err, &cmdErr) {
		return cmdErr.HasErrorCode(27) || cmdErr.Name == "IndexNotFound"
	}
	return strings.Contains(err.Error(), "index not found") || strings.Contains(err.Error(), "IndexNotFound")
}
