// Package store provides persistence for academy-service: a Store interface,
// an in-process MemoryStore (testing and localhost dev), and a MongoStore
// (shared MongoDB persistence).
package store

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"log"
	"sort"
	"sync"
	"time"

	"github.com/omarmaarouf18/wael-app/academy-service/internal/models"
)

// Common store errors.
var (
	ErrDuplicate            = errors.New("store: duplicate key")
	ErrDuplicateReference   = errors.New("store: duplicate transfer reference")
	ErrPendingRequestExists = errors.New("store: pending request exists")
	ErrNotFound             = errors.New("store: not found")
	ErrSubjectExpired       = errors.New("store: subject access has expired")
	// ErrSubjectPublished: a published subject cannot be deleted; unpublish
	// it first (owner amendment 2026-11-02 to ADR-0012 decision 5).
	ErrSubjectPublished = errors.New("store: subject is published")
	// ErrSubjectHasStudents: an entitlement, purchase request or payment
	// record (any state) references the subject, so it cannot be deleted.
	ErrSubjectHasStudents = errors.New("store: subject has students")
	// ErrConflict: the requested operation conflicts with existing data (e.g.
	// deleting a package that has recorded purchases).
	ErrConflict = errors.New("store: conflict")
)

// SubjectFilter specifies criteria for querying subjects.
type SubjectFilter struct {
	LevelKey string
	Term     string
	Status   string
	Page     int
	Limit    int
}

// RequestFilter specifies criteria for the admin request queue.
type RequestFilter struct {
	Status    string
	SubjectID string
	Page      int
	Limit     int
}

// OrderRequestFilter specifies criteria for the console's grouped pending read
// model. It is deliberately unpaginated: grouping happens after the store
// returns every matching request.
type OrderRequestFilter struct {
	Status string
	UserID string
}

// IAPPurchaseFilter specifies filter criteria for admin purchase queries.
type IAPPurchaseFilter = models.IAPPurchaseFilter

// Store is the academy persistence contract.
type Store interface {
	Ping(ctx context.Context) error
	Close(ctx context.Context) error
	EnsureIndexes(ctx context.Context) error
	SeedLevels(ctx context.Context) error
	ListLevels(ctx context.Context, onlyWithPublishedSubjects bool) ([]*models.Level, error)
	GetLevelByKey(ctx context.Context, key string) (*models.Level, error)
	CreateLevel(ctx context.Context, lvl *models.Level) error
	UpdateLevel(ctx context.Context, lvl *models.Level) error
	DeleteLevel(ctx context.Context, key string) error
	CountSubjectsByLevel(ctx context.Context, levelKey, status string) (int, error)
	CreateSubject(ctx context.Context, s *models.Subject) error
	UpdateSubject(ctx context.Context, s *models.Subject) error
	ListSubjects(ctx context.Context, filter SubjectFilter) ([]*models.Subject, int, error)
	GetSubjectByID(ctx context.Context, id string) (*models.Subject, error)
	GetSubjectCounts(ctx context.Context, subjectID string) (models.SubjectCountsDTO, error)
	CreateVideo(ctx context.Context, v *models.Video) error
	UpdateVideo(ctx context.Context, v *models.Video) error
	GetVideoByID(ctx context.Context, id string) (*models.Video, error)
	ListVideosBySubject(ctx context.Context, subjectID string, onlyPublished bool) ([]*models.Video, error)
	CreateFile(ctx context.Context, f *models.SubjectFile) error
	ListFilesBySubject(ctx context.Context, subjectID string) ([]*models.SubjectFile, error)
	// GetFile returns the file only when it belongs to subjectID; a missing
	// file, or a file of another subject, is (nil, nil).
	GetFile(ctx context.Context, subjectID, fileID string) (*models.SubjectFile, error)
	// DeleteFile removes the file row of subjectID; no matching row (missing,
	// or a file of another subject) is ErrNotFound.
	DeleteFile(ctx context.Context, subjectID, fileID string) error
	// DeleteDraftSubject hard-deletes a draft subject that no student ever
	// touched, with its videos and file rows (owner amendment 2026-11-02 to
	// ADR-0012 decision 5). It returns the deleted file rows so the caller
	// can remove their stored objects. A missing subject is ErrNotFound, a
	// published one ErrSubjectPublished, and one referenced by any
	// entitlement, purchase request or payment record (any state)
	// ErrSubjectHasStudents.
	DeleteDraftSubject(ctx context.Context, id string) ([]*models.SubjectFile, error)
	// MigrateSubjectPriceVisibility sets price_visibility="follow_global" on
	// existing subjects that have no price_visibility set.
	MigrateSubjectPriceVisibility(ctx context.Context) error

	// Entitlements (Phase 3.1)
	Grant(ctx context.Context, e *models.Entitlement) error
	HasActiveEntitlement(ctx context.Context, userID, subjectID string) (bool, error)
	GetActiveEntitlement(ctx context.Context, userID, subjectID string) (*models.Entitlement, error)
	GetActiveEntitlements(ctx context.Context, userID string) (map[string]*models.Entitlement, error)
	GetActiveEntitlementSubjectIDs(ctx context.Context, userID string) (map[string]bool, error)
	ListEntitlementsByUser(ctx context.Context, userID string) ([]*models.Entitlement, error)
	GetEntitlementByID(ctx context.Context, id string) (*models.Entitlement, error)
	// RevokeEntitlement ends access now (active=false plus revoke fields).
	// It reports whether the row changed; an already-revoked row is a
	// no-op success (false, nil), a missing row is ErrNotFound.
	RevokeEntitlement(ctx context.Context, id, revokedBy, reason string, now time.Time) (bool, error)

	// Purchase Requests (Phase 3.3, web ADR-0014 W4)
	CreateOrGetPendingRequest(ctx context.Context, req *models.PurchaseRequest) (*models.PurchaseRequest, bool, error)
	CreatePurchaseRequest(ctx context.Context, req *models.PurchaseRequest) error
	HasPurchaseRequestWithReference(ctx context.Context, normalizedRef string) (bool, error)
	GetPendingRequest(ctx context.Context, userID, subjectID string) (*models.PurchaseRequest, error)
	ListRequestsByUser(ctx context.Context, userID string) ([]*models.PurchaseRequest, error)
	GetRequestByID(ctx context.Context, id string) (*models.PurchaseRequest, error)
	ListRequests(ctx context.Context, filter RequestFilter) ([]*models.PurchaseRequest, int, error)
	CountPendingRequests(ctx context.Context) (int, error)
	// DecideRequest compare-and-sets a request from fromStatus to toStatus.
	// It reports whether the row changed; a row in another status is a
	// no-op (false, nil), a missing row is ErrNotFound.
	DecideRequest(ctx context.Context, id, fromStatus, toStatus, decidedBy string, decidedAt time.Time, reason string) (bool, error)
	// UndoAccept puts an accepted request back to pending, but only when it
	// is still the accept made by decidedBy at decidedAt (the claim of one
	// accept call), clearing decided_at and decided_by. It reports whether
	// the row changed; any other state is a no-op (false, nil).
	UndoAccept(ctx context.Context, id, decidedBy string, decidedAt time.Time) (bool, error)

	// Orders (2026-11-05 orders brief, ADR-0014 W4/C2).
	// CreateOrderWithRequests creates one order and its requests all-or-none.
	// A normalized-reference collision (in orders or legacy single requests)
	// or a duplicate pending request is ErrDuplicate.
	CreateOrderWithRequests(ctx context.Context, order *models.Order, reqs []*models.PurchaseRequest) error
	HasOrderWithNormalizedReference(ctx context.Context, normalizedRef string) (bool, error)
	GetOrderByID(ctx context.Context, id string) (*models.Order, error)
	ListOrdersByUser(ctx context.Context, userID string) ([]*models.Order, error)
	ListRequestsByOrder(ctx context.Context, orderID string) ([]*models.PurchaseRequest, error)
	// CreateOrGetPendingAppRequest attaches an app request to the student's
	// open app order, creating that order when none exists. Concurrent calls
	// for the same student end in one open app order.
	CreateOrGetPendingAppRequest(ctx context.Context, req *models.PurchaseRequest) (*models.PurchaseRequest, bool, error)
	// CloseOrderIfNoPending closes the order when it has no pending request.
	// It is idempotent and reports whether it changed the order.
	CloseOrderIfNoPending(ctx context.Context, orderID string, closedAt time.Time) (bool, error)
	// ListRequestsForGroups returns every matching request, newest first,
	// for the console's grouped read model (no pagination clamp).
	ListRequestsForGroups(ctx context.Context, filter OrderRequestFilter) ([]*models.PurchaseRequest, error)
	// DeleteOrphanWebOrders removes web orders created before before that have
	// no purchase request attached, and returns how many it removed. Checkout
	// compensates on error, but a crash between the order insert and the
	// request insert can still leave an orphan (Mongo is standalone, SPEC R4).
	// A small age threshold keeps an in-flight checkout out of reach.
	DeleteOrphanWebOrders(ctx context.Context, before time.Time) (int, error)
	// MigrateLegacyWebRequests wraps every pending web request that has no
	// order into its own web order of one, copying its proof fields (payment
	// method, transfer reference, sender number, amount). It is idempotent and
	// never touches accepted/rejected history.
	MigrateLegacyWebRequests(ctx context.Context) error

	// Payment records (Phase 4.5, append-only: no update and no delete).
	// A duplicate entitlement_id is ErrDuplicate: the activation was
	// already recorded, which the caller treats as success.
	CreatePaymentRecord(ctx context.Context, rec *models.PaymentRecord) error
	ListPaymentRecordsByUser(ctx context.Context, userID string) ([]*models.PaymentRecord, error)

	// Video Plays (Phase 3.5 fix)
	RecordVideoPlay(ctx context.Context, play *models.VideoPlay) error
	ListVideoPlaysByVideo(ctx context.Context, videoID string) ([]*models.VideoPlay, error)
	ListVideoPlaysByUser(ctx context.Context, userID string) ([]*models.VideoPlay, error)

	// Admin audit log (Phase 4.1, per-service ownership per ADR-0008 Section 9).
	// Same shape as auth-service's admin_audit_log; newest first.
	CreateAuditLog(ctx context.Context, entry *models.AuditLog) error
	ListAuditLogs(ctx context.Context, page, limit int) ([]*models.AuditLog, int, error)

	// App Settings (2026-10-08, A1)
	GetAppSettings(ctx context.Context) (*models.AppSettings, error)
	SaveAppSettings(ctx context.Context, settings *models.AppSettings) error

	// IAP Packages & Purchases (Issue #52, ADR-0015)
	ListIAPPackages(ctx context.Context, publishedOnly bool) ([]*models.IAPPackage, error)
	GetIAPPackage(ctx context.Context, id string) (*models.IAPPackage, error)
	GetIAPPackageByProductID(ctx context.Context, productID string) (*models.IAPPackage, error)
	CreateIAPPackage(ctx context.Context, pkg *models.IAPPackage) error
	UpdateIAPPackage(ctx context.Context, pkg *models.IAPPackage) error
	DeleteIAPPackage(ctx context.Context, id string) error
	CountIAPPurchasesByPackage(ctx context.Context, packageID string) (int, error)
	InsertIAPPurchase(ctx context.Context, purchase *models.IAPPurchase) error
	GetIAPPurchaseByTransactionID(ctx context.Context, transactionID string) (*models.IAPPurchase, error)
	ListIAPPurchases(ctx context.Context, filter models.IAPPurchaseFilter, limit int) ([]*models.IAPPurchase, error)
	RemoveSubjectFromAllPackages(ctx context.Context, subjectID string) error
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
	orders           []*models.Order
	paymentRecords   []*models.PaymentRecord
	videoPlays       []*models.VideoPlay
	auditLogs        []*models.AuditLog
	settings         *models.AppSettings
	cachedSettings   *models.AppSettings
	cachedSettingsAt time.Time
	settingsMu       sync.RWMutex
	iapPackages      map[string]*models.IAPPackage
	iapPurchases     []*models.IAPPurchase
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
		orders:           make([]*models.Order, 0),
		paymentRecords:   make([]*models.PaymentRecord, 0),
		videoPlays:       make([]*models.VideoPlay, 0),
		auditLogs:        make([]*models.AuditLog, 0),
		iapPackages:      make(map[string]*models.IAPPackage),
		iapPurchases:     make([]*models.IAPPurchase, 0),
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

// GetLevelByKey retrieves a single level by its key, or nil if missing.
func (s *MemoryStore) GetLevelByKey(_ context.Context, key string) (*models.Level, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	lvl, exists := s.levels[key]
	if !exists {
		return nil, nil
	}
	cp := *lvl
	return &cp, nil
}

// CreateLevel stores a new level. Duplicate keys return ErrDuplicate.
func (s *MemoryStore) CreateLevel(_ context.Context, lvl *models.Level) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.levels[lvl.Key]; exists {
		return ErrDuplicate
	}
	cp := *lvl
	s.levels[lvl.Key] = &cp
	return nil
}

// UpdateLevel replaces an existing level. Missing keys return ErrNotFound.
func (s *MemoryStore) UpdateLevel(_ context.Context, lvl *models.Level) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.levels[lvl.Key]; !exists {
		return ErrNotFound
	}
	cp := *lvl
	s.levels[lvl.Key] = &cp
	return nil
}

// DeleteLevel removes a level. Missing keys return ErrNotFound.
func (s *MemoryStore) DeleteLevel(_ context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.levels[key]; !exists {
		return ErrNotFound
	}
	delete(s.levels, key)
	return nil
}

// CountSubjectsByLevel counts subjects in a level, optionally filtered by
// status (empty status counts all).
func (s *MemoryStore) CountSubjectsByLevel(_ context.Context, levelKey, status string) (int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	n := 0
	for _, subj := range s.subjects {
		if subj.LevelKey != levelKey {
			continue
		}
		if status != "" && subj.Status != status {
			continue
		}
		n++
	}
	return n, nil
}

// CreateSubject stores a subject in memory.
func (s *MemoryStore) CreateSubject(_ context.Context, subj *models.Subject) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.subjects[subj.ID]; exists {
		return ErrDuplicate
	}
	cp := *subj
	if cp.PriceVisibility == "" {
		cp.PriceVisibility = models.PriceVisibilityFollowGlobal
	}
	s.subjects[subj.ID] = &cp
	return nil
}

// MigrateSubjectPriceVisibility updates existing subjects with empty price_visibility to "follow_global".
func (s *MemoryStore) MigrateSubjectPriceVisibility(_ context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, subj := range s.subjects {
		if subj.PriceVisibility == "" {
			subj.PriceVisibility = models.PriceVisibilityFollowGlobal
		}
	}
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
		if matches[i].Order != matches[j].Order {
			return matches[i].Order < matches[j].Order
		}
		return matches[i].CreatedAt.Before(matches[j].CreatedAt)
	})

	total := len(matches)
	const maxPage = 10000
	page := filter.Page
	if page < 1 {
		page = 1
	} else if page > maxPage {
		page = maxPage
	}
	limit := filter.Limit
	if limit <= 0 {
		limit = 20
	} else if limit > 100 {
		limit = 100
	}

	start := 0
	if page > 1 {
		if total == 0 || (page-1) > total/limit {
			start = total
		} else {
			start = (page - 1) * limit
		}
	}
	if start < 0 {
		start = 0
	} else if start > total {
		start = total
	}
	end := start + limit
	if end < start || end > total {
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
		if v.SubjectID == subjectID && v.Published && !v.Deleted {
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

// UpdateVideo replaces an existing video. Missing ids return ErrNotFound.
func (s *MemoryStore) UpdateVideo(_ context.Context, v *models.Video) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.videos[v.ID]; !exists {
		return ErrNotFound
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

// ListVideosBySubject returns non-deleted videos for a subject sorted by position.
func (s *MemoryStore) ListVideosBySubject(_ context.Context, subjectID string, onlyPublished bool) ([]*models.Video, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var result []*models.Video
	for _, v := range s.videos {
		if v.SubjectID == subjectID {
			if v.Deleted {
				continue
			}
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

// GetFile returns a copy of the file when it belongs to subjectID.
func (s *MemoryStore) GetFile(_ context.Context, subjectID, fileID string) (*models.SubjectFile, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	f, ok := s.files[fileID]
	if !ok || f.SubjectID != subjectID {
		return nil, nil
	}
	cp := *f
	return &cp, nil
}

// DeleteFile removes the file row when it belongs to subjectID.
func (s *MemoryStore) DeleteFile(_ context.Context, subjectID, fileID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	f, ok := s.files[fileID]
	if !ok || f.SubjectID != subjectID {
		return ErrNotFound
	}
	delete(s.files, fileID)
	return nil
}

// DeleteDraftSubject removes a draft subject nobody references, with its
// videos and file rows, under one lock.
func (s *MemoryStore) DeleteDraftSubject(_ context.Context, id string) ([]*models.SubjectFile, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	subj, ok := s.subjects[id]
	if !ok {
		return nil, ErrNotFound
	}
	if subj.Status == models.StatusPublished {
		return nil, ErrSubjectPublished
	}
	for _, e := range s.entitlements {
		if e.SubjectID == id {
			return nil, ErrSubjectHasStudents
		}
	}
	for _, pr := range s.purchaseRequests {
		if pr.SubjectID == id {
			return nil, ErrSubjectHasStudents
		}
	}
	for _, rec := range s.paymentRecords {
		if rec.SubjectID == id {
			return nil, ErrSubjectHasStudents
		}
	}

	var files []*models.SubjectFile
	for fid, f := range s.files {
		if f.SubjectID == id {
			cp := *f
			files = append(files, &cp)
			delete(s.files, fid)
		}
	}
	for vid, v := range s.videos {
		if v.SubjectID == id {
			delete(s.videos, vid)
		}
	}
	s.removeSubjectFromAllPackagesLocked(id)
	delete(s.subjects, id)
	return files, nil
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
		if ent.UserID == e.UserID && ent.SubjectID == e.SubjectID && ent.Active && !ent.IsRevoked() && ent.ExpiresAt.After(now) {
			return ErrDuplicate
		}
	}

	e.Active = true
	cp := *e
	s.entitlements = append(s.entitlements, &cp)
	return nil
}

// HasActiveEntitlement reports whether user owns the subject with expires_at > now and not revoked.
func (s *MemoryStore) HasActiveEntitlement(_ context.Context, userID, subjectID string) (bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	now := time.Now()
	for _, e := range s.entitlements {
		if e.UserID == userID && e.SubjectID == subjectID && e.Active && !e.IsRevoked() && e.ExpiresAt.After(now) {
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
		if e.UserID == userID && e.SubjectID == subjectID && e.Active && !e.IsRevoked() && e.ExpiresAt.After(now) {
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
		if e.UserID == userID && e.Active && !e.IsRevoked() && e.ExpiresAt.After(now) {
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
		if e.UserID == userID && e.Active && !e.IsRevoked() && e.ExpiresAt.After(now) {
			result[e.SubjectID] = true
		}
	}
	return result, nil
}

// ListEntitlementsByUser returns all entitlements for a user (including expired and revoked history), newest first.
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
	sort.Slice(result, func(i, j int) bool {
		return result[i].GrantedAt.After(result[j].GrantedAt)
	})
	return result, nil
}

// GetEntitlementByID returns an entitlement by its primary key id.
func (s *MemoryStore) GetEntitlementByID(_ context.Context, id string) (*models.Entitlement, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, e := range s.entitlements {
		if e.ID == id {
			cp := *e
			return &cp, nil
		}
	}
	return nil, nil
}

// RevokeEntitlement ends access now (active=false plus revoke fields).
// It reports whether the row changed; an already-revoked row is a
// no-op success (false, nil), a missing row is ErrNotFound.
func (s *MemoryStore) RevokeEntitlement(_ context.Context, id, revokedBy, reason string, now time.Time) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, e := range s.entitlements {
		if e.ID == id {
			if e.IsRevoked() {
				return false, nil
			}
			e.Active = false
			revAt := now
			e.RevokedAt = &revAt
			e.RevokedBy = revokedBy
			e.RevokeReason = reason
			return true, nil
		}
	}
	return false, ErrNotFound
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
	if req.Source == "" {
		req.Source = "mobile"
	}
	if req.CreatedAt.IsZero() {
		req.CreatedAt = time.Now().UTC()
	}

	cp := *req
	s.purchaseRequests = append(s.purchaseRequests, &cp)
	res := *req
	return &res, true, nil
}

// HasPurchaseRequestWithReference checks if any request exists with the normalized transfer reference.
func (s *MemoryStore) HasPurchaseRequestWithReference(_ context.Context, normalizedRef string) (bool, error) {
	if normalizedRef == "" {
		return false, nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, r := range s.purchaseRequests {
		if r.NormalizedReference != "" && r.NormalizedReference == normalizedRef {
			return true, nil
		}
	}
	return false, nil
}

// CreatePurchaseRequest inserts a new purchase request enforcing uniqueness of pending requests per user+subject and normalized reference.
func (s *MemoryStore) CreatePurchaseRequest(_ context.Context, req *models.PurchaseRequest) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, r := range s.purchaseRequests {
		if r.UserID == req.UserID && r.SubjectID == req.SubjectID && r.Status == models.RequestStatusPending {
			return ErrPendingRequestExists
		}
		if req.NormalizedReference != "" && r.NormalizedReference == req.NormalizedReference {
			return ErrDuplicateReference
		}
	}

	if req.ID == "" {
		req.ID = generateID()
	}
	if req.Status == "" {
		req.Status = models.RequestStatusPending
	}
	if req.Source == "" {
		req.Source = "web"
	}
	if req.CreatedAt.IsZero() {
		req.CreatedAt = time.Now().UTC()
	}

	cp := *req
	s.purchaseRequests = append(s.purchaseRequests, &cp)
	return nil
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

// GetRequestByID returns a purchase request by its primary key ID.
func (s *MemoryStore) GetRequestByID(_ context.Context, id string) (*models.PurchaseRequest, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, r := range s.purchaseRequests {
		if r.ID == id {
			cp := *r
			return &cp, nil
		}
	}
	return nil, nil
}

// ListRequests returns filtered purchase requests, newest first.
func (s *MemoryStore) ListRequests(_ context.Context, filter RequestFilter) ([]*models.PurchaseRequest, int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var matched []*models.PurchaseRequest
	for _, r := range s.purchaseRequests {
		if filter.Status != "" && r.Status != filter.Status {
			continue
		}
		if filter.SubjectID != "" && r.SubjectID != filter.SubjectID {
			continue
		}
		cp := *r
		matched = append(matched, &cp)
	}

	// Newest first: created_at descending
	sort.Slice(matched, func(i, j int) bool {
		return matched[i].CreatedAt.After(matched[j].CreatedAt)
	})

	total := len(matched)
	const maxPage = 10000
	page := filter.Page
	if page < 1 {
		page = 1
	} else if page > maxPage {
		page = maxPage
	}
	limit := filter.Limit
	if limit <= 0 {
		limit = 20
	} else if limit > 100 {
		limit = 100
	}

	start := 0
	if page > 1 {
		if total == 0 || (page-1) > total/limit {
			start = total
		} else {
			start = (page - 1) * limit
		}
	}
	if start < 0 {
		start = 0
	} else if start > total {
		start = total
	}
	end := start + limit
	if end < start || end > total {
		end = total
	}
	return matched[start:end], total, nil
}

// CountPendingRequests returns the number of requests currently pending.
func (s *MemoryStore) CountPendingRequests(_ context.Context) (int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	count := 0
	for _, r := range s.purchaseRequests {
		if r.Status == models.RequestStatusPending {
			count++
		}
	}
	return count, nil
}

// DecideRequest compare-and-sets a request from fromStatus to toStatus.
// It reports whether the row changed; a row in another status is a
// no-op (false, nil), a missing row is ErrNotFound.
func (s *MemoryStore) DecideRequest(_ context.Context, id, fromStatus, toStatus, decidedBy string, decidedAt time.Time, reason string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, r := range s.purchaseRequests {
		if r.ID == id {
			if r.Status != fromStatus {
				return false, nil
			}
			r.Status = toStatus
			decAt := decidedAt
			r.DecidedAt = &decAt
			r.DecidedBy = decidedBy
			if reason != "" {
				r.RejectReason = reason
			}
			return true, nil
		}
	}
	return false, ErrNotFound
}

// UndoAccept reverts this accept call's claim (accepted -> pending).
func (s *MemoryStore) UndoAccept(_ context.Context, id, decidedBy string, decidedAt time.Time) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, r := range s.purchaseRequests {
		if r.ID != id {
			continue
		}
		if r.Status != models.RequestStatusAccepted || r.DecidedBy != decidedBy || r.DecidedAt == nil || !r.DecidedAt.Equal(decidedAt) {
			return false, nil
		}
		r.Status = models.RequestStatusPending
		r.DecidedAt = nil
		r.DecidedBy = ""
		return true, nil
	}
	return false, nil
}

// CreateOrderWithRequests creates one order and its requests all-or-none.
// All checks run before anything is appended.
func (s *MemoryStore) CreateOrderWithRequests(_ context.Context, order *models.Order, reqs []*models.PurchaseRequest) error {
	if order == nil {
		return errors.New("store: nil order")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if order.ID == "" {
		order.ID = generateID()
	}
	if order.Status == "" {
		order.Status = models.OrderStatusOpen
	}
	if order.Source == "" {
		order.Source = models.OrderSourceWeb
	}
	if order.CreatedAt.IsZero() {
		order.CreatedAt = time.Now().UTC()
	}

	if order.NormalizedReference != "" {
		for _, o := range s.orders {
			if o.NormalizedReference != "" && o.NormalizedReference == order.NormalizedReference {
				return ErrDuplicateReference
			}
		}
		for _, r := range s.purchaseRequests {
			if r.NormalizedReference != "" && r.NormalizedReference == order.NormalizedReference {
				return ErrDuplicateReference
			}
		}
	}

	seen := make(map[string]bool, len(reqs))
	for _, req := range reqs {
		if req == nil {
			return errors.New("store: nil request")
		}
		if seen[req.SubjectID] {
			return ErrPendingRequestExists
		}
		seen[req.SubjectID] = true
		for _, existing := range s.purchaseRequests {
			if existing.UserID == req.UserID && existing.SubjectID == req.SubjectID && existing.Status == models.RequestStatusPending {
				return ErrPendingRequestExists
			}
		}
	}

	orderCopy := *order
	s.orders = append(s.orders, &orderCopy)

	for _, req := range reqs {
		req.OrderID = order.ID
		if req.ID == "" {
			req.ID = generateID()
		}
		if req.Status == "" {
			req.Status = models.RequestStatusPending
		}
		if req.Source == "" {
			req.Source = order.Source
		}
		if req.CreatedAt.IsZero() {
			req.CreatedAt = order.CreatedAt
		}
		cp := *req
		s.purchaseRequests = append(s.purchaseRequests, &cp)
	}
	return nil
}

// HasOrderWithNormalizedReference checks if any order uses the normalized transfer reference.
func (s *MemoryStore) HasOrderWithNormalizedReference(_ context.Context, normalizedRef string) (bool, error) {
	if normalizedRef == "" {
		return false, nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, o := range s.orders {
		if o.NormalizedReference != "" && o.NormalizedReference == normalizedRef {
			return true, nil
		}
	}
	return false, nil
}

// GetOrderByID returns an order by id, or nil when it does not exist.
func (s *MemoryStore) GetOrderByID(_ context.Context, id string) (*models.Order, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, o := range s.orders {
		if o.ID == id {
			cp := *o
			return &cp, nil
		}
	}
	return nil, nil
}

// ListOrdersByUser returns a user's orders, newest first.
func (s *MemoryStore) ListOrdersByUser(_ context.Context, userID string) ([]*models.Order, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var result []*models.Order
	for _, o := range s.orders {
		if o.UserID == userID {
			cp := *o
			result = append(result, &cp)
		}
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].CreatedAt.After(result[j].CreatedAt)
	})
	return result, nil
}

// ListRequestsByOrder returns an order's requests in creation order.
func (s *MemoryStore) ListRequestsByOrder(_ context.Context, orderID string) ([]*models.PurchaseRequest, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var result []*models.PurchaseRequest
	for _, r := range s.purchaseRequests {
		if r.OrderID == orderID {
			cp := *r
			result = append(result, &cp)
		}
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].CreatedAt.Before(result[j].CreatedAt)
	})
	return result, nil
}

// CreateOrGetPendingAppRequest attaches an app request to the student's open
// app order, creating the order when none exists (R5 idempotency preserved).
func (s *MemoryStore) CreateOrGetPendingAppRequest(_ context.Context, req *models.PurchaseRequest) (*models.PurchaseRequest, bool, error) {
	if req == nil {
		return nil, false, errors.New("store: nil request")
	}

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
	if req.Source == "" {
		req.Source = "mobile"
	}
	if req.CreatedAt.IsZero() {
		req.CreatedAt = time.Now().UTC()
	}

	var open *models.Order
	for _, o := range s.orders {
		if o.UserID == req.UserID && o.Source == models.OrderSourceApp && o.Status == models.OrderStatusOpen {
			open = o
			break
		}
	}
	if open == nil {
		open = &models.Order{
			ID:        generateID(),
			UserID:    req.UserID,
			Source:    models.OrderSourceApp,
			Status:    models.OrderStatusOpen,
			CreatedAt: req.CreatedAt,
		}
		cp := *open
		s.orders = append(s.orders, &cp)
	}

	req.OrderID = open.ID
	cp := *req
	s.purchaseRequests = append(s.purchaseRequests, &cp)
	res := *req
	return &res, true, nil
}

// CloseOrderIfNoPending closes the order when it has no pending request.
func (s *MemoryStore) CloseOrderIfNoPending(_ context.Context, orderID string, closedAt time.Time) (bool, error) {
	if orderID == "" {
		return false, nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	for _, r := range s.purchaseRequests {
		if r.OrderID == orderID && r.Status == models.RequestStatusPending {
			return false, nil
		}
	}
	for _, o := range s.orders {
		if o.ID == orderID {
			if o.Status == models.OrderStatusClosed {
				return false, nil
			}
			o.Status = models.OrderStatusClosed
			t := closedAt
			o.ClosedAt = &t
			return true, nil
		}
	}
	return false, nil
}

// ListRequestsForGroups returns every matching request for the console read model, newest first.
func (s *MemoryStore) ListRequestsForGroups(_ context.Context, filter OrderRequestFilter) ([]*models.PurchaseRequest, error) {
	status := filter.Status
	if status == "" {
		status = models.RequestStatusPending
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	var result []*models.PurchaseRequest
	for _, r := range s.purchaseRequests {
		if r.Status != status {
			continue
		}
		if filter.UserID != "" && r.UserID != filter.UserID {
			continue
		}
		cp := *r
		result = append(result, &cp)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].CreatedAt.After(result[j].CreatedAt)
	})
	return result, nil
}

// DeleteOrphanWebOrders removes old web orders that have no request rows.
// Only source=web is considered; app orders are never touched.
func (s *MemoryStore) DeleteOrphanWebOrders(_ context.Context, before time.Time) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	hasRequests := make(map[string]bool, len(s.purchaseRequests))
	for _, r := range s.purchaseRequests {
		if r.OrderID != "" {
			hasRequests[r.OrderID] = true
		}
	}

	kept := make([]*models.Order, 0, len(s.orders))
	removed := 0
	for _, o := range s.orders {
		if o.Source == models.OrderSourceWeb && o.CreatedAt.Before(before) && !hasRequests[o.ID] {
			removed++
			continue
		}
		kept = append(kept, o)
	}
	s.orders = kept
	return removed, nil
}

// MigrateLegacyWebRequests wraps every pending web request without an order
// into its own web order of one, copying its proof fields. Accepted/rejected
// history is never touched. It is idempotent: a request that already carries
// an order_id is skipped.
func (s *MemoryStore) MigrateLegacyWebRequests(_ context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	usedRefs := make(map[string]bool, len(s.orders))
	for _, o := range s.orders {
		if o.NormalizedReference != "" {
			usedRefs[o.NormalizedReference] = true
		}
	}

	migrated := 0
	for _, r := range s.purchaseRequests {
		if r.Status != models.RequestStatusPending || r.Source != models.OrderSourceWeb || r.OrderID != "" {
			continue
		}
		if r.NormalizedReference != "" && usedRefs[r.NormalizedReference] {
			log.Printf("[WARN] orders migration: skipped legacy web request %s: transfer reference %q is already used by an order", r.ID, r.NormalizedReference)
			continue
		}
		order := &models.Order{
			ID:                  generateID(),
			UserID:              r.UserID,
			Source:              models.OrderSourceWeb,
			Status:              models.OrderStatusOpen,
			PaymentMethod:       r.PaymentMethod,
			TransferReference:   r.TransferReference,
			NormalizedReference: r.NormalizedReference,
			SenderNumber:        r.SenderNumber,
			AmountPaid:          r.Amount,
			CreatedAt:           r.CreatedAt,
		}
		cp := *order
		s.orders = append(s.orders, &cp)
		if order.NormalizedReference != "" {
			usedRefs[order.NormalizedReference] = true
		}
		r.OrderID = order.ID
		migrated++
	}
	if migrated > 0 {
		log.Printf("[ACADEMY] orders migration: wrapped %d legacy pending web request(s) into web orders of one", migrated)
	}
	return nil
}

// CreatePaymentRecord appends a payment record. A duplicate entitlement_id returns ErrDuplicate.
func (s *MemoryStore) CreatePaymentRecord(_ context.Context, rec *models.PaymentRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, p := range s.paymentRecords {
		if p.EntitlementID == rec.EntitlementID {
			return ErrDuplicate
		}
	}

	if rec.ID == "" {
		rec.ID = generateID()
	}
	if rec.RecordedAt.IsZero() {
		rec.RecordedAt = time.Now().UTC()
	}

	cp := *rec
	s.paymentRecords = append(s.paymentRecords, &cp)
	return nil
}

// ListPaymentRecordsByUser returns payment records for a user, newest first.
func (s *MemoryStore) ListPaymentRecordsByUser(_ context.Context, userID string) ([]*models.PaymentRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var result []*models.PaymentRecord
	for _, p := range s.paymentRecords {
		if p.UserID == userID {
			cp := *p
			result = append(result, &cp)
		}
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].RecordedAt.After(result[j].RecordedAt)
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
	const maxPage = 10000
	if page < 1 {
		page = 1
	} else if page > maxPage {
		page = maxPage
	}
	if limit <= 0 {
		limit = 20
	} else if limit > 100 {
		limit = 100
	}

	start := 0
	if page > 1 {
		if total == 0 || (page-1) > total/limit {
			start = total
		} else {
			start = (page - 1) * limit
		}
	}
	if start < 0 {
		start = 0
	} else if start > total {
		start = total
	}
	end := start + limit
	if end < start || end > total {
		end = total
	}

	return logs[start:end], total, nil
}

const settingsCacheTTL = 60 * time.Second

// GetAppSettings returns the stored app settings for MemoryStore (empty
// settings when none were saved). Cached in-process for 60s (A1). Env
// defaults are applied by the caller (handlers), not here.
func (s *MemoryStore) GetAppSettings(_ context.Context) (*models.AppSettings, error) {
	s.settingsMu.RLock()
	if s.cachedSettings != nil && time.Since(s.cachedSettingsAt) < settingsCacheTTL {
		val := s.cachedSettings.Clone()
		s.settingsMu.RUnlock()
		return val, nil
	}
	s.settingsMu.RUnlock()

	s.settingsMu.Lock()
	defer s.settingsMu.Unlock()
	if s.cachedSettings != nil && time.Since(s.cachedSettingsAt) < settingsCacheTTL {
		return s.cachedSettings.Clone(), nil
	}

	s.mu.RLock()
	raw := s.settings
	s.mu.RUnlock()

	stored := &models.AppSettings{ID: models.AppSettingsID}
	if raw != nil {
		stored = raw.Clone()
	}
	s.cachedSettings = stored.Clone()
	s.cachedSettingsAt = time.Now()
	return stored, nil
}

// SaveAppSettings updates the single app settings for MemoryStore
// and invalidates the in-process cache (A1).
func (s *MemoryStore) SaveAppSettings(_ context.Context, settings *models.AppSettings) error {
	if settings == nil {
		return errors.New("store: nil app settings")
	}
	toSave := settings.Clone()
	toSave.ID = models.AppSettingsID

	s.mu.Lock()
	s.settings = toSave.Clone()
	s.mu.Unlock()

	s.settingsMu.Lock()
	s.cachedSettings = nil
	s.cachedSettingsAt = time.Time{}
	s.settingsMu.Unlock()

	return nil
}

// ListIAPPackages returns IAP packages, optionally filtered by published status,
// ordered by position ascending, then title_ar ascending.
func (s *MemoryStore) ListIAPPackages(_ context.Context, publishedOnly bool) ([]*models.IAPPackage, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var result []*models.IAPPackage
	for _, pkg := range s.iapPackages {
		if publishedOnly && !pkg.Published {
			continue
		}
		result = append(result, cloneIAPPackage(pkg))
	}

	sort.Slice(result, func(i, j int) bool {
		if result[i].Position != result[j].Position {
			return result[i].Position < result[j].Position
		}
		return result[i].TitleAr < result[j].TitleAr
	})

	if result == nil {
		result = []*models.IAPPackage{}
	}
	return result, nil
}

// GetIAPPackage returns a package by ID, or nil if not found.
func (s *MemoryStore) GetIAPPackage(_ context.Context, id string) (*models.IAPPackage, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	pkg, ok := s.iapPackages[id]
	if !ok {
		return nil, nil
	}
	return cloneIAPPackage(pkg), nil
}

// GetIAPPackageByProductID returns a package by its Apple Product ID, or nil if not found.
func (s *MemoryStore) GetIAPPackageByProductID(_ context.Context, productID string) (*models.IAPPackage, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, pkg := range s.iapPackages {
		if pkg.ProductID == productID {
			return cloneIAPPackage(pkg), nil
		}
	}
	return nil, nil
}

// CreateIAPPackage stores a new IAP package. ProductID and ID must be unique.
func (s *MemoryStore) CreateIAPPackage(_ context.Context, pkg *models.IAPPackage) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if pkg == nil {
		return errors.New("store: nil iap package")
	}
	if pkg.ID == "" {
		pkg.ID = generateID()
	}
	if _, exists := s.iapPackages[pkg.ID]; exists {
		return ErrDuplicate
	}
	for _, existing := range s.iapPackages {
		if existing.ProductID == pkg.ProductID {
			return ErrDuplicate
		}
	}

	cp := cloneIAPPackage(pkg)
	now := time.Now().UTC()
	if cp.CreatedAt.IsZero() {
		cp.CreatedAt = now
	}
	if cp.UpdatedAt.IsZero() {
		cp.UpdatedAt = now
	}
	s.iapPackages[cp.ID] = cp
	return nil
}

// UpdateIAPPackage updates package fields. ProductID is immutable and never updated.
func (s *MemoryStore) UpdateIAPPackage(_ context.Context, pkg *models.IAPPackage) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if pkg == nil {
		return errors.New("store: nil iap package")
	}
	existing, ok := s.iapPackages[pkg.ID]
	if !ok {
		return ErrNotFound
	}

	// Update mutable fields only; preserve existing.ProductID
	existing.TitleAr = pkg.TitleAr
	existing.TitleEn = pkg.TitleEn
	existing.DescriptionAr = pkg.DescriptionAr
	existing.DescriptionEn = pkg.DescriptionEn
	if pkg.SubjectIDs != nil {
		existing.SubjectIDs = append([]string(nil), pkg.SubjectIDs...)
	} else {
		existing.SubjectIDs = []string{}
	}
	if pkg.AvailableUntil != nil {
		t := *pkg.AvailableUntil
		existing.AvailableUntil = &t
	} else {
		existing.AvailableUntil = nil
	}
	existing.Published = pkg.Published
	existing.Position = pkg.Position
	existing.UpdatedAt = time.Now().UTC()

	return nil
}

// DeleteIAPPackage deletes a package if no purchases reference it. Returns ErrConflict
// if purchases exist, or ErrNotFound if the package does not exist.
func (s *MemoryStore) DeleteIAPPackage(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.iapPackages[id]; !ok {
		return ErrNotFound
	}

	for _, p := range s.iapPurchases {
		if p.PackageID == id {
			return ErrConflict
		}
	}

	delete(s.iapPackages, id)
	return nil
}

// CountIAPPurchasesByPackage counts purchases referencing packageID.
func (s *MemoryStore) CountIAPPurchasesByPackage(_ context.Context, packageID string) (int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	count := 0
	for _, p := range s.iapPurchases {
		if p.PackageID == packageID {
			count++
		}
	}
	return count, nil
}

// InsertIAPPurchase stores a purchase. TransactionID must be unique.
func (s *MemoryStore) InsertIAPPurchase(_ context.Context, purchase *models.IAPPurchase) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if purchase == nil {
		return errors.New("store: nil iap purchase")
	}
	for _, existing := range s.iapPurchases {
		if existing.TransactionID == purchase.TransactionID {
			return ErrDuplicate
		}
	}

	cp := cloneIAPPurchase(purchase)
	if cp.ID == "" {
		cp.ID = generateID()
	}
	if cp.CreatedAt.IsZero() {
		cp.CreatedAt = time.Now().UTC()
	}
	if cp.GrantedSubjectIDs == nil {
		cp.GrantedSubjectIDs = []string{}
	}
	if cp.Skipped == nil {
		cp.Skipped = []models.IAPSkipped{}
	}
	s.iapPurchases = append(s.iapPurchases, cp)
	return nil
}

// GetIAPPurchaseByTransactionID returns a purchase by Apple transaction ID, or nil if not found.
func (s *MemoryStore) GetIAPPurchaseByTransactionID(_ context.Context, transactionID string) (*models.IAPPurchase, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, p := range s.iapPurchases {
		if p.TransactionID == transactionID {
			return cloneIAPPurchase(p), nil
		}
	}
	return nil, nil
}

// ListIAPPurchases returns purchases matching filter, newest first (by CreatedAt desc),
// capped by limit if limit > 0.
func (s *MemoryStore) ListIAPPurchases(_ context.Context, filter models.IAPPurchaseFilter, limit int) ([]*models.IAPPurchase, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var result []*models.IAPPurchase
	for _, p := range s.iapPurchases {
		if filter.UserID != "" && p.UserID != filter.UserID {
			continue
		}
		if filter.PackageID != "" && p.PackageID != filter.PackageID {
			continue
		}
		result = append(result, cloneIAPPurchase(p))
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].CreatedAt.After(result[j].CreatedAt)
	})

	if limit > 0 && len(result) > limit {
		result = result[:limit]
	}
	if result == nil {
		result = []*models.IAPPurchase{}
	}
	return result, nil
}

// RemoveSubjectFromAllPackages removes a subject ID from all packages.
func (s *MemoryStore) RemoveSubjectFromAllPackages(_ context.Context, subjectID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.removeSubjectFromAllPackagesLocked(subjectID)
	return nil
}

func (s *MemoryStore) removeSubjectFromAllPackagesLocked(subjectID string) {
	now := time.Now().UTC()
	for _, pkg := range s.iapPackages {
		var newIDs []string
		changed := false
		for _, id := range pkg.SubjectIDs {
			if id == subjectID {
				changed = true
				continue
			}
			newIDs = append(newIDs, id)
		}
		if changed {
			pkg.SubjectIDs = newIDs
			pkg.UpdatedAt = now
		}
	}
}

func cloneIAPPackage(pkg *models.IAPPackage) *models.IAPPackage {
	if pkg == nil {
		return nil
	}
	cp := *pkg
	if pkg.SubjectIDs != nil {
		cp.SubjectIDs = append([]string(nil), pkg.SubjectIDs...)
	} else {
		cp.SubjectIDs = []string{}
	}
	if pkg.AvailableUntil != nil {
		t := *pkg.AvailableUntil
		cp.AvailableUntil = &t
	}
	return &cp
}

func cloneIAPPurchase(p *models.IAPPurchase) *models.IAPPurchase {
	if p == nil {
		return nil
	}
	cp := *p
	if p.GrantedSubjectIDs != nil {
		cp.GrantedSubjectIDs = append([]string(nil), p.GrantedSubjectIDs...)
	} else {
		cp.GrantedSubjectIDs = []string{}
	}
	if p.Skipped != nil {
		cp.Skipped = append([]models.IAPSkipped(nil), p.Skipped...)
	} else {
		cp.Skipped = []models.IAPSkipped{}
	}
	return &cp
}
