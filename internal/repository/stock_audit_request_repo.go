package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mohi/pms-marg-inspired/internal/models"
)

// StockAuditRequestRepo owns the employee stock-audit workflow. Submission
// snapshots each batch's live system stock; approval compares that snapshot
// against live stock (a divergence means the audit is stale and must be
// re-validated) and applies the variance through the same locked reconcile core
// a direct owner reconciliation uses — atomically and exactly once.
type StockAuditRequestRepo struct {
	db    *pgxpool.Pool
	recon *ReconcileRepo
}

func NewStockAuditRequestRepo(db *pgxpool.Pool) *StockAuditRequestRepo {
	return &StockAuditRequestRepo{db: db, recon: NewReconcileRepo(db)}
}

// AuditItemInput is one counted batch in an audit request.
type AuditItemInput struct {
	MedicineID       string `json:"medicine_id"`
	BatchID          string `json:"batch_id"`
	PhysicalQuantity int    `json:"physical_quantity"`
	Reason           string `json:"reason"`
}

// Create snapshots system stock and records a PENDING audit request. Batches
// are locked while their system quantity is read so the snapshot is exact.
func (r *StockAuditRequestRepo) Create(ctx context.Context, storeID, requestedBy, notes string, items []AuditItemInput) (*models.StockAuditRequest, []models.StockAuditRequestItem, error) {
	if err := validateAuditItems(items); err != nil {
		return nil, nil, err
	}

	var (
		req   models.StockAuditRequest
		outed = make([]models.StockAuditRequestItem, 0, len(items))
	)
	err := pgx.BeginFunc(ctx, r.db, func(tx pgx.Tx) error {
		var reqID string
		if err := tx.QueryRow(ctx, `
			INSERT INTO stock_audit_requests (store_id, requested_by, notes)
			VALUES ($1, $2, $3)
			RETURNING id::text, status::text, created_at, updated_at`,
			storeID, requestedBy, notes).Scan(&reqID, &req.Status, &req.CreatedAt, &req.UpdatedAt); err != nil {
			return err
		}
		req.ID, req.StoreID, req.RequestedBy, req.Notes = reqID, storeID, requestedBy, notes

		dedup := make(map[string]AuditItemInput, len(items))
		order := make([]string, 0, len(items))
		for _, it := range items {
			if _, seen := dedup[it.BatchID]; !seen {
				order = append(order, it.BatchID)
			}
			dedup[it.BatchID] = it
		}

		// Centralized deterministic locking (canonical order, store-scoped).
		locked, err := LockBatchesForUpdate(ctx, tx, storeID, order)
		if err != nil {
			return err
		}

		for _, batchID := range order {
			it := dedup[batchID]
			lb, ok := locked[batchID]
			if !ok {
				return fmt.Errorf("batch %s not found in store", batchID)
			}
			medicineID, medicineName, batchNumber, systemStock :=
				lb.MedicineID, lb.MedicineName, lb.BatchNumber, lb.CurrentStock

			var itemID string
			if err := tx.QueryRow(ctx, `
				INSERT INTO stock_audit_request_items
					(request_id, medicine_id, batch_id, batch_number, system_quantity, physical_quantity, reason)
				VALUES ($1, $2, $3, $4, $5, $6, $7)
				RETURNING id::text`,
				reqID, medicineID, batchID, batchNumber, systemStock, it.PhysicalQuantity, it.Reason).
				Scan(&itemID); err != nil {
				return err
			}
			outed = append(outed, models.StockAuditRequestItem{
				ID:               itemID,
				RequestID:        reqID,
				MedicineID:       medicineID,
				MedicineName:     medicineName,
				BatchID:          batchID,
				BatchNumber:      batchNumber,
				SystemQuantity:   systemStock,
				PhysicalQuantity: it.PhysicalQuantity,
				Reason:           it.Reason,
			})
		}
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	return &req, outed, nil
}

// Approve locks the request, refuses self-approval and non-PENDING states,
// verifies no stock moved since submission, then applies the counted variances
// through the reconcile core — all in the same transaction.
func (r *StockAuditRequestRepo) Approve(ctx context.Context, storeID, requestID, reviewerID string) (*models.ReconciliationJournal, []models.ReconciliationItem, error) {
	var (
		journal models.ReconciliationJournal
		items   []models.ReconciliationItem
	)
	err := pgx.BeginFunc(ctx, r.db, func(tx pgx.Tx) error {
		var requestedBy, status, notes string
		err := tx.QueryRow(ctx, `
			SELECT requested_by::text, status::text, notes
			FROM stock_audit_requests
			WHERE id = $1 AND store_id = $2
			FOR UPDATE`, requestID, storeID).
			Scan(&requestedBy, &status, &notes)
		if errors.Is(err, pgx.ErrNoRows) {
			return models.ErrNotFound
		}
		if err != nil {
			return err
		}
		if status != "PENDING" {
			return models.ErrRequestNotPending
		}
		if requestedBy == reviewerID {
			return models.ErrRequesterApprover
		}

		rows, err := tx.Query(ctx, `
			SELECT i.batch_id::text, i.system_quantity, i.physical_quantity, i.reason,
			       b.current_stock
			FROM stock_audit_request_items i
			JOIN batches b ON b.id = i.batch_id
			WHERE i.request_id = $1
			ORDER BY i.batch_id
			FOR UPDATE OF b`, requestID)
		if err != nil {
			return err
		}
		type auditLine struct {
			system, physical int
			reason           string
		}
		lines := make(map[string]auditLine, 0)
		order := make([]string, 0)
		for rows.Next() {
			var batchID, reason string
			var system, physical, live int
			if err := rows.Scan(&batchID, &system, &physical, &reason, &live); err != nil {
				rows.Close()
				return err
			}
			if live != system {
				rows.Close()
				return models.ErrStaleStock
			}
			if _, seen := lines[batchID]; !seen {
				order = append(order, batchID)
			}
			lines[batchID] = auditLine{system: system, physical: physical, reason: reason}
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return err
		}
		rows.Close()

		dedup := make(map[string]int, len(order))
		for _, id := range order {
			dedup[id] = lines[id].physical
		}
		j, i, err := r.recon.applyReconcileCore(ctx, tx, storeID, &reviewerID, notes, dedup, order)
		if err != nil {
			return err
		}
		journal, items = *j, i

		if _, err := tx.Exec(ctx, `
			UPDATE stock_audit_requests
			SET status = 'APPROVED', journal_id = $3, reviewed_by = $4,
			    reviewed_at = now(), updated_at = now()
			WHERE id = $1 AND store_id = $2`, requestID, storeID, journal.ID, reviewerID); err != nil {
			return err
		}
		return insertAuditTx(ctx, tx, storeID, reviewerID, "stock_audit_request.approve", "stock_audit_request", requestID,
			map[string]string{"journal_id": journal.ID})
	})
	if err != nil {
		return nil, nil, err
	}
	return &journal, items, nil
}

// Reject marks the request REJECTED with a mandatory reason.
func (r *StockAuditRequestRepo) Reject(ctx context.Context, storeID, requestID, reviewerID, reason string) (*models.StockAuditRequest, error) {
	if reason == "" {
		return nil, errors.New("rejection reason is required")
	}
	return r.transition(ctx, storeID, requestID, reviewerID, "REJECTED", func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `
			UPDATE stock_audit_requests
			SET status = 'REJECTED', reviewed_by = $3, reviewed_at = now(),
			    rejection_reason = $4, updated_at = now()
			WHERE id = $1 AND store_id = $2`, requestID, storeID, reviewerID, reason); err != nil {
			return err
		}
		return insertAuditTx(ctx, tx, storeID, reviewerID, "stock_audit_request.reject", "stock_audit_request", requestID,
			map[string]string{"reason": reason})
	})
}

// Cancel lets the requester withdraw a pending request.
func (r *StockAuditRequestRepo) Cancel(ctx context.Context, storeID, requestID, userID string) (*models.StockAuditRequest, error) {
	return r.transition(ctx, storeID, requestID, userID, "CANCELLED", func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `
			UPDATE stock_audit_requests
			SET status = 'CANCELLED', updated_at = now()
			WHERE id = $1 AND store_id = $2`, requestID, storeID); err != nil {
			return err
		}
		return insertAuditTx(ctx, tx, storeID, userID, "stock_audit_request.cancel", "stock_audit_request", requestID, nil)
	})
}

func (r *StockAuditRequestRepo) transition(ctx context.Context, storeID, requestID, actorID, target string, mutate func(tx pgx.Tx) error) (*models.StockAuditRequest, error) {
	var req models.StockAuditRequest
	err := pgx.BeginFunc(ctx, r.db, func(tx pgx.Tx) error {
		var requestedBy, status string
		err := tx.QueryRow(ctx, `
			SELECT requested_by::text, status::text
			FROM stock_audit_requests
			WHERE id = $1 AND store_id = $2
			FOR UPDATE`, requestID, storeID).Scan(&requestedBy, &status)
		if errors.Is(err, pgx.ErrNoRows) {
			return models.ErrNotFound
		}
		if err != nil {
			return err
		}
		if status != "PENDING" {
			return models.ErrRequestNotPending
		}
		if target == "REJECTED" && requestedBy == actorID {
			return models.ErrRequesterApprover
		}
		if target == "CANCELLED" && requestedBy != actorID {
			return models.ErrNotAMember
		}
		if err := mutate(tx); err != nil {
			return err
		}
		return tx.QueryRow(ctx, `
			SELECT id::text, store_id::text, requested_by::text, status::text, notes,
			       journal_id::text, reviewed_by::text, reviewed_at, rejection_reason,
			       created_at, updated_at
			FROM stock_audit_requests
			WHERE id = $1`, requestID).
			Scan(&req.ID, &req.StoreID, &req.RequestedBy, &req.Status, &req.Notes,
				&req.JournalID, &req.ReviewedBy, &req.ReviewedAt, &req.RejectionReason,
				&req.CreatedAt, &req.UpdatedAt)
	})
	if err != nil {
		return nil, err
	}
	return &req, nil
}

// Get returns a single audit request with its counted items.
func (r *StockAuditRequestRepo) Get(ctx context.Context, storeID, requestID string) (*models.StockAuditRequest, []models.StockAuditRequestItem, error) {
	var req models.StockAuditRequest
	err := r.db.QueryRow(ctx, `
		SELECT r.id::text, r.store_id::text, r.requested_by::text, r.status::text, r.notes,
		       r.journal_id::text, r.reviewed_by::text, r.reviewed_at, r.rejection_reason,
		       r.created_at, r.updated_at,
		       u.name, COALESCE(rv.name, '')
		FROM stock_audit_requests r
		JOIN users u ON u.id = r.requested_by
		LEFT JOIN users rv ON rv.id = r.reviewed_by
		WHERE r.id = $1 AND r.store_id = $2`, requestID, storeID).
		Scan(&req.ID, &req.StoreID, &req.RequestedBy, &req.Status, &req.Notes,
			&req.JournalID, &req.ReviewedBy, &req.ReviewedAt, &req.RejectionReason,
			&req.CreatedAt, &req.UpdatedAt, &req.RequesterName, &req.ReviewerName)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil, models.ErrNotFound
	}
	if err != nil {
		return nil, nil, err
	}

	items, err := r.items(ctx, storeID, requestID)
	if err != nil {
		return nil, nil, err
	}
	return &req, items, nil
}

// List returns a page of audit requests, optionally narrowed to one status,
// plus the total matching count for pagination metadata.
func (r *StockAuditRequestRepo) List(ctx context.Context, storeID, status string, limit, offset int) ([]models.StockAuditRequest, int64, error) {
	if limit <= 0 {
		limit = 100
	}
	if limit > 200 {
		limit = 200
	}
	if offset < 0 {
		offset = 0
	}
	if status == "" || status == "ALL" {
		status = ""
	}
	var total int64
	if err := r.db.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM stock_audit_requests r
		WHERE r.store_id = $1 AND ($2 = '' OR r.status::text = $2)`,
		storeID, status).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := r.db.Query(ctx, `
		SELECT r.id::text, r.store_id::text, r.requested_by::text, r.status::text, r.notes,
		       r.journal_id::text, r.reviewed_by::text, r.reviewed_at, r.rejection_reason,
		       r.created_at, r.updated_at,
		       u.name, COALESCE(rv.name, '')
		FROM stock_audit_requests r
		JOIN users u ON u.id = r.requested_by
		LEFT JOIN users rv ON rv.id = r.reviewed_by
		WHERE r.store_id = $1 AND ($2 = '' OR r.status::text = $2)
		ORDER BY r.created_at DESC
		LIMIT $3 OFFSET $4`, storeID, status, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	out := make([]models.StockAuditRequest, 0)
	for rows.Next() {
		var req models.StockAuditRequest
		if err := rows.Scan(&req.ID, &req.StoreID, &req.RequestedBy, &req.Status, &req.Notes,
			&req.JournalID, &req.ReviewedBy, &req.ReviewedAt, &req.RejectionReason,
			&req.CreatedAt, &req.UpdatedAt, &req.RequesterName, &req.ReviewerName); err != nil {
			return nil, 0, err
		}
		out = append(out, req)
	}
	return out, total, rows.Err()
}

func (r *StockAuditRequestRepo) items(ctx context.Context, storeID, requestID string) ([]models.StockAuditRequestItem, error) {
	rows, err := r.db.Query(ctx, `
		SELECT i.id::text, i.request_id::text, i.medicine_id::text, m.name,
		       i.batch_id::text, i.batch_number, i.system_quantity, i.physical_quantity, i.reason
		FROM stock_audit_request_items i
		JOIN stock_audit_requests r ON r.id = i.request_id AND r.store_id = $2
		JOIN medicines m ON m.id = i.medicine_id
		WHERE i.request_id = $1
		ORDER BY i.batch_number`, requestID, storeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]models.StockAuditRequestItem, 0)
	for rows.Next() {
		var it models.StockAuditRequestItem
		if err := rows.Scan(&it.ID, &it.RequestID, &it.MedicineID, &it.MedicineName,
			&it.BatchID, &it.BatchNumber, &it.SystemQuantity, &it.PhysicalQuantity, &it.Reason); err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

func validateAuditItems(items []AuditItemInput) error {
	if len(items) == 0 {
		return errors.New("stock audit requires at least one item")
	}
	for _, it := range items {
		if it.BatchID == "" {
			return errors.New("item batch_id is required")
		}
		if it.PhysicalQuantity < 0 {
			return errors.New("physical_quantity must be >= 0")
		}
		if it.Reason == "" {
			return errors.New("a reason is required for every counted batch")
		}
	}
	return nil
}