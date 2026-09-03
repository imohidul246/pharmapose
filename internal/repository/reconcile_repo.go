package repository

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mohi/pms-marg-inspired/internal/models"
)

type ReconcileRepo struct {
	db *pgxpool.Pool
}

func NewReconcileRepo(db *pgxpool.Pool) *ReconcileRepo { return &ReconcileRepo{db: db} }

type ReconcileItemInput struct {
	BatchID       string `json:"batch_id"`
	PhysicalCount int    `json:"physical_count"`
	Reason        string `json:"reason"`
}

type ReconcileInput struct {
	VerifiedByUserID *string              `json:"verified_by_user_id"`
	Notes            string               `json:"notes"`
	Items            []ReconcileItemInput `json:"items"`
}

func (in *ReconcileInput) validate() error {
	if len(in.Items) == 0 {
		return models.NewValidationError("reconciliation requires at least one item")
	}
	for _, it := range in.Items {
		if it.BatchID == "" {
			return models.NewValidationError("item batch_id is required")
		}
		if it.PhysicalCount < 0 {
			return models.NewValidationError("physical_count must be >= 0")
		}
		if it.Reason == "" {
			return models.NewValidationError("a reason is required for every adjusted batch")
		}
	}
	return nil
}

// Reconcile compares live system stock against physical counts and force-corrects
// batches.current_stock inside one transaction. Historical invoices are never
// rewritten; every variance is appended to an immutable audit journal.
// Only the store owner may call this directly; employee-submitted counts flow
// through a StockAuditRequest and are applied by the identical core below.
func (r *ReconcileRepo) Reconcile(ctx context.Context, storeID string, in *ReconcileInput) (*models.ReconciliationJournal, []models.ReconciliationItem, error) {
	if err := in.validate(); err != nil {
		return nil, nil, err
	}

	dedup, order := reconcileItems(in.Items)

	var (
		journal models.ReconciliationJournal
		items   = make([]models.ReconciliationItem, 0, len(order))
	)
	err := pgx.BeginFunc(ctx, r.db, func(tx pgx.Tx) error {
		j, i, err := r.applyReconcileCore(ctx, tx, storeID, in.VerifiedByUserID, in.Notes, dedup, order)
		if err != nil {
			return err
		}
		journal, items = *j, i
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	return &journal, items, nil
}

// reconcileItems flattens the input, keeping the last physical count per batch
// while preserving first-seen order.
func reconcileItems(items []ReconcileItemInput) (map[string]int, []string) {
	dedup := make(map[string]int, len(items))
	order := make([]string, 0, len(items))
	for _, it := range items {
		if _, seen := dedup[it.BatchID]; !seen {
			order = append(order, it.BatchID)
		}
		dedup[it.BatchID] = it.PhysicalCount
	}
	return dedup, order
}

// applyReconcileCore performs the locked compare-and-adjust inside the caller's
// transaction. It is the single code path used by direct owner reconciliations
// and by approved stock-audit requests, so both record variances identically.
func (r *ReconcileRepo) applyReconcileCore(ctx context.Context, tx pgx.Tx, storeID string, verifiedBy *string, notes string, dedup map[string]int, order []string) (*models.ReconciliationJournal, []models.ReconciliationItem, error) {
	// Centralized deterministic locking: LockBatchesForUpdate sorts and locks
	// in canonical order (deadlock-safe vs concurrent checkouts) and scopes
	// every lock to this store.
	locked, err := LockBatchesForUpdate(ctx, tx, storeID, append([]string(nil), order...))
	if err != nil {
		return nil, nil, err
	}

	for _, id := range order {
		if _, ok := locked[id]; !ok {
			return nil, nil, models.ErrNotFound
		}
	}

	journal := models.ReconciliationJournal{}
	if err := tx.QueryRow(ctx, `
		INSERT INTO reconciliation_journals (store_id, verified_by_user_id, notes)
		VALUES ($1, $2, $3)
		RETURNING id::text, verified_by_user_id, notes, created_at`,
		storeID, verifiedBy, notes,
	).Scan(&journal.ID, &journal.VerifiedByUserID, &journal.Notes, &journal.CreatedAt); err != nil {
		return nil, nil, err
	}

	items := make([]models.ReconciliationItem, 0, len(order))
	for _, id := range order {
		lb := locked[id]
		variance := dedup[id] - lb.CurrentStock

		costImpact := round2(float64(variance) * lb.PurchasePrice)

		item := models.ReconciliationItem{
			JournalID:        journal.ID,
			MedicineID:       lb.MedicineID,
			BatchID:          id,
			SystemStock:      lb.CurrentStock,
			PhysicalStock:    dedup[id],
			VarianceQuantity: variance,
			CostImpact:       costImpact,
			BatchNumber:      lb.BatchNumber,
		}
		err := tx.QueryRow(ctx, `
			INSERT INTO reconciliation_items
				(journal_id, medicine_id, batch_id, system_stock, physical_stock, variance_quantity, cost_impact)
			VALUES ($1, $2, $3, $4, $5, $6, $7)
			RETURNING id::text`,
			journal.ID, item.MedicineID, item.BatchID,
			item.SystemStock, item.PhysicalStock, item.VarianceQuantity, item.CostImpact,
		).Scan(&item.ID)
		if err != nil {
			return nil, nil, err
		}
		items = append(items, item)

		if _, err := tx.Exec(ctx, `
			UPDATE batches SET current_stock = $2, updated_at = now() WHERE id = $1 AND store_id = $3`,
			id, dedup[id], storeID); err != nil {
			return nil, nil, err
		}
	}

	journal.ItemCount = len(items)
	return &journal, items, nil
}

// ListJournals returns recent audit journals with their aggregated variance totals.
func (r *ReconcileRepo) ListJournals(ctx context.Context, storeID string, limit int) ([]models.ReconciliationJournal, []models.ReconciliationItem, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}

	journalRows, err := r.db.Query(ctx, `
		SELECT j.id::text, j.verified_by_user_id, j.notes, j.created_at,
		       COALESCE(COUNT(i.id), 0)::int
		FROM reconciliation_journals j
		LEFT JOIN reconciliation_items i ON i.journal_id = j.id
		WHERE j.store_id = $1
		GROUP BY j.id
		ORDER BY j.created_at DESC
		LIMIT $2`, storeID, limit)
	if err != nil {
		return nil, nil, err
	}
	defer journalRows.Close()

	journals := make([]models.ReconciliationJournal, 0)
	ids := make([]string, 0)
	for journalRows.Next() {
		var j models.ReconciliationJournal
		if err := journalRows.Scan(&j.ID, &j.VerifiedByUserID, &j.Notes,
			&j.CreatedAt, &j.ItemCount); err != nil {
			return nil, nil, err
		}
		journals = append(journals, j)
		ids = append(ids, j.ID)
	}
	if err := journalRows.Err(); err != nil {
		return nil, nil, err
	}
	if len(journals) == 0 {
		return journals, []models.ReconciliationItem{}, nil
	}

	itemRows, err := r.db.Query(ctx, `
		SELECT i.id::text, i.journal_id::text, i.medicine_id::text, i.batch_id::text,
		       i.system_stock, i.physical_stock, i.variance_quantity, i.cost_impact::float8,
		       b.batch_number, m.name
		FROM reconciliation_items i
		JOIN batches b ON b.id = i.batch_id
		JOIN medicines m ON m.id = i.medicine_id
		WHERE i.journal_id = ANY($1)
		ORDER BY i.journal_id, m.name`, ids)
	if err != nil {
		return nil, nil, err
	}
	defer itemRows.Close()

	items := make([]models.ReconciliationItem, 0)
	for itemRows.Next() {
		var it models.ReconciliationItem
		if err := itemRows.Scan(&it.ID, &it.JournalID, &it.MedicineID, &it.BatchID,
			&it.SystemStock, &it.PhysicalStock, &it.VarianceQuantity, &it.CostImpact,
			&it.BatchNumber, &it.MedicineName); err != nil {
			return nil, nil, err
		}
		items = append(items, it)
	}
	return journals, items, itemRows.Err()
}
