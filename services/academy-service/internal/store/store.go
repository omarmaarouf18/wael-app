// Package store provides persistence for academy-service: a Store interface,
// an in-process MemoryStore (testing and localhost dev), and a MongoStore
// (shared MongoDB persistence).
package store

import (
	"context"
	"errors"
	"sort"
	"sync"

	"github.com/omarmaarouf18/wael-app/academy-service/internal/models"
)

// Common store errors.
var (
	ErrDuplicate = errors.New("store: duplicate key")
	ErrNotFound  = errors.New("store: not found")
)

// Store is the academy persistence contract.
type Store interface {
	Ping(ctx context.Context) error
	Close(ctx context.Context) error
	EnsureIndexes(ctx context.Context) error
	SeedLevels(ctx context.Context) error
	ListLevels(ctx context.Context, onlyWithPublishedSubjects bool) ([]*models.Level, error)
}

// MemoryStore is an in-memory Store for local dev and unit testing.
type MemoryStore struct {
	mu              sync.RWMutex
	levels          map[string]*models.Level
	publishedLevels map[string]bool
}

// NewMemoryStore creates an empty MemoryStore.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		levels:          make(map[string]*models.Level),
		publishedLevels: make(map[string]bool),
	}
}

// Ping checks store availability (always nil for memory store).
func (s *MemoryStore) Ping(_ context.Context) error {
	return nil
}

// Close releases store resources (noop for memory store).
func (s *MemoryStore) Close(_ context.Context) error {
	return nil
}

// EnsureIndexes creates nothing yet in MemoryStore.
func (s *MemoryStore) EnsureIndexes(_ context.Context) error {
	return nil
}

// SeedLevels idempotently initializes the 5 fixed academic levels.
func (s *MemoryStore) SeedLevels(_ context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, lvl := range models.SeededLevels {
		if _, exists := s.levels[lvl.Key]; !exists {
			cp := lvl
			s.levels[lvl.Key] = &cp
		}
	}
	return nil
}

// SetLevelPublished marks a level as having published subjects (for testing and memory store).
func (s *MemoryStore) SetLevelPublished(levelKey string, published bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.publishedLevels[levelKey] = published
}

// ListLevels returns levels sorted by position. If onlyWithPublished is true,
// levels with no published subjects are omitted.
func (s *MemoryStore) ListLevels(_ context.Context, onlyWithPublished bool) ([]*models.Level, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]*models.Level, 0, len(s.levels))
	for _, l := range s.levels {
		if onlyWithPublished && !s.publishedLevels[l.Key] {
			continue
		}
		cp := *l
		result = append(result, &cp)
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].Position < result[j].Position
	})

	return result, nil
}
