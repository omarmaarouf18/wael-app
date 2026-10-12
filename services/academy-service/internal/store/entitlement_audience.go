// Active owner paging for broadcast audiences (SPEC Phase 7, ADR-0013).
//
// These methods live in their own file so audience resolution needs no
// changes to the Store interface or existing files: handlers reach them
// through the OwnerIDStore interface below. Active follows SPEC R1 exactly:
// active flag true, expires_at in the future, no revoked_at marker.
package store

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// OwnerIDStore pages active entitlement owners per subject.
type OwnerIDStore interface {
	ListActiveOwnerIDsBySubject(ctx context.Context, subjectID, cursor string, limit int) ([]string, error)
	CountActiveOwnersBySubject(ctx context.Context, subjectID string) (int, error)
}

// audienceLimit bounds one owner-ids page.
func audienceLimit(limit int) int {
	if limit <= 0 {
		return 100
	}
	if limit > 1000 {
		return 1000
	}
	return limit
}

// activeOwnerFilter matches unexpired, unrevoked, active entitlements.
func activeOwnerFilter(subjectID string, now time.Time) bson.M {
	return bson.M{
		"subject_id": subjectID,
		"active":     true,
		"expires_at": bson.M{"$gt": now},
		"$or": []bson.M{
			{"revoked_at": nil},
			{"revoked_at": bson.M{"$exists": false}},
		},
	}
}

var ensureAudienceIndexOnce sync.Once

// ensureAudienceIndex creates the (subject_id, user_id) index once, lazily,
// so audience paging never needs a startup-time change elsewhere.
func (s *MongoStore) ensureAudienceIndex(ctx context.Context) {
	ensureAudienceIndexOnce.Do(func() {
		_, err := s.db.Collection("entitlements").Indexes().CreateOne(ctx, mongo.IndexModel{
			Keys:    bson.D{{Key: "subject_id", Value: 1}, {Key: "user_id", Value: 1}},
			Options: options.Index().SetName("idx_entitlements_subject_user"),
		})
		if err != nil {
			// Index creation is best-effort: queries stay correct without it.
			fmt.Printf("store: ensure entitlements subject_user index: %v\n", err)
		}
	})
}

// ListActiveOwnerIDsBySubject returns up to limit active owner user ids for
// a subject, after cursor (exclusive), ordered by user id. An empty cursor
// starts from the beginning.
func (s *MongoStore) ListActiveOwnerIDsBySubject(ctx context.Context, subjectID, cursor string, limit int) ([]string, error) {
	s.ensureAudienceIndex(ctx)
	limit = audienceLimit(limit)
	filter := activeOwnerFilter(subjectID, time.Now())
	if cursor != "" {
		filter["user_id"] = bson.M{"$gt": cursor}
	}
	opts := options.Find().
		SetSort(bson.D{{Key: "user_id", Value: 1}}).
		SetLimit(int64(limit)).
		SetProjection(bson.M{"user_id": 1, "_id": 0})
	cur, err := s.db.Collection("entitlements").Find(ctx, filter, opts)
	if err != nil {
		return nil, fmt.Errorf("store: list active owners: %w", err)
	}
	defer func() { _ = cur.Close(ctx) }()
	var rows []struct {
		UserID string `bson:"user_id"`
	}
	if err := cur.All(ctx, &rows); err != nil {
		return nil, fmt.Errorf("store: decode active owners: %w", err)
	}
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		if r.UserID != "" {
			out = append(out, r.UserID)
		}
	}
	return out, nil
}

// CountActiveOwnersBySubject counts active owners of a subject.
func (s *MongoStore) CountActiveOwnersBySubject(ctx context.Context, subjectID string) (int, error) {
	n, err := s.db.Collection("entitlements").CountDocuments(ctx, activeOwnerFilter(subjectID, time.Now()))
	if err != nil {
		return 0, fmt.Errorf("store: count active owners: %w", err)
	}
	return int(n), nil
}

// ListActiveOwnerIDsBySubject returns up to limit active owner user ids for
// a subject, after cursor (exclusive), ordered by user id.
func (s *MemoryStore) ListActiveOwnerIDsBySubject(_ context.Context, subjectID, cursor string, limit int) ([]string, error) {
	limit = audienceLimit(limit)
	now := time.Now()
	s.mu.RLock()
	defer s.mu.RUnlock()
	var ids []string
	for _, e := range s.entitlements {
		if e.SubjectID != subjectID || !e.Active || e.IsRevoked() || !e.ExpiresAt.After(now) {
			continue
		}
		if cursor != "" && e.UserID <= cursor {
			continue
		}
		ids = append(ids, e.UserID)
	}
	sort.Strings(ids)
	// One active row per (user, subject) is enforced by Grant, but dedupe
	// defensively so an id is never returned twice.
	out := ids[:0]
	for i, id := range ids {
		if i == 0 || id != ids[i-1] {
			out = append(out, id)
		}
	}
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// CountActiveOwnersBySubject counts active owners of a subject.
func (s *MemoryStore) CountActiveOwnersBySubject(_ context.Context, subjectID string) (int, error) {
	now := time.Now()
	s.mu.RLock()
	defer s.mu.RUnlock()
	seen := map[string]bool{}
	for _, e := range s.entitlements {
		if e.SubjectID != subjectID || !e.Active || e.IsRevoked() || !e.ExpiresAt.After(now) {
			continue
		}
		seen[e.UserID] = true
	}
	return len(seen), nil
}
