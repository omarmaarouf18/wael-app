// Package store provides persistence for academy-service: a Store interface,
// an in-process MemoryStore (testing and localhost dev), and a MongoStore
// (shared MongoDB persistence).
package store

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"sort"
	"sync"
	"time"

	"github.com/omarmaarouf18/wael-app/academy-service/internal/models"
)

// Common store errors.
var (
	ErrDuplicate      = errors.New("store: duplicate key")
	ErrNotFound       = errors.New("store: not found")
	ErrSubjectExpired = errors.New("store: subject access has expired")
)

// SubjectFilter specifies criteria for querying subjects.
type SubjectFilter struct {
	LevelKey string
	Term     string
	Status   string
	Page     int
	Limit    int
}

// Store is the academy persistence contract.
type Store interface {
	Ping(ctx context.Context) error
	Close(ctx context.Context) error
	EnsureIndexes(ctx context.Context) error
	SeedLevels(ctx context.Context) error
	ListLevels(ctx context.Context, onlyWithPublishedSubjects bool) ([]*models.Level, error)
	CreateSubject(ctx context.Context, s *models.Subject) error
	UpdateSubject(ctx context.Context, s *models.Subject) error
	ListSubjects(ctx context.Context, filter SubjectFilter) ([]*models.Subject, int, error)
	GetSubjectByID(ctx context.Context, id string) (*models.Subject, error)
	GetSubjectCounts(ctx context.Context, subjectID string) (models.SubjectCountsDTO, error)
	CreateVideo(ctx context.Context, v *models.Video) error
	GetVideoByID(ctx context.Context, id string) (*models.Video, error)
	ListVideosBySubject(ctx context.Context, subjectID string, onlyPublished bool) ([]*models.Video, error)
	CreateFile(ctx context.Context, f *models.SubjectFile) error
	ListFilesBySubject(ctx context.Context, subjectID string) ([]*models.SubjectFile, error)

	// Entitlements (Phase 3.1)
	Grant(ctx context.Context, e *models.Entitlement) error
	HasActiveEntitlement(ctx context.Context, userID, subjectID string) (bool, error)
	GetActiveEntitlement(ctx context.Context, userID, subjectID string) (*models.Entitlement, error)
	GetActiveEntitlements(ctx context.Context, userID string) (map[string]*models.Entitlement, error)
	GetActiveEntitlementSubjectIDs(ctx context.Context, userID string) (map[string]bool, error)
	ListEntitlementsByUser(ctx context.Context, userID string) ([]*models.Entitlement, error)

	// Purchase Requests (Phase 3.3)
	CreateOrGetPendingRequest(ctx context.Context, req *models.PurchaseRequest) (*models.PurchaseRequest, bool, error)
	GetPendingRequest(ctx context.Context, userID, subjectID string) (*models.PurchaseRequest, error)
	ListRequestsByUser(ctx context.Context, userID string) ([]*models.PurchaseRequest, error)

	// Video Plays (Phase 3.5 fix)
	RecordVideoPlay(ctx context.Context, play *models.VideoPlay) error
	ListVideoPlaysByVideo(ctx context.Context, videoID string) ([]*models.VideoPlay, error)
	ListVideoPlaysByUser(ctx context.Context, userID string) ([]*models.VideoPlay, error)

	// Admin audit log (Phase 4.1, per-service ownership per ADR-0008 Section 9).
	// Same shape as auth-service's admin_audit_log; newest first.
	CreateAuditLog(ctx context.Context, entry *models.AuditLog) error
	ListAuditLogs(ctx context.Context, page, limit int) ([]*models.AuditLog, int, error)
}

// MemoryStore is an in-memory Store for local dev and unit testing.
type MemoryStore struct {
	mu               sync.RWMutex
	levels           map[string]*models.Level
	subjects         map[string]*models.Subject
	videos           map[string]*models.Video
	files            map[string]*models.SubjectFile
	entitlements     []*models.Entitlement
	purchaseRequests []*models.PurchaseRequest
	videoPlays       []*models.VideoPlay
	auditLogs        []*models.AuditLog
}

// NewMemoryStore creates an empty MemoryStore.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		levels:           make(map[string]*models.Level),
		subjects:         make(map[string]*models.Subject),
		videos:           make(map[string]*models.Video),
		files:            make(map[string]*models.SubjectFile),
		entitlements:     make([]*models.Entitlement, 0),
		purchaseRequests: make([]*models.PurchaseRequest, 0),
		videoPlays:       make([]*models.VideoPlay, 0),
		auditLogs:        make([]*models.AuditLog, 0),
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

// ListLevels returns levels sorted by position, then key. If onlyWithPublished
// is true, levels with no published subjects are omitted (GET /academy/levels
// passes false: the catalog axes are always visible).
func (s *MemoryStore) ListLevels(_ context.Context, onlyWithPublished bool) ([]*models.Level, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	publishedLevelKeys := make(map[string]bool)
	if onlyWithPublished {
		for _, subj := range s.subjects {
			if subj.Status == models.StatusPublished {
				publishedLevelKeys[subj.LevelKey] = true
			}
		}
	}

	var result []*models.Level
	for _, l := range s.levels {
		if onlyWithPublished && !publishedLevelKeys[l.Key] {
			continue
		}
		cp := *l
		result = append(result, &cp)
	}

	sort.Slice(result, func(i, j int) bool {
		if result[i].Position != result[j].Position {
			return result[i].Position < result[j].Position
		}
		return result[i].Key < result[j].Key
	})

	return result, nil
}

// PutLevel stores or replaces one level. It is for tests and for the later
// admin diploma endpoints; the seeded levels come from SeedLevels.
func (s *MemoryStore) PutLevel(lvl models.Level) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := lvl
	s.levels[lvl.Key] = &cp
}

// CreateSubject stores a subject in memory.
func (s *MemoryStore) CreateSubject(_ context.Context, subj *models.Subject) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.subjects[subj.ID]; exists {
		return ErrDuplicate
	}
	cp := *subj
	s.subjects[subj.ID] = &cp
	return nil
}

// UpdateSubject updates an existing subject in memory.
func (s *MemoryStore) UpdateSubject(_ context.Context, subj *models.Subject) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.subjects[subj.ID]; !exists {
		return ErrNotFound
	}
	cp := *subj
	s.subjects[subj.ID] = &cp
	return nil
}

// ListSubjects queries subjects based on filter criteria.
func (s *MemoryStore) ListSubjects(_ context.Context, filter SubjectFilter) ([]*models.Subject, int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var matches []*models.Subject
	for _, subj := range s.subjects {
		if filter.LevelKey != "" && subj.LevelKey != filter.LevelKey {
			continue
		}
		if filter.Term != "" && subj.Term != filter.Term {
			continue
		}
		if filter.Status != "" && subj.Status != filter.Status {
			continue
		}
		cp := *subj
		matches = append(matches, &cp)
	}

	sort.Slice(matches, func(i, j int) bool {
		return matches[i].CreatedAt.Before(matches[j].CreatedAt)
	})

	total := len(matches)
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
	if start >= total {
		return []*models.Subject{}, total, nil
	}
	end := start + limit
	if end > total {
		end = total
	}

	return matches[start:end], total, nil
}

// GetSubjectByID retrieves a single subject by its ID.
func (s *MemoryStore) GetSubjectByID(_ context.Context, id string) (*models.Subject, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	subj, exists := s.subjects[id]
	if !exists {
		return nil, nil
	}
	cp := *subj
	return &cp, nil
}

// GetSubjectCounts returns the counts of published videos, books, and notes.
func (s *MemoryStore) GetSubjectCounts(_ context.Context, subjectID string) (models.SubjectCountsDTO, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var counts models.SubjectCountsDTO
	for _, v := range s.videos {
		if v.SubjectID == subjectID && v.Published {
			counts.Videos++
		}
	}
	for _, f := range s.files {
		if f.SubjectID == subjectID {
			if f.Kind == "book" {
				counts.Books++
			} else if f.Kind == "note" {
				counts.Notes++
			}
		}
	}
	return counts, nil
}

// CreateVideo stores a video in memory.
func (s *MemoryStore) CreateVideo(_ context.Context, v *models.Video) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.videos[v.ID]; exists {
		return ErrDuplicate
	}
	cp := *v
	s.videos[v.ID] = &cp
	return nil
}

// GetVideoByID retrieves a single video by its ID.
func (s *MemoryStore) GetVideoByID(_ context.Context, id string) (*models.Video, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	v, ok := s.videos[id]
	if !ok {
		return nil, nil
	}
	cp := *v
	return &cp, nil
}

// ListVideosBySubject returns videos for a subject sorted by position.
func (s *MemoryStore) ListVideosBySubject(_ context.Context, subjectID string, onlyPublished bool) ([]*models.Video, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var result []*models.Video
	for _, v := range s.videos {
		if v.SubjectID == subjectID {
			if onlyPublished && !v.Published {
				continue
			}
			cp := *v
			result = append(result, &cp)
		}
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].Position < result[j].Position
	})

	return result, nil
}

// CreateFile stores a file in memory.
func (s *MemoryStore) CreateFile(_ context.Context, f *models.SubjectFile) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.files[f.ID]; exists {
		return ErrDuplicate
	}
	cp := *f
	s.files[f.ID] = &cp
	return nil
}

// ListFilesBySubject returns files for a subject sorted by creation date.
func (s *MemoryStore) ListFilesBySubject(_ context.Context, subjectID string) ([]*models.SubjectFile, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var result []*models.SubjectFile
	for _, f := range s.files {
		if f.SubjectID == subjectID {
			cp := *f
			result = append(result, &cp)
		}
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].CreatedAt.Before(result[j].CreatedAt)
	})

	return result, nil
}

func generateID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// Grant grants an entitlement to a user for a subject.
// Enforces:
// 1. Subject must exist and not be expired (D20: ErrSubjectExpired).
// 2. Copies expires_at from subject's access_expires_at (D21).
// 3. Deactivates any expired entitlements for this (user_id, subject_id).
// 4. Guarantees at most ONE unexpired entitlement per (user_id, subject_id).
func (s *MemoryStore) Grant(ctx context.Context, e *models.Entitlement) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	subj, exists := s.subjects[e.SubjectID]
	if !exists {
		return ErrNotFound
	}

	now := time.Now()
	if !subj.AccessExpiresAt.After(now) {
		return ErrSubjectExpired
	}

	e.ExpiresAt = subj.AccessExpiresAt
	if e.GrantedAt.IsZero() {
		e.GrantedAt = now
	}
	if e.ID == "" {
		e.ID = generateID()
	}
	if e.Source == "" {
		e.Source = models.EntitlementSourceAdminGrant
	}

	// Deactivate any expired entitlements for this user and subject
	for _, ent := range s.entitlements {
		if ent.UserID == e.UserID && ent.SubjectID == e.SubjectID && ent.Active && !ent.ExpiresAt.After(now) {
			ent.Active = false
		}
	}

	// Check if an unexpired active entitlement already exists
	for _, ent := range s.entitlements {
		if ent.UserID == e.UserID && ent.SubjectID == e.SubjectID && ent.Active && ent.ExpiresAt.After(now) {
			return ErrDuplicate
		}
	}

	e.Active = true
	cp := *e
	s.entitlements = append(s.entitlements, &cp)
	return nil
}

// HasActiveEntitlement reports whether user owns the subject with expires_at > now.
func (s *MemoryStore) HasActiveEntitlement(_ context.Context, userID, subjectID string) (bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	now := time.Now()
	for _, e := range s.entitlements {
		if e.UserID == userID && e.SubjectID == subjectID && e.Active && e.ExpiresAt.After(now) {
			return true, nil
		}
	}
	return false, nil
}

// GetActiveEntitlement returns the active unexpired entitlement for (userID, subjectID), or nil if none.
func (s *MemoryStore) GetActiveEntitlement(_ context.Context, userID, subjectID string) (*models.Entitlement, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	now := time.Now()
	for _, e := range s.entitlements {
		if e.UserID == userID && e.SubjectID == subjectID && e.Active && e.ExpiresAt.After(now) {
			cp := *e
			return &cp, nil
		}
	}
	return nil, nil
}

// GetActiveEntitlements returns a map of subjectID -> active unexpired entitlement for the user.
func (s *MemoryStore) GetActiveEntitlements(_ context.Context, userID string) (map[string]*models.Entitlement, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	now := time.Now()
	result := make(map[string]*models.Entitlement)
	for _, e := range s.entitlements {
		if e.UserID == userID && e.Active && e.ExpiresAt.After(now) {
			cp := *e
			result[e.SubjectID] = &cp
		}
	}
	return result, nil
}

// GetActiveEntitlementSubjectIDs returns a set of subject IDs owned by user with expires_at > now.
func (s *MemoryStore) GetActiveEntitlementSubjectIDs(_ context.Context, userID string) (map[string]bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	now := time.Now()
	result := make(map[string]bool)
	for _, e := range s.entitlements {
		if e.UserID == userID && e.Active && e.ExpiresAt.After(now) {
			result[e.SubjectID] = true
		}
	}
	return result, nil
}

// ListEntitlementsByUser returns all entitlements for a user (including expired history).
func (s *MemoryStore) ListEntitlementsByUser(_ context.Context, userID string) ([]*models.Entitlement, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var result []*models.Entitlement
	for _, e := range s.entitlements {
		if e.UserID == userID {
			cp := *e
			result = append(result, &cp)
		}
	}
	return result, nil
}

// CreateOrGetPendingRequest atomically creates a pending request or returns the existing pending request (R5).
func (s *MemoryStore) CreateOrGetPendingRequest(_ context.Context, req *models.PurchaseRequest) (*models.PurchaseRequest, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, r := range s.purchaseRequests {
		if r.UserID == req.UserID && r.SubjectID == req.SubjectID && r.Status == models.RequestStatusPending {
			cp := *r
			return &cp, false, nil
		}
	}

	if req.ID == "" {
		req.ID = generateID()
	}
	if req.Status == "" {
		req.Status = models.RequestStatusPending
	}
	if req.CreatedAt.IsZero() {
		req.CreatedAt = time.Now().UTC()
	}

	cp := *req
	s.purchaseRequests = append(s.purchaseRequests, &cp)
	res := *req
	return &res, true, nil
}

// GetPendingRequest returns the pending request for a user and subject, or nil if none exists.
func (s *MemoryStore) GetPendingRequest(_ context.Context, userID, subjectID string) (*models.PurchaseRequest, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, r := range s.purchaseRequests {
		if r.UserID == userID && r.SubjectID == subjectID && r.Status == models.RequestStatusPending {
			cp := *r
			return &cp, nil
		}
	}
	return nil, nil
}

// ListRequestsByUser returns all requests for a user ordered by CreatedAt ascending.
func (s *MemoryStore) ListRequestsByUser(_ context.Context, userID string) ([]*models.PurchaseRequest, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var result []*models.PurchaseRequest
	for _, r := range s.purchaseRequests {
		if r.UserID == userID {
			cp := *r
			result = append(result, &cp)
		}
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].CreatedAt.Before(result[j].CreatedAt)
	})
	return result, nil
}

// RecordVideoPlay appends a video playback event to the in-memory log.
// Deliberately contains no IP address.
func (s *MemoryStore) RecordVideoPlay(_ context.Context, play *models.VideoPlay) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if play.ID == "" {
		play.ID = generateID()
	}
	if play.PlayedAt.IsZero() {
		play.PlayedAt = time.Now().UTC()
	}

	cp := *play
	s.videoPlays = append(s.videoPlays, &cp)
	return nil
}

// ListVideoPlaysByVideo lists playback events for a video.
func (s *MemoryStore) ListVideoPlaysByVideo(_ context.Context, videoID string) ([]*models.VideoPlay, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var result []*models.VideoPlay
	for _, p := range s.videoPlays {
		if p.VideoID == videoID {
			cp := *p
			result = append(result, &cp)
		}
	}
	return result, nil
}

// ListVideoPlaysByUser lists playback events for a user.
func (s *MemoryStore) ListVideoPlaysByUser(_ context.Context, userID string) ([]*models.VideoPlay, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var result []*models.VideoPlay
	for _, p := range s.videoPlays {
		if p.UserID == userID {
			cp := *p
			result = append(result, &cp)
		}
	}
	return result, nil
}

// CreateAuditLog records an admin action in admin_audit_log.
// Mirrors auth-service semantics: assigns an id and timestamp when empty.
func (s *MemoryStore) CreateAuditLog(_ context.Context, entry *models.AuditLog) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if entry.ID == "" {
		entry.ID = generateID()
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
	if start >= total {
		return []*models.AuditLog{}, total, nil
	}
	end := start + limit
	if end > total {
		end = total
	}

	return logs[start:end], total, nil
}
