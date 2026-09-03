package repository

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"

	"github.com/mohi/pms-marg-inspired/internal/models"
	"github.com/mohi/pms-marg-inspired/internal/services"
	"github.com/mohi/pms-marg-inspired/internal/tax"
)

type SaleRepo struct {
	db      *pgxpool.Pool
	taxRepo *TaxRepo
	seq     *services.InvoiceSequence
}

func NewSaleRepo(db *pgxpool.Pool) *SaleRepo {
	return &SaleRepo{db: db, taxRepo: NewTaxRepo(db), seq: services.NewInvoiceSequence(db)}
}

type LineDiscount struct {
	Type  string  `json:"type"`
	Value float64 `json:"value"`
}

type CheckoutItemInput struct {
	BatchID       string        `json:"batch_id"`
	Quantity      int           `json:"quantity"`
	Discount      *LineDiscount `json:"discount,omitempty"`
	SellPrice     *float64      `json:"sell_price,omitempty"`     // B2B: custom sell price (nil = use batch sale_price)
	BonusQuantity int           `json:"bonus_quantity,omitempty"` // B2B: free items
}

type CheckoutInput struct {
	CustomerID    *string             `json:"customer_id"`
	PaymentType   models.PaymentType  `json:"payment_type"`
	Items         []CheckoutItemInput `json:"items"`
	StoreID       *string             `json:"store_id,omitempty"`
	PlaceOfSupply *string             `json:"place_of_supply,omitempty"` // state code of buyer

	// B2B fields
	SaleType     string  `json:"sale_type,omitempty"` // "RETAIL" (default) or "B2B"
	BuyerName    *string `json:"buyer_name,omitempty"`
	BuyerGSTIN   *string `json:"buyer_gstin,omitempty"`
	BuyerAddress *string `json:"buyer_address,omitempty"`

	invoice *models.SalesInvoice
}

type CheckoutResult struct {
	Invoice models.SalesInvoice       `json:"invoice"`
	Items   []models.SalesInvoiceItem `json:"items"`
}

// round2 clamps monetary math to two decimals to avoid float drift.
func round2(v float64) float64 {
	return math.Round(v*100) / 100
}

func (in *CheckoutInput) validate() error {
	if !in.PaymentType.Valid() {
		return models.NewValidationError("payment_type must be CASH or CREDIT")
	}
	if len(in.Items) == 0 {
		return models.NewValidationError("checkout requires at least one item")
	}
	if in.SaleType == "" {
		in.SaleType = "RETAIL"
	}
	if in.SaleType != "RETAIL" && in.SaleType != "B2B" {
		return models.NewValidationError("sale_type must be RETAIL or B2B")
	}
	for _, it := range in.Items {
		if it.Quantity <= 0 {
			return models.NewValidationError("item quantity must be positive")
		}
		if it.BatchID == "" {
			return models.NewValidationError("item batch_id is required")
		}
		if it.BonusQuantity < 0 {
			return models.NewValidationError("bonus_quantity must be non-negative")
		}
		if it.SellPrice != nil && *it.SellPrice < 0 {
			return models.NewValidationError("sell_price must be non-negative")
		}
		if it.Discount != nil {
			switch it.Discount.Type {
			case "percent", "amount":
			default:
				return models.NewValidationError("discount type must be 'percent' or 'amount'")
			}
			if it.Discount.Value < 0 {
				return models.NewValidationError("discount value must be non-negative")
			}
		}
	}
	if in.PaymentType == models.PaymentCredit && (in.CustomerID == nil || *in.CustomerID == "") {
		return models.NewValidationError("credit sale requires a customer")
	}
	if in.SaleType == "B2B" {
		if in.BuyerGSTIN == nil || *in.BuyerGSTIN == "" {
			return models.NewValidationError("B2B sale requires a buyer GSTIN")
		}
		if !tax.ValidateGSTIN(*in.BuyerGSTIN) {
			return models.NewValidationError("invalid B2B buyer GSTIN")
		}
	}
	return nil
}

// mergedItems collapses duplicate batch lines so stock checks see net demand.
// Duplicate lines carrying conflicting discounts are rejected outright.
func mergedItems(items []CheckoutItemInput) ([]CheckoutItemInput, error) {
	type key struct {
		batchID string
		dtype   string
		value   float64
		sellP   float64
		bonus   int
	}
	byKey := make(map[key]int, len(items))
	order := make([]key, 0, len(items))
	for _, it := range items {
		k := key{batchID: it.BatchID, bonus: it.BonusQuantity}
		if it.Discount != nil && it.Discount.Value > 0 {
			k.dtype = it.Discount.Type
			k.value = it.Discount.Value
		} else if it.Discount != nil && it.Discount.Value < 0 {
			return nil, models.NewValidationError("discount value must be non-negative")
		}
		if it.SellPrice != nil {
			k.sellP = *it.SellPrice
		}

		conflict := false
		for existing := range byKey {
			if existing.batchID == k.batchID && existing != k {
				conflict = true
				break
			}
		}
		if conflict {
			return nil, models.NewValidationError("conflicting discount settings for batch " + it.BatchID + " — merge them into one line")
		}
		if _, seen := byKey[k]; !seen {
			order = append(order, k)
		}
		byKey[k] += it.Quantity
	}
	out := make([]CheckoutItemInput, 0, len(order))
	for _, k := range order {
		item := CheckoutItemInput{BatchID: k.batchID, Quantity: byKey[k], BonusQuantity: k.bonus}
		if k.dtype != "" {
			item.Discount = &LineDiscount{Type: k.dtype, Value: k.value}
		}
		if k.sellP > 0 {
			item.SellPrice = &k.sellP
		}
		out = append(out, item)
	}
	return out, nil
}

// lineDiscount resolves the rupee concession for a line, clamped so the net
// amount can never drop below zero.
func lineDiscount(gross float64, d *LineDiscount) (amount float64, dtype string, dvalue float64) {
	if d == nil || d.Value <= 0 || gross <= 0 {
		return 0, "NONE", 0
	}
	if d.Type == "percent" {
		amount = gross * d.Value / 100
	} else {
		amount = d.Value
	}
	if amount > gross {
		amount = gross
	}
	return round2(amount), d.Type, d.Value
}

// Checkout executes the full sale atomically:
//
//  1. Locks every involved batch row (deterministic id order prevents deadlocks).
//  2. Verifies per-batch stock; aborts with InsufficientStockError otherwise.
//  3. For credit sales locks the customer and enforces balance+total <= credit_limit.
//  4. Inserts the invoice + items, decrements batch stock, increments the ledger.
//
// Any failure rolls back everything, so historical data is never partially mutated.
func (r *SaleRepo) Checkout(ctx context.Context, in *CheckoutInput) (*CheckoutResult, error) {
	return r.checkout(ctx, in, time.Time{})
}

// CheckoutAt behaves like Checkout but stamps an explicit invoice timestamp
// (seeding/demo history only; the live billing path uses Checkout).
func (r *SaleRepo) CheckoutAt(ctx context.Context, in *CheckoutInput, when time.Time) (*CheckoutResult, error) {
	return r.checkout(ctx, in, when)
}

func (r *SaleRepo) checkout(ctx context.Context, in *CheckoutInput, when time.Time) (*CheckoutResult, error) {
	if err := in.validate(); err != nil {
		return nil, err
	}
	items, err := mergedItems(in.Items)
	if err != nil {
		return nil, err
	}
	isB2B := in.SaleType == "B2B"

	// The invoice date is the tax "as of" date. For historical seeding
	// (CheckoutAt) this must be the back-dated timestamp, NOT time.Now(),
	// so that an invoice records the tax that applied on its own invoice
	// date. Do not recompute taxes from today's tax master for old invoices.
	invoiceDate := time.Now().UTC()
	if !when.IsZero() {
		invoiceDate = when
	}

	// The store is derived from the invoice input (validated at the handler
	// against the authenticated principal); it scopes every tax/medicine lookup.
	storeID := ""
	if in.StoreID != nil {
		storeID = *in.StoreID
	}

	var result *CheckoutResult
	err = pgx.BeginFunc(ctx, r.db, func(tx pgx.Tx) error {
		batchIDs := make([]string, len(items))
		for i, it := range items {
			batchIDs[i] = it.BatchID
		}
		sort.Strings(batchIDs)

		rows, err := tx.Query(ctx, `
			SELECT b.id::text, b.medicine_id::text, b.sale_price::float8, b.current_stock
			FROM batches b
			WHERE b.id = ANY($1)
			ORDER BY b.id
			FOR UPDATE`, batchIDs)
		if err != nil {
			return err
		}

		type lockedBatch struct {
			medicineID   string
			salePrice    float64
			currentStock int
		}
		locked := make(map[string]lockedBatch, len(batchIDs))
		for rows.Next() {
			var id string
			var lb lockedBatch
			if err := rows.Scan(&id, &lb.medicineID, &lb.salePrice, &lb.currentStock); err != nil {
				rows.Close()
				return err
			}
			locked[id] = lb
		}
		if err := rows.Err(); err != nil {
			return err
		}
		rows.Close()

	// Determine supply type from store vs customer state
	supplyType := tax.SupplyTypeIntraState
	var gstRegistrationID *string
	var sellerStateCode, buyerStateCode string
		if in.StoreID != nil && *in.StoreID != "" {
			store, err := r.taxRepo.GetStore(ctx, *in.StoreID)
			if err == nil && store.GSTRegistrationID != nil {
				gstRegistrationID = store.GSTRegistrationID
				gr, err := r.taxRepo.GetGSTRegistration(ctx, *store.GSTRegistrationID)
				if err == nil {
					sellerStateCode = gr.StateCode
				}
			}
		}
		if in.PlaceOfSupply != nil {
			buyerStateCode = *in.PlaceOfSupply
		}

		// Resolve the registered-buyer GSTIN so B2B invoices land in the
		// GSTR-1 B2B section. B2B wholesale uses buyer_gstin; retail to a
		// registered customer falls back to the customer's GSTIN.
		var customerGSTIN *string
		if isB2B {
			if in.BuyerGSTIN != nil && *in.BuyerGSTIN != "" {
				customerGSTIN = in.BuyerGSTIN
			}
		} else if in.CustomerID != nil {
			var custGSTIN *string
			var custStateCode *string
			err := tx.QueryRow(ctx,
				`SELECT gstin, state_code FROM customers WHERE id = $1`, *in.CustomerID).
				Scan(&custGSTIN, &custStateCode)
			if err == nil {
				if custGSTIN != nil && *custGSTIN != "" {
					customerGSTIN = custGSTIN
				}
				if custStateCode != nil && buyerStateCode == "" {
					buyerStateCode = *custStateCode
				}
			}
		}
		supplyType = tax.DetermineSupplyType(sellerStateCode, buyerStateCode)
		var customerStateCode *string
		if buyerStateCode != "" {
			customerStateCode = &buyerStateCode
		}

		// Calculate tax for each line using the tax engine
		total := 0.0
		discountTotal := 0.0
		var taxLines []tax.TaxLineResult
		hasTaxConfig := false
		var priceIncludesTax bool
		lineItems := make([]models.SalesInvoiceItem, 0, len(items))
		for _, it := range items {
			lb, ok := locked[it.BatchID]
			if !ok {
				return &models.InsufficientStockError{BatchID: it.BatchID,
					RequestedQty: it.Quantity, AvailableStock: 0}
			}
			totalNeeded := it.Quantity + it.BonusQuantity
			if totalNeeded > lb.currentStock {
				return &models.InsufficientStockError{BatchID: it.BatchID,
					RequestedQty: totalNeeded, AvailableStock: lb.currentStock}
			}

			// B2B: use custom sell price; Retail: use batch sale_price
			unitPrice := lb.salePrice
			var mrpPtr *float64
			if isB2B && it.SellPrice != nil && *it.SellPrice > 0 {
				unitPrice = *it.SellPrice
				mrp := lb.salePrice
				mrpPtr = &mrp
			}

			gross := float64(it.Quantity) * unitPrice
			discAmount, discType, discValue := lineDiscount(gross, it.Discount)
			net := round2(gross - discAmount)
			total = round2(total + net)
			discountTotal = round2(discountTotal + discAmount)

			li := models.SalesInvoiceItem{
				MedicineID:     lb.medicineID,
				BatchID:        it.BatchID,
				Quantity:       it.Quantity,
				UnitSalePrice:  unitPrice,
				Subtotal:       net,
				DiscountType:   discType,
				DiscountValue:  discValue,
				DiscountAmount: discAmount,
				MRP:            mrpPtr,
				BonusQuantity:  it.BonusQuantity,
			}

			// Look up tax configuration for this medicine as of the invoice date
			// (not time.Now()), so historical invoices keep the tax applicable
			// on their own invoice date rather than today's tax master.
			taxConfig, err := r.taxRepo.GetMedicineTaxConfig(ctx, storeID, lb.medicineID, invoiceDate)
			if err != nil {
				return fmt.Errorf("lookup tax config for medicine %s: %w", lb.medicineID, err)
			}
			if taxConfig != nil && taxConfig.TaxRate != nil {
				hasTaxConfig = true
				priceIncludesTax = taxConfig.PriceIncludesTax
				// Use tax engine for calculation
				taxInput := tax.TaxInput{
					Quantity:       decimal.NewFromInt(int64(it.Quantity)),
					UnitPrice:      decimal.NewFromFloat(unitPrice),
					DiscountAmount: decimal.NewFromFloat(discAmount),
					TaxRate: tax.TaxRate{
						GSTRate:  decimal.NewFromFloat(taxConfig.TaxRate.GSTRate),
						CGSTRate: decimal.NewFromFloat(taxConfig.TaxRate.CGSTRate),
						SGSTRate: decimal.NewFromFloat(taxConfig.TaxRate.SGSTRate),
						IGSTRate: decimal.NewFromFloat(taxConfig.TaxRate.IGSTRate),
						CessRate: decimal.NewFromFloat(taxConfig.TaxRate.CessRate),
					},
					PriceIncludesTax: taxConfig.PriceIncludesTax,
					SupplyType:       supplyType,
					HSNCode:          taxConfig.HSNCode,
				}
				tr := tax.CalculateLineTax(taxInput)
				taxLines = append(taxLines, tr)

				// Populate tax snapshot fields on the line item
				grossF, _ := tr.GrossAmount.Float64()
				taxableF, _ := tr.TaxableValue.Float64()
				gstRateF, _ := tr.CGSTRate.Add(tr.SGSTRate).Add(tr.IGSTRate).Float64()
				cgstRateF, _ := tr.CGSTRate.Float64()
				cgstAmtF, _ := tr.CGSTAmount.Float64()
				sgstRateF, _ := tr.SGSTRate.Float64()
				sgstAmtF, _ := tr.SGSTAmount.Float64()
				igstRateF, _ := tr.IGSTRate.Float64()
				igstAmtF, _ := tr.IGSTAmount.Float64()
				cessRateF, _ := tr.CessRate.Float64()
				cessAmtF, _ := tr.CessAmount.Float64()
				lineTotalF, _ := tr.LineTotal.Float64()

				li.HSNCode = &taxConfig.HSNCode
				li.GrossAmount = &grossF
				li.TaxableValue = &taxableF
				li.GSTRate = &gstRateF
				li.CGSTRate = &cgstRateF
				li.CGSTAmount = &cgstAmtF
				li.SGSTRate = &sgstRateF
				li.SGSTAmount = &sgstAmtF
				li.IGSTRate = &igstRateF
				li.IGSTAmount = &igstAmtF
				li.CessRate = &cessRateF
				li.CessAmount = &cessAmtF
				li.LineTotal = &lineTotalF
			} else {
				taxLines = append(taxLines, tax.ZeroTaxResult(
					decimal.NewFromInt(int64(it.Quantity)),
					decimal.NewFromFloat(unitPrice),
					decimal.NewFromFloat(discAmount),
					"",
				))
			}

			lineItems = append(lineItems, li)
		}

		// Compute invoice-level GST totals (single calculation for credit check + INSERT)
		var invoiceResult *tax.TaxInvoiceResult
		chargeableTotal := total
		if len(taxLines) > 0 && hasTaxConfig {
			r := tax.CalculateInvoiceTax(taxLines, supplyType)
			invoiceResult = &r
			chargeableTotal, _ = invoiceResult.GrandTotal.Float64()
		}

		var customerName string
		if in.PaymentType == models.PaymentCredit {
			row := tx.QueryRow(ctx, `
				SELECT name, credit_limit::float8, current_balance::float8
				FROM customers WHERE id = $1
				FOR UPDATE`, *in.CustomerID)
			var limit, balance float64
			if err := row.Scan(&customerName, &limit, &balance); err != nil {
				if errors.Is(err, pgx.ErrNoRows) {
					return models.ErrNotFound
				}
				return err
			}
			if round2(balance+chargeableTotal) > limit {
				return &models.CreditLimitExceededError{
					CustomerID:   *in.CustomerID,
					CustomerName: customerName,
					Outstanding:  balance,
					InvoiceTotal: chargeableTotal,
					CreditLimit:  limit,
				}
			}
		}

		var inv models.SalesInvoice

		// Determine store ID for sequence generation (from the outer function scope)
		invoicePrefix := "INV/"
		if isB2B {
			invoicePrefix = "B2B/"
		}
		invoiceNo, fy, err := r.seq.NextInvoiceNumber(ctx, tx, storeID, invoicePrefix)
		if err != nil {
			return fmt.Errorf("generate invoice number: %w", err)
		}

		if when.IsZero() {
			err = tx.QueryRow(ctx, `
				INSERT INTO sales_invoices (invoice_no, customer_id, payment_type, total_amount, discount_total,
					invoice_date, financial_year,
					sale_type, buyer_name, buyer_gstin, buyer_address,
					store_id, gst_registration_id, customer_gstin, customer_state_code,
					supply_type, gross_amount, taxable_amount,
					cgst_total, sgst_total, igst_total, cess_total, tax_total,
					round_off, grand_total, price_includes_tax)
				VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21, $22, $23, $24, $25, $26)
				RETURNING id::text, invoice_no, total_amount::float8, discount_total::float8, created_at, invoice_date, financial_year`,
				invoiceNo, in.CustomerID, in.PaymentType, total, discountTotal,
				invoiceDate, fy,
				in.SaleType, in.BuyerName, in.BuyerGSTIN, in.BuyerAddress,
				sqlStr(in.StoreID), gstRegistrationID, sqlStr(customerGSTIN), sqlStr(customerStateCode),
				supplyType.String(),
				derefFloatPtr(invoiceResult, "GrossAmount"), derefFloatPtr(invoiceResult, "TaxableAmount"),
				derefFloatPtr(invoiceResult, "CGSTTotal"), derefFloatPtr(invoiceResult, "SGSTTotal"),
				derefFloatPtr(invoiceResult, "IGSTTotal"), derefFloatPtr(invoiceResult, "CessTotal"),
				derefFloatPtr(invoiceResult, "TaxTotal"),
				derefFloatPtr(invoiceResult, "RoundOff"), chargeableTotal, hasTaxConfig && priceIncludesTax,
			).Scan(&inv.ID, &inv.InvoiceNo, &inv.TotalAmount, &inv.DiscountTotal, &inv.CreatedAt, &inv.InvoiceDate, &inv.FinancialYear)
		} else {
			err = tx.QueryRow(ctx, `
				INSERT INTO sales_invoices (invoice_no, customer_id, payment_type, total_amount, discount_total,
					invoice_date, financial_year, created_at,
					sale_type, buyer_name, buyer_gstin, buyer_address,
					store_id, gst_registration_id, customer_gstin, customer_state_code,
					supply_type, gross_amount, taxable_amount,
					cgst_total, sgst_total, igst_total, cess_total, tax_total,
					round_off, grand_total, price_includes_tax)
				VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21, $22, $23, $24, $25, $26, $27)
				RETURNING id::text, invoice_no, total_amount::float8, discount_total::float8, created_at, invoice_date, financial_year`,
				invoiceNo, in.CustomerID, in.PaymentType, total, discountTotal,
				invoiceDate, fy, when,
				in.SaleType, in.BuyerName, in.BuyerGSTIN, in.BuyerAddress,
				sqlStr(in.StoreID), gstRegistrationID, sqlStr(customerGSTIN), sqlStr(customerStateCode),
				supplyType.String(),
				derefFloatPtr(invoiceResult, "GrossAmount"), derefFloatPtr(invoiceResult, "TaxableAmount"),
				derefFloatPtr(invoiceResult, "CGSTTotal"), derefFloatPtr(invoiceResult, "SGSTTotal"),
				derefFloatPtr(invoiceResult, "IGSTTotal"), derefFloatPtr(invoiceResult, "CessTotal"),
				derefFloatPtr(invoiceResult, "TaxTotal"),
				derefFloatPtr(invoiceResult, "RoundOff"), chargeableTotal, hasTaxConfig && priceIncludesTax,
			).Scan(&inv.ID, &inv.InvoiceNo, &inv.TotalAmount, &inv.DiscountTotal, &inv.CreatedAt, &inv.InvoiceDate, &inv.FinancialYear)
		}
		if err != nil {
			return err
		}
		inv.CustomerID = in.CustomerID
		inv.PaymentType = in.PaymentType
		inv.SaleType = in.SaleType
		inv.BuyerName = in.BuyerName
		inv.BuyerGSTIN = in.BuyerGSTIN
		inv.BuyerAddress = in.BuyerAddress
		inv.StoreID = in.StoreID
		inv.GSTRegistrationID = gstRegistrationID
		inv.CustomerGSTIN = customerGSTIN
		inv.CustomerStateCode = customerStateCode
		inv.SupplyType = strPtr(supplyType.String())
		if hasTaxConfig {
			inv.PriceIncludesTax = boolPtr(priceIncludesTax)
			if invoiceResult != nil {
				grossF, _ := invoiceResult.GrossAmount.Float64()
				taxableF, _ := invoiceResult.TaxableAmount.Float64()
				cgstF, _ := invoiceResult.CGSTTotal.Float64()
				sgstF, _ := invoiceResult.SGSTTotal.Float64()
				igstF, _ := invoiceResult.IGSTTotal.Float64()
				cessF, _ := invoiceResult.CessTotal.Float64()
				taxF, _ := invoiceResult.TaxTotal.Float64()
				inv.GrossAmount = &grossF
				inv.TaxableAmount = &taxableF
				inv.CGSTTotal = &cgstF
				inv.SGSTTotal = &sgstF
				inv.IGSTTotal = &igstF
				inv.CessTotal = &cessF
				inv.TaxTotal = &taxF
			}
			inv.GrandTotal = &chargeableTotal
		}

		for i := range lineItems {
			li := &lineItems[i]
			err := tx.QueryRow(ctx, `
				INSERT INTO sales_invoice_items
					(invoice_id, medicine_id, batch_id, quantity, unit_sale_price, subtotal,
					 discount_type, discount_value, discount_amount,
					 mrp, bonus_quantity,
					 hsn_code, gross_amount, taxable_value, gst_rate,
					 cgst_rate, cgst_amount, sgst_rate, sgst_amount,
					 igst_rate, igst_amount, cess_rate, cess_amount, line_total)
				VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9,
				        $10, $11,
				        $12, $13, $14, $15, $16, $17, $18, $19, $20, $21, $22, $23, $24)
				RETURNING id::text`,
				inv.ID, li.MedicineID, li.BatchID, li.Quantity, li.UnitSalePrice, li.Subtotal,
				li.DiscountType, li.DiscountValue, li.DiscountAmount,
				li.MRP, li.BonusQuantity,
				li.HSNCode, li.GrossAmount, li.TaxableValue, li.GSTRate,
				li.CGSTRate, li.CGSTAmount, li.SGSTRate, li.SGSTAmount,
				li.IGSTRate, li.IGSTAmount, li.CessRate, li.CessAmount, li.LineTotal,
			).Scan(&li.ID)
			if err != nil {
				return err
			}
			li.InvoiceID = inv.ID

			// Deduct quantity + bonus from stock
			totalDeduct := li.Quantity + li.BonusQuantity
			tag, err := tx.Exec(ctx, `
				UPDATE batches SET current_stock = current_stock - $2, updated_at = now()
				WHERE id = $1 AND current_stock >= $2`,
				li.BatchID, totalDeduct)
			if err != nil {
				return err
			}
			if tag.RowsAffected() == 0 {
				return &models.InsufficientStockError{BatchID: li.BatchID,
					RequestedQty: totalDeduct, AvailableStock: locked[li.BatchID].currentStock}
			}
		}

		if in.PaymentType == models.PaymentCredit {
			var newBalance float64
			err := tx.QueryRow(ctx, `
				UPDATE customers SET current_balance = current_balance + $2, updated_at = now()
				WHERE id = $1 RETURNING current_balance::float8`,
				*in.CustomerID, chargeableTotal).Scan(&newBalance)
			if err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `
				INSERT INTO customer_ledger (customer_id, entry_type, amount, balance_after, notes)
				VALUES ($1, 'CREDIT_SALE', $2, $3, $4)`,
				*in.CustomerID, chargeableTotal, round2(newBalance),
				fmt.Sprintf("Invoice %s", inv.InvoiceNo)); err != nil {
				return err
			}
		}

		result = &CheckoutResult{Invoice: inv, Items: lineItems}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

// sqlStr returns nil (SQL NULL) when the pointer is nil or the string is empty.
// This prevents pgx from sending "" to UUID/text columns.
func sqlStr(p *string) interface{} {
	if p == nil || *p == "" {
		return nil
	}
	return *p
}

func strPtr(s string) *string { return &s }
func boolPtr(b bool) *bool    { return &b }

// derefFloatPtr returns the float64 from a TaxInvoiceResult field, or nil.
func derefFloatPtr(r *tax.TaxInvoiceResult, field string) *float64 {
	if r == nil {
		return nil
	}
	var v float64
	switch field {
	case "GrossAmount":
		v, _ = r.GrossAmount.Float64()
	case "TaxableAmount":
		v, _ = r.TaxableAmount.Float64()
	case "CGSTTotal":
		v, _ = r.CGSTTotal.Float64()
	case "SGSTTotal":
		v, _ = r.SGSTTotal.Float64()
	case "IGSTTotal":
		v, _ = r.IGSTTotal.Float64()
	case "CessTotal":
		v, _ = r.CessTotal.Float64()
	case "TaxTotal":
		v, _ = r.TaxTotal.Float64()
	}
	return &v
}
