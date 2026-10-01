// MongoStore is a MongoDB-backed Store for academy-service persistence.
package store

import (
	"context"
	"fmt"
	"time"

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

// EnsureIndexes creates required collection indexes. In Phase 2.1, no collections
// are defined yet; subsequent phases will add indexes for levels, subjects, etc.
func (s *MongoStore) EnsureIndexes(_ context.Context) error {
	// Phase 2.1 skeleton: creates nothing yet except what 2.1 defines (no collections invented).
	return nil
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
