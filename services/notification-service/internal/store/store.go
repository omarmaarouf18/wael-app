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
	CreateBroadcast(ctx context.Context, b *models.Broadcast) error
	GetBroadcast(ctx context.Context, id string) (*models.Broadcast, error)
	ListBroadcasts(ctx context.Context, page, limit int) ([]*models.Broadcast, int, error)
	UpdateBroadcast(ctx context.Context, b *models.Broadcast) error
	UpsertBroadcastTarget(ctx context.Context, t *models.BroadcastTarget) (bool, error)
	ListPendingTargets(ctx context.Context, broadcastID string, limit int) ([]*models.BroadcastTarget, error)
	MarkBroadcastTarget(ctx context.Context, broadcastID, userID, status, lastErr string) error
	ResetFailedTargets(ctx context.Context, broadcastID string) (int, error)
	CountTargets(ctx context.Context, broadcastID, status string) (int, error)
	CreateAuditLog(ctx context.Context, entry *models.AuditLog) error
	ListAuditLogs(ctx context.Context, page, limit int) ([]*models.AuditLog, int, error)
}

// MemoryStore is a concurrency-safe in-process Store.
type MemoryStore struct {
	mu        sync.RWMutex
	data      map[string]*models.Notification
	broadcast map[string]*models.Broadcast
	targets   map[string]*models.BroadcastTarget
	auditLogs []*models.AuditLog
}

// NewMemoryStore creates an empty MemoryStore.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		data:      map[string]*models.Notification{},
		broadcast: map[string]*models.Broadcast{},
		targets:   map[string]*models.BroadcastTarget{},
	}
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

// TargetID returns the deterministic id for one (broadcast, user) target.
func TargetID(broadcastID, userID string) string {
	return broadcastID + ":" + userID
}

// CreateBroadcast inserts a broadcast record.
func (s *MemoryStore) CreateBroadcast(_ context.Context, b *models.Broadcast) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.broadcast[b.ID]; exists {
		return fmt.Errorf("store: broadcast already exists")
	}
	if b.CreatedAt.IsZero() {
		b.CreatedAt = time.Now()
	}
	cp := *b
	s.broadcast[b.ID] = &cp
	return nil
}

// GetBroadcast returns one broadcast by id, or an error when missing.
func (s *MemoryStore) GetBroadcast(_ context.Context, id string) (*models.Broadcast, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	b, ok := s.broadcast[id]
	if !ok {
		return nil, fmt.Errorf("store: broadcast not found")
	}
	cp := *b
	return &cp, nil
}

// ListBroadcasts returns broadcasts newest-first, paginated (page from 1),
// plus the total count.
func (s *MemoryStore) ListBroadcasts(_ context.Context, page, limit int) ([]*models.Broadcast, int, error) {
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 100 {
		limit = 20
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	all := make([]*models.Broadcast, 0, len(s.broadcast))
	for _, b := range s.broadcast {
		cp := *b
		all = append(all, &cp)
	}
	sort.Slice(all, func(i, j int) bool {
		if all[i].CreatedAt.Equal(all[j].CreatedAt) {
			return all[i].ID > all[j].ID
		}
		return all[i].CreatedAt.After(all[j].CreatedAt)
	})
	total := len(all)
	start := (page - 1) * limit
	if start >= total {
		return []*models.Broadcast{}, total, nil
	}
	end := start + limit
	if end > total {
		end = total
	}
	return all[start:end], total, nil
}

// UpdateBroadcast replaces the stored broadcast record.
func (s *MemoryStore) UpdateBroadcast(_ context.Context, b *models.Broadcast) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.broadcast[b.ID]; !ok {
		return fmt.Errorf("store: broadcast not found")
	}
	cp := *b
	s.broadcast[b.ID] = &cp
	return nil
}

// UpsertBroadcastTarget inserts a pending target; an existing row is left
// untouched so resolution and retries stay idempotent. It reports whether
// the row was inserted.
func (s *MemoryStore) UpsertBroadcastTarget(_ context.Context, t *models.BroadcastTarget) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.targets[t.ID]; exists {
		return false, nil
	}
	if t.UpdatedAt.IsZero() {
		t.UpdatedAt = time.Now()
	}
	cp := *t
	s.targets[t.ID] = &cp
	return true, nil
}

// ListPendingTargets returns up to limit pending targets for a broadcast,
// ordered by user id for stable paging.
func (s *MemoryStore) ListPendingTargets(_ context.Context, broadcastID string, limit int) ([]*models.BroadcastTarget, error) {
	if limit < 1 || limit > 1000 {
		limit = 100
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []*models.BroadcastTarget
	for _, t := range s.targets {
		if t.BroadcastID == broadcastID && t.Status == models.TargetPending {
			cp := *t
			out = append(out, &cp)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].UserID < out[j].UserID })
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// MarkBroadcastTarget sets a target's status (sent or failed).
func (s *MemoryStore) MarkBroadcastTarget(_ context.Context, broadcastID, userID, status, lastErr string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.targets[TargetID(broadcastID, userID)]
	if !ok {
		return fmt.Errorf("store: broadcast target not found")
	}
	t.Status = status
	t.Attempts++
	t.LastError = lastErr
	t.UpdatedAt = time.Now()
	return nil
}

// ResetFailedTargets moves every failed target of a broadcast back to
// pending and reports how many were reset.
func (s *MemoryStore) ResetFailedTargets(_ context.Context, broadcastID string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, t := range s.targets {
		if t.BroadcastID == broadcastID && t.Status == models.TargetFailed {
			t.Status = models.TargetPending
			t.LastError = ""
			t.UpdatedAt = time.Now()
			n++
		}
	}
	return n, nil
}

// CountTargets counts a broadcast's targets in one status.
func (s *MemoryStore) CountTargets(_ context.Context, broadcastID, status string) (int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	n := 0
	for _, t := range s.targets {
		if t.BroadcastID == broadcastID && t.Status == status {
			n++
		}
	}
	return n, nil
}

// CreateAuditLog records an admin action. It assigns an id and timestamp
// when empty, mirroring auth-service and academy-service semantics.
func (s *MemoryStore) CreateAuditLog(_ context.Context, entry *models.AuditLog) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if entry.ID == "" {
		entry.ID = fmt.Sprintf("audit-%d", len(s.auditLogs)+1)
	}
	if entry.CreatedAt.IsZero() {
		entry.CreatedAt = time.Now().UTC()
	}
	cp := *entry
	s.auditLogs = append(s.auditLogs, &cp)
	return nil
}

// ListAuditLogs returns audit entries newest-first, paginated (page from 1),
// plus the total count.
func (s *MemoryStore) ListAuditLogs(_ context.Context, page, limit int) ([]*models.AuditLog, int, error) {
	if page < 1 {
		page = 1
	} else if page > 10000 {
		page = 10000
	}
	if limit <= 0 {
		limit = 20
	} else if limit > 100 {
		limit = 100
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	logs := make([]*models.AuditLog, len(s.auditLogs))
	for i, l := range s.auditLogs {
		cp := *l
		logs[i] = &cp
	}
	sort.Slice(logs, func(i, j int) bool {
		if logs[i].CreatedAt.Equal(logs[j].CreatedAt) {
			return logs[i].ID > logs[j].ID
		}
		return logs[i].CreatedAt.After(logs[j].CreatedAt)
	})
	total := len(logs)
	start := (page - 1) * limit
	if start >= total {
		return []*models.AuditLog{}, total, nil
	}
	end := start + limit
	if end > total {
		end = total
	}
	return logs[start:end], total, nil
}
