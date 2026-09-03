package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mohi/pms-marg-inspired/internal/models"
)

// PurchaseRequestRepo owns the employee purchase-approval workflow. Submitting
// is free (any authenticated employee of the store); approving replays the
// snapshotted inward through the same createInwardTx that a direct owner entry
// uses, so approval == direct entry exactly once, atomically.
type PurchaseRequestRepo struct {
	db     *pgxpool.Pool
	purch  *PurchaseRepo
}

func NewPurchaseRequestRepo(db *pgxpool.Pool) *PurchaseRequestRepo {
	return &PurchaseRequestRepo{db: db, purch: NewPurchaseRepo(db)}
}

// Create snapshots a proposed inward as a PENDING request.
func (r *PurchaseRequestRepo) Create(ctx context.Context, storeID, requestedBy string, in *PurchaseInput) (*models.PurchaseRequest, error) {
	if err := in.validate(); err != nil {
		return nil, err
	}
	if in.StoreID == nil || *in.StoreID == "" {
		return nil, errors.New("store_id is required")
	}

	// Serialize AFTER the handler has pinned StoreID (and the requester), so
	// the snapshot is a complete, reviewable inward.
	snapshot, err := json.Marshal(in)
	if err != nil {
		return nil, fmt.Errorf("serialize purchase snapshot: %w", err)
	}

	var req models.PurchaseRequest
	err = r.db.QueryRow(ctx, `
		INSERT INTO purchase_requests (store_id, requested_by, purchase_snapshot)
		VALUES ($1, $2, $3)
		RETURNING id::text, store_id::text, requested_by::text, status::text, created_at, updated_at`,
		storeID, requestedBy, snapshot).
		Scan(&req.ID, &req.StoreID, &req.RequestedBy, &req.Status, &req.CreatedAt, &req.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &req, nil
}

// Approve locks the request, refuses self-approval and non-PENDING states, then
// records the snapshotted inward and marks the request approved — all in one
// transaction, so a failed inward never leaves an "APPROVED" request without
// a purchase attached (and vice versa).
func (r *PurchaseRequestRepo) Approve(ctx context.Context, storeID, requestID, reviewerID string) (*models.PurchaseOrder, []models.PurchaseOrderItem, error) {
	var (
		po    models.PurchaseOrder
		items []models.PurchaseOrderItem
	)
	err := pgx.BeginFunc(ctx, r.db, func(tx pgx.Tx) error {
		var requestedBy, status string
		var snapshot []byte
		err := tx.QueryRow(ctx, `
			SELECT requested_by::text, status::text, purchase_snapshot
			FROM purchase_requests
			WHERE id = $1 AND store_id = $2
			FOR UPDATE`, requestID, storeID).
			Scan(&requestedBy, &status, &snapshot)
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

		var in PurchaseInput
		if err := json.Unmarshal(snapshot, &in); err != nil {
			return fmt.Errorf("corrupt purchase snapshot: %w", err)
		}
		// Defense-in-depth: the request, not the snapshot, is the authority for
		// scope and authorship.
		in.StoreID = &storeID
		creator := reviewerID
		in.CreatedBy = &creator

		p, i, err := r.purch.createInwardTx(ctx, tx, &in)
		if err != nil {
			return err
		}
		po, items = *p, i

		if _, err := tx.Exec(ctx, `
			UPDATE purchase_requests
			SET status = 'APPROVED', purchase_id = $3, reviewed_by = $4,
			    reviewed_at = now(), updated_at = now()
			WHERE id = $1 AND store_id = $2`, requestID, storeID, po.ID, reviewerID); err != nil {
			return err
		}
		return insertAuditTx(ctx, tx, storeID, reviewerID, "purchase_request.approve", "purchase_request", requestID,
			map[string]string{"purchase_id": po.ID, "invoice_no": po.InvoiceNo})
	})
	if err != nil {
		return nil, nil, err
	}
	return &po, items, nil
}

// Reject marks the request REJECTED with a mandatory reason.
func (r *PurchaseRequestRepo) Reject(ctx context.Context, storeID, requestID, reviewerID, reason string) (*models.PurchaseRequest, error) {
	if reason == "" {
		return nil, errors.New("rejection reason is required")
	}
	return r.transition(ctx, storeID, requestID, reviewerID, "REJECTED", func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `
			UPDATE purchase_requests
			SET status = 'REJECTED', reviewed_by = $3, reviewed_at = now(),
			    rejection_reason = $4, updated_at = now()
			WHERE id = $1 AND store_id = $2`, requestID, storeID, reviewerID, reason); err != nil {
			return err
		}
		return insertAuditTx(ctx, tx, storeID, reviewerID, "purchase_request.reject", "purchase_request", requestID,
			map[string]string{"reason": reason})
	})
}

// Cancel lets the requester withdraw a request that is still pending.
func (r *PurchaseRequestRepo) Cancel(ctx context.Context, storeID, requestID, userID string) (*models.PurchaseRequest, error) {
	return r.transition(ctx, storeID, requestID, userID, "CANCELLED", func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `
			UPDATE purchase_requests
			SET status = 'CANCELLED', updated_at = now()
			WHERE id = $1 AND store_id = $2`, requestID, storeID); err != nil {
			return err
		}
		return insertAuditTx(ctx, tx, storeID, userID, "purchase_request.cancel", "purchase_request", requestID, nil)
	})
}

// transition fetches the request locked, enforces the caller's right to act,
// runs the state change, and returns the fresh row.
func (r *PurchaseRequestRepo) transition(ctx context.Context, storeID, requestID, actorID, target string, mutate func(tx pgx.Tx) error) (*models.PurchaseRequest, error) {
	var req models.PurchaseRequest
	err := pgx.BeginFunc(ctx, r.db, func(tx pgx.Tx) error {
		var requestedBy, status string
		err := tx.QueryRow(ctx, `
			SELECT requested_by::text, status::text
			FROM purchase_requests
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
		// Reject is owner-only; cancel is requester-only.
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
			SELECT id::text, store_id::text, requested_by::text, status::text,
			       purchase_id::text, reviewed_by::text, reviewed_at, rejection_reason,
			       created_at, updated_at
			FROM purchase_requests
			WHERE id = $1`, requestID).
			Scan(&req.ID, &req.StoreID, &req.RequestedBy, &req.Status,
				&req.PurchaseID, &req.ReviewedBy, &req.ReviewedAt, &req.RejectionReason,
				&req.CreatedAt, &req.UpdatedAt)
	})
	if err != nil {
		return nil, err
	}
	return &req, nil
}

// Get returns a single request including its snapshot and reviewer name.
func (r *PurchaseRequestRepo) Get(ctx context.Context, storeID, requestID string) (*models.PurchaseRequest, error) {
	var req models.PurchaseRequest
	var snapshot []byte
	err := r.db.QueryRow(ctx, `
		SELECT r.id::text, r.store_id::text, r.requested_by::text, r.status::text,
		       r.purchase_snapshot, r.purchase_id::text, r.reviewed_by::text,
		       r.reviewed_at, r.rejection_reason, r.created_at, r.updated_at,
		       u.name,
		       COALESCE(rv.name, '')
		FROM purchase_requests r
		JOIN users u ON u.id = r.requested_by
		LEFT JOIN users rv ON rv.id = r.reviewed_by
		WHERE r.id = $1 AND r.store_id = $2`, requestID, storeID).
		Scan(&req.ID, &req.StoreID, &req.RequestedBy, &req.Status,
			&snapshot, &req.PurchaseID, &req.ReviewedBy,
			&req.ReviewedAt, &req.RejectionReason, &req.CreatedAt, &req.UpdatedAt,
			&req.RequesterName, &req.ReviewerName)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, models.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	req.PurchaseSnapshot = snapshot
	return &req, nil
}

// List returns a page of requests for the store, optionally narrowed to one
// status, plus the total matching count for pagination metadata.
// Snapshots and reviewer names are included so the review screens have
// everything in a single call.
func (r *PurchaseRequestRepo) List(ctx context.Context, storeID, status string, limit, offset int) ([]models.PurchaseRequest, int64, error) {
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
		FROM purchase_requests r
		WHERE r.store_id = $1 AND ($2 = '' OR r.status::text = $2)`,
		storeID, status).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := r.db.Query(ctx, `
		SELECT r.id::text, r.store_id::text, r.requested_by::text, r.status::text,
		       r.purchase_snapshot, r.purchase_id::text, r.reviewed_by::text,
		       r.reviewed_at, r.rejection_reason, r.created_at, r.updated_at,
		       u.name,
		       COALESCE(rv.name, '')
		FROM purchase_requests r
		JOIN users u ON u.id = r.requested_by
		LEFT JOIN users rv ON rv.id = r.reviewed_by
		WHERE r.store_id = $1 AND ($2 = '' OR r.status::text = $2)
		ORDER BY r.created_at DESC
		LIMIT $3 OFFSET $4`, storeID, status, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	out := make([]models.PurchaseRequest, 0)
	for rows.Next() {
		var req models.PurchaseRequest
		var snapshot []byte
		if err := rows.Scan(&req.ID, &req.StoreID, &req.RequestedBy, &req.Status,
			&snapshot, &req.PurchaseID, &req.ReviewedBy,
			&req.ReviewedAt, &req.RejectionReason, &req.CreatedAt, &req.UpdatedAt,
			&req.RequesterName, &req.ReviewerName); err != nil {
			return nil, 0, err
		}
		req.PurchaseSnapshot = snapshot
		out = append(out, req)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	return out, total, nil
}