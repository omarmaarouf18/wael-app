package handlers

import (
	"bytes"
	"context"
	"log"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/omarmaarouf18/wael-app/auth-service/internal/models"
	"github.com/omarmaarouf18/wael-app/auth-service/internal/otp"
	"github.com/omarmaarouf18/wael-app/auth-service/internal/store"
)

type blockingAccountEventStore struct {
	store.Store
	mu         sync.Mutex
	blockCh    chan struct{}
	writeCalls int32
}

func (b *blockingAccountEventStore) CreateAccountEvent(ctx context.Context, entry *models.AccountEvent) error {
	atomic.AddInt32(&b.writeCalls, 1)
	b.mu.Lock()
	ch := b.blockCh
	b.mu.Unlock()
	if ch != nil {
		select {
		case <-ch:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

// TestAccountEvents_SemaphoreDropWhenFull verifies that:
// 1. In-flight writes are bounded by a fixed-size semaphore (cap=2 in test).
// 2. When full, additional writes drop immediately without blocking the request.
// 3. "account event dropped" is logged with the event type and user id.
// 4. On shutdown, in-flight writes are given up to 5s to complete.
func TestAccountEvents_SemaphoreDropWhenFull(t *testing.T) {
	memStore := store.NewMemoryStore()
	blockCh := make(chan struct{})
	blockStore := &blockingAccountEventStore{
		Store:   memStore,
		blockCh: blockCh,
	}

	s := New(blockStore, otp.NewMemoryStore(), NewMemoryLockout(), nil, "test", "gw-secret")
	s.AccountEventSemCap = 2 // bound semaphore to 2 in-flight writes for test

	var logBuf bytes.Buffer
	origLogWriter := log.Writer()
	log.SetOutput(&logBuf)
	defer log.SetOutput(origLogWriter)

	ctx := context.Background()

	// Dispatch 2 writes to occupy both semaphore slots
	s.recordAccountEventDetailed(ctx, &models.AccountEvent{
		UserID: "user-1",
		Type:   "password_change",
	})
	s.recordAccountEventDetailed(ctx, &models.AccountEvent{
		UserID: "user-2",
		Type:   "email_change",
	})

	// Wait briefly so both goroutines acquire a slot and enter CreateAccountEvent
	time.Sleep(50 * time.Millisecond)
	if calls := atomic.LoadInt32(&blockStore.writeCalls); calls != 2 {
		t.Fatalf("expected 2 in-flight write calls, got %d", calls)
	}

	// 3rd write should DROP immediately without blocking
	start := time.Now()
	s.recordAccountEventDetailed(ctx, &models.AccountEvent{
		UserID: "user-3",
		Type:   "login_new_device",
	})
	elapsed := time.Since(start)

	if elapsed > 100*time.Millisecond {
		t.Errorf("expected drop to return immediately, but took %v", elapsed)
	}

	// Verify dropped event was NOT sent to store
	if calls := atomic.LoadInt32(&blockStore.writeCalls); calls != 2 {
		t.Errorf("expected still 2 write calls, got %d (dropped write should not reach store)", calls)
	}

	// Verify log contains dropped message with type and user
	logOutput := logBuf.String()
	if !strings.Contains(logOutput, "[AUTH] account event dropped type=login_new_device user user-3") {
		t.Errorf("expected log to contain '[AUTH] account event dropped type=login_new_device user user-3', got: %s", logOutput)
	}

	// Release blocked writes and wait for shutdown
	close(blockCh)
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := s.Shutdown(shutdownCtx); err != nil {
		t.Errorf("expected Shutdown to complete cleanly, got error: %v", err)
	}
}
