package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/omarmaarouf18/wael-app/auth-service/internal/store"
)

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

func runRevokeAdmin(args []string, stdout, stderr io.Writer, getStore storeProvider) int {
	fs := flag.NewFlagSet("revoke-admin", flag.ContinueOnError)
	fs.SetOutput(stderr)
	id := fs.String("id", "", "ID of the admin to revoke (required)")

	if err := fs.Parse(args); err != nil {
		return 1
	}

	adminID := strings.TrimSpace(*id)
	if adminID == "" {
		fmt.Fprintln(stderr, "error: --id is required")
		return 1
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	st, err := getStore(ctx)
	if err != nil {
		fmt.Fprintf(stderr, "error connecting to store: %v\n", err)
		return 1
	}

	now := time.Now().UTC()
	if err := st.RevokeAdmin(ctx, adminID, now); err != nil {
		if errors.Is(err, store.ErrAdminNotFound) {
			fmt.Fprintf(stderr, "error: admin %q not found\n", adminID)
			return 1
		}
		fmt.Fprintf(stderr, "error revoking admin: %v\n", err)
		return 1
	}

	fmt.Fprintf(stdout, "admin %q revoked\n", adminID)
	return 0
}

func main() {
	os.Exit(runRevokeAdmin(os.Args[1:], os.Stdout, os.Stderr, defaultMongoStore))
}
