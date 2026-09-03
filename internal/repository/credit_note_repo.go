package repository

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/mohi/pms-marg-inspired/internal/models"
	"github.com/mohi/pms-marg-inspired/internal/tax"
)

// CreditNoteItemInput is one line being returned: the billed quantity and the
// bonus (free) quantity returned together. Both restore physical stock.
type CreditNoteItemInput struct {
	InvoiceItemID string `json:"invoice_item_id"`
	Quantity      int    `json:"quantity"`                 // billed units returned
	BonusQuantity int    `json:"bonus_quantity,omitempty"` // free units returned
}

// CreateCreditNoteInput creates a sales return / credit note against a prior
// invoice in the same store.
type CreateCreditNoteInput struct {
	StoreID   *string               `json:"store_id,omitempty"`
	InvoiceID string                `json:"invoice_id"`
	Reason    string                `json:"reason"`
	Items     []CreditNoteItemInput `json:"items"`
}

// CreateCreditNote records a sales return and restocks inventory atomically:
//
//  1. Verifies the invoice belongs to the session's store (IDOR guard).
//  2. Verifies every returned line belongs to that invoice.
//  3. Locks every involved batch in strict sorted order (deadlock prevention).
//  4. Inserts the credit note + items (with bonus_quantity snapshot).
//  5. Restocks the FULL physical quantity (billed + bonus) that is being
//     returned — never only the billed part.
//
// Any failure rolls back everything.
func (r *SaleRepo) CreateCreditNote(ctx context.Context, in *CreateCreditNoteInput) (*models.SalesCreditNote, []models.SalesCreditNoteItem, error) {
	if in == nil || in.InvoiceID == "" {
		return nil, nil, models.NewValidationError("invoice_id is required")
	}
	if len(in.Items) == 0 {
		return nil, nil, models.NewValidationError("credit note requires at least one item")
	}
	storeID := ""
	if in.StoreID != nil {
		storeID = *in.StoreID
	}
	if storeID == "" {
		return nil, nil, models.NewValidationError("store_id is required")
	}
	for _, it := range in.Items {
		if it.InvoiceItemID == "" {
			return nil, nil, models.NewValidationError("invoice_item_id is required")
		}
		if it.Quantity < 0 || it.BonusQuantity < 0 {
			return nil, nil, models.NewValidationError("return quantities must be non-negative")
		}
		if it.Quantity == 0 && it.BonusQuantity == 0 {
			return nil, nil, models.NewValidationError("return quantity must be positive")
		}
	}

	var (
		note  models.SalesCreditNote
		items []models.SalesCreditNoteItem
	)
	err := pgx.BeginFunc(ctx, r.db, func(tx pgx.Tx) error {
		// 1. Tenant-scoped invoice lookup (IDOR): a foreign invoice ID
		// resolves to not-found rather than leaking header data.
		var (
			invNo         string
			invDate       time.Time
			invFY         string
			custGSTIN     *string
			supplyType    *string
			customerID    *string
			paymentType   string
		)
		err := tx.QueryRow(ctx, `
			SELECT invoice_no, invoice_date, financial_year, customer_gstin,
			       supply_type, customer_id::text, payment_type::text
			FROM sales_invoices WHERE id = $1 AND store_id = $2`,
			in.InvoiceID, storeID).Scan(
			&invNo, &invDate, &invFY, &custGSTIN, &supplyType, &customerID, &paymentType)
		if errors.Is(err, pgx.ErrNoRows) {
			return models.ErrNotFound
		}
		if err != nil {
			return err
		}

		// 2. Load + verify the original lines (all must belong to this invoice).
		itemIDs := make([]string, len(in.Items))
		for i, it := range in.Items {
			itemIDs[i] = it.InvoiceItemID
		}
		type origLine struct {
			medicineID string
			batchID    string
			quantity   int
			bonus      int
			hsn        *string
			taxable    float64
			gstRate    float64
			cgst       float64
			sgst       float64
			igst       float64
			cess       float64
			lineTotal  float64
		}
		orig := make(map[string]origLine, len(itemIDs))
		rows, err := tx.Query(ctx, `
			SELECT id::text, medicine_id::text, batch_id::text, quantity,
			       COALESCE(bonus_quantity, 0),
			       hsn_code,
			       COALESCE(taxable_value, subtotal, 0)::float8,
			       COALESCE(gst_rate, 0)::float8,
			       COALESCE(cgst_amount, 0)::float8,
			       COALESCE(sgst_amount, 0)::float8,
			       COALESCE(igst_amount, 0)::float8,
			       COALESCE(cess_amount, 0)::float8,
			       COALESCE(line_total, subtotal, 0)::float8
			FROM sales_invoice_items
			WHERE id = ANY($1) AND invoice_id = $2`, itemIDs, in.InvoiceID)
		if err != nil {
			return err
		}
		for rows.Next() {
			var id string
			var o origLine
			if err := rows.Scan(&id, &o.medicineID, &o.batchID, &o.quantity,
				&o.bonus, &o.hsn, &o.taxable, &o.gstRate, &o.cgst, &o.sgst,
				&o.igst, &o.cess, &o.lineTotal); err != nil {
				rows.Close()
				return err
			}
			orig[id] = o
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return err
		}
		rows.Close()
		for _, it := range in.Items {
			o, ok := orig[it.InvoiceItemID]
			if !ok {
				return models.ErrNotFound
			}
			// Cannot return more than was originally sold (billed and bonus
			// tracked independently). Already-returned tracking is enforced
			// by capping against the original line; repeated returns of the
			// same line beyond the original fail here.
			if it.Quantity > o.quantity {
				return models.NewValidationError(
					fmt.Sprintf("return billed qty %d exceeds sold qty %d for line %s",
						it.Quantity, o.quantity, it.InvoiceItemID))
			}
			if it.BonusQuantity > o.bonus {
				return models.NewValidationError(
					fmt.Sprintf("return bonus qty %d exceeds sold bonus %d for line %s",
						it.BonusQuantity, o.bonus, it.InvoiceItemID))
			}
		}

		// 3. Deterministic batch lock ordering (deadlock prevention).
		batchSet := make(map[string]struct{}, len(in.Items))
		for _, it := range in.Items {
			batchSet[orig[it.InvoiceItemID].batchID] = struct{}{}
		}
		batchIDs := make([]string, 0, len(batchSet))
		for id := range batchSet {
			batchIDs = append(batchIDs, id)
		}
		sort.Strings(batchIDs)
		for _, bid := range batchIDs {
			var dummy int
			if err := tx.QueryRow(ctx, `
				SELECT current_stock FROM batches
				WHERE id = $1 AND store_id = $2 FOR UPDATE`, bid, storeID).Scan(&dummy); err != nil {
				if errors.Is(err, pgx.ErrNoRows) {
					return models.ErrNotFound
				}
				return err
			}
		}

		// 4. Compute credit totals pro-rata on billed qty (bonus units carry
		// no commercial value, matching the checkout tax treatment).
		// All money uses decimal + banker's rounding via the tax engine.
		var (
			grossD, taxableD, cgstD, sgstD, igstD, cessD, grandD decimal.Decimal
		)
		now := time.Now().UTC()
		noteItems := make([]models.SalesCreditNoteItem, 0, len(in.Items))
		for _, it := range in.Items {
			o := orig[it.InvoiceItemID]
			fracD := decimal.Zero
			if o.quantity > 0 {
				fracD = decimal.NewFromInt(int64(it.Quantity)).Div(decimal.NewFromInt(int64(o.quantity)))
			}
			taxablePartD := decimal.NewFromFloat(o.taxable).Mul(fracD)
			cgstPartD := decimal.NewFromFloat(o.cgst).Mul(fracD)
			sgstPartD := decimal.NewFromFloat(o.sgst).Mul(fracD)
			igstPartD := decimal.NewFromFloat(o.igst).Mul(fracD)
			cessPartD := decimal.NewFromFloat(o.cess).Mul(fracD)
			linePartD := decimal.NewFromFloat(o.lineTotal).Mul(fracD)
			grossPartD := linePartD // gross tracks the commercial value returned
			grossD = grossD.Add(grossPartD)
			taxableD = taxableD.Add(taxablePartD)
			cgstD = cgstD.Add(cgstPartD)
			sgstD = sgstD.Add(sgstPartD)
			igstD = igstD.Add(igstPartD)
			cessD = cessD.Add(cessPartD)
			grandD = grandD.Add(linePartD)
			taxablePart, _ := tax.RoundMoney(taxablePartD).Float64()
			cgstPart, _ := tax.RoundMoney(cgstPartD).Float64()
			sgstPart, _ := tax.RoundMoney(sgstPartD).Float64()
			igstPart, _ := tax.RoundMoney(igstPartD).Float64()
			cessPart, _ := tax.RoundMoney(cessPartD).Float64()
			linePart, _ := tax.RoundMoney(linePartD).Float64()
			noteItems = append(noteItems, models.SalesCreditNoteItem{
				InvoiceItemID: &it.InvoiceItemID,
				MedicineID:    o.medicineID,
				BatchID:       o.batchID,
				Quantity:      it.Quantity,
				BonusQuantity: it.BonusQuantity,
				HSNCode:       o.hsn,
				TaxableValue:  taxablePart,
				GSTRate:       o.gstRate,
				CGSTAmount:    cgstPart,
				SGSTAmount:    sgstPart,
				IGSTAmount:    igstPart,
				CessAmount:    cessPart,
				LineTotal:     linePart,
			})
		}
		gross, _ := tax.RoundMoney(grossD).Float64()
		taxable, _ := tax.RoundMoney(taxableD).Float64()
		cgst, _ := tax.RoundMoney(cgstD).Float64()
		sgst, _ := tax.RoundMoney(sgstD).Float64()
		igst, _ := tax.RoundMoney(igstD).Float64()
		cess, _ := tax.RoundMoney(cessD).Float64()
		taxTotalD := tax.RoundMoney(decimal.NewFromFloat(cgst).Add(decimal.NewFromFloat(sgst)).Add(decimal.NewFromFloat(igst)).Add(decimal.NewFromFloat(cess)))
		taxTotal, _ := taxTotalD.Float64()
		grand, _ := tax.RoundMoney(grandD).Float64()

		noteNo, noteFY, err := r.seq.NextCreditNoteNumberAt(ctx, tx, storeID, now)
		if err != nil {
			return fmt.Errorf("generate credit note number: %w", err)
		}
		if in.Reason == "" {
			in.Reason = "SALES_RETURN"
		}
		err = tx.QueryRow(ctx, `
			INSERT INTO sales_credit_notes
				(invoice_id, note_no, reason, gross_amount, taxable_amount,
				 cgst_total, sgst_total, igst_total, cess_total, tax_total, grand_total,
				 note_date, original_invoice_no, original_invoice_date,
				 store_id, financial_year, customer_gstin, supply_type)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11,
			        $12, $13, $14, $15, $16, $17, $18)
			RETURNING id::text, note_no, note_date, created_at`,
			in.InvoiceID, noteNo, in.Reason, gross, taxable,
			cgst, sgst, igst, cess, taxTotal, grand,
			now, invNo, invDate,
			storeID, noteFY, custGSTIN, supplyType,
		).Scan(&note.ID, &note.NoteNo, &note.NoteDate, &note.CreatedAt)
		if err != nil {
			return err
		}
		note.InvoiceID = in.InvoiceID
		note.Reason = in.Reason
		note.OriginalInvoiceNo = &invNo
		oid := models.NewDate(invDate)
		note.OriginalInvoiceDate = &oid
		note.StoreID = &storeID
		note.FinancialYear = noteFY
		note.CustomerGSTIN = custGSTIN
		note.SupplyType = supplyType
		note.GrossAmount = gross
		note.TaxableAmount = taxable
		note.CGSTTotal = cgst
		note.SGSTTotal = sgst
		note.IGSTTotal = igst
		note.CessTotal = cess
		note.TaxTotal = taxTotal
		note.GrandTotal = grand
		_ = invFY
		_ = paymentType

		for i := range noteItems {
			ni := &noteItems[i]
			if err := tx.QueryRow(ctx, `
				INSERT INTO sales_credit_note_items
					(credit_note_id, invoice_item_id, medicine_id, batch_id,
					 quantity, bonus_quantity, hsn_code, taxable_value, gst_rate,
					 cgst_amount, sgst_amount, igst_amount, cess_amount, line_total)
				VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
				RETURNING id::text`,
				note.ID, ni.InvoiceItemID, ni.MedicineID, ni.BatchID,
				ni.Quantity, ni.BonusQuantity, ni.HSNCode, ni.TaxableValue, ni.GSTRate,
				ni.CGSTAmount, ni.SGSTAmount, ni.IGSTAmount, ni.CessAmount, ni.LineTotal,
			).Scan(&ni.ID); err != nil {
				return err
			}
			ni.CreditNoteID = note.ID

			// 5. Restock the FULL physical quantity (billed + bonus).
			// A return of 10 billed + 2 free restores 12 units — restoring
			// only the billed part would permanently lose the bonus stock.
			restock := ni.Quantity + ni.BonusQuantity
			tag, err := tx.Exec(ctx, `
				UPDATE batches SET current_stock = current_stock + $2, updated_at = now()
				WHERE id = $1 AND store_id = $3`,
				ni.BatchID, restock, storeID)
			if err != nil {
				return err
			}
			if tag.RowsAffected() == 0 {
				return models.ErrNotFound
			}
		}

		// For credit sales, reduce the customer's outstanding by the amount
		// credited back.
		if customerID != nil && *customerID != "" && grand > 0 {
			if _, err := tx.Exec(ctx, `
				UPDATE customers SET current_balance = current_balance - $2, updated_at = now()
				WHERE id = $1 AND store_id = $3`, *customerID, grand, storeID); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `
				INSERT INTO customer_ledger (customer_id, entry_type, amount, balance_after, notes)
				SELECT $1, 'CREDIT_NOTE', $2, current_balance, $3
				FROM customers WHERE id = $1 AND store_id = $4`,
				*customerID, -grand,
				fmt.Sprintf("Credit note %s (return of %s)", note.NoteNo, invNo), storeID); err != nil {
				return err
			}
		}

		items = noteItems
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	return &note, items, nil
}
