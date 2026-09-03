package repository

import (
	"context"
	"errors"
	"sync"

	"github.com/jackc/pgx/v5/pgxpool"
)

// storeIDRef resolves the store a constructor-bound repo is pinned to
// ("single-tenant server" model).
//
// On a seeded install the store exists before startup, so the ID is known at
// construction and pinned forever. On a fresh boot the first POST
// /api/auth/register creates the store AFTER the repos were built; the ref
// lazily adopts the first store on its first use so a bare install can
// bootstrap normally instead of resolving an empty UUID.
type storeIDRef struct {
	mu     sync.Mutex
	boot   string
	cached string
	db     *pgxpool.Pool
}

func newStoreIDRef(db *pgxpool.Pool, boot string) *storeIDRef {
	return &storeIDRef{db: db, boot: boot}
}

func (s *storeIDRef) get(ctx context.Context) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.boot != "" {
		return s.boot, nil
	}
	if s.cached == "" {
		id, err := FirstStoreID(ctx, s.db)
		if err != nil {
			return "", err
		}
		if id == "" {
			return "", errors.New("no store bound: complete tenant bootstrap first")
		}
		s.cached = id
	}
	return s.cached, nil
}