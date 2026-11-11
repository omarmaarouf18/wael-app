package store

import (
	"context"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

func TestMongoStore_AccountEvents_TTLAndFilterIndexes_TwoStarts(t *testing.T) {
	mongoURI, _ := requireDB(t)
	dbName := randomDBName("test_account_events_ttl")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	client, err := mongo.Connect(options.Client().ApplyURI(mongoURI))
	if err != nil {
		t.Fatalf("mongo.Connect: %v", err)
	}
	defer client.Disconnect(ctx)

	db := client.Database(dbName)
	t.Cleanup(func() {
		_ = db.Drop(context.Background())
	})

	// Pre-create legacy index `created_at_-1` on account_events without TTL to simulate legacy database.
	accountEvtColl := db.Collection("account_events")
	_, err = accountEvtColl.Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys: bson.D{
			{Key: "created_at", Value: -1},
		},
	})
	if err != nil {
		t.Fatalf("pre-create legacy index: %v", err)
	}

	verifyIndexes := func(startLabel string) {
		t.Helper()
		cursor, err := accountEvtColl.Indexes().List(ctx)
		if err != nil {
			t.Fatalf("[%s] list indexes: %v", startLabel, err)
		}
		type indexDoc struct {
			Name               string `bson:"name"`
			ExpireAfterSeconds *int32 `bson:"expireAfterSeconds,omitempty"`
			Key                bson.D `bson:"key"`
		}
		var indexes []indexDoc
		for cursor.Next(ctx) {
			var doc indexDoc
			if err := cursor.Decode(&doc); err != nil {
				t.Fatalf("[%s] decode index: %v", startLabel, err)
			}
			indexes = append(indexes, doc)
		}
		_ = cursor.Close(ctx)

		var (
			hasOldCreatedAt      bool
			hasTTL               bool
			ttlSeconds           int32
			hasUserTypeCreatedAt bool
			hasTypeCreatedAt     bool
			hasUserCreatedAt     bool
		)

		for _, idx := range indexes {
			if idx.Name == "created_at_-1" {
				hasOldCreatedAt = true
			}
			if idx.Name == "account_events_created_at_ttl" {
				hasTTL = true
				if idx.ExpireAfterSeconds != nil {
					ttlSeconds = *idx.ExpireAfterSeconds
				}
			}
			// check keys
			keyStr := bsonKeyString(idx.Key)
			if keyStr == "user_id:1,type:1,created_at:-1" {
				hasUserTypeCreatedAt = true
			}
			if keyStr == "type:1,created_at:-1" {
				hasTypeCreatedAt = true
			}
			if keyStr == "user_id:1,created_at:-1" {
				hasUserCreatedAt = true
			}
		}

		if hasOldCreatedAt {
			t.Errorf("[%s] expected legacy index 'created_at_-1' to be dropped, but it still exists", startLabel)
		}
		if !hasTTL {
			t.Errorf("[%s] expected TTL index 'account_events_created_at_ttl', not found", startLabel)
		} else if ttlSeconds != 180*24*3600 {
			t.Errorf("[%s] expected TTL 180 days (%d s), got %d s", startLabel, 180*24*3600, ttlSeconds)
		}
		if !hasUserTypeCreatedAt {
			t.Errorf("[%s] expected compound index {user_id: 1, type: 1, created_at: -1}, not found", startLabel)
		}
		if !hasTypeCreatedAt {
			t.Errorf("[%s] expected compound index {type: 1, created_at: -1}, not found", startLabel)
		}
		if !hasUserCreatedAt {
			t.Errorf("[%s] expected compound index {user_id: 1, created_at: -1}, not found", startLabel)
		}
	}

	// 1. First start: drops legacy created_at_-1, creates TTL index and compound filter indexes
	s1, err := NewMongoStore(ctx, mongoURI, dbName)
	if err != nil {
		t.Fatalf("first NewMongoStore: %v", err)
	}
	_ = s1
	verifyIndexes("first-start")

	// 2. Second start: must keep TTL index, old index stays gone, no errors
	s2, err := NewMongoStore(ctx, mongoURI, dbName)
	if err != nil {
		t.Fatalf("second NewMongoStore: %v", err)
	}
	_ = s2
	verifyIndexes("second-start")
}

func bsonKeyString(keys bson.D) string {
	var s string
	for i, elem := range keys {
		if i > 0 {
			s += ","
		}
		s += elem.Key + ":" + bsonValueString(elem.Value)
	}
	return s
}

func bsonValueString(v any) string {
	switch val := v.(type) {
	case int:
		if val < 0 {
			return "-1"
		}
		return "1"
	case int32:
		if val < 0 {
			return "-1"
		}
		return "1"
	case int64:
		if val < 0 {
			return "-1"
		}
		return "1"
	default:
		return "1"
	}
}
