package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/omarmaarouf18/wael-app/auth-service/internal/models"
	"github.com/omarmaarouf18/wael-app/auth-service/internal/store"
)

func parseTTL(raw string) (time.Duration, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 90 * 24 * time.Hour, nil
	}
	if strings.HasSuffix(raw, "d") {
		daysStr := strings.TrimSuffix(raw, "d")
		days, err := strconv.Atoi(daysStr)
		if err != nil || days <= 0 {
			return 0, fmt.Errorf("invalid ttl days %q", raw)
		}
		return time.Duration(days) * 24 * time.Hour, nil
	}
	d, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("invalid ttl duration %q: %w", raw, err)
	}
	if d <= 0 {
		return 0, fmt.Errorf("ttl must be positive: %v", d)
	}
	return d, nil
}

type storeProvider func(ctx context.Context) (store.Store, error)

func defaultMongoStore(ctx context.Context) (store.Store, error) {
	mongoURI := os.Getenv("MONGO_URI")
	if mongoURI == "" {
		return nil, fmt.Errorf("MONGO_URI is empty")
	}
	dbName := os.Getenv("AUTH_MONGO_DATABASE")
	if dbName == "" {
		dbName = os.Getenv("MONGO_INITDB_DATABASE")
	}
	if dbName == "" {
		dbName = "auth_db"
	}
	return store.NewMongoStore(ctx, mongoURI, dbName)
}

func runOnboardAdmin(args []string, stdout, stderr io.Writer, getStore storeProvider) int {
	fs := flag.NewFlagSet("onboard-admin", flag.ContinueOnError)
	fs.SetOutput(stderr)
	name := fs.String("name", "", "name of the admin operator (required)")
	ttlStr := fs.String("ttl", "90d", "token TTL (e.g. 90d, default 90d, max 365d)")

	if err := fs.Parse(args); err != nil {
		return 1
	}

	adminName := strings.TrimSpace(*name)
	if adminName == "" {
		fmt.Fprintln(stderr, "error: --name is required")
		return 1
	}

	ttl, err := parseTTL(*ttlStr)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}

	const maxTTL = 365 * 24 * time.Hour
	if ttl > maxTTL {
		fmt.Fprintln(stderr, "error: ttl exceeds maximum allowed (365d)")
		return 1
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	st, err := getStore(ctx)
	if err != nil {
		fmt.Fprintf(stderr, "error connecting to store: %v\n", err)
		return 1
	}

	// Generate >= 32 cryptographically secure random bytes
	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		fmt.Fprintf(stderr, "error generating token: %v\n", err)
		return 1
	}
	token := base64.RawURLEncoding.EncodeToString(tokenBytes)

	// Compute SHA-256 hex hash
	hashBytes := sha256.Sum256([]byte(token))
	tokenHash := hex.EncodeToString(hashBytes[:])

	// Generate admin ID
	idBytes := make([]byte, 12)
	if _, err := rand.Read(idBytes); err != nil {
		fmt.Fprintf(stderr, "error generating id: %v\n", err)
		return 1
	}
	adminID := "adm_" + hex.EncodeToString(idBytes)

	now := time.Now().UTC()
	adm := &models.Admin{
		ID:        adminID,
		Name:      adminName,
		TokenHash: tokenHash,
		CreatedAt: now,
		ExpiresAt: now.Add(ttl),
	}

	if err := st.CreateAdmin(ctx, adm); err != nil {
		fmt.Fprintf(stderr, "error storing admin: %v\n", err)
		return 1
	}

	// Print the token ONCE to stdout, never log it
	fmt.Fprintln(stdout, token)
	return 0
}

func main() {
	os.Exit(runOnboardAdmin(os.Args[1:], os.Stdout, os.Stderr, defaultMongoStore))
}
