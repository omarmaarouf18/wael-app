// Package store provides user persistence: a Store interface with an
// in-process MemoryStore (tests, localhost dev without mongo) and a
// MongoStore (shared persistence used by the server in compose/prod and by
// the ops CLI tools under cmd/).
package store

import (
	"context"
	"errors"
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
	// ErrInvalidStatus is returned when SetStatus is called with an invalid destination status.
	ErrInvalidStatus = errors.New("store: invalid status")
	// ErrDuplicate is returned when a unique constraint (email or phone) is violated.
	ErrDuplicate = errors.New("store: duplicate key")
)

// Store is the user persistence contract.
type Store interface {
	Create(ctx context.Context, u *models.User) error
	FindByEmail(ctx context.Context, email string) (*models.User, error)
	FindByID(ctx context.Context, id string) (*models.User, error)
	FindByPhone(ctx context.Context, phone string) (*models.User, error)
	Update(ctx context.Context, u *models.User) error
	SetStatus(ctx context.Context, userID, from, to, reason string, at time.Time) error
	Count(ctx context.Context) (int, error)

	IsBlocked(ctx context.Context, kind, hash string) (bool, error)
	AddToBlocklist(ctx context.Context, kind, hash, reason string, at time.Time) error
}

type blockEntry struct {
	kind      string
	hash      string
	reason    string
	createdAt time.Time
}

// MemoryStore is a concurrency-safe in-process Store.
type MemoryStore struct {
	mu        sync.RWMutex
	byID      map[string]*models.User
	byMail    map[string]*models.User
	blocklist map[string]blockEntry
}

// NewMemoryStore creates an empty MemoryStore.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		byID:      map[string]*models.User{},
		byMail:    map[string]*models.User{},
		blocklist: map[string]blockEntry{},
	}
}

func cloneUser(u *models.User) *models.User {
	if u == nil {
		return nil
	}
	cp := *u
	return &cp
}

// Create inserts a new user; emails and active/suspended phones must be unique.
// Status defaults to "active" explicitly for new users.
func (s *MemoryStore) Create(_ context.Context, u *models.User) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if u.Status == "" {
		u.Status = models.StatusActive
	}

	if _, exists := s.byMail[u.Email]; exists {
		return ErrDuplicate
	}
	if _, exists := s.byID[u.ID]; exists {
		return ErrDuplicate
	}

	if u.Phone != "" && (u.EffectiveStatus() == models.StatusActive || u.EffectiveStatus() == models.StatusSuspended) {
		for _, existing := range s.byID {
			if existing.Phone == u.Phone && (existing.EffectiveStatus() == models.StatusActive || existing.EffectiveStatus() == models.StatusSuspended) {
				return ErrDuplicate
			}
		}
	}

	now := time.Now().UTC()
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

// FindByPhone returns an active or suspended user with the given phone, or nil when absent.
func (s *MemoryStore) FindByPhone(_ context.Context, phone string) (*models.User, error) {
	if phone == "" {
		return nil, nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, u := range s.byID {
		if u.Phone == phone && (u.EffectiveStatus() == models.StatusActive || u.EffectiveStatus() == models.StatusSuspended) {
			return cloneUser(u), nil
		}
	}
	return nil, nil
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
			return ErrDuplicate
		}
		delete(s.byMail, existing.Email)
	}
	if u.Phone != "" && u.Phone != existing.Phone && (existing.EffectiveStatus() == models.StatusActive || existing.EffectiveStatus() == models.StatusSuspended) {
		for id, other := range s.byID {
			if id != u.ID && other.Phone == u.Phone && (other.EffectiveStatus() == models.StatusActive || other.EffectiveStatus() == models.StatusSuspended) {
				return ErrDuplicate
			}
		}
	}

	now := time.Now().UTC()
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
// Rejects any `to` status that is not active|suspended|deleted with ErrInvalidStatus.
// When from is "active", a user with an empty status also matches.
// Returns ErrStatusConflict if the current status does not match `from`.
func (s *MemoryStore) SetStatus(_ context.Context, userID, from, to, reason string, at time.Time) error {
	switch models.UserStatus(to) {
	case models.StatusActive, models.StatusSuspended, models.StatusDeleted:
	default:
		return ErrInvalidStatus
	}

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

	if existing.Phone != "" && (models.UserStatus(to) == models.StatusActive || models.UserStatus(to) == models.StatusSuspended) {
		for id, other := range s.byID {
			if id != userID && other.Phone == existing.Phone && (other.EffectiveStatus() == models.StatusActive || other.EffectiveStatus() == models.StatusSuspended) {
				return ErrDuplicate
			}
		}
	}

	if at.IsZero() {
		at = time.Now().UTC()
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

// IsBlocked checks whether the given identity hash (kind: "email"|"phone") is blocklisted.
func (s *MemoryStore) IsBlocked(_ context.Context, kind, hash string) (bool, error) {
	if kind == "" || hash == "" {
		return false, nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, ok := s.blocklist[kind+":"+hash]
	return ok, nil
}

// AddToBlocklist records a blocked identity hash.
func (s *MemoryStore) AddToBlocklist(_ context.Context, kind, hash, reason string, at time.Time) error {
	if kind == "" || hash == "" {
		return errors.New("store: kind and hash are required")
	}
	if at.IsZero() {
		at = time.Now().UTC()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.blocklist[kind+":"+hash] = blockEntry{
		kind:      kind,
		hash:      hash,
		reason:    reason,
		createdAt: at,
	}
	return nil
}
