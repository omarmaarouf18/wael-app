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

	return nil
}

// Ping checks store availability.
func (s *MongoStore) Ping(ctx context.Context) error {
	return s.client.Ping(ctx, nil)
}

// Close disconnects MongoDB client.
func (s *MongoStore) Close(ctx context.Context) error {
	return s.client.Disconnect(ctx)
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
		dr := s.db.Collection("subjects").Distinct(ctx, "level_key", bson.M{"status": "published"})
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
