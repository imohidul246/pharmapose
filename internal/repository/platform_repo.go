package repository

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mohi/pms-marg-inspired/internal/models"
)

// PlatformRepo handles global platform administration: listing every tenant
// store with subscription metrics, recording offline cash payments (which
// extend validity), toggling store status, and reading the payment ledger.
// Store operations elsewhere stay strictly tenant-scoped; only this repo
// deliberately crosses store boundaries, and every caller must be gated by
// auth.RequirePlatformAdmin().
type PlatformRepo struct {
	db *pgxpool.Pool
}

func NewPlatformRepo(db *pgxpool.Pool) *PlatformRepo {
	return &PlatformRepo{db: db}
}

// ListStores returns all stores with owner contact details, validity dates,
// status, and computed days remaining, newest last (creation order).
func (r *PlatformRepo) ListStores(ctx context.Context) ([]models.PlatformStoreInfo, error) {
	rows, err := r.db.Query(ctx, `
		SELECT s.id::text, s.name, COALESCE(s.address, ''), COALESCE(s.phone, ''),
		       s.is_active,
		       COALESCE(s.subscription_valid_until, NULL),
		       COALESCE(s.subscription_status, 'ACTIVE'),
		       s.created_at,
		       COALESCE(owner.name, ''), COALESCE(owner.phone, '')
		FROM stores s
		LEFT JOIN LATERAL (
			SELECT u.name, u.phone
			FROM store_memberships m
			JOIN users u ON u.id = m.user_id
			WHERE m.store_id = s.id AND m.role = 'STORE_OWNER' AND m.is_active = true
			ORDER BY m.created_at LIMIT 1
		) owner ON true
		ORDER BY s.created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	now := time.Now().UTC()
	out := make([]models.PlatformStoreInfo, 0)
	for rows.Next() {
		var info models.PlatformStoreInfo
		var validUntil *time.Time
		if err := rows.Scan(
			&info.StoreID, &info.StoreName, &info.StoreAddress, &info.StorePhone,
			&info.IsActive,
			&validUntil,
			&info.SubscriptionStatus,
			&info.CreatedAt,
			&info.OwnerName, &info.OwnerPhone,
		); err != nil {
			return nil, err
		}
		info.SubscriptionValidUntil = validUntil
		info.DaysRemaining = models.DaysRemainingUntil(validUntil, now)
		out = append(out, info)
	}
	return out, rows.Err()
}

// RecordPaymentAndExtend atomically extends a store's subscription and appends
// a ledger row. When the current validity lies in the future the extension
// starts from that date; when expired or unset it starts from now().
// 1_MONTH = +30 days, 6_MONTHS = +180 days, 1_YEAR = +365 days.
// The store is re-activated (subscription_status = 'ACTIVE') on every payment.
func (r *PlatformRepo) RecordPaymentAndExtend(ctx context.Context, storeID string, planType string, amount float64, notes string) (*models.StoreSubscriptionPayment, error) {
	days, ok := models.PlanDays(planType)
	if !ok {
		return nil, models.NewValidationError("plan_type must be one of 1_MONTH, 6_MONTHS, 1_YEAR")
	}
	if amount < 0 {
		return nil, models.NewValidationError("amount must be non-negative")
	}
	if storeID == "" {
		return nil, models.NewValidationError("store id is required")
	}

	var payment models.StoreSubscriptionPayment
	err := pgx.BeginFunc(ctx, r.db, func(tx pgx.Tx) error {
		var current *time.Time
		var exists bool
		if err := tx.QueryRow(ctx, `
			SELECT EXISTS(SELECT 1 FROM stores WHERE id = $1)`, storeID).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return models.ErrNotFound
		}
		if err := tx.QueryRow(ctx, `
			SELECT subscription_valid_until FROM stores WHERE id = $1`, storeID).Scan(&current); err != nil {
			return err
		}

		now := time.Now().UTC()
		base := now
		if current != nil && current.After(now) {
			base = *current
		}
		validFrom := base
		validUntil := base.AddDate(0, 0, days)

		if err := tx.QueryRow(ctx, `
			INSERT INTO store_subscription_payments (store_id, plan_type, amount, valid_from, valid_until, notes)
			VALUES ($1, $2, $3, $4, $5, $6)
			RETURNING id::text, store_id::text, plan_type, amount::float8, valid_from, valid_until, COALESCE(notes, ''), created_at`,
			storeID, planType, amount, validFrom, validUntil, notes).Scan(
			&payment.ID, &payment.StoreID, &payment.PlanType, &payment.Amount,
			&payment.ValidFrom, &payment.ValidUntil, &payment.Notes, &payment.CreatedAt); err != nil {
			return err
		}

		if _, err := tx.Exec(ctx, `
			UPDATE stores
			SET subscription_valid_until = $2, subscription_status = 'ACTIVE', updated_at = now()
			WHERE id = $1`, storeID, validUntil); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &payment, nil
}

// UpdateStoreStatus flips a store between ACTIVE and SUSPENDED.
func (r *PlatformRepo) UpdateStoreStatus(ctx context.Context, storeID string, status string) error {
	if status != "ACTIVE" && status != "SUSPENDED" {
		return models.NewValidationError("status must be ACTIVE or SUSPENDED")
	}
	if storeID == "" {
		return models.NewValidationError("store id is required")
	}
	tag, err := r.db.Exec(ctx, `
		UPDATE stores SET subscription_status = $2, updated_at = now()
		WHERE id = $1`, storeID, status)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return models.ErrNotFound
	}
	return nil
}

// ListPayments returns the cash-payment ledger for one store, newest first.
func (r *PlatformRepo) ListPayments(ctx context.Context, storeID string) ([]models.StoreSubscriptionPayment, error) {
	if storeID == "" {
		return nil, errors.New("store id is required")
	}
	rows, err := r.db.Query(ctx, `
		SELECT id::text, store_id::text, plan_type, amount::float8,
		       valid_from, valid_until, COALESCE(notes, ''), created_at
		FROM store_subscription_payments
		WHERE store_id = $1
		ORDER BY created_at DESC`, storeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]models.StoreSubscriptionPayment, 0)
	for rows.Next() {
		var p models.StoreSubscriptionPayment
		if err := rows.Scan(&p.ID, &p.StoreID, &p.PlanType, &p.Amount,
			&p.ValidFrom, &p.ValidUntil, &p.Notes, &p.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}
