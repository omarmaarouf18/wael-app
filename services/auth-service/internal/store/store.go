// Package store provides user persistence: a Store interface with an
// in-process MemoryStore (tests, localhost dev without mongo) and a
// MongoStore (shared persistence used by the server in compose/prod and by
// the ops CLI tools under cmd/).
package store

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/omarmaarouf18/wael-app/auth-service/internal/models"
)

var (
	// ErrUserNotFound is returned when an operation references a non-existent user.
	ErrUserNotFound = errors.New("store: user not found")
	// ErrStatusConflict is returned when SetStatus compare-and-set fails because
	// the user's current status does not match the expected `from` status.
	ErrStatusConflict = errors.New("store: status conflict")
)

// Store is the user persistence contract.
type Store interface {
	Create(ctx context.Context, u *models.User) error
	FindByEmail(ctx context.Context, email string) (*models.User, error)
	FindByID(ctx context.Context, id string) (*models.User, error)
	Update(ctx context.Context, u *models.User) error
	SetStatus(ctx context.Context, userID, from, to, reason string, at time.Time) error
	Count(ctx context.Context) (int, error)
}

// MemoryStore is a concurrency-safe in-process Store.
type MemoryStore struct {
	mu     sync.RWMutex
	byID   map[string]*models.User
	byMail map[string]*models.User
}

// NewMemoryStore creates an empty MemoryStore.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{byID: map[string]*models.User{}, byMail: map[string]*models.User{}}
}

func cloneUser(u *models.User) *models.User {
	if u == nil {
		return nil
	}
	cp := *u
	return &cp
}

// Create inserts a new user; emails must be unique (case-sensitive; callers lowercase first).
func (s *MemoryStore) Create(_ context.Context, u *models.User) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.byMail[u.Email]; exists {
		return fmt.Errorf("store: email already registered")
	}
	if _, exists := s.byID[u.ID]; exists {
		return fmt.Errorf("store: id already exists")
	}
	now := time.Now()
	if u.CreatedAt.IsZero() {
		u.CreatedAt = now
	}
	u.UpdatedAt = now
	cp := cloneUser(u)
	s.byID[u.ID] = cp
	s.byMail[u.Email] = cp
	return nil
}

// FindByEmail returns a copy of the user with the given email, or nil when absent.
func (s *MemoryStore) FindByEmail(_ context.Context, email string) (*models.User, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return cloneUser(s.byMail[email]), nil
}

// FindByID returns a copy of the user with the given id, or nil when absent.
func (s *MemoryStore) FindByID(_ context.Context, id string) (*models.User, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return cloneUser(s.byID[id]), nil
}

// Update replaces non-status fields of the stored user record.
// Status fields are never modified by Update to prevent lost updates (finding P-1).
func (s *MemoryStore) Update(_ context.Context, u *models.User) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	existing, ok := s.byID[u.ID]
	if !ok {
		return ErrUserNotFound
	}
	if u.Email != existing.Email {
		if _, taken := s.byMail[u.Email]; taken {
			return fmt.Errorf("store: email already registered")
		}
		delete(s.byMail, existing.Email)
	}
	now := time.Now()
	u.UpdatedAt = now

	cp := cloneUser(u)
	// Preserve existing status fields to avoid overwriting concurrent status changes
	cp.Status = existing.Status
	cp.StatusReason = existing.StatusReason
	cp.SuspendedAt = existing.SuspendedAt
	cp.ReactivatedAt = existing.ReactivatedAt
	cp.DeletedAt = existing.DeletedAt
	cp.UpdatedAt = now

	s.byID[u.ID] = cp
	s.byMail[u.Email] = cp
	return nil
}

// SetStatus performs an atomic compare-and-set of the user's status under mutex lock.
// When from is "active", a user with an empty status also matches.
// Returns ErrStatusConflict if the current status does not match `from`.
func (s *MemoryStore) SetStatus(_ context.Context, userID, from, to, reason string, at time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	existing, ok := s.byID[userID]
	if !ok {
		return ErrUserNotFound
	}

	currentStatus := string(existing.EffectiveStatus())
	expectedFrom := from
	if expectedFrom == "" {
		expectedFrom = string(models.StatusActive)
	}

	if currentStatus != expectedFrom {
		return ErrStatusConflict
	}

	if at.IsZero() {
		at = time.Now()
	}

	cp := cloneUser(existing)
	cp.Status = models.UserStatus(to)
	cp.StatusReason = reason
	cp.UpdatedAt = at
	switch models.UserStatus(to) {
	case models.StatusSuspended:
		cp.SuspendedAt = at
	case models.StatusActive:
		cp.ReactivatedAt = at
	case models.StatusDeleted:
		cp.DeletedAt = at
	}

	s.byID[userID] = cp
	s.byMail[cp.Email] = cp
	return nil
}

// Count returns the number of stored users.
func (s *MemoryStore) Count(_ context.Context) (int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.byID), nil
}
