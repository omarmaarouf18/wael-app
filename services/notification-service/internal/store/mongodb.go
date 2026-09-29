package store

import (
	"context"
	"fmt"
	"time"

	"github.com/omarmaarouf18/wael-app/notification-service/internal/models"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// MongoStore persists notifications in the "notifications" collection.
type MongoStore struct {
	coll *mongo.Collection
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
	coll := client.Database(dbName).Collection("notifications")
	_, err = coll.Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys: bson.D{{Key: "user_id", Value: 1}, {Key: "created_at", Value: -1}},
	})
	if err != nil {
		return nil, fmt.Errorf("store: ensure index: %w", err)
	}
	return &MongoStore{coll: coll}, nil
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
