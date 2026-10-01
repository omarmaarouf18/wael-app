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

	opts := options.Find().SetSort(bson.D{{Key: "position", Value: 1}})
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
