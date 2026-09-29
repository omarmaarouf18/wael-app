// MongoStore is a MongoDB-backed Store for shared server/CLI persistence.
package store

import (
	"context"
	"fmt"
	"time"

	"github.com/omarmaarouf18/wael-app/auth-service/internal/models"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// MongoStore persists users in the "users" collection.
type MongoStore struct {
	coll *mongo.Collection
}

// NewMongoStore connects to MongoDB, ensures a unique index on email, and
// returns a Store shared by the server and ops CLI tools.
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
	coll := client.Database(dbName).Collection("users")
	_, err = coll.Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys:    bson.D{{Key: "email", Value: 1}},
		Options: options.Index().SetUnique(true),
	})
	if err != nil {
		return nil, fmt.Errorf("store: ensure email index: %w", err)
	}
	return &MongoStore{coll: coll}, nil
}

// Create inserts a new user.
func (s *MongoStore) Create(ctx context.Context, u *models.User) error {
	if u.CreatedAt.IsZero() {
		u.CreatedAt = time.Now()
	}
	u.UpdatedAt = time.Now()
	_, err := s.coll.InsertOne(ctx, u)
	if err != nil {
		if mongo.IsDuplicateKeyError(err) {
			return fmt.Errorf("store: email already registered")
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
		if err == mongo.ErrNoDocuments {
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
		if err == mongo.ErrNoDocuments {
			return nil, nil
		}
		return nil, fmt.Errorf("store: find by id: %w", err)
	}
	return &u, nil
}

// Update replaces the stored user record.
func (s *MongoStore) Update(ctx context.Context, u *models.User) error {
	u.UpdatedAt = time.Now()
	res, err := s.coll.ReplaceOne(ctx, bson.M{"_id": u.ID}, u)
	if err != nil {
		if mongo.IsDuplicateKeyError(err) {
			return fmt.Errorf("store: email already registered")
		}
		return fmt.Errorf("store: update: %w", err)
	}
	if res.MatchedCount == 0 {
		return fmt.Errorf("store: user not found")
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
