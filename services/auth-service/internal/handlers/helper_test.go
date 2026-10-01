package handlers

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"testing"
	"time"
)

func requireDB(t *testing.T) (mongoURI, redisURI string) {
	t.Helper()
	mongoURI = os.Getenv("MONGO_URI")
	redisURI = os.Getenv("REDIS_URI")
	if mongoURI == "" {
		if os.Getenv("REQUIRE_DB") == "1" {
			t.Fatalf("MONGO_URI is required when REQUIRE_DB=1")
		}
		t.Skip("skipping test: MONGO_URI is empty")
	}
	return mongoURI, redisURI
}

func randomDBName(prefix string) string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return fmt.Sprintf("%s_%d_%s", prefix, time.Now().UnixNano(), hex.EncodeToString(b))
}
