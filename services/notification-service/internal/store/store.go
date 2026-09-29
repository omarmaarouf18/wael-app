// Package store persists notifications: Store interface with MemoryStore
// (tests, localhost without mongo) and MongoStore (shared persistence).
package store

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/omarmaarouf18/wael-app/notification-service/internal/models"
)

// Store is the notification persistence contract.
type Store interface {
	Create(ctx context.Context, n *models.Notification) error
	List(ctx context.Context, userID string, page, limit int) ([]*models.Notification, error)
	MarkRead(ctx context.Context, userID, id string) error
}

// MemoryStore is a concurrency-safe in-process Store.
type MemoryStore struct {
	mu   sync.RWMutex
	data map[string]*models.Notification
}

// NewMemoryStore creates an empty MemoryStore.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{data: map[string]*models.Notification{}}
}

func cloneNotif(n *models.Notification) *models.Notification {
	if n == nil {
		return nil
	}
	cp := *n
	return &cp
}

// Create inserts a notification.
func (s *MemoryStore) Create(_ context.Context, n *models.Notification) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.data[n.ID]; exists {
		return fmt.Errorf("store: notification already exists")
	}
	if n.CreatedAt.IsZero() {
		n.CreatedAt = time.Now()
	}
	s.data[n.ID] = cloneNotif(n)
	return nil
}

// List returns the user's notifications newest-first, paginated (page from 1).
func (s *MemoryStore) List(_ context.Context, userID string, page, limit int) ([]*models.Notification, error) {
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 100 {
		limit = 20
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	var all []*models.Notification
	for _, n := range s.data {
		if n.UserID == userID {
			all = append(all, cloneNotif(n))
		}
	}
	sort.Slice(all, func(i, j int) bool {
		if all[i].CreatedAt.Equal(all[j].CreatedAt) {
			return all[i].ID > all[j].ID
		}
		return all[i].CreatedAt.After(all[j].CreatedAt)
	})
	start := (page - 1) * limit
	if start >= len(all) {
		return []*models.Notification{}, nil
	}
	end := start + limit
	if end > len(all) {
		end = len(all)
	}
	return all[start:end], nil
}

// MarkRead flags the user's notification as read.
func (s *MemoryStore) MarkRead(_ context.Context, userID, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	n, ok := s.data[id]
	if !ok || n.UserID != userID {
		return fmt.Errorf("store: notification not found")
	}
	n.Read = true
	return nil
}
