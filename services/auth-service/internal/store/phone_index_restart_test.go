package store

import (
	"context"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// TestMongoStore_PhoneIndex_TwoStartsInARowNeverDropIndex tests that
// starting the store twice in a row against an existing database keeps the
// verified phone index and never drops it on the second start (Item 1.3).
func TestMongoStore_PhoneIndex_TwoStartsInARowNeverDropIndex(t *testing.T) {
	mongoURI, _ := requireDB(t)
	dbName := randomDBName("test_phone_index_restart")
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

	// Enable profiling level 2 so all commands are logged to system.profile
	var profRes bson.M
	if err := db.RunCommand(ctx, bson.D{{Key: "profile", Value: 2}}).Decode(&profRes); err != nil {
		t.Fatalf("set profiling level 2: %v", err)
	}

	// 1. First start: creates the store and indexes.
	s1, err := NewMongoStore(ctx, mongoURI, dbName)
	if err != nil {
		t.Fatalf("first NewMongoStore: %v", err)
	}
	_ = s1

	// Verify the new index has its dedicated name (not the default phone_1)
	cursor, err := db.Collection("users").Indexes().List(ctx)
	if err != nil {
		t.Fatalf("list indexes: %v", err)
	}
	var indexNames []string
	for cursor.Next(ctx) {
		var doc struct {
			Name string `bson:"name"`
		}
		if err := cursor.Decode(&doc); err != nil {
			t.Fatalf("decode index doc: %v", err)
		}
		indexNames = append(indexNames, doc.Name)
	}
	_ = cursor.Close(ctx)

	hasDedicatedName := false
	for _, n := range indexNames {
		if n == "uniq_phone_verified_v2" {
			hasDedicatedName = true
		}
	}
	if !hasDedicatedName {
		t.Errorf("after first start, expected index 'uniq_phone_verified_v2', found indexes: %v", indexNames)
	}

	// Turn off profiling to drop system.profile, then re-enable for the second start
	if err := db.RunCommand(ctx, bson.D{{Key: "profile", Value: 0}}).Decode(&profRes); err != nil {
		t.Fatalf("profile 0: %v", err)
	}
	_ = db.Collection("system.profile").Drop(ctx)
	if err := db.RunCommand(ctx, bson.D{{Key: "profile", Value: 2}}).Decode(&profRes); err != nil {
		t.Fatalf("profile 2: %v", err)
	}

	// 2. Second start: must KEEP the index and NEVER drop it!
	s2, err := NewMongoStore(ctx, mongoURI, dbName)
	if err != nil {
		t.Fatalf("second NewMongoStore: %v", err)
	}
	_ = s2

	// Check if any dropIndexes command succeeded on users during the second start
	cur, err := db.Collection("system.profile").Find(ctx, bson.M{
		"command.dropIndexes": "users",
		"ok":                  bson.M{"$ne": 0},
	})
	if err != nil {
		t.Fatalf("query system.profile: %v", err)
	}
	var dropped []bson.M
	if err := cur.All(ctx, &dropped); err != nil {
		t.Fatalf("decode system.profile dropped: %v", err)
	}

	if len(dropped) > 0 {
		t.Fatalf("second start dropped an index on users: %+v", dropped)
	}
}
