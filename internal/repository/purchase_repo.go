package repository

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"

	"github.com/mohi/pms-marg-inspired/internal/models"
	"github.com/mohi/pms-marg-inspired/internal/services"
	"github.com/mohi/pms-marg-inspired/internal/tax"
)

type PurchaseRepo struct {
	db      *pgxpool.Pool
	taxRepo *TaxRepo
}

func NewPurchaseRepo(db *pgxpool.Pool) *PurchaseRepo {
	return &PurchaseRepo{db: db, taxRepo: NewTaxRepo(db)}
}

type PurchaseItemInput struct {
	// MedicineID references an existing catalogue entry. Leave empty together
	// with MedicineName set to register a brand-new medicine on first inward.
	MedicineID      string      `json:"medicine_id"`
	MedicineName    string      `json:"medicine_name"`
	SaltComposition string      `json:"salt_composition"`
	Manufacturer    string      `json:"manufacturer"`
	Packing         string      `json:"packing"`
	MinReorderLevel int         `json:"min_reorder_level"`
	HSNCode         string      `json:"hsn_code"`
	PriceIncludesTax *bool      `json:"price_includes_tax,omitempty"`
	BatchNumber     string      `json:"batch_number"`
	ExpiryDate      models.Date `json:"expiry_date"`
	Quantity        int         `json:"quantity"`
	BonusQuantity   int         `json:"bonus_quantity"`
	PurchasePrice   float64     `json:"purchase_price"`
	SalePrice       float64     `json:"sale_price"`
	DiscountType    string      `json:"discount_type"`
	DiscountValue   float64     `json:"discount_value"`
}

type PurchaseInput struct {
	InvoiceNo     string  `json:"invoice_no"`
	InvoiceDate   *string `json:"invoice_date,omitempty"` // YYYY-MM-DD, defaults to today
	SupplierName  string  `json:"supplier_name"`
	SupplierID    *string `json:"supplier_id,omitempty"`
	SupplierGSTIN *string `json:"supplier_gstin,omitempty"`
	SupplierState *string `json:"supplier_state,omitempty"`
	// StoreID identifies the store the inward belongs to. Every request handler
	// overwrites it with the authenticated principal's store — a client-supplied
	// value is never trusted.
	StoreID       *string `json:"store_id,omitempty"`
	PlaceOfSupply *string `json:"place_of_supply,omitempty"` // state code of supplier
	// CreatedBy is the user who records the inward (owner on a direct entry,
	// the approving owner on an approved request). Handlers override it from the
	// principal; it feeds purchase_orders.created_by for the audit trail.
	CreatedBy   *string `json:"created_by,omitempty"`
	DiscountTotal float64 `json:"discount_total"`
	// ReverseCharge marks a supply where the pharmacy (not the supplier) is
	// liable to pay the tax (e.g. GTA, specified services).
	ReverseCharge bool `json:"reverse_charge"`
	// ITCEligible records whether the GST on this inward may be claimed as
	// input tax credit. It is an explicit decision, never an assumption.
	ITCEligible *bool `json:"itc_eligible,omitempty"`
	// ITCAmount overrides the computed ITC claim (nil = computed from tax).
	ITCAmount *float64 `json:"itc_amount,omitempty"`
	Items     []PurchaseItemInput `json:"items"`
}

func (in *PurchaseInput) validate() error {
	if in.SupplierName == "" {
		return errors.New("supplier_name is required")
	}
	if len(in.Items) == 0 {
		return errors.New("purchase requires at least one item")
	}
	for _, it := range in.Items {
		switch {
		case it.MedicineID == "" && it.MedicineName == "":
			return errors.New("item requires medicine_id or medicine_name")
		case it.BatchNumber == "":
			return errors.New("item batch_number is required")
		case it.Quantity <= 0:
			return errors.New("item quantity must be positive")
		case it.BonusQuantity < 0:
			return errors.New("item bonus_quantity must be non-negative")
		case it.PurchasePrice < 0 || it.SalePrice < 0:
			return errors.New("prices must be non-negative")
		case it.ExpiryDate.IsZero():
			return errors.New("item expiry_date is required (YYYY-MM-DD)")
		case it.MinReorderLevel < 0:
			return errors.New("min_reorder_level must be non-negative")
		case it.DiscountType != "" && it.DiscountType != "NONE" && it.DiscountType != "percent" && it.DiscountType != "amount":
			return errors.New("discount_type must be 'NONE', 'percent', or 'amount'")
		case it.DiscountValue < 0:
			return errors.New("discount_value must be non-negative")
		}
	}
	if in.DiscountTotal < 0 {
		return errors.New("discount_total must be non-negative")
	}
	return nil
}

// lineResult is the resolved per-item state used to build a purchase order: the
// discount arithmetic, the effective batch cost, and the GST line snapshot.
type lineResult struct {
	input            PurchaseItemInput
	discAmt          float64
	discType         string
	discVal          float64
	netPrice         float64
	taxLine          *tax.TaxLineResult
	hsnCode          string
	uqc              string
	priceIncludesTax bool
}

// CreateInward records a supplier invoice and upserts physical batches.
// A batch is identified by (medicine_id, batch_number): re-inwarding an existing
// batch number merges stock into that same physical batch, refreshing its prices
// and expiry. Items without a medicine_id but with a medicine_name register the
// new catalogue entry in the same transaction, so first-time inward immediately
// updates inventory. Everything runs in one transaction, including the creation
// of new medicines and their tax configs, so a failed inward never leaves
// orphaned rows behind.
func (r *PurchaseRepo) CreateInward(ctx context.Context, in *PurchaseInput) (*models.PurchaseOrder, []models.PurchaseOrderItem, error) {
	if err := in.validate(); err != nil {
		return nil, nil, err
	}
	if in.StoreID == nil || *in.StoreID == "" {
		return nil, nil, errors.New("store_id is required")
	}

	var (
		po    models.PurchaseOrder
		items []models.PurchaseOrderItem
	)
	err := pgx.BeginFunc(ctx, r.db, func(tx pgx.Tx) error {
		p, i, err := r.createInwardTx(ctx, tx, in)
		if err != nil {
			return err
		}
		po, items = *p, i
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	return &po, items, nil
}

// createInwardTx is the single code path that writes a purchase inward. Direct
// owner entries and owner-approval of a submitted purchase request both funnel
// through here (the latter inside the approval transaction), so "approve" is
// exactly "record the same inward the owner could have recorded directly".
func (r *PurchaseRepo) createInwardTx(ctx context.Context, tx pgx.Tx, in *PurchaseInput) (*models.PurchaseOrder, []models.PurchaseOrderItem, error) {
	storeID := *in.StoreID

	// The invoice date is the tax "as of" date: effective-dated tax lookup
	// must use it rather than time.Now() so a back-dated inward records the
	// tax that applied when the supplier invoiced.
	invoiceDate := time.Now().UTC()
	if in.InvoiceDate != nil && *in.InvoiceDate != "" {
		if parsed, err := time.Parse("2006-01-02", *in.InvoiceDate); err == nil {
			invoiceDate = parsed
		}
	}

	lineResults := make([]lineResult, 0, len(in.Items))
	var hasTaxConfig bool
	var lastPriceIncludesTax bool

	// Determine supply type from the store (recipient) vs supplier state. The
	// place of supply on an inward is the supplier's jurisdiction.
	supplyType := tax.SupplyTypeIntraState
	var storeStateCode, posStateCode string
	var gstRegID *string
	if store, err := r.taxRepo.GetStore(ctx, storeID); err == nil && store.GSTRegistrationID != nil {
		gstRegID = store.GSTRegistrationID
		if gr, err := r.taxRepo.GetGSTRegistration(ctx, *store.GSTRegistrationID); err == nil {
			storeStateCode = gr.StateCode
		}
	}
	if in.PlaceOfSupply != nil {
		posStateCode = *in.PlaceOfSupply
	}
	supplyType = tax.DetermineSupplyType(storeStateCode, posStateCode)

	totalD := decimal.Zero
	for i := range in.Items {
		it := in.Items[i]
		gross := float64(it.Quantity) * it.PurchasePrice
		discAmount, discType, discValue := lineDiscount(gross, &LineDiscount{Type: it.DiscountType, Value: it.DiscountValue})
		netD := tax.RoundMoney(decimal.NewFromFloat(gross).Sub(decimal.NewFromFloat(discAmount)))
		totalD = tax.RoundMoney(totalD.Add(netD))

		// Effective per-unit purchase price after discount (for batch storage).
		// Uses blended cost: total paid / total received (including bonus).
		// Inventory valuation only — GST tax base strictly uses the billed
		// PurchasePrice (see fillTaxLine), never this blended price.
		totalReceived := it.Quantity + it.BonusQuantity
		effectivePrice := it.PurchasePrice
		if totalReceived > 0 {
			if discAmount > 0 {
				effD := tax.RoundMoney(decimal.NewFromFloat(gross).Sub(decimal.NewFromFloat(discAmount)).Div(decimal.NewFromInt(int64(totalReceived))))
				effectivePrice, _ = effD.Float64()
			} else if it.BonusQuantity > 0 {
				effD := tax.RoundMoney(decimal.NewFromFloat(gross).Div(decimal.NewFromInt(int64(totalReceived))))
				effectivePrice, _ = effD.Float64()
			}
		}

		lr := lineResult{
			input:    it,
			discAmt:  discAmount,
			discType: discType,
			discVal:  discValue,
			netPrice: effectivePrice,
		}

		// Existing medicines resolve their effective tax config immediately.
		// New medicines (medicine_name only) are resolved inside the
		// transaction where the catalogue entry and its config are created,
		// and their line tax is recomputed there.
		if it.MedicineID != "" {
			if err := r.taxLineFromPool(ctx, storeID, invoiceDate, supplyType, &lr); err != nil {
				return nil, nil, err
			}
			if lr.taxLine != nil {
				hasTaxConfig = true
				lastPriceIncludesTax = lr.priceIncludesTax
			}
		}

		lineResults = append(lineResults, lr)
	}
	totalD = tax.RoundMoney(totalD.Sub(decimal.NewFromFloat(in.DiscountTotal)))
	total, _ := totalD.Float64()

	var (
		po    models.PurchaseOrder
		items = make([]models.PurchaseOrderItem, 0, len(in.Items))
	)

	// Resolve catalogue references first: known IDs are verified (within the
	// store), blank IDs with a name register the medicine right here so the
	// very first inward both creates the catalogue entry and stocks it.
	resolved := make([]lineResult, len(lineResults))
	copy(resolved, lineResults)
	createdMeds := make(map[string]string)
	for i := range resolved {
		lr := &resolved[i]
		it := &lr.input
		if it.MedicineID != "" {
			var exists bool
			var medUQC *string
			if err := tx.QueryRow(ctx,
				`SELECT EXISTS(SELECT 1 FROM medicines WHERE id = $1 AND store_id = $2 AND deleted_at IS NULL)`,
				it.MedicineID, storeID).Scan(&exists); err != nil {
				return nil, nil, err
			}
			if !exists {
				return nil, nil, fmt.Errorf("medicine %s not found", it.MedicineID)
			}
			// Snapshot the medicine's UQC for the purchase line item.
			if err := tx.QueryRow(ctx,
				`SELECT uqc FROM medicines WHERE id = $1 AND store_id = $2`,
				it.MedicineID, storeID).Scan(&medUQC); err == nil && medUQC != nil && *medUQC != "" {
				lr.uqc = *medUQC
			} else if lr.uqc == "" {
				lr.uqc = "OTH"
			}
			continue
		}
		key := strings.ToLower(strings.TrimSpace(it.MedicineName))
		if existingID, ok := createdMeds[key]; ok {
			it.MedicineID = existingID
			var reuseUQC string
			if err := tx.QueryRow(ctx, `SELECT COALESCE(uqc,'OTH') FROM medicines WHERE id = $1`, existingID).Scan(&reuseUQC); err == nil && reuseUQC != "" {
				lr.uqc = reuseUQC
			} else if lr.uqc == "" {
				lr.uqc = "OTH"
			}
			if err := r.taxLineFromTx(ctx, tx, storeID, invoiceDate, supplyType, lr); err != nil {
				return nil, nil, err
			}
			if lr.taxLine != nil {
				hasTaxConfig = true
				lastPriceIncludesTax = lr.priceIncludesTax
			}
			continue
		}
		var newID string
		if err := tx.QueryRow(ctx, `
			INSERT INTO medicines (name, salt_composition, manufacturer, min_reorder_level, packing, store_id)
			VALUES ($1, $2, $3, $4, $5, $6)
			RETURNING id::text`,
			it.MedicineName, it.SaltComposition, it.Manufacturer,
			it.MinReorderLevel, it.Packing, storeID,
		).Scan(&newID); err != nil {
			return nil, nil, err
		}
		createdMeds[key] = newID
		it.MedicineID = newID
		// Snapshot UQC for the newly registered medicine (defaults to NOS).
		var newUQC string
		if err := tx.QueryRow(ctx, `SELECT COALESCE(uqc,'OTH') FROM medicines WHERE id = $1`, newID).Scan(&newUQC); err == nil && newUQC != "" {
			lr.uqc = newUQC
		} else if lr.uqc == "" {
			lr.uqc = "OTH"
		}

		// If an HSN code is provided, classify the new medicine and link
		// it to the HSN's active tax rate. All of this happens on the same
		// transaction so a rolled-back inward never leaves orphaned
		// medicine_tax_config or hsn_codes rows behind.
		if it.HSNCode == "" {
			continue
		}
		hsnID, err := hsnIDForTx(ctx, tx, storeID, it.HSNCode)
		if err != nil {
			return nil, nil, err
		}
		taxRateID, err := activeTaxRateIDForTx(ctx, tx, storeID, hsnID)
		if err != nil {
			return nil, nil, err
		}
		if taxRateID == "" {
			continue // HSN not rated yet → left unclassified until rated
		}
		priceIncl := false
		if it.PriceIncludesTax != nil {
			priceIncl = *it.PriceIncludesTax
		}
		if err := insertMedicineTaxConfigForTx(ctx, tx, storeID, newID, hsnID, taxRateID, priceIncl, invoiceDate); err != nil {
			return nil, nil, err
		}
		// Recompute the line tax with the freshly created config so a
		// first-time medicine is taxed on its own inward.
		if err := r.taxLineFromTx(ctx, tx, storeID, invoiceDate, supplyType, lr); err != nil {
			return nil, nil, err
		}
		if lr.taxLine != nil {
			hasTaxConfig = true
			lastPriceIncludesTax = lr.priceIncludesTax
		}
	}

	invoiceNo := in.InvoiceNo
	if invoiceNo == "" {
		invoiceNo = fmt.Sprintf("PINV-%s", time.Now().UTC().Format("20060102-150405"))
	}
	fy := services.FinancialYear(invoiceDate)

	// Compute invoice-level GST totals from the (now fully resolved) lines.
	lineResults = resolved
	var taxLines []tax.TaxLineResult
	for _, lr := range lineResults {
		if lr.taxLine != nil {
			taxLines = append(taxLines, *lr.taxLine)
		}
	}
	var invoiceResult *tax.TaxInvoiceResult
	if len(taxLines) > 0 {
		r := tax.CalculateInvoiceTax(taxLines, supplyType)
		invoiceResult = &r
	}
	chargeableTotal := total
	if invoiceResult != nil {
		grandTotal, _ := invoiceResult.GrandTotal.Float64()
		chargeableTotal = math.Max(0, grandTotal-in.DiscountTotal)
	}

	itcEligible := true
	if in.ITCEligible != nil {
		itcEligible = *in.ITCEligible
	}
	itcAmount := 0.0
	if in.ITCAmount != nil {
		itcAmount = *in.ITCAmount
	} else if itcEligible && invoiceResult != nil {
		itcAmount, _ = invoiceResult.TaxTotal.Float64()
	}

	pos := ""
	if in.PlaceOfSupply != nil {
		pos = *in.PlaceOfSupply
	}

	err := tx.QueryRow(ctx, `
		INSERT INTO purchase_orders (invoice_no, supplier_name, total_amount, discount_total,
			invoice_date, financial_year,
			supplier_id, supplier_gstin, supplier_state_code, store_id, gst_registration_id,
			supply_type, place_of_supply, reverse_charge, itc_eligible, itc_amount,
			gross_amount, taxable_amount,
			cgst_total, sgst_total, igst_total, cess_total, tax_total,
			grand_total, price_includes_tax, created_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11,
		        $12, $13, $14, $15, $16, $17, $18, $19, $20, $21, $22, $23, $24, $25, $26)
		RETURNING id::text, invoice_no, total_amount::float8, discount_total::float8, created_at, invoice_date, financial_year`,
		invoiceNo, in.SupplierName, total, in.DiscountTotal,
		invoiceDate, fy,
		sqlStr(in.SupplierID), sqlStr(in.SupplierGSTIN), sqlStr(in.SupplierState),
		storeID, sqlStr(gstRegID),
		supplyType.String(), pos, in.ReverseCharge, itcEligible, itcAmount,
		derefFloatPtr(invoiceResult, "GrossAmount"), derefFloatPtr(invoiceResult, "TaxableAmount"),
		derefFloatPtr(invoiceResult, "CGSTTotal"), derefFloatPtr(invoiceResult, "SGSTTotal"),
		derefFloatPtr(invoiceResult, "IGSTTotal"), derefFloatPtr(invoiceResult, "CessTotal"),
		derefFloatPtr(invoiceResult, "TaxTotal"),
		chargeableTotal, hasTaxConfig && lastPriceIncludesTax,
		nullableString(in.CreatedBy),
	).Scan(&po.ID, &po.InvoiceNo, &po.TotalAmount, &po.DiscountTotal, &po.CreatedAt, &po.InvoiceDate, &po.FinancialYear)
	if err != nil {
		return nil, nil, err
	}
	po.SupplierName = in.SupplierName
	po.ReverseCharge = in.ReverseCharge
	po.ITCEligible = itcEligible
	if invoiceResult != nil {
		gf, gfOK := invoiceResult.GrossAmount.Float64()
		tf, tfOK := invoiceResult.TaxableAmount.Float64()
		cg, cgOK := invoiceResult.CGSTTotal.Float64()
		sg, sgOK := invoiceResult.SGSTTotal.Float64()
		ig, igOK := invoiceResult.IGSTTotal.Float64()
		ce, ceOK := invoiceResult.CessTotal.Float64()
		tax, taxOK := invoiceResult.TaxTotal.Float64()
		_, gtOK := invoiceResult.GrandTotal.Float64()
		if gfOK {
			po.GrossAmount = &gf
		}
		if tfOK {
			po.TaxableAmount = &tf
		}
		if cgOK {
			po.CGSTTotal = &cg
		}
		if sgOK {
			po.SGSTTotal = &sg
		}
		if igOK {
			po.IGSTTotal = &ig
		}
		if ceOK {
			po.CessTotal = &ce
		}
		if taxOK {
			po.TaxTotal = &tax
		}
		if gtOK {
			ct := chargeableTotal
			po.GrandTotal = &ct
		}
		itc := itcAmount
		po.ITCAmount = &itc
	}
	if pos != "" {
		po.PlaceOfSupply = &pos
	}

	for _, lr := range resolved {
		it := lr.input
		totalStock := it.Quantity + it.BonusQuantity

		batchID := ""
		err := tx.QueryRow(ctx, `
			INSERT INTO batches (medicine_id, batch_number, expiry_date, purchase_price, sale_price, current_stock, store_id)
			VALUES ($1, $2, $3, $4, $5, $6, $7)
			ON CONFLICT (medicine_id, batch_number) DO UPDATE SET
				expiry_date    = EXCLUDED.expiry_date,
				purchase_price = EXCLUDED.purchase_price,
				sale_price     = EXCLUDED.sale_price,
				current_stock  = batches.current_stock + EXCLUDED.current_stock,
				updated_at     = now()
			RETURNING id::text`,
			it.MedicineID, it.BatchNumber, it.ExpiryDate.Time,
			lr.netPrice, it.SalePrice, totalStock, storeID,
		).Scan(&batchID)
		if err != nil {
			return nil, nil, err
		}

		var itemID string
		uqc := lr.uqc
		if uqc == "" {
			uqc = "OTH"
		}
		if err := tx.QueryRow(ctx, `
			INSERT INTO purchase_order_items
				(purchase_id, medicine_id, batch_number, expiry_date, quantity, bonus_quantity,
				 purchase_price, sale_price, discount_type, discount_value, discount_amount,
				 hsn_code, uqc, gross_amount, taxable_value, gst_rate,
				 cgst_rate, cgst_amount, sgst_rate, sgst_amount,
				 igst_rate, igst_amount, cess_rate, cess_amount, line_total)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11,
			        $12, $13, $14, $15, $16, $17, $18, $19, $20, $21, $22, $23, $24, $25)
			RETURNING id::text`,
			po.ID, it.MedicineID, it.BatchNumber, it.ExpiryDate.Time,
			it.Quantity, it.BonusQuantity,
			it.PurchasePrice, it.SalePrice,
			lr.discType, lr.discVal, lr.discAmt,
			sqlStr(&lr.hsnCode), uqc, derefFloatPtrTax(lr.taxLine, "GrossAmount"),
			derefFloatPtrTax(lr.taxLine, "TaxableValue"), derefFloatPtrTax(lr.taxLine, "GSTRate"),
			derefFloatPtrTax(lr.taxLine, "CGSTRate"), derefFloatPtrTax(lr.taxLine, "CGSTAmount"),
			derefFloatPtrTax(lr.taxLine, "SGSTRate"), derefFloatPtrTax(lr.taxLine, "SGSTAmount"),
			derefFloatPtrTax(lr.taxLine, "IGSTRate"), derefFloatPtrTax(lr.taxLine, "IGSTAmount"),
			derefFloatPtrTax(lr.taxLine, "CessRate"), derefFloatPtrTax(lr.taxLine, "CessAmount"),
			derefFloatPtrTax(lr.taxLine, "LineTotal"),
		).Scan(&itemID); err != nil {
			return nil, nil, err
		}

		li := models.PurchaseOrderItem{
			ID:             itemID,
			PurchaseID:     po.ID,
			MedicineID:     it.MedicineID,
			BatchNumber:    it.BatchNumber,
			ExpiryDate:     it.ExpiryDate,
			Quantity:       it.Quantity,
			BonusQuantity:  it.BonusQuantity,
			PurchasePrice:  it.PurchasePrice,
			SalePrice:      it.SalePrice,
			DiscountType:   lr.discType,
			DiscountValue:  lr.discVal,
			DiscountAmount: lr.discAmt,
			UQC:            uqc,
		}
		if lr.taxLine != nil {
			grossF, _ := lr.taxLine.GrossAmount.Float64()
			taxableF, _ := lr.taxLine.TaxableValue.Float64()
			gstRateF, _ := lr.taxLine.CGSTRate.Add(lr.taxLine.SGSTRate).Add(lr.taxLine.IGSTRate).Float64()
			cgstRateF, _ := lr.taxLine.CGSTRate.Float64()
			cgstAmtF, _ := lr.taxLine.CGSTAmount.Float64()
			sgstRateF, _ := lr.taxLine.SGSTRate.Float64()
			sgstAmtF, _ := lr.taxLine.SGSTAmount.Float64()
			igstRateF, _ := lr.taxLine.IGSTRate.Float64()
			igstAmtF, _ := lr.taxLine.IGSTAmount.Float64()
			cessRateF, _ := lr.taxLine.CessRate.Float64()
			cessAmtF, _ := lr.taxLine.CessAmount.Float64()
			lineTotalF, _ := lr.taxLine.LineTotal.Float64()

			li.HSNCode = &lr.hsnCode
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
		}

		items = append(items, li)
	}

	return &po, items, nil
}

// taxLineFromPool resolves the effective tax config (as of asOf) for the line's
// medicine via the pool and fills in the line tax. A medicine without a config
// is a legacy/unclassified item and is recorded without tax.
func (r *PurchaseRepo) taxLineFromPool(ctx context.Context, storeID string, asOf time.Time, supplyType tax.SupplyType, lr *lineResult) error {
	cfg, err := r.taxRepo.GetMedicineTaxConfig(ctx, storeID, lr.input.MedicineID, asOf)
	if err != nil {
		return fmt.Errorf("lookup tax config for medicine %s: %w", lr.input.MedicineID, err)
	}
	if cfg == nil || cfg.TaxRate == nil {
		return nil
	}
	fillTaxLine(lr, cfg, supplyType)
	return nil
}

// taxLineFromTx is the transaction-scoped twin of taxLineFromPool, used right
// after a new medicine and its config are created inside the running tx.
func (r *PurchaseRepo) taxLineFromTx(ctx context.Context, tx pgx.Tx, storeID string, asOf time.Time, supplyType tax.SupplyType, lr *lineResult) error {
	cfg, err := taxConfigFromTx(ctx, tx, storeID, lr.input.MedicineID, asOf)
	if err != nil {
		return fmt.Errorf("lookup tax config for medicine %s: %w", lr.input.MedicineID, err)
	}
	if cfg == nil {
		return nil
	}
	fillTaxLine(lr, cfg, supplyType)
	return nil
}

func fillTaxLine(lr *lineResult, cfg *models.MedicineTaxConfig, supplyType tax.SupplyType) {
	lr.hsnCode = cfg.HSNCode
	lr.priceIncludesTax = cfg.PriceIncludesTax
	// GST valuation: tax applies to the transaction value of billed goods
	// only (quantity x pre-bonus PurchasePrice less discount). The blended
	// effectivePrice (incl. bonus) is for inventory valuation and MUST NOT
	// enter the tax engine.
	taxInput := tax.TaxInput{
		Quantity:       decimal.NewFromInt(int64(lr.input.Quantity)),
		UnitPrice:      decimal.NewFromFloat(lr.input.PurchasePrice),
		DiscountAmount: decimal.NewFromFloat(lr.discAmt),
		TaxRate: tax.TaxRate{
			GSTRate:  decimal.NewFromFloat(cfg.TaxRate.GSTRate),
			CGSTRate: decimal.NewFromFloat(cfg.TaxRate.CGSTRate),
			SGSTRate: decimal.NewFromFloat(cfg.TaxRate.SGSTRate),
			IGSTRate: decimal.NewFromFloat(cfg.TaxRate.IGSTRate),
			CessRate: decimal.NewFromFloat(cfg.TaxRate.CessRate),
		},
		PriceIncludesTax: cfg.PriceIncludesTax,
		SupplyType:       supplyType,
		HSNCode:          cfg.HSNCode,
	}
	tr := tax.CalculateLineTax(taxInput)
	lr.taxLine = &tr
}

// taxConfigFromTx reads the active medicine tax config via the running tx,
// mirroring TaxRepo.GetMedicineTaxConfig.
func taxConfigFromTx(ctx context.Context, tx pgx.Tx, storeID, medicineID string, asOf time.Time) (*models.MedicineTaxConfig, error) {
	var cfg models.MedicineTaxConfig
	var taxRate models.TaxRate
	var effectiveTo *time.Time

	err := tx.QueryRow(ctx, `
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
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	cfg.EffectiveTo = effectiveTo
	cfg.TaxRate = &taxRate
	return &cfg, nil
}

// hsnIDForTx returns the HSN code's ID, creating the code row if needed (scoped
// to the given store), all within the running transaction.
func hsnIDForTx(ctx context.Context, tx pgx.Tx, storeID, code string) (string, error) {
	var id string
	err := tx.QueryRow(ctx, `SELECT id::text FROM hsn_codes WHERE code = $1 AND store_id = $2`, code, storeID).Scan(&id)
	if err == nil {
		return id, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return "", err
	}
	err = tx.QueryRow(ctx,
		`INSERT INTO hsn_codes (store_id, code) VALUES ($1, $2) RETURNING id::text`, storeID, code).Scan(&id)
	return id, err
}

// activeTaxRateIDForTx returns the active tax rate ID for the HSN, or "" if the
// HSN has no active rate yet.
func activeTaxRateIDForTx(ctx context.Context, tx pgx.Tx, storeID, hsnCodeID string) (string, error) {
	var id string
	err := tx.QueryRow(ctx, `
		SELECT id::text FROM tax_rates
		WHERE hsn_code_id = $1 AND store_id = $2 AND effective_to IS NULL
		ORDER BY effective_from DESC LIMIT 1`, hsnCodeID, storeID).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	return id, err
}

// insertMedicineTaxConfigForTx links a medicine to an HSN + tax rate on the
// running transaction, effective from the invoice date.
func insertMedicineTaxConfigForTx(ctx context.Context, tx pgx.Tx, storeID, medicineID, hsnCodeID, taxRateID string, priceIncludesTax bool, effectiveFrom time.Time) error {
	if _, err := tx.Exec(ctx,
		`UPDATE medicine_tax_config SET effective_to = $2::date + 1
		 WHERE medicine_id = $1 AND store_id = $3 AND effective_to IS NULL`, medicineID, effectiveFrom, storeID); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `
		INSERT INTO medicine_tax_config (store_id, medicine_id, hsn_code_id, tax_rate_id, price_includes_tax, effective_from)
		VALUES ($1, $2, $3, $4, $5, $6)`,
		storeID, medicineID, hsnCodeID, taxRateID, priceIncludesTax, effectiveFrom)
	return err
}

// derefFloatPtrTax returns a float64 pointer from a TaxLineResult field, or nil.
func derefFloatPtrTax(tr *tax.TaxLineResult, field string) *float64 {
	if tr == nil {
		return nil
	}
	var v float64
	switch field {
	case "GrossAmount":
		v, _ = tr.GrossAmount.Float64()
	case "TaxableValue":
		v, _ = tr.TaxableValue.Float64()
	case "GSTRate":
		v, _ = tr.CGSTRate.Add(tr.SGSTRate).Add(tr.IGSTRate).Float64()
	case "CGSTRate":
		v, _ = tr.CGSTRate.Float64()
	case "CGSTAmount":
		v, _ = tr.CGSTAmount.Float64()
	case "SGSTRate":
		v, _ = tr.SGSTRate.Float64()
	case "SGSTAmount":
		v, _ = tr.SGSTAmount.Float64()
	case "IGSTRate":
		v, _ = tr.IGSTRate.Float64()
	case "IGSTAmount":
		v, _ = tr.IGSTAmount.Float64()
	case "CessRate":
		v, _ = tr.CessRate.Float64()
	case "CessAmount":
		v, _ = tr.CessAmount.Float64()
	case "LineTotal":
		v, _ = tr.LineTotal.Float64()
	}
	return &v
}
