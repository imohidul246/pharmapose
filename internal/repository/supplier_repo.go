package repository

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mohi/pms-marg-inspired/internal/models"
	"github.com/mohi/pms-marg-inspired/internal/tax"
)

type SupplierRepo struct {
	db    *pgxpool.Pool
	store *storeIDRef
}

func NewSupplierRepo(db *pgxpool.Pool, storeID string) *SupplierRepo {
	return &SupplierRepo{db: db, store: newStoreIDRef(db, storeID)}
}

func (r *SupplierRepo) storeID(ctx context.Context) (string, error) {
	return r.store.get(ctx)
}

func ValidateSupplier(s *models.Supplier) error {
	if s.LegalName == "" {
		return errors.New("supplier legal name is required")
	}
	if s.GSTIN != nil && *s.GSTIN != "" {
		if !tax.ValidateGSTIN(*s.GSTIN) {
			return errors.New("invalid GSTIN")
		}
	}
	return nil
}

const supplierColumns = `id, legal_name, trade_name, gstin, pan, address, state, state_code,
	phone, email, created_at, updated_at`

func scanSupplier(row pgx.Row) (*models.Supplier, error) {
	var s models.Supplier
	err := row.Scan(&s.ID, &s.LegalName, &s.TradeName, &s.GSTIN, &s.PAN,
		&s.Address, &s.State, &s.StateCode, &s.Phone, &s.Email,
		&s.CreatedAt, &s.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &s, nil
}

func (r *SupplierRepo) Create(ctx context.Context, s *models.Supplier) error {
	sid, err := r.storeID(ctx)
	if err != nil {
		return err
	}
	return r.db.QueryRow(ctx, `
		INSERT INTO suppliers (legal_name, trade_name, gstin, pan, address, state, state_code, phone, email, store_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		RETURNING `+supplierColumns,
		s.LegalName, s.TradeName, s.GSTIN, s.PAN, s.Address, s.State, s.StateCode, s.Phone, s.Email, sid,
	).Scan(&s.ID, &s.LegalName, &s.TradeName, &s.GSTIN, &s.PAN,
		&s.Address, &s.State, &s.StateCode, &s.Phone, &s.Email,
		&s.CreatedAt, &s.UpdatedAt)
}

func (r *SupplierRepo) GetByID(ctx context.Context, id string) (*models.Supplier, error) {
	sid, err := r.storeID(ctx)
	if err != nil {
		return nil, err
	}
	s, err := scanSupplier(r.db.QueryRow(ctx,
		`SELECT `+supplierColumns+` FROM suppliers WHERE id = $1 AND store_id = $2`, id, sid))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, models.ErrNotFound
	}
	return s, err
}

func (r *SupplierRepo) List(ctx context.Context) ([]models.Supplier, error) {
	sid, err := r.storeID(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := r.db.Query(ctx,
		`SELECT `+supplierColumns+` FROM suppliers WHERE store_id = $1 ORDER BY legal_name`, sid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]models.Supplier, 0)
	for rows.Next() {
		var s models.Supplier
		if err := rows.Scan(&s.ID, &s.LegalName, &s.TradeName, &s.GSTIN, &s.PAN,
			&s.Address, &s.State, &s.StateCode, &s.Phone, &s.Email,
			&s.CreatedAt, &s.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func (r *SupplierRepo) Update(ctx context.Context, s *models.Supplier) error {
	sid, err := r.storeID(ctx)
	if err != nil {
		return err
	}
	tag, err := r.db.Exec(ctx, `
		UPDATE suppliers
		SET legal_name = $2, trade_name = $3, gstin = $4, pan = $5,
		    address = $6, state = $7, state_code = $8, phone = $9, email = $10,
		    updated_at = now()
		WHERE id = $1 AND store_id = $11`,
		s.ID, s.LegalName, s.TradeName, s.GSTIN, s.PAN,
		s.Address, s.State, s.StateCode, s.Phone, s.Email, sid)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return models.ErrNotFound
	}
	return nil
}

func (r *SupplierRepo) Delete(ctx context.Context, id string) error {
	sid, err := r.storeID(ctx)
	if err != nil {
		return err
	}
	tag, err := r.db.Exec(ctx, `DELETE FROM suppliers WHERE id = $1 AND store_id = $2`, id, sid)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return models.ErrNotFound
	}
	return nil
}
