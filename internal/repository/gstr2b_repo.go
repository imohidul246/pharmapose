package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mohi/pms-marg-inspired/internal/models"
)

// GSTR2BBatchStatus values.
const (
	gstr2bStatusImported   = "IMPORTED"
	gstr2bStatusReconciled = "RECONCILED"
)

// GSTR2BMatchStatus values.
const (
	gstr2bMatchUnmatched      = "UNMATCHED"
	gstr2bMatchMatched        = "MATCHED"
	gstr2bMatchAmountMismatch = "AMOUNT_MISMATCH"
)

// matchTolerance is the maximum absolute difference between the invoice value
// reported by the supplier in GSTR-2B and the value recorded in our purchase
// ledger for a document to be considered matched to the paisa.
const matchTolerance = 0.02

type GSTR2BRepo struct {
	db *pgxpool.Pool
}

func NewGSTR2BRepo(db *pgxpool.Pool) *GSTR2BRepo {
	return &GSTR2BRepo{db: db}
}

// Import stores a GSTR-2B document set for a supplier and period and runs the
// reconciliation against purchase_orders. Re-importing the same (supplier,
// period) replaces the previous documents so the latest GSTN file is always
// the active comparison set; the older batch remains as history.
//
// The returned reconciliation reflects the matching outcome of this import.
func (r *GSTR2BRepo) Import(ctx context.Context, storeID string, in *models.GSTR2BImportInput) (*models.GSTR2BReconciliation, error) {
	if err := validateImport(in); err != nil {
		return nil, err
	}

	var rec *models.GSTR2BReconciliation
	err := pgx.BeginFunc(ctx, r.db, func(tx pgx.Tx) error {
		// A nil/empty store filter must become an empty string so the SQL
		// guard `($3 = '' OR po.store_id::text = $3)` evaluates true instead
		// of NULL (which would match nothing).
		matchStore := storeID
		// Replace any prior active import for the same supplier+period (and
		// store, when one is supplied) so the reconciliation always reflects
		// the latest GSTN file.
		firstGSTIN := in.Docs[0].SupplierGSTIN
		if _, err := tx.Exec(ctx, `
			DELETE FROM gstr2b_imports gi
			USING gstr2b_import_batches gib
			WHERE gi.import_batch_id = gib.id
			  AND gib.gstin = $1 AND gib.period = $2
			  AND ($3 = '' OR gib.store_id::text = $3)`,
			firstGSTIN, in.Period, storeID); err != nil {
			return err
		}

		var batchID string
		if err := tx.QueryRow(ctx, `
			INSERT INTO gstr2b_import_batches (store_id, gstin, period, file_name, status)
			VALUES ($1, $2, $3, $4, $5)
			RETURNING id::text`,
			sqlStr(&storeID), firstGSTIN, in.Period, in.Source, gstr2bStatusImported).
			Scan(&batchID); err != nil {
			return err
		}

		matched := 0
		unmatched := 0
		amountMismatch := 0
		var matchedTaxable, unmatchedTaxable float64

		for _, d := range in.Docs {
			docID := ""
			if err := tx.QueryRow(ctx, `
				INSERT INTO gstr2b_imports
					(import_batch_id, store_id, supplier_gstin, doc_type, period,
					 invoice_no, invoice_date, taxable_value,
					 igst_amount, cgst_amount, sgst_amount, cess_amount, total_value)
				VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
				RETURNING id::text`,
				batchID, sqlStr(&storeID), d.SupplierGSTIN, d.DocType, in.Period,
				d.InvoiceNo, d.InvoiceDate, d.TaxableValue,
				d.IGST, d.CGST, d.SGST, d.Cess, d.TotalValue,
			).Scan(&docID); err != nil {
				return err
			}

			// Only invoices are matched against the purchase ledger. Credit and
			// debit notes from suppliers stay unmatched until purchase-side
			// notes are modelled.
			if d.DocType != "INV" {
				unmatched++
				unmatchedTaxable += d.TaxableValue
				continue
			}

			var poID string
			var poGrand, poTaxable *float64
			err := tx.QueryRow(ctx, `
				SELECT po.id::text, po.grand_total::float8, po.taxable_amount::float8
				FROM purchase_orders po
				WHERE po.supplier_gstin = $1
				  AND po.invoice_no = $2
				  AND ($3 = '' OR po.store_id::text = $3)
				ORDER BY po.invoice_date DESC, po.created_at DESC
				LIMIT 1`,
				d.SupplierGSTIN, d.InvoiceNo, matchStore).
				Scan(&poID, &poGrand, &poTaxable)
			if errors.Is(err, pgx.ErrNoRows) {
				status := gstr2bMatchUnmatched
				if _, uerr := tx.Exec(ctx, `
					UPDATE gstr2b_imports
					SET match_status = $2, notes = 'No matching purchase invoice found in books'
					WHERE id = $1`, docID, status); uerr != nil {
					return uerr
				}
				unmatched++
				unmatchedTaxable += d.TaxableValue
				continue
			}
			if err != nil {
				return err
			}

			// Compare invoice values. Nullable purchase totals fall back to the
			// supplier-reported value so legacy rows do not spuriously mismatch.
			expected := d.TotalValue
			if expected == 0 {
				expected = d.TaxableValue
			}
			actual := 0.0
			if poGrand != nil {
				actual = *poGrand
			}
			if actual == 0 && poTaxable != nil {
				actual = *poTaxable
			}
			diff := actual - expected
			status := gstr2bMatchMatched
			if absf(diff) > matchTolerance {
				status = gstr2bMatchAmountMismatch
				amountMismatch++
				matched++
			} else {
				matched++
			}
			matchedTaxable += d.TaxableValue

			if _, err := tx.Exec(ctx, `
				UPDATE gstr2b_imports
				SET match_status = $2, matched_purchase_id = $3, matched_difference = $4
				WHERE id = $1`, docID, status, poID, diff); err != nil {
				return err
			}
		}

		status := gstr2bStatusReconciled
		if _, err := tx.Exec(ctx, `
			UPDATE gstr2b_import_batches
			SET status = $2, doc_count = $3, matched_count = $4, unmatched_count = $5
			WHERE id = $1`,
			batchID, status, len(in.Docs), matched, unmatched); err != nil {
			return err
		}

		rec = &models.GSTR2BReconciliation{
			BatchID:          batchID,
			Period:           in.Period,
			GSTIN:            firstGSTIN,
			TotalDocs:        len(in.Docs),
			Matched:          matched,
			Unmatched:        unmatched,
			AmountMismatch:   amountMismatch,
			MatchedTaxable:   round2(matchedTaxable),
			UnmatchedTaxable: round2(unmatchedTaxable),
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return rec, nil
}

// BatchDocs returns the documents of an import batch.
func (r *GSTR2BRepo) BatchDocs(ctx context.Context, storeID, batchID string) ([]models.GSTR2BImport, error) {
	rows, err := r.db.Query(ctx, `
		SELECT id::text, import_batch_id::text, store_id::text, supplier_gstin, doc_type, period,
		       invoice_no, invoice_date, taxable_value::float8,
		       igst_amount::float8, cgst_amount::float8, sgst_amount::float8, cess_amount::float8,
		       total_value::float8, match_status, matched_purchase_id::text, matched_difference::float8,
		       COALESCE(notes, ''), created_at
		FROM gstr2b_imports
		WHERE import_batch_id = $1 AND store_id = $2
		ORDER BY doc_type, supplier_gstin, invoice_date, invoice_no`, batchID, storeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]models.GSTR2BImport, 0)
	for rows.Next() {
		var d models.GSTR2BImport
		var storeID, matchedPurchaseID *string
		var matchedDiff *float64
		var invDate time.Time
		if err := rows.Scan(&d.ID, &d.ImportBatchID, &storeID, &d.SupplierGSTIN, &d.DocType,
			&d.Period, &d.InvoiceNo, &invDate, &d.TaxableValue,
			&d.IGSTAmount, &d.CGSTAmount, &d.SGSTAmount, &d.CessAmount,
			&d.TotalValue, &d.MatchStatus, &matchedPurchaseID, &matchedDiff,
			&d.Notes, &d.CreatedAt); err != nil {
			return nil, err
		}
		d.StoreID = storeID
		d.MatchedPurchaseID = matchedPurchaseID
		d.MatchedDifference = matchedDiff
		d.InvoiceDate = models.NewDate(invDate)
		out = append(out, d)
	}
	return out, rows.Err()
}

// ListBatches returns import batches, newest first.
func (r *GSTR2BRepo) ListBatches(ctx context.Context, storeID string) ([]models.GSTR2BImportBatch, error) {
	rows, err := r.db.Query(ctx, `
		SELECT id::text, store_id::text, gstin, period, file_name, doc_count, matched_count,
		       unmatched_count, status, created_at
		FROM gstr2b_import_batches
		WHERE $1 = '' OR store_id::text = $1
		ORDER BY created_at DESC`, storeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]models.GSTR2BImportBatch, 0)
	for rows.Next() {
		var b models.GSTR2BImportBatch
		var storeID *string
		if err := rows.Scan(&b.ID, &storeID, &b.GSTIN, &b.Period, &b.FileName,
			&b.DocCount, &b.MatchedCount, &b.UnmatchedCount, &b.Status, &b.CreatedAt); err != nil {
			return nil, err
		}
		b.StoreID = storeID
		out = append(out, b)
	}
	return out, rows.Err()
}

// GetBatch returns a single import batch.
func (r *GSTR2BRepo) GetBatch(ctx context.Context, storeID, batchID string) (*models.GSTR2BImportBatch, error) {
	var b models.GSTR2BImportBatch
	var storeID2 *string
	err := r.db.QueryRow(ctx, `
		SELECT id::text, store_id::text, gstin, period, file_name, doc_count, matched_count,
		       unmatched_count, status, created_at
		FROM gstr2b_import_batches WHERE id = $1 AND ($2 = '' OR store_id::text = $2)`, batchID, storeID).
		Scan(&b.ID, &storeID2, &b.GSTIN, &b.Period, &b.FileName,
			&b.DocCount, &b.MatchedCount, &b.UnmatchedCount, &b.Status, &b.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, models.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	b.StoreID = storeID2
	return &b, nil
}

func validateImport(in *models.GSTR2BImportInput) error {
	if in == nil || len(in.Docs) == 0 {
		return errors.New("gstr2b import requires at least one document")
	}
	if in.Period == "" {
		return errors.New("gstr2b import requires a period (YYYY-MM)")
	}
	if _, err := time.Parse("2006-01", in.Period); err != nil {
		return errors.New("gstr2b period must be YYYY-MM")
	}
	seen := map[string]bool{}
	for _, d := range in.Docs {
		if d.SupplierGSTIN == "" {
			return fmt.Errorf("document %q requires supplier_gstin", d.InvoiceNo)
		}
		if d.InvoiceNo == "" {
			return fmt.Errorf("document requires invoice_no")
		}
		if d.InvoiceDate == "" {
			return fmt.Errorf("document %q requires invoice_date (YYYY-MM-DD)", d.InvoiceNo)
		}
		if _, err := time.Parse("2006-01-02", d.InvoiceDate); err != nil {
			return fmt.Errorf("document %q invoice_date must be YYYY-MM-DD", d.InvoiceNo)
		}
		if d.DocType == "" {
			d.DocType = "INV"
		}
		if d.DocType != "INV" && d.DocType != "CRN" && d.DocType != "DBN" {
			return fmt.Errorf("document %q doc_type must be INV, CRN or DBN", d.InvoiceNo)
		}
		key := d.SupplierGSTIN + "|" + d.InvoiceNo
		if seen[key] {
			return fmt.Errorf("duplicate document %q for supplier %s", d.InvoiceNo, d.SupplierGSTIN)
		}
		seen[key] = true
	}
	if in.GSTIN != "" && in.Docs[0].SupplierGSTIN != in.GSTIN {
		for _, d := range in.Docs {
			if d.SupplierGSTIN != in.GSTIN {
				return fmt.Errorf("all documents must belong to supplier GSTIN %s (found %s)", in.GSTIN, d.SupplierGSTIN)
			}
		}
	}
	return nil
}

func absf(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}
