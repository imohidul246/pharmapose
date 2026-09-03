package repository

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mohi/pms-marg-inspired/internal/models"
)

// TaxRepo manages the store-scoped HSN / tax master. Every scoped method takes
// an explicit storeID so the caller can pass the store derived from the
// authenticated request context (never a client-supplied value). Repos that
// resolve tax per-invoice (Sales, Purchases) pass the invoice's store.
type TaxRepo struct {
	db *pgxpool.Pool
}

func NewTaxRepo(db *pgxpool.Pool) *TaxRepo { return &TaxRepo{db: db} }

// GetMedicineTaxConfig returns the active tax configuration for a medicine in
// the given store as of the given date. Returns nil if no config exists (legacy medicine).
func (r *TaxRepo) GetMedicineTaxConfig(ctx context.Context, storeID, medicineID string, asOf time.Time) (*models.MedicineTaxConfig, error) {
	var cfg models.MedicineTaxConfig
	var taxRate models.TaxRate
	var effectiveTo *time.Time

	err := r.db.QueryRow(ctx, `
		SELECT mtc.id::text, mtc.medicine_id::text, mtc.hsn_code_id::text,
		       mtc.tax_rate_id::text, mtc.price_includes_tax,
		       mtc.effective_from, mtc.effective_to,
		       h.code,
		       tr.gst_rate, tr.cgst_rate, tr.sgst_rate, tr.igst_rate, tr.cess_rate
		FROM medicine_tax_config mtc
		JOIN hsn_codes h ON h.id = mtc.hsn_code_id
		JOIN tax_rates tr ON tr.id = mtc.tax_rate_id
		WHERE mtc.medicine_id = $1
		  AND mtc.store_id = $2
		  AND mtc.effective_from <= $3
		  AND (mtc.effective_to IS NULL OR mtc.effective_to >= $3)
		ORDER BY (mtc.effective_to IS NULL) DESC, mtc.effective_from DESC, mtc.id
		LIMIT 1`, medicineID, storeID, asOf).
		Scan(&cfg.ID, &cfg.MedicineID, &cfg.HSNCodeID, &cfg.TaxRateID,
			&cfg.PriceIncludesTax, &cfg.EffectiveFrom, &effectiveTo,
			&cfg.HSNCode,
			&taxRate.GSTRate, &taxRate.CGSTRate, &taxRate.SGSTRate,
			&taxRate.IGSTRate, &taxRate.CessRate)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil // no tax config → legacy medicine
	}
	if err != nil {
		return nil, err
	}
	cfg.EffectiveTo = effectiveTo
	cfg.TaxRate = &taxRate
	return &cfg, nil
}

// GetMedicineTaxConfigByMedicine returns the active tax config for a medicine
// in the given store (no date filter).
func (r *TaxRepo) GetMedicineTaxConfigByMedicine(ctx context.Context, storeID, medicineID string) (*models.MedicineTaxConfig, error) {
	return r.GetMedicineTaxConfig(ctx, storeID, medicineID, time.Now())
}

// GetHSNByCode returns an HSN code record by its code string within a store.
func (r *TaxRepo) GetHSNByCode(ctx context.Context, storeID, code string) (*models.HSNCode, error) {
	var h models.HSNCode
	err := r.db.QueryRow(ctx,
		`SELECT id::text, code, description, created_at FROM hsn_codes WHERE code = $1 AND store_id = $2`, code, storeID).
		Scan(&h.ID, &h.Code, &h.Description, &h.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, models.ErrNotFound
	}
	return &h, err
}

// GetActiveTaxRate returns the currently active tax rate for an HSN code
// within a store.
func (r *TaxRepo) GetActiveTaxRate(ctx context.Context, storeID, hsnCodeID string) (*models.TaxRate, error) {
	var tr models.TaxRate
	var effectiveTo *time.Time
	err := r.db.QueryRow(ctx, `
		SELECT id::text, hsn_code_id::text, gst_rate, cgst_rate, sgst_rate, igst_rate, cess_rate,
		       effective_from, effective_to, created_at
		FROM tax_rates
		WHERE hsn_code_id = $1
		  AND store_id = $2
		  AND effective_to IS NULL
		ORDER BY effective_from DESC
		LIMIT 1`, hsnCodeID, storeID).
		Scan(&tr.ID, &tr.HSNCodeID, &tr.GSTRate, &tr.CGSTRate, &tr.SGSTRate,
			&tr.IGSTRate, &tr.CessRate, &tr.EffectiveFrom, &effectiveTo, &tr.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	tr.EffectiveTo = effectiveTo
	return &tr, nil
}

// GetDefaultStore returns the first active store, or nil if none exists.
func (r *TaxRepo) GetDefaultStore(ctx context.Context) (*models.Store, error) {
	var s models.Store
	var gstRegID *string
	err := r.db.QueryRow(ctx, `
		SELECT id::text, gst_registration_id::text, name, address, is_active, created_at, updated_at
		FROM stores WHERE is_active = true
		ORDER BY created_at ASC LIMIT 1`).
		Scan(&s.ID, &gstRegID, &s.Name, &s.Address, &s.IsActive, &s.CreatedAt, &s.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	s.GSTRegistrationID = gstRegID
	return &s, nil
}

// GetStore returns a store by ID.
//
// NOTE: the stores table carries the shop-detail columns (phone, drug licence
// etc., added by migration 034). The phone is required by the B2B PDF seller
// block, so it is selected here as well as the legacy identity fields.
func (r *TaxRepo) GetStore(ctx context.Context, id string) (*models.Store, error) {
	var s models.Store
	var gstRegID *string
	err := r.db.QueryRow(ctx, `
		SELECT id::text, gst_registration_id::text, name, address, phone, is_active, created_at, updated_at
		FROM stores WHERE id = $1`, id).
		Scan(&s.ID, &gstRegID, &s.Name, &s.Address, &s.Phone, &s.IsActive, &s.CreatedAt, &s.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, models.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	s.GSTRegistrationID = gstRegID
	return &s, nil
}

// GetGSTRegistration returns a GST registration by ID.
func (r *TaxRepo) GetGSTRegistration(ctx context.Context, id string) (*models.GSTRegistration, error) {
	var gr models.GSTRegistration
	var gstin *string
	var pan *string
	err := r.db.QueryRow(ctx, `
		SELECT id::text, business_id::text, gstin, legal_name, trade_name, pan,
		       state_code, state_name, address, is_active, created_at, updated_at
		FROM gst_registrations WHERE id = $1`, id).
		Scan(&gr.ID, &gr.BusinessID, &gstin, &gr.LegalName, &gr.TradeName, &pan,
			&gr.StateCode, &gr.StateName, &gr.Address, &gr.IsActive,
			&gr.CreatedAt, &gr.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, models.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	gr.GSTIN = gstin
	gr.PAN = pan
	return &gr, nil
}

// ListHSNCodes returns all HSN codes for a store.
func (r *TaxRepo) ListHSNCodes(ctx context.Context, storeID string) ([]models.HSNCode, error) {
	rows, err := r.db.Query(ctx,
		`SELECT id::text, code, description, created_at FROM hsn_codes WHERE store_id = $1 ORDER BY code`, storeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.HSNCode
	for rows.Next() {
		var h models.HSNCode
		if err := rows.Scan(&h.ID, &h.Code, &h.Description, &h.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

// UpsertTaxRate creates or updates the active tax rate for an HSN code within a
// store. If an active (effective_to IS NULL) rate exists, it is ended and a new
// one created — a new effective-dated row, so historical invoices are untouched.
func (r *TaxRepo) UpsertTaxRate(ctx context.Context, storeID, hsnCodeID string, gstRate, cgstRate, sgstRate, igstRate, cessRate float64) (*models.TaxRate, error) {
	var tr models.TaxRate
	err := pgx.BeginFunc(ctx, r.db, func(tx pgx.Tx) error {
		// Close any existing active rate for this HSN in this store. effective_to
		// is +1 day past CURRENT_DATE so it is always > effective_from (the row's
		// effective_from may also be CURRENT_DATE for a same-day resave), which the
		// chk_tr_effective constraint (effective_to NULL OR effective_to > effective_from) requires.
		_, err := tx.Exec(ctx,
			`UPDATE tax_rates SET effective_to = CURRENT_DATE + 1
			 WHERE hsn_code_id = $1 AND store_id = $2 AND effective_to IS NULL`, hsnCodeID, storeID)
		if err != nil {
			return err
		}
		// Insert new active rate
		return tx.QueryRow(ctx, `
			INSERT INTO tax_rates (store_id, hsn_code_id, gst_rate, cgst_rate, sgst_rate, igst_rate, cess_rate, effective_from)
			VALUES ($1, $2, $3, $4, $5, $6, $7, CURRENT_DATE)
			RETURNING id::text, hsn_code_id::text, gst_rate, cgst_rate, sgst_rate, igst_rate, cess_rate,
			          effective_from, effective_to, created_at`,
			storeID, hsnCodeID, gstRate, cgstRate, sgstRate, igstRate, cessRate).
			Scan(&tr.ID, &tr.HSNCodeID, &tr.GSTRate, &tr.CGSTRate, &tr.SGSTRate,
				&tr.IGSTRate, &tr.CessRate, &tr.EffectiveFrom, &tr.EffectiveTo, &tr.CreatedAt)
	})
	if err != nil {
		return nil, err
	}
	return &tr, nil
}

// UpsertMedicineTaxConfig assigns a tax configuration to a medicine within a
// store. If an active config exists, it is ended and a new one created.
func (r *TaxRepo) UpsertMedicineTaxConfig(ctx context.Context, storeID, medicineID, hsnCodeID, taxRateID string, priceIncludesTax bool) (*models.MedicineTaxConfig, error) {
	// Cross-entity store integrity is enforced by the 033 trigger; the caller
	// passes a store that is validated against the medicine, HSN and rate.
	var cfg models.MedicineTaxConfig
	err := pgx.BeginFunc(ctx, r.db, func(tx pgx.Tx) error {
		// Close any existing active config for this medicine. effective_to is +1
		// day past CURRENT_DATE so it is always > effective_from (may also be
		// CURRENT_DATE for a same-day resave), satisfying chk_mtc_effective.
		_, err := tx.Exec(ctx,
			`UPDATE medicine_tax_config SET effective_to = CURRENT_DATE + 1
			 WHERE medicine_id = $1 AND store_id = $2 AND effective_to IS NULL`, medicineID, storeID)
		if err != nil {
			return err
		}
		// Insert new active config
		return tx.QueryRow(ctx, `
			INSERT INTO medicine_tax_config (store_id, medicine_id, hsn_code_id, tax_rate_id, price_includes_tax, effective_from)
			VALUES ($1, $2, $3, $4, $5, CURRENT_DATE)
			RETURNING id::text, medicine_id::text, hsn_code_id::text, tax_rate_id::text,
			          price_includes_tax, effective_from, effective_to, created_at`,
			storeID, medicineID, hsnCodeID, taxRateID, priceIncludesTax).
			Scan(&cfg.ID, &cfg.MedicineID, &cfg.HSNCodeID, &cfg.TaxRateID,
				&cfg.PriceIncludesTax, &cfg.EffectiveFrom, &cfg.EffectiveTo, &cfg.CreatedAt)
	})
	if err != nil {
		return nil, err
	}
	return &cfg, nil
}

// CreateHSNCode creates a new HSN code for a store. Returns models.ErrDuplicate
// if the code already exists for this store.
func (r *TaxRepo) CreateHSNCode(ctx context.Context, storeID, code, description string) (*models.HSNCode, error) {
	var h models.HSNCode
	err := r.db.QueryRow(ctx,
		`INSERT INTO hsn_codes (store_id, code, description) VALUES ($1, $2, $3)
		 RETURNING id::text, code, description, created_at`,
		storeID, code, description).
		Scan(&h.ID, &h.Code, &h.Description, &h.CreatedAt)
	if err != nil {
		if isUniqueViolation(err) {
			return nil, models.ErrDuplicate
		}
		return nil, err
	}
	return &h, nil
}

// TaxSyncSnapshot is the store-scoped payload shipped to the frontend to mirror
// HSN/tax configuration into IndexedDB.
type TaxSyncSnapshot struct {
	HSNCodes   []models.HSNWithRate       `json:"hsn_codes"`
	TaxConfigs []models.MedicineTaxConfig `json:"tax_configs"`
}

// ListStoreTaxSnapshot returns every HSN code (with its active rate) and every
// medicine tax config for a store, in one call. Used by GET /api/sync/tax.
func (r *TaxRepo) ListStoreTaxSnapshot(ctx context.Context, storeID string) (*TaxSyncSnapshot, error) {
	snap := &TaxSyncSnapshot{HSNCodes: []models.HSNWithRate{}, TaxConfigs: []models.MedicineTaxConfig{}}

	hsnRows, err := r.db.Query(ctx, `
		SELECT h.id::text, h.code, h.description, h.created_at,
		       COALESCE(tr.gst_rate, 0), COALESCE(tr.cgst_rate, 0),
		       COALESCE(tr.sgst_rate, 0), COALESCE(tr.igst_rate, 0), COALESCE(tr.cess_rate, 0)
		FROM hsn_codes h
		LEFT JOIN LATERAL (
			SELECT gst_rate, cgst_rate, sgst_rate, igst_rate, cess_rate
			FROM tax_rates tr
			WHERE tr.hsn_code_id = h.id AND tr.store_id = $1 AND tr.effective_to IS NULL
			ORDER BY tr.effective_from DESC LIMIT 1
		) tr ON true
		WHERE h.store_id = $1
		ORDER BY h.code`, storeID)
	if err != nil {
		return nil, err
	}
	defer hsnRows.Close()
	for hsnRows.Next() {
		var hr models.HSNWithRate
		if err := hsnRows.Scan(&hr.ID, &hr.Code, &hr.Description, &hr.CreatedAt,
			&hr.GSTRate, &hr.CGSTRate, &hr.SGSTRate, &hr.IGSTRate, &hr.CessRate); err != nil {
			return nil, err
		}
		snap.HSNCodes = append(snap.HSNCodes, hr)
	}
	if err := hsnRows.Err(); err != nil {
		return nil, err
	}

	cfgRows, err := r.db.Query(ctx, `
		SELECT mtc.id::text, mtc.medicine_id::text, mtc.hsn_code_id::text, mtc.tax_rate_id::text,
		       mtc.price_includes_tax, mtc.effective_from, mtc.effective_to, h.code,
		       tr.gst_rate, tr.cgst_rate, tr.sgst_rate, tr.igst_rate, tr.cess_rate
		FROM medicine_tax_config mtc
		JOIN hsn_codes h ON h.id = mtc.hsn_code_id
		JOIN tax_rates tr ON tr.id = mtc.tax_rate_id
		WHERE mtc.store_id = $1 AND mtc.effective_to IS NULL
		ORDER BY mtc.medicine_id`, storeID)
	if err != nil {
		return nil, err
	}
	defer cfgRows.Close()
	for cfgRows.Next() {
		var cfg models.MedicineTaxConfig
		var tr models.TaxRate
		var effectiveTo *time.Time
		if err := cfgRows.Scan(&cfg.ID, &cfg.MedicineID, &cfg.HSNCodeID, &cfg.TaxRateID,
			&cfg.PriceIncludesTax, &cfg.EffectiveFrom, &effectiveTo, &cfg.HSNCode,
			&tr.GSTRate, &tr.CGSTRate, &tr.SGSTRate, &tr.IGSTRate, &tr.CessRate); err != nil {
			return nil, err
		}
		cfg.EffectiveTo = effectiveTo
		cfg.TaxRate = &tr
		snap.TaxConfigs = append(snap.TaxConfigs, cfg)
	}
	if err := cfgRows.Err(); err != nil {
		return nil, err
	}
	return snap, nil
}

// BackdateTaxConfig rewrites the effective_from of a medicine tax config and
// its linked tax rate to a fixed date. Used by the deterministic seed so that
// effective_from is anchored to the seed date rather than CURRENT_DATE, keeping
// every seeded (back-dated) invoice within the tax window.
func (r *TaxRepo) BackdateTaxConfig(ctx context.Context, configID, taxRateID string, effectiveFrom time.Time) error {
	if _, err := r.db.Exec(ctx,
		`UPDATE medicine_tax_config SET effective_from = $2 WHERE id = $1`,
		configID, effectiveFrom); err != nil {
		return err
	}
	_, err := r.db.Exec(ctx,
		`UPDATE tax_rates SET effective_from = LEAST(effective_from, $2) WHERE id = $1`,
		taxRateID, effectiveFrom)
	return err
}
