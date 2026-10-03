// Package store provides user persistence: a Store interface with an
// in-process MemoryStore (tests, localhost dev without mongo) and a
// MongoStore (shared persistence used by the server in compose/prod and by
// the ops CLI tools under cmd/).
package store

import (
	"context"
	"errors"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/omarmaarouf18/wael-app/auth-service/internal/models"
	"github.com/omarmaarouf18/wael-app/shared/infra/jwtutil"
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
	// ErrAdminNotFound is returned when an operation references a non-existent admin.
	ErrAdminNotFound = errors.New("store: admin not found")
)

// FromActiveOrSuspended matches active (incl. legacy empty) or suspended.
const FromActiveOrSuspended = "active|suspended"

// UserFilter specifies filtering and pagination parameters for ListUsers.
type UserFilter struct {
	Search          string
	NormalizedPhone string
	Status          string
	Page            int
	Limit           int
}

// Store is the user persistence contract.
type Store interface {
	Create(ctx context.Context, u *models.User) error
	FindByEmail(ctx context.Context, email string) (*models.User, error)
	FindByID(ctx context.Context, id string) (*models.User, error)
	FindByPhone(ctx context.Context, phone string) (*models.User, error)
	Update(ctx context.Context, u *models.User) error
	SetStatus(ctx context.Context, userID, from, to, reason string, at time.Time) error
	Count(ctx context.Context) (int, error)
	ListUsers(ctx context.Context, filter UserFilter) ([]*models.User, int, error)

	IsBlocked(ctx context.Context, kind, hash string) (bool, error)
	AddToBlocklist(ctx context.Context, kind, hash, reason string, at time.Time) error

	CreateAdmin(ctx context.Context, a *models.Admin) error
	FindAdminByTokenHash(ctx context.Context, tokenHash string) (*models.Admin, error)
	FindAdminByID(ctx context.Context, id string) (*models.Admin, error)
	RevokeAdmin(ctx context.Context, id string, at time.Time) error

	CreateAuditLog(ctx context.Context, entry *models.AuditLog) error
	ListAuditLogs(ctx context.Context, page, limit int) ([]*models.AuditLog, int, error)

	// Sessions (Phase 1.7)
	CreateOrReplaceSession(ctx context.Context, s *models.Session) ([]*models.Session, error)
	GetSession(ctx context.Context, sid string) (*models.Session, error)
	FindSessionByRefreshHash(ctx context.Context, refreshHash string) (*models.Session, error)
	UpdateSessionActivity(ctx context.Context, sid string, refreshHash string, lastUsedAt time.Time) error
	EndSession(ctx context.Context, sid string, reason models.SessionEndReason, at time.Time) error
	EndAllUserSessions(ctx context.Context, userID string, reason models.SessionEndReason, at time.Time) ([]*models.Session, error)
	ListActiveSessions(ctx context.Context, userID string) ([]*models.Session, error)
}

type blockEntry struct {
	kind      string
	hash      string
	reason    string
	createdAt time.Time
}

// MemoryStore is a concurrency-safe in-process Store.
type MemoryStore struct {
	mu           sync.RWMutex
	byID         map[string]*models.User
	byMail       map[string]*models.User
	blocklist    map[string]blockEntry
	adminsByID   map[string]*models.Admin
	adminsByHash map[string]*models.Admin
	auditLogs    []*models.AuditLog
	sessions     map[string]*models.Session
}

// NewMemoryStore creates an empty MemoryStore.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		byID:         map[string]*models.User{},
		byMail:       map[string]*models.User{},
		blocklist:    map[string]blockEntry{},
		adminsByID:   map[string]*models.Admin{},
		adminsByHash: map[string]*models.Admin{},
		auditLogs:    []*models.AuditLog{},
		sessions:     map[string]*models.Session{},
	}
}

func cloneUser(u *models.User) *models.User {
	if u == nil {
		return nil
	}
	cp := *u
	return &cp
}

// Create inserts a new user; emails must be unique, and phones must be unique
// across verified active/suspended accounts (unverified signups do not reserve
// phones). Status defaults to "active" explicitly for new users.
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
			if existing.Phone == u.Phone && existing.EmailVerified && (existing.EffectiveStatus() == models.StatusActive || existing.EffectiveStatus() == models.StatusSuspended) {
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
	if u.Phone != "" && (existing.EffectiveStatus() == models.StatusActive || existing.EffectiveStatus() == models.StatusSuspended) {
		// Enforce verified-phone uniqueness when the record will be verified
		// (e.g. OTP verification promotes an unverified record while its phone
		// is unchanged) or when an unverified record takes a new phone.
		// Verified holders block; unverified holders never block.
		if u.EmailVerified || u.Phone != existing.Phone {
			for id, other := range s.byID {
				if id != u.ID && other.Phone == u.Phone && other.EmailVerified && (other.EffectiveStatus() == models.StatusActive || other.EffectiveStatus() == models.StatusSuspended) {
					return ErrDuplicate
				}
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
	if from == FromActiveOrSuspended {
		if currentStatus != string(models.StatusActive) && currentStatus != string(models.StatusSuspended) {
			return ErrStatusConflict
		}
	} else {
		expectedFrom := from
		if expectedFrom == "" {
			expectedFrom = string(models.StatusActive)
		}

		if currentStatus != expectedFrom {
			return ErrStatusConflict
		}
	}

	if existing.Phone != "" && (models.UserStatus(to) == models.StatusActive || models.UserStatus(to) == models.StatusSuspended) {
		for id, other := range s.byID {
			if id != userID && other.Phone == existing.Phone && other.EmailVerified && (other.EffectiveStatus() == models.StatusActive || other.EffectiveStatus() == models.StatusSuspended) {
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

func cloneAdmin(a *models.Admin) *models.Admin {
	if a == nil {
		return nil
	}
	cp := *a
	return &cp
}

// CreateAdmin inserts a new admin identity; token_hash and id must be unique.
func (s *MemoryStore) CreateAdmin(_ context.Context, a *models.Admin) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.adminsByHash[a.TokenHash]; exists {
		return ErrDuplicate
	}
	if _, exists := s.adminsByID[a.ID]; exists {
		return ErrDuplicate
	}
	now := time.Now().UTC()
	if a.CreatedAt.IsZero() {
		a.CreatedAt = now
	}
	cp := cloneAdmin(a)
	s.adminsByID[a.ID] = cp
	s.adminsByHash[a.TokenHash] = cp
	return nil
}

// FindAdminByTokenHash looks up an admin by the SHA-256 hash of their token.
func (s *MemoryStore) FindAdminByTokenHash(_ context.Context, tokenHash string) (*models.Admin, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return cloneAdmin(s.adminsByHash[tokenHash]), nil
}

// FindAdminByID looks up an admin by id.
func (s *MemoryStore) FindAdminByID(_ context.Context, id string) (*models.Admin, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return cloneAdmin(s.adminsByID[id]), nil
}

// RevokeAdmin sets revoked_at timestamp for the admin. Idempotent.
func (s *MemoryStore) RevokeAdmin(_ context.Context, id string, at time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	adm, exists := s.adminsByID[id]
	if !exists {
		return ErrAdminNotFound
	}
	adm.RevokedAt = at
	return nil
}

// ListUsers returns paginated users matching filter criteria.
func (s *MemoryStore) ListUsers(_ context.Context, filter UserFilter) ([]*models.User, int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var matched []*models.User
	searchLower := strings.ToLower(filter.Search)

	for _, u := range s.byID {
		if filter.Status != "" {
			if string(u.EffectiveStatus()) != filter.Status {
				continue
			}
		}

		if filter.Search != "" {
			matches := u.ID == filter.Search ||
				strings.Contains(strings.ToLower(u.FullName), searchLower) ||
				strings.Contains(strings.ToLower(u.Email), searchLower) ||
				strings.Contains(u.Phone, filter.Search) ||
				(filter.NormalizedPhone != "" && u.Phone == filter.NormalizedPhone)
			if !matches {
				continue
			}
		}

		matched = append(matched, cloneUser(u))
	}

	sort.Slice(matched, func(i, j int) bool {
		return matched[i].CreatedAt.After(matched[j].CreatedAt)
	})

	total := len(matched)
	page := filter.Page
	if page < 1 {
		page = 1
	}
	limit := filter.Limit
	if limit <= 0 {
		limit = 20
	} else if limit > 100 {
		limit = 100
	}

	start := (page - 1) * limit
	if start > total {
		start = total
	}
	end := start + limit
	if end > total {
		end = total
	}

	return matched[start:end], total, nil
}

// CreateAuditLog records an admin action in admin_audit_log.
func (s *MemoryStore) CreateAuditLog(_ context.Context, entry *models.AuditLog) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if entry.ID == "" {
		id, err := jwtutil.GenerateUUID()
		if err != nil {
			return err
		}
		entry.ID = id
	}
	if entry.CreatedAt.IsZero() {
		entry.CreatedAt = time.Now().UTC()
	}
	cp := *entry
	s.auditLogs = append(s.auditLogs, &cp)
	return nil
}

// ListAuditLogs returns paginated audit log entries, newest first.
func (s *MemoryStore) ListAuditLogs(_ context.Context, page, limit int) ([]*models.AuditLog, int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	logs := make([]*models.AuditLog, len(s.auditLogs))
	for i, l := range s.auditLogs {
		cp := *l
		logs[i] = &cp
	}

	sort.Slice(logs, func(i, j int) bool {
		return logs[i].CreatedAt.After(logs[j].CreatedAt)
	})

	total := len(logs)
	if page < 1 {
		page = 1
	}
	if limit <= 0 {
		limit = 20
	} else if limit > 100 {
		limit = 100
	}

	start := (page - 1) * limit
	if start > total {
		start = total
	}
	end := start + limit
	if end > total {
		end = total
	}

	return logs[start:end], total, nil
}

func cloneSession(s *models.Session) *models.Session {
	if s == nil {
		return nil
	}
	cp := *s
	if s.EndedAt != nil {
		t := *s.EndedAt
		cp.EndedAt = &t
	}
	return &cp
}

// CreateOrReplaceSession inserts a new session, replaces any existing active session for (user_id, device_id),
// and ends any active sessions beyond the newest 2 by last_used_at. Returns all ended sessions.
func (s *MemoryStore) CreateOrReplaceSession(_ context.Context, sess *models.Session) ([]*models.Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var ended []*models.Session
	// 1. If an active session exists for this (user_id, device_id), end it.
	for _, existing := range s.sessions {
		if existing.UserID == sess.UserID && existing.DeviceID == sess.DeviceID && existing.EndedAt == nil {
			t := sess.CreatedAt
			existing.EndedAt = &t
			existing.EndReason = models.EndReasonReplaced
			ended = append(ended, cloneSession(existing))
		}
	}

	// 2. Insert new session
	s.sessions[sess.ID] = cloneSession(sess)

	// 3. Re-read all active sessions for this user, sorted by last_used_at DESC, created_at DESC, ID DESC
	var active []*models.Session
	for _, existing := range s.sessions {
		if existing.UserID == sess.UserID && existing.EndedAt == nil {
			active = append(active, existing)
		}
	}
	sort.Slice(active, func(i, j int) bool {
		if !active[i].LastUsedAt.Equal(active[j].LastUsedAt) {
			return active[i].LastUsedAt.After(active[j].LastUsedAt)
		}
		if !active[i].CreatedAt.Equal(active[j].CreatedAt) {
			return active[i].CreatedAt.After(active[j].CreatedAt)
		}
		return active[i].ID > active[j].ID
	})

	// 4. End sessions beyond the newest 2
	if len(active) > 2 {
		for _, excess := range active[2:] {
			t := sess.CreatedAt
			excess.EndedAt = &t
			excess.EndReason = models.EndReasonReplaced
			ended = append(ended, cloneSession(excess))
		}
	}

	return ended, nil
}

// GetSession returns a session by sid.
func (s *MemoryStore) GetSession(_ context.Context, sid string) (*models.Session, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	sess, ok := s.sessions[sid]
	if !ok {
		return nil, nil
	}
	return cloneSession(sess), nil
}

// FindSessionByRefreshHash returns a session matching refreshHash.
func (s *MemoryStore) FindSessionByRefreshHash(_ context.Context, refreshHash string) (*models.Session, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, sess := range s.sessions {
		if sess.RefreshHash == refreshHash {
			return cloneSession(sess), nil
		}
	}
	return nil, nil
}

// UpdateSessionActivity updates last_used_at and refresh_hash for a session.
func (s *MemoryStore) UpdateSessionActivity(_ context.Context, sid string, refreshHash string, lastUsedAt time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, ok := s.sessions[sid]
	if !ok {
		return nil
	}
	sess.LastUsedAt = lastUsedAt
	sess.RefreshHash = refreshHash
	return nil
}

// EndSession marks a session as ended.
func (s *MemoryStore) EndSession(_ context.Context, sid string, reason models.SessionEndReason, at time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, ok := s.sessions[sid]
	if !ok {
		return nil
	}
	if sess.EndedAt == nil {
		t := at
		sess.EndedAt = &t
		sess.EndReason = reason
	}
	return nil
}

// EndAllUserSessions terminates all active sessions for a user.
func (s *MemoryStore) EndAllUserSessions(_ context.Context, userID string, reason models.SessionEndReason, at time.Time) ([]*models.Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var ended []*models.Session
	for _, sess := range s.sessions {
		if sess.UserID == userID && sess.EndedAt == nil {
			t := at
			sess.EndedAt = &t
			sess.EndReason = reason
			ended = append(ended, cloneSession(sess))
		}
	}
	return ended, nil
}

// ListActiveSessions returns all currently active sessions for a user, sorted by last_used_at DESC.
func (s *MemoryStore) ListActiveSessions(_ context.Context, userID string) ([]*models.Session, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var active []*models.Session
	for _, sess := range s.sessions {
		if sess.UserID == userID && sess.EndedAt == nil {
			active = append(active, cloneSession(sess))
		}
	}
	sort.Slice(active, func(i, j int) bool {
		if !active[i].LastUsedAt.Equal(active[j].LastUsedAt) {
			return active[i].LastUsedAt.After(active[j].LastUsedAt)
		}
		return active[i].CreatedAt.After(active[j].CreatedAt)
	})
	return active, nil
}
