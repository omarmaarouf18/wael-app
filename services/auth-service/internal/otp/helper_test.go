package otp

import (
	"os"
	"testing"
)

func requireDB(t *testing.T) (mongoURI, redisURI string) {
	t.Helper()
	mongoURI = os.Getenv("MONGO_URI")
	redisURI = os.Getenv("REDIS_URI")
	if redisURI == "" {
		if os.Getenv("REQUIRE_DB") == "1" {
			t.Fatalf("REDIS_URI is required when REQUIRE_DB=1")
		}
		t.Skip("skipping test: REDIS_URI is empty")
	}
	return mongoURI, redisURI
}
