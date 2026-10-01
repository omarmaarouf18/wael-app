// Package store provides persistence for academy-service: a Store interface,
// an in-process MemoryStore (testing and localhost dev), and a MongoStore
// (shared MongoDB persistence).
package store

import (
	"context"
	"sync"
)

// Store is the academy persistence contract.
// Methods are added as domain phases are implemented.
type Store interface {
	Ping(ctx context.Context) error
	Close(ctx context.Context) error
	EnsureIndexes(ctx context.Context) error
}

// MemoryStore is an in-memory Store for local dev and unit testing.
type MemoryStore struct {
	mu sync.RWMutex
}

// NewMemoryStore creates an empty MemoryStore.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{}
}

// Ping checks store availability (always nil for memory store).
func (s *MemoryStore) Ping(_ context.Context) error {
	return nil
}

// Close releases store resources (noop for memory store).
func (s *MemoryStore) Close(_ context.Context) error {
	return nil
}

// EnsureIndexes creates nothing yet in Phase 2.1 (no collections invented).
func (s *MemoryStore) EnsureIndexes(_ context.Context) error {
	return nil
}
