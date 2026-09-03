// Package testutil provides the fixed test-store identity shared by every
// package that exercises store-scoped repositories against the testing
// database. Tests are bound to a single synthetic store because the repos take
// their store at construction time; reset() (re)creates this row so a
// TRUNCATE between tests never leaves the repos pointing at a deleted store.
package testutil

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
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