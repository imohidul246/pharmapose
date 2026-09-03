package services

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type InvoiceSequence struct {
	db *pgxpool.Pool
	// fallback counter for stores without a store_id (tests, legacy data)
	fallbackSeq atomic.Int64
}

func NewInvoiceSequence(db *pgxpool.Pool) *InvoiceSequence {
	return &InvoiceSequence{db: db}
}

// FinancialYear returns the GST financial year string for a given date.
// April 1 2026 – March 31 2027 => "2026-27"
func FinancialYear(t time.Time) string {
	year := t.Year()
	if t.Month() >= time.April {
		return fmt.Sprintf("%d-%02d", year, (year+1)%100)
	}
	return fmt.Sprintf("%d-%02d", year-1, year%100)
}

// NextInvoiceNumber generates the next gapless invoice number for a store
// within the current financial year. It delegates to NextInvoiceNumberAt
// with the current time; back-dated flows must call NextInvoiceNumberAt with
// the invoice date so the number lands in the invoice's own financial year.
//
// Atomicity: the INSERT ... ON CONFLICT DO NOTHING plus the conditional
// UPDATE ... RETURNING run inside the caller's transaction. Concurrent
// checkouts serialize on the sequence row lock, so each commits with a
// distinct last_value — duplicates are impossible. A rolled-back transaction
// burns its number (the increment does not roll back visibly to others),
// which is the standard, acceptable source of gaps; committed numbers are
// strictly gapless per (store, financial year, prefix).
//
// Format: INV/YY-YY/NNNNN (e.g. INV/26-27/00001)
func (s *InvoiceSequence) NextInvoiceNumber(ctx context.Context, tx pgx.Tx, storeID string, prefix string) (string, string, error) {
	return s.NextInvoiceNumberAt(ctx, tx, storeID, prefix, time.Now().UTC())
}

// NextInvoiceNumberAt behaves like NextInvoiceNumber but attributes the
// number to the financial year containing `at` (the invoice date), so
// back-dated invoices (CheckoutAt, historical seeding) never borrow numbers
// from the wrong financial year.
func (s *InvoiceSequence) NextInvoiceNumberAt(ctx context.Context, tx pgx.Tx, storeID string, prefix string, at time.Time) (string, string, error) {
	fy := FinancialYear(at)

	// If no store_id, use an in-memory atomic fallback (for tests/legacy)
	if storeID == "" {
		seq := s.fallbackSeq.Add(1)
		invoiceNo := fmt.Sprintf("%s%s/%05d", prefix, formatFY(fy), seq)
		return invoiceNo, fy, nil
	}

	// Upsert the sequence row; create with last_value = 0 if it doesn't exist.
	_, err := tx.Exec(ctx, `
		INSERT INTO invoice_sequences (store_id, financial_year, prefix, last_value)
		VALUES ($1, $2, $3, 0)
		ON CONFLICT (store_id, financial_year, prefix) DO NOTHING`,
		storeID, fy, prefix)
	if err != nil {
		return "", fy, fmt.Errorf("upsert invoice sequence: %w", err)
	}

	// Lock the row and increment atomically.
	var nextVal int
	err = tx.QueryRow(ctx, `
		UPDATE invoice_sequences
		SET last_value = last_value + 1
		WHERE store_id = $1 AND financial_year = $2 AND prefix = $3
		RETURNING last_value`,
		storeID, fy, prefix).Scan(&nextVal)
	if err != nil {
		return "", fy, fmt.Errorf("increment invoice sequence: %w", err)
	}

	invoiceNo := fmt.Sprintf("%s%s/%05d", prefix, formatFY(fy), nextVal)
	return invoiceNo, fy, nil
}

// NextCreditNoteNumber generates the next credit note number for a store.
//
// Format: CN/YY-YY/NNNNN
func (s *InvoiceSequence) NextCreditNoteNumber(ctx context.Context, tx pgx.Tx, storeID string) (string, string, error) {
	return s.NextInvoiceNumber(ctx, tx, storeID, "CN/")
}

// formatFY converts "2026-27" to "26-27" for display in invoice numbers.
func formatFY(fy string) string {
	if len(fy) == 7 && fy[4] == '-' {
		return fy[2:]
	}
	return fy
}
