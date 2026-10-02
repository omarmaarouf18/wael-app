// MongoStore is a MongoDB-backed Store for academy-service persistence.
package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/omarmaarouf18/wael-app/academy-service/internal/models"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// MongoStore persists academy entities in MongoDB.
type MongoStore struct {
	client *mongo.Client
	db     *mongo.Database
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

	return nil
}

// SeedLevels idempotently initializes the 5 fixed academic levels using $setOnInsert.
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
			},
		}
		opts := options.UpdateOne().SetUpsert(true)
		if _, err := s.db.Collection("levels").UpdateOne(ctx, filter, update, opts); err != nil {
			return fmt.Errorf("store: seed level %s: %w", lvl.Key, err)
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

// CreateSubject inserts a new subject into the subjects collection.
func (s *MongoStore) CreateSubject(ctx context.Context, subj *models.Subject) error {
	_, err := s.db.Collection("subjects").InsertOne(ctx, subj)
	if err != nil {
		if mongo.IsDuplicateKeyError(err) {
			return ErrDuplicate
		}
		return fmt.Errorf("store: insert subject: %w", err)
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

	skip := int64((page - 1) * limit)
	opts := options.Find().
		SetSort(bson.D{{Key: "created_at", Value: 1}}).
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

	vCount, err := s.db.Collection("videos").CountDocuments(ctx, bson.M{"subject_id": subjectID, "published": true})
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

// ListVideosBySubject returns videos for a subject sorted by position.
func (s *MongoStore) ListVideosBySubject(ctx context.Context, subjectID string, onlyPublished bool) ([]*models.Video, error) {
	filter := bson.M{"subject_id": subjectID}
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
