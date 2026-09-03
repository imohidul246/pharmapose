package repository

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// FirstStoreID resolves the store this install is bound to (single-tenant:
// the first store by creation order). Returns "" when no store exists yet, so
// callers can fall back to the bootstrap/register flow.
func FirstStoreID(ctx context.Context, pool *pgxpool.Pool) (string, error) {
	var id string
	err := pool.QueryRow(ctx,
		`SELECT id::text FROM stores ORDER BY created_at LIMIT 1`).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return id, nil
}

// normalizePhone folds a phone into a comparable form (digits only) so the
// unique index is not defeated by formatting differences.
func normalizePhone(phone string) string {
	var b strings.Builder
	for _, r := range phone {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// trimSpaces collapses surrounding whitespace on a single-line field.
func trimSpaces(s string) string { return strings.TrimSpace(s) }

// nullableString converts a *string into NULL-safe SQL binding.
func nullableString(p *string) interface{} {
	if p == nil {
		return nil
	}
	if *p == "" {
		return nil
	}
	return *p
}

// nullableDate converts a "YYYY-MM-DD" string pointer into a NULL-safe
// time.Time binding for a DATE column. An empty or nil value binds NULL.
func nullableDate(p *string) interface{} {
	if p == nil || *p == "" {
		return nil
	}
	t, err := time.Parse("2006-01-02", *p)
	if err != nil {
		return nil
	}
	return t
}

// isUniqueViolation reports whether err is a PostgreSQL unique_constraint
// violation (SQLSTATE 23505).
func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}