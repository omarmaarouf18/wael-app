package handlers

import (
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/omarmaarouf18/wael-app/shared/infra/jwtutil"
	"github.com/redis/go-redis/v9"
)

// TestSSE_HeartbeatRecheck_SustainedRedisError_ClosesStream tests item 4.3:
// When Redis suffers a sustained outage during SSE heartbeat revocation checks,
// the stream must fail closed and terminate after consecutive errors, rather than
// remaining open indefinitely (failing open).
func TestSSE_HeartbeatRecheck_SustainedRedisError_ClosesStream(t *testing.T) {
	jwtutil.Init("test-jwt-secret-0123456789abcdef")
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr(), MaxRetries: -1})
	defer rdb.Close()
	jwtutil.SetRedisClient(rdb)
	defer jwtutil.SetRedisClient(nil)

	s := testServer()
	s.HeartbeatInterval = 20 * time.Millisecond
	s.CheckEveryNth = 1
	s.MaxHeartbeatRevocationErrors = 3

	sid := "sid-redis-sustained-fail"
	tok, err := jwtutil.GenerateTokenWithSession("user-redis-sustained", "user", "sustained@example.com", sid)
	if err != nil {
		t.Fatal(err)
	}

	_, cancel, done := openTestStream(t, s, tok)
	defer cancel()

	// Inject sustained Redis error
	mr.SetError("ERR sustained redis connection refused")

	// The stream must fail-closed and terminate within reasonable time (e.g. 5 heartbeats * 20ms = ~100ms)
	select {
	case <-done:
		// Stream terminated due to sustained Redis errors (fail closed)
	case <-time.After(300 * time.Millisecond):
		t.Fatal("stream remained open after sustained Redis errors; expected stream to fail closed and close")
	}
}
