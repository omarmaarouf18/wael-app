// Command create-admin bootstraps an admin account directly in MongoDB.
// Ops use only: it is not exposed via HTTP. Requires MONGO_URI.
//
//	MONGO_URI=mongodb://localhost:27017 go run ./services/auth-service/cmd/create-admin --email=admin@example.com --password=... --yes
package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/omarmaarouf18/wael-app/auth-service/internal/models"
	"github.com/omarmaarouf18/wael-app/auth-service/internal/store"
	"github.com/omarmaarouf18/wael-app/shared/infra/jwtutil"
	"golang.org/x/crypto/bcrypt"
)

func main() {
	email := flag.String("email", "", "admin email (required)")
	password := flag.String("password", "", "admin password, min 8 chars (required)")
	yes := flag.Bool("yes", false, "skip confirmation prompt")
	flag.Parse()

	if strings.TrimSpace(*email) == "" || len(*password) < 8 {
		fmt.Fprintln(os.Stderr, "usage: create-admin --email=admin@example.com --password=<min-8-chars> [--yes]")
		os.Exit(2)
	}
	mongoURI := os.Getenv("MONGO_URI")
	if mongoURI == "" {
		log.Fatal("MONGO_URI is empty: the CLI requires shared MongoDB persistence (memory store is in-process only)")
	}
	dbName := os.Getenv("AUTH_MONGO_DATABASE")
	if dbName == "" {
		dbName = os.Getenv("MONGO_INITDB_DATABASE")
	}
	if dbName == "" {
		dbName = "auth_db"
	}

	lowered := strings.ToLower(strings.TrimSpace(*email))
	if !*yes {
		fmt.Printf("Create admin %q in database %q? (y/N): ", lowered, dbName)
		reader := bufio.NewReader(os.Stdin)
		line, _ := reader.ReadString('\n')
		answer := strings.ToLower(strings.TrimSpace(line))
		if answer != "y" && answer != "yes" {
			fmt.Println("aborted")
			return
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	st, err := store.NewMongoStore(ctx, mongoURI, dbName)
	if err != nil {
		log.Fatalf("mongo: %v", err)
	}
	if existing, _ := st.FindByEmail(ctx, lowered); existing != nil {
		log.Fatalf("admin %q already exists", lowered)
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(*password), bcrypt.DefaultCost)
	if err != nil {
		log.Fatalf("hash password: %v", err)
	}
	id, err := jwtutil.GenerateUUID()
	if err != nil {
		log.Fatalf("generate id: %v", err)
	}
	u := &models.User{ID: id, Email: lowered, PasswordHash: string(hash), Role: models.RoleAdmin, EmailVerified: true}
	if err := st.Create(ctx, u); err != nil {
		log.Fatalf("create admin: %v", err)
	}
	fmt.Printf("admin %q created (id %s)\n", lowered, id)
}
