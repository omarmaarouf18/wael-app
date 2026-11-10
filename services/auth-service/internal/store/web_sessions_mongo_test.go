package store

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/omarmaarouf18/wael-app/auth-service/internal/models"
)

// TestMongoStore_WebSessionsCappedAt5 verifies that MongoStore trims active web sessions
// to at most 5 per user, ending older ones when a 6th is created (Item 1.1 / M3).
func TestMongoStore_WebSessionsCappedAt5(t *testing.T) {
	mongoURI, _ := requireDB(t)
	dbName := randomDBName("test_web_session_cap")
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	s, err := NewMongoStore(ctx, mongoURI, dbName)
	if err != nil {
		t.Fatalf("NewMongoStore: %v", err)
	}
	t.Cleanup(func() {
		_ = s.coll.Database().Drop(context.Background())
	})

	userID := "u-mongo-webcap"
	now := time.Now().UTC()

	// Insert 6 web sessions sequentially
	var allEnded []*models.Session
	for i := 1; i <= 6; i++ {
		sess := &models.Session{
			ID:          fmt.Sprintf("sid-web-%d", i),
			UserID:      userID,
			DeviceID:    fmt.Sprintf("web-dev-%d", i),
			DeviceLabel: fmt.Sprintf("Browser %d", i),
			Client:      "web",
			RefreshHash: fmt.Sprintf("ref-hash-%d", i),
			CreatedAt:   now.Add(time.Duration(i) * time.Second),
			LastUsedAt:  now.Add(time.Duration(i) * time.Second),
		}
		ended, err := s.CreateOrReplaceSession(ctx, sess)
		if err != nil {
			t.Fatalf("CreateOrReplaceSession %d: %v", i, err)
		}
		allEnded = append(allEnded, ended...)
	}

	active, err := s.ListActiveSessions(ctx, userID)
	if err != nil {
		t.Fatalf("ListActiveSessions: %v", err)
	}

	var activeWeb []*models.Session
	for _, sess := range active {
		if sess.Client == "web" {
			activeWeb = append(activeWeb, sess)
		}
	}

	if len(activeWeb) != 5 {
		t.Fatalf("expected exactly 5 active web sessions, got %d", len(activeWeb))
	}

	// Verify that the 1st session (oldest) was ended
	s1, err := s.GetSession(ctx, "sid-web-1")
	if err != nil || s1 == nil {
		t.Fatalf("GetSession sid-web-1: %v", err)
	}
	if s1.EndedAt == nil {
		t.Fatalf("expected sid-web-1 to be ended, got active")
	}
	if s1.EndReason != models.EndReasonReplaced {
		t.Fatalf("expected sid-web-1 end_reason = 'replaced', got %q", s1.EndReason)
	}
}
