// Package testutil provides the fixed test-store identity shared by every
// package that exercises store-scoped repositories against testing
// databases. Tests are bound to a single synthetic store because the repos take
// their store at construction time; reset() (re)creates this row so a
// TRUNCATE between tests never leaves the repos pointing at a deleted store.
package testutil

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mohi/pms-marg-inspired/internal/database"
)

// StoreID is the fixed UUID identifying the single canonical test store.
const StoreID = "00000000-0000-0000-0000-000000000001"

// StoreIDPtr returns a pointer to StoreID for inputs that take *string.
func StoreIDPtr() *string {
	s := StoreID
	return &s
}

// SeedStore inserts the canonical test store (no-op if it already exists).
func SeedStore(ctx context.Context, pool *pgxpool.Pool) error {
	_, err := pool.Exec(ctx, `
		INSERT INTO stores (id, name, address, max_employees)
		VALUES ('00000000-0000-0000-0000-000000000001', 'Test Store', '', 2)
		ON CONFLICT (id) DO NOTHING`)
	return err
}

// testDBNames maps each DB-backed test package to its own database. `go test
// ./...` runs packages in parallel, and every package TRUNCATEs shared table
// names between tests — sharing one database across packages deadlocks on
// table locks (ACCESS EXCLUSIVE vs concurrent writers). One database per
// package keeps `go test ./...` hermetic.
var testDBNames = map[string]string{
	"repository": "pms_test",
	"handlers":   "pms_test_handlers",
	"gst":        "pms_test_gst",
}

// TestDBURL resolves the database URL for a test package: TEST_DATABASE_URL
// supplies host/credentials (its path is replaced with the package's own
// database), otherwise the local-dev default is used.
func TestDBURL(pkg string) string {
	name, ok := testDBNames[pkg]
	if !ok || name == "" {
		name = "pms_test"
	}
	base := os.Getenv("TEST_DATABASE_URL")
	if base == "" {
		base = "postgres://postgres:postgres@localhost:5432/pms_test?sslmode=disable"
	}
	u, err := url.Parse(base)
	if err != nil || u.Scheme == "" {
		return base
	}
	u.Path = "/" + name
	return u.String()
}

// ConnectTestDB connects to the package's test database, creating it (via
// the maintenance database) when it does not exist yet, and applies all
// migrations. It is the single entry point for every TestMain that needs a
// database.
func ConnectTestDB(ctx context.Context, pkg string) (*pgxpool.Pool, error) {
	target := TestDBURL(pkg)
	if pool, err := database.Connect(ctx, target); err == nil {
		if err := database.Migrate(ctx, pool); err != nil {
			pool.Close()
			return nil, fmt.Errorf("migrate %s: %w", target, err)
		}
		return pool, nil
	}

	// Target missing (or unreachable) — try to create it, then connect.
	u, err := url.Parse(target)
	if err != nil {
		return nil, fmt.Errorf("parse test db url: %w", err)
	}
	dbName := strings.TrimPrefix(u.Path, "/")
	u.Path = "/postgres"
	admin, err := database.Connect(ctx, u.String())
	if err != nil {
		return nil, fmt.Errorf("connect maintenance db: %w", err)
	}
	defer admin.Close()
	if _, err := admin.Exec(ctx, `CREATE DATABASE "`+dbName+`"`); err != nil &&
		!strings.Contains(err.Error(), "already exists") {
		return nil, fmt.Errorf("create test db %s: %w", dbName, err)
	}
	pool, err := database.Connect(ctx, target)
	if err != nil {
		return nil, fmt.Errorf("connect test db %s: %w", dbName, err)
	}
	if err := database.Migrate(ctx, pool); err != nil {
		pool.Close()
		return nil, fmt.Errorf("migrate %s: %w", dbName, err)
	}
	return pool, nil
}
