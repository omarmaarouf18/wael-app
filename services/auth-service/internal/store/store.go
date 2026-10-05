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
	// ErrPhoneTaken is returned when an operation would leave two accounts
	// holding one phone (e.g. restoring a pending_deletion account whose
	// phone was taken in the meantime). Distinct from ErrDuplicate so callers
	// can answer without a retry loop.
	ErrPhoneTaken = errors.New("store: phone taken")
	// ErrChangeTooSoon is returned when a 30-day-limited profile field is
	// changed too soon.
	ErrChangeTooSoon = errors.New("store: change too soon")
	// ErrDuplicate is returned when a unique constraint (email or phone) is violated.
	ErrDuplicate = errors.New("store: duplicate key")
	// ErrAdminNotFound is returned when an operation references a non-existent admin.
	ErrAdminNotFound = errors.New("store: admin not found")
)

// ProfileChangeCooldown is the owner-decided per-field profile edit limit
// (F-UX2): name and phone each change at most once per 30 days.
const ProfileChangeCooldown = 30 * 24 * time.Hour

// ProfileFields describes one atomic self-service profile edit. A nil
// pointer means "don't change this field".
type ProfileFields struct {
	Name  *string
	Phone *string
}

// FromActiveOrSuspended matches active (incl. legacy empty) or suspended.
const FromActiveOrSuspended = "active|suspended"

// UserFilter specifies filtering and pagination parameters for ListUsers.
type UserFilter struct {
	Search          string
	NormalizedPhone string
	Status          string
	IDs             []string
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
	EndAllUserSessionsExcept(ctx context.Context, userID, exceptSID string, reason models.SessionEndReason, at time.Time) ([]*models.Session, error)
	ListActiveSessions(ctx context.Context, userID string) ([]*models.Session, error)

	// Self-service account events (F-UX2): one append-only entry per change,
	// carrying user_id, type and created_at only (no PII values).
	CreateAccountEvent(ctx context.Context, e *models.AccountEvent) error

	// Self-deletion grace period (F-UX2, 30 days). All three are atomic
	// compare-and-set operations on status, so a login that races the purge
	// cannot restore a half-purged account and vice versa.
	RequestDeletion(ctx context.Context, userID string, at, purgeAfter time.Time) error
	CancelDeletion(ctx context.Context, userID string, at time.Time) error
	PurgeDeletion(ctx context.Context, userID, anonymizedEmail string, at time.Time) error
	ListDeletionsDue(ctx context.Context, now time.Time) ([]*models.User, error)

	// Targeted self-service writes (F-UX2 review): each $set-touches only the
	// fields its action changes and matches only when the account is active,
	// so concurrent actions cannot clobber each other's fields. A status
	// mismatch surfaces as ErrStatusConflict (callers answer 401 when the
	// account is gone or no longer active, else 409).
	UpdatePassword(ctx context.Context, userID, hash string, at time.Time) error
	UpdateProfileFields(ctx context.Context, userID string, f ProfileFields, at time.Time) error
	SetEmail(ctx context.Context, userID, oldEmail, newEmail string, at time.Time) error
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
	accountEvts  []*models.AccountEvent
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
		accountEvts:  []*models.AccountEvent{},
	}
}

func cloneUser(u *models.User) *models.User {
	if u == nil {
		return nil
	}
	cp := *u
	return &cp
}

// reservesPhone reports whether a status reserves its phone number: active,
// suspended, and pending_deletion accounts all hold their identifiers until
// deletion is final (owner decision D2, F-UX2 review). Deleted and purged
// accounts free them.
func reservesPhone(st models.UserStatus) bool {
	switch st {
	case models.StatusActive, models.StatusSuspended, models.StatusPendingDeletion:
		return true
	default:
		return false
	}
}

// Create inserts a new user; emails must be unique, and phones must be unique
// across verified phone-reserving accounts (unverified signups do not reserve
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

	if u.Phone != "" && reservesPhone(u.EffectiveStatus()) {
		for _, existing := range s.byID {
			if existing.Phone == u.Phone && existing.EmailVerified && reservesPhone(existing.EffectiveStatus()) {
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

// FindByPhone returns a phone-reserving (active, suspended, or
// pending_deletion) user with the given phone, or nil when absent.
func (s *MemoryStore) FindByPhone(_ context.Context, phone string) (*models.User, error) {
	if phone == "" {
		return nil, nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, u := range s.byID {
		if u.Phone == phone && reservesPhone(u.EffectiveStatus()) {
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
	if u.Phone != "" && reservesPhone(existing.EffectiveStatus()) {
		// Enforce verified-phone uniqueness when the record will be verified
		// (e.g. OTP verification promotes an unverified record while its phone
		// is unchanged) or when an unverified record takes a new phone.
		// Verified holders block; unverified holders never block.
		if u.EmailVerified || u.Phone != existing.Phone {
			for id, other := range s.byID {
				if id != u.ID && other.Phone == u.Phone && other.EmailVerified && reservesPhone(other.EffectiveStatus()) {
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
// Rejects any `to` status that is not active|suspended|deleted|pending_deletion
// with ErrInvalidStatus.
// When from is "active", a user with an empty status also matches.
// Returns ErrStatusConflict if the current status does not match `from`.
func (s *MemoryStore) SetStatus(_ context.Context, userID, from, to, reason string, at time.Time) error {
	switch models.UserStatus(to) {
	case models.StatusActive, models.StatusSuspended, models.StatusDeleted, models.StatusPendingDeletion:
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
			if id != userID && other.Phone == existing.Phone && other.EmailVerified && reservesPhone(other.EffectiveStatus()) {
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
	case models.StatusPendingDeletion:
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

		if len(filter.IDs) > 0 {
			found := false
			for _, id := range filter.IDs {
				if u.ID == id {
					found = true
					break
				}
			}
			if !found {
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

// EndAllUserSessionsExcept terminates all active sessions for a user except
// the given sid (used by password change, which keeps the current session).
func (s *MemoryStore) EndAllUserSessionsExcept(_ context.Context, userID, exceptSID string, reason models.SessionEndReason, at time.Time) ([]*models.Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var ended []*models.Session
	for _, sess := range s.sessions {
		if sess.UserID == userID && sess.EndedAt == nil && sess.ID != exceptSID {
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

// CreateAccountEvent appends one self-service account event (user_id, type,
// created_at; no PII values).
func (s *MemoryStore) CreateAccountEvent(_ context.Context, e *models.AccountEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if e.ID == "" {
		id, err := jwtutil.GenerateUUID()
		if err != nil {
			return err
		}
		e.ID = id
	}
	if e.CreatedAt.IsZero() {
		e.CreatedAt = time.Now().UTC()
	}
	cp := *e
	s.accountEvts = append(s.accountEvts, &cp)
	return nil
}

// RequestDeletion compare-and-sets an active account to pending_deletion with
// the grace-period fields. Any other current status is ErrStatusConflict.
func (s *MemoryStore) RequestDeletion(_ context.Context, userID string, at, purgeAfter time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	existing, ok := s.byID[userID]
	if !ok {
		return ErrUserNotFound
	}
	if existing.EffectiveStatus() != models.StatusActive {
		return ErrStatusConflict
	}
	if at.IsZero() {
		at = time.Now().UTC()
	}
	cp := cloneUser(existing)
	cp.Status = models.StatusPendingDeletion
	cp.DeletionRequestedAt = at
	cp.PurgeAfter = purgeAfter
	cp.UpdatedAt = at
	s.byID[userID] = cp
	s.byMail[cp.Email] = cp
	return nil
}

// CancelDeletion compare-and-sets a pending_deletion account back to active
// (login during the grace period) and clears the grace-period fields. If
// another verified phone-reserving account holds the phone in the meantime,
// restoring would create a duplicate, so it returns ErrPhoneTaken instead
// (unreachable while the grace period reserves the phone; Login answers it
// with 409, never a retry loop).
func (s *MemoryStore) CancelDeletion(_ context.Context, userID string, at time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	existing, ok := s.byID[userID]
	if !ok {
		return ErrUserNotFound
	}
	if existing.Status != models.StatusPendingDeletion {
		return ErrStatusConflict
	}
	if existing.Phone != "" {
		for id, other := range s.byID {
			if id != userID && other.Phone == existing.Phone && other.EmailVerified && reservesPhone(other.EffectiveStatus()) {
				return ErrPhoneTaken
			}
		}
	}
	if at.IsZero() {
		at = time.Now().UTC()
	}
	cp := cloneUser(existing)
	cp.Status = models.StatusActive
	cp.StatusReason = ""
	cp.DeletionRequestedAt = time.Time{}
	cp.PurgeAfter = time.Time{}
	cp.UpdatedAt = at
	s.byID[userID] = cp
	s.byMail[cp.Email] = cp
	return nil
}

// PurgeDeletion compare-and-sets a pending_deletion account whose grace period
// has passed to deleted and anonymizes it: name, email, phone and password
// hash are cleared (email becomes a unique non-PII placeholder so the unique
// index still holds); the user id is kept because entitlements and payment
// records are history. No blocklist entry is written: a self-deleted identity
// may sign up again (SPEC D2 amendment, F-UX2 A6).
func (s *MemoryStore) PurgeDeletion(_ context.Context, userID, anonymizedEmail string, at time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	existing, ok := s.byID[userID]
	if !ok {
		return ErrUserNotFound
	}
	if existing.Status != models.StatusPendingDeletion {
		return ErrStatusConflict
	}
	if !existing.PurgeAfter.IsZero() && at.Before(existing.PurgeAfter) {
		return ErrStatusConflict
	}
	if at.IsZero() {
		at = time.Now().UTC()
	}
	delete(s.byMail, existing.Email)
	cp := cloneUser(existing)
	cp.Status = models.StatusDeleted
	cp.StatusReason = ""
	cp.FullName = ""
	cp.Email = anonymizedEmail
	cp.Phone = ""
	cp.PasswordHash = ""
	cp.OTPHash = ""
	cp.PendingIDHash = ""
	cp.ResetTokenHash = ""
	cp.NameChangedAt = time.Time{}
	cp.PhoneChangedAt = time.Time{}
	cp.DeletionRequestedAt = time.Time{}
	cp.PurgeAfter = time.Time{}
	cp.DeletedAt = at
	cp.UpdatedAt = at
	s.byID[userID] = cp
	s.byMail[cp.Email] = cp
	return nil
}

// ListDeletionsDue returns pending_deletion accounts whose purge_after has passed.
func (s *MemoryStore) ListDeletionsDue(_ context.Context, now time.Time) ([]*models.User, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var due []*models.User
	for _, u := range s.byID {
		if u.Status == models.StatusPendingDeletion && !u.PurgeAfter.IsZero() && !u.PurgeAfter.After(now) {
			due = append(due, cloneUser(u))
		}
	}
	sort.Slice(due, func(i, j int) bool {
		return due[i].PurgeAfter.Before(due[j].PurgeAfter)
	})
	return due, nil
}

// UpdatePassword sets only the password hash (plus updated_at) and only
// while the account is active. Any other status is ErrStatusConflict.
func (s *MemoryStore) UpdatePassword(_ context.Context, userID, hash string, at time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	existing, ok := s.byID[userID]
	if !ok {
		return ErrUserNotFound
	}
	if existing.EffectiveStatus() != models.StatusActive {
		return ErrStatusConflict
	}
	if at.IsZero() {
		at = time.Now().UTC()
	}
	existing.PasswordHash = hash
	existing.UpdatedAt = at
	return nil
}

// UpdateProfileFields sets only the supplied name/phone fields (plus their
// change stamps and updated_at) and only while the account is active. The
// 30-day per-field limit is enforced inside the same critical section, so two
// concurrent edits cannot both pass; the loser gets ErrChangeTooSoon. A phone
// claimed by another verified phone-reserving account is ErrDuplicate. A
// no-op (same values) changes nothing and succeeds.
func (s *MemoryStore) UpdateProfileFields(_ context.Context, userID string, f ProfileFields, at time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	existing, ok := s.byID[userID]
	if !ok {
		return ErrUserNotFound
	}
	if existing.EffectiveStatus() != models.StatusActive {
		return ErrStatusConflict
	}
	if at.IsZero() {
		at = time.Now().UTC()
	}
	changeName := f.Name != nil && *f.Name != existing.FullName
	changePhone := f.Phone != nil && *f.Phone != existing.Phone
	if changeName {
		if !existing.NameChangedAt.IsZero() && at.Before(existing.NameChangedAt.Add(ProfileChangeCooldown)) {
			return ErrChangeTooSoon
		}
	}
	if changePhone {
		if !existing.PhoneChangedAt.IsZero() && at.Before(existing.PhoneChangedAt.Add(ProfileChangeCooldown)) {
			return ErrChangeTooSoon
		}
		for id, other := range s.byID {
			if id != userID && other.Phone == *f.Phone && other.EmailVerified && reservesPhone(other.EffectiveStatus()) {
				return ErrDuplicate
			}
		}
	}
	if changeName {
		existing.FullName = *f.Name
		existing.NameChangedAt = at
	}
	if changePhone {
		existing.Phone = *f.Phone
		existing.PhoneChangedAt = at
	}
	existing.UpdatedAt = at
	return nil
}

// SetEmail compare-and-sets the email from oldEmail to newEmail (plus
// updated_at) and only while the account is active. A stale oldEmail (the
// change already applied) or any other status is ErrStatusConflict; a
// newEmail held by another account is ErrDuplicate.
func (s *MemoryStore) SetEmail(_ context.Context, userID, oldEmail, newEmail string, at time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	existing, ok := s.byID[userID]
	if !ok {
		return ErrUserNotFound
	}
	if existing.EffectiveStatus() != models.StatusActive {
		return ErrStatusConflict
	}
	if existing.Email != oldEmail {
		return ErrStatusConflict
	}
	if other, taken := s.byMail[newEmail]; taken && other.ID != userID {
		return ErrDuplicate
	}
	if at.IsZero() {
		at = time.Now().UTC()
	}
	delete(s.byMail, existing.Email)
	existing.Email = newEmail
	existing.UpdatedAt = at
	s.byMail[newEmail] = existing
	return nil
}
