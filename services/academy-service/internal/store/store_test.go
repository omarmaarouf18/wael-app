package store

import (
	"context"
	"testing"
	"time"
)

func runStoreSuite(t *testing.T, s Store) {
	ctx := context.Background()
	if err := s.Ping(ctx); err != nil {
		t.Fatalf("Ping: %v", err)
	}
	if err := s.EnsureIndexes(ctx); err != nil {
		t.Fatalf("EnsureIndexes: %v", err)
	}
	if err := s.Close(ctx); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

func TestMemoryStore(t *testing.T) {
	s := NewMemoryStore()
	runStoreSuite(t, s)
}

func TestMongoStore(t *testing.T) {
	mongoURI := requireDB(t)
	dbName := randomDBName("test_academy_store")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	s, err := NewMongoStore(ctx, mongoURI, dbName)
	if err != nil {
		t.Fatalf("NewMongoStore failed: %v", err)
	}
	t.Cleanup(func() {
		dropCtx, dropCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer dropCancel()
		_ = s.Database().Drop(dropCtx)
		_ = s.Close(dropCtx)
	})

	if err := s.Ping(ctx); err != nil {
		t.Fatalf("Ping failed: %v", err)
	}
	if err := s.EnsureIndexes(ctx); err != nil {
		t.Fatalf("EnsureIndexes failed: %v", err)
	}
}
