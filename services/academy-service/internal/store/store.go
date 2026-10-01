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
	ListSubjects(ctx context.Context, filter SubjectFilter) ([]*models.Subject, int, error)
	GetSubjectByID(ctx context.Context, id string) (*models.Subject, error)
	GetSubjectCounts(ctx context.Context, subjectID string) (models.SubjectCountsDTO, error)
	CreateVideo(ctx context.Context, v *models.Video) error
	ListVideosBySubject(ctx context.Context, subjectID string, onlyPublished bool) ([]*models.Video, error)
	CreateFile(ctx context.Context, f *models.SubjectFile) error
	ListFilesBySubject(ctx context.Context, subjectID string) ([]*models.SubjectFile, error)
}

// MemoryStore is an in-memory Store for local dev and unit testing.
type MemoryStore struct {
	mu       sync.RWMutex
	levels   map[string]*models.Level
	subjects map[string]*models.Subject
	videos   map[string]*models.Video
	files    map[string]*models.SubjectFile
}

// NewMemoryStore creates an empty MemoryStore.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		levels:   make(map[string]*models.Level),
		subjects: make(map[string]*models.Subject),
		videos:   make(map[string]*models.Video),
		files:    make(map[string]*models.SubjectFile),
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

// ListLevels returns levels sorted by position. If onlyWithPublished is true,
// levels with no published subjects are omitted.
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
		return result[i].Position < result[j].Position
	})

	return result, nil
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
