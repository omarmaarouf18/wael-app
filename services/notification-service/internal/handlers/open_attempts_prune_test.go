package handlers

import (
	"testing"
	"time"
)

// TestStreamLimiter_OpenAttempts_PrunedForInactiveUsers tests item 4.4:
// Users who stop connecting must be pruned from openAttempts so the map
// does not grow indefinitely with stale keys.
func TestStreamLimiter_OpenAttempts_PrunedForInactiveUsers(t *testing.T) {
	window := time.Minute
	sl := NewStreamLimiter(3, 10, window)

	t0 := time.Date(2026, 11, 11, 10, 0, 0, 0, time.UTC)

	// User 1 and User 2 connect at t0
	sl.allowStreamOpenAt("user-inactive-1", t0)
	sl.allowStreamOpenAt("user-inactive-2", t0)

	if count := sl.OpenAttemptsCount(); count != 2 {
		t.Fatalf("expected 2 tracked users in openAttempts, got %d", count)
	}

	// Fast-forward 2 minutes: all attempts for user-1 and user-2 are now stale (> 1 minute window)
	t1 := t0.Add(2 * time.Minute)

	// Prune stale attempts
	pruned := sl.PruneStaleAttempts(t1)
	if pruned != 2 {
		t.Errorf("expected 2 pruned users, got %d", pruned)
	}

	if count := sl.OpenAttemptsCount(); count != 0 {
		t.Errorf("expected 0 tracked users after prune, got %d (stale users not pruned)", count)
	}

	// Also verify opportunistic sweep in allowStreamOpenAt prunes stale users
	sl.allowStreamOpenAt("user-inactive-3", t1)
	t2 := t1.Add(2 * time.Minute)
	// At t2, user-3 is stale; user-4 connects and triggers opportunistic sweep
	for i := 0; i < 64; i++ {
		sl.allowStreamOpenAt("user-active-4", t2)
	}

	// user-inactive-3 must have been pruned by the opportunistic sweep
	sl.mu.Lock()
	_, foundUser3 := sl.openAttempts["user-inactive-3"]
	sl.mu.Unlock()

	if foundUser3 {
		t.Errorf("expected user-inactive-3 to be opportunistically pruned, but was still present")
	}
}
