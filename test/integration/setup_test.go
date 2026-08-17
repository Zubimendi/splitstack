package integration

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"

	"github.com/Zubimendi/splitstack/internal/db"
	"github.com/Zubimendi/splitstack/internal/ledger"
)

func setupTestDB(t *testing.T) (*pgxpool.Pool, *ledger.Engine) {
	t.Helper()

	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		t.Skip("DATABASE_URL not set, skipping integration test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	pool, err := db.Connect(ctx, dbURL)
	if err != nil {
		t.Fatalf("failed to connect to db: %v", err)
	}

	log, _ := zap.NewDevelopment()
	engine := ledger.NewEngine(pool, log)

	// Clean up all tables before each test to ensure isolation.
	// Since ON DELETE CASCADE is set up, deleting groups and users should clear everything.
	_, err = pool.Exec(ctx, `TRUNCATE TABLE users, groups CASCADE;`)
	if err != nil {
		t.Fatalf("failed to truncate tables: %v", err)
	}

	return pool, engine
}
