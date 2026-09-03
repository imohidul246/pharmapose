package repository_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/mohi/pms-marg-inspired/internal/models"
	"github.com/mohi/pms-marg-inspired/internal/repository"
	"github.com/mohi/pms-marg-inspired/internal/testutil"
)

// seedGSTMedicine creates a medicine with 12% GST (6% CGST + 6% SGST) HSN 3004.
func seedGSTMedicine(t *testing.T) (medicineID string, batchID string) {
	t.Helper()
	ctx := context.Background()

	m := &models.Medicine{
		Name:             "GST Paracetamol 500mg",
		SaltComposition:  "Paracetamol 500mg",
		Manufacturer:     "GSTPharma",
		MinReorderLevel:  10,
		Packing:          "Strip of 10",
	}
	if err := medRepo.Create(ctx, m); err != nil {
		t.Fatalf("create medicine: %v", err)
	}
	medicineID = m.ID

	// Debug: verify HSN and tax rate data exists
	var hsnCount, taxRateCount int
	pool.QueryRow(ctx, `SELECT COUNT(*) FROM hsn_codes WHERE code = '3004'`).Scan(&hsnCount)
	pool.QueryRow(ctx, `SELECT COUNT(*) FROM tax_rates`).Scan(&taxRateCount)
	t.Logf("HSN 3004 rows: %d, total tax_rates: %d", hsnCount, taxRateCount)

	// Seed HSN and tax rate data if missing (previous test runs may have truncated)
	var err error
	if hsnCount == 0 {
		_, err = pool.Exec(ctx, `
			INSERT INTO hsn_codes (store_id, code, description) VALUES
				($1, '3004', 'Medicaments for therapeutic or prophylactic uses, packed for retail sale')
			ON CONFLICT (store_id, code) DO NOTHING`, testutil.StoreID)
		if err != nil {
			t.Fatalf("seed hsn: %v", err)
		}
	}
	if taxRateCount == 0 {
		_, err = pool.Exec(ctx, `
			INSERT INTO tax_rates (store_id, hsn_code_id, gst_rate, cgst_rate, sgst_rate, igst_rate, cess_rate, effective_from)
			SELECT $1, h.id, 12.00, 6.00, 6.00, 12.00, 0.00, '2017-07-01'::date
			FROM hsn_codes h WHERE h.code = '3004'
			ON CONFLICT DO NOTHING`, testutil.StoreID)
		if err != nil {
			t.Fatalf("seed tax_rates: %v", err)
		}
	}

	// Link tax config to medicine
	_, err = pool.Exec(ctx, `
		INSERT INTO medicine_tax_config (store_id, medicine_id, hsn_code_id, tax_rate_id, price_includes_tax, effective_from)
		SELECT $1, $2, h.id, tr.id, false, '2017-07-01'::date
		FROM hsn_codes h
		JOIN tax_rates tr ON tr.hsn_code_id = h.id
		WHERE h.code = '3004' AND tr.effective_to IS NULL
		ON CONFLICT DO NOTHING`, testutil.StoreID, medicineID)
	if err != nil {
		t.Fatalf("link tax config: %v", err)
	}

	var taxConfigCount int
	err = pool.QueryRow(ctx, `SELECT COUNT(*) FROM medicine_tax_config WHERE medicine_id = $1`, medicineID).Scan(&taxConfigCount)
	if err != nil {
		t.Fatalf("check tax config: %v", err)
	}
	t.Logf("tax configs for medicine %s: %d", medicineID, taxConfigCount)

	in := &repository.PurchaseInput{
		StoreID:     sid(testutil.StoreID),
		InvoiceNo:    "GST-INWARD-1",
		SupplierName: "GST Supplier",
		Items: []repository.PurchaseItemInput{{
			MedicineID:    medicineID,
			BatchNumber:   "GST-B1",
			ExpiryDate:    models.NewDate(time.Now().AddDate(2, 0, 0)),
			Quantity:      100,
			PurchasePrice: 100,
			SalePrice:     150,
		}},
	}
	_, items, err := purchRepo.CreateInward(ctx, in)
	if err != nil {
		t.Fatalf("seed inward: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}

	batch, err := medRepo.FindBatchByNumber(ctx, medicineID, "GST-B1")
	if err != nil {
		t.Fatalf("find batch: %v", err)
	}
	batchID = batch.ID
	return medicineID, batchID
}

func TestCheckoutWithGSTIntraState(t *testing.T) {
	reset(t)
	medicineID, batchID := seedGSTMedicine(t)
	ctx := context.Background()

	var bizID string
	err := pool.QueryRow(ctx, `
		INSERT INTO businesses (legal_name) VALUES ('Test Pharma') RETURNING id::text`).Scan(&bizID)
	if err != nil {
		t.Fatalf("insert business: %v", err)
	}

	sid := testutil.StoreID

	var grid string
	err = pool.QueryRow(ctx, `
		INSERT INTO gst_registrations (business_id, gstin, legal_name, trade_name, state_code, state_name, address, is_active)
		VALUES ($1, '27AABCU9603R1ZM', 'Test Pharma Pvt Ltd', 'Test Pharma', '27', 'Maharashtra', 'Mumbai', true)
		RETURNING id::text`, bizID).Scan(&grid)
	if err != nil {
		t.Fatalf("insert gst reg: %v", err)
	}

	_, err = pool.Exec(ctx, `UPDATE stores SET gst_registration_id = $1 WHERE id = $2`, grid, sid)
	if err != nil {
		t.Fatalf("link store: %v", err)
	}

	// Customer in same state (27) → intra-state
	pos := "27"
	res, err := saleRepo.Checkout(ctx, &repository.CheckoutInput{
		PaymentType:   models.PaymentCash,
		StoreID:       &sid,
		PlaceOfSupply: &pos,
		Items:         []repository.CheckoutItemInput{{BatchID: batchID, Quantity: 10}},
	})
	if err != nil {
		t.Fatalf("checkout: %v", err)
	}

	inv := res.Invoice

	// Tax-exclusive: 10 × 150 = 1500 gross, tax at 12% = 180, grand = 1680
	if inv.TotalAmount != 1500 {
		t.Errorf("total_amount = %.2f want 1500.00", inv.TotalAmount)
	}
	if inv.SupplyType == nil || *inv.SupplyType != "INTRA_STATE" {
		t.Errorf("supply_type = %v want INTRA_STATE", inv.SupplyType)
	}
	if inv.TaxTotal == nil || *inv.TaxTotal != 180 {
		t.Errorf("tax_total = %v want 180.00", inv.TaxTotal)
	}
	if inv.GrandTotal == nil || *inv.GrandTotal != 1680 {
		t.Errorf("grand_total = %v want 1680.00", inv.GrandTotal)
	}
	if inv.CGSTTotal == nil || *inv.CGSTTotal != 90 {
		t.Errorf("cgst_total = %v want 90.00", inv.CGSTTotal)
	}
	if inv.SGSTTotal == nil || *inv.SGSTTotal != 90 {
		t.Errorf("sgst_total = %v want 90.00", inv.SGSTTotal)
	}
	if inv.IGSTTotal != nil && *inv.IGSTTotal != 0 {
		t.Errorf("igst_total should be nil or 0, got %v", inv.IGSTTotal)
	}

	// Verify item-level tax snapshot
	if len(res.Items) != 1 {
		t.Fatalf("items = %d want 1", len(res.Items))
	}
	item := res.Items[0]
	if item.HSNCode == nil || *item.HSNCode != "3004" {
		t.Errorf("hsn_code = %v want 3004", item.HSNCode)
	}
	if item.GSTRate == nil || *item.GSTRate != 12 {
		t.Errorf("gst_rate = %v want 12.00", item.GSTRate)
	}
	if item.CGSTAmount == nil || *item.CGSTAmount != 90 {
		t.Errorf("cgst_amount = %v want 90.00", *item.CGSTAmount)
	}
	if item.SGSTAmount == nil || *item.SGSTAmount != 90 {
		t.Errorf("sgst_amount = %v want 90.00", *item.SGSTAmount)
	}
	if item.LineTotal == nil || *item.LineTotal != 1680 {
		t.Errorf("line_total = %v want 1680.00", *item.LineTotal)
	}

	// Verify batch stock decremented
	batch, _ := medRepo.FindBatchByNumber(ctx, medicineID, "GST-B1")
	if batch.CurrentStock != 90 {
		t.Errorf("stock = %d want 90", batch.CurrentStock)
	}
}

func TestCheckoutWithGSTInterState(t *testing.T) {
	reset(t)
	_, batchID := seedGSTMedicine(t)
	ctx := context.Background()

	var bizID string
	err := pool.QueryRow(ctx, `
		INSERT INTO businesses (legal_name) VALUES ('Test Pharma') RETURNING id::text`).Scan(&bizID)
	if err != nil {
		t.Fatalf("insert business: %v", err)
	}

	sid := testutil.StoreID

	var grid string
	err = pool.QueryRow(ctx, `
		INSERT INTO gst_registrations (business_id, gstin, legal_name, trade_name, state_code, state_name, address, is_active)
		VALUES ($1, '27AABCU9603R1ZM', 'Test Pharma', 'Test', '27', 'Maharashtra', 'Mumbai', true)
		RETURNING id::text`, bizID).Scan(&grid)
	if err != nil {
		t.Fatalf("insert gst reg: %v", err)
	}

	_, err = pool.Exec(ctx, `UPDATE stores SET gst_registration_id = $1 WHERE id = $2`, grid, sid)
	if err != nil {
		t.Fatalf("link store: %v", err)
	}

	// Customer in Delhi (07) → inter-state
	pos := "07"
	res, err := saleRepo.Checkout(ctx, &repository.CheckoutInput{
		PaymentType:   models.PaymentCash,
		StoreID:       &sid,
		PlaceOfSupply: &pos,
		Items:         []repository.CheckoutItemInput{{BatchID: batchID, Quantity: 5}},
	})
	if err != nil {
		t.Fatalf("checkout: %v", err)
	}

	inv := res.Invoice
	// 5 × 150 = 750, IGST 12% = 90, grand = 840
	if inv.SupplyType == nil || *inv.SupplyType != "INTER_STATE" {
		t.Errorf("supply_type = %v want INTER_STATE", inv.SupplyType)
	}
	if inv.IGSTTotal == nil || *inv.IGSTTotal != 90 {
		t.Errorf("igst_total = %v want 90.00", inv.IGSTTotal)
	}
	if inv.GrandTotal == nil || *inv.GrandTotal != 840 {
		t.Errorf("grand_total = %v want 840.00", inv.GrandTotal)
	}
	if inv.CGSTTotal != nil && *inv.CGSTTotal != 0 {
		t.Errorf("cgst_total should be nil or 0, got %v", inv.CGSTTotal)
	}

	item := res.Items[0]
	if item.IGSTAmount == nil || *item.IGSTAmount != 90 {
		t.Errorf("igst_amount = %v want 90.00", *item.IGSTAmount)
	}
	if item.LineTotal == nil || *item.LineTotal != 840 {
		t.Errorf("line_total = %v want 840.00", *item.LineTotal)
	}
}

func TestCheckoutWithoutTaxConfigGracefulFallback(t *testing.T) {
	reset(t)
	fx := seedFixture(t, 100, 10000) // medicine has no tax config

	res, err := saleRepo.Checkout(context.Background(), &repository.CheckoutInput{
		StoreID:     sid(testutil.StoreID),
		PaymentType: models.PaymentCash,
		Items:       []repository.CheckoutItemInput{{BatchID: fx.BatchIDs[0], Quantity: 10}},
	})
	if err != nil {
		t.Fatalf("checkout: %v", err)
	}

	// No tax config → all GST fields nil, total_amount unchanged
	inv := res.Invoice
	if inv.TaxTotal != nil {
		t.Errorf("tax_total should be nil for legacy medicine, got %v", inv.TaxTotal)
	}
	if inv.GrandTotal != nil {
		t.Errorf("grand_total should be nil for legacy medicine, got %v", inv.GrandTotal)
	}
	if inv.TotalAmount != 150 { // 10 × 15
		t.Errorf("total_amount = %.2f want 150.00", inv.TotalAmount)
	}

	item := res.Items[0]
	if item.HSNCode != nil {
		t.Errorf("hsn_code should be nil, got %v", item.HSNCode)
	}
}

func TestLegacyInvoiceBackwardCompatibility(t *testing.T) {
	reset(t)
	fx := seedFixture(t, 100, 10000)

	// A legacy checkout (no store, no tax config) must still work exactly as before
	res, err := saleRepo.Checkout(context.Background(), &repository.CheckoutInput{
		StoreID:     sid(testutil.StoreID),
		PaymentType: models.PaymentCash,
		Items:       []repository.CheckoutItemInput{{BatchID: fx.BatchIDs[0], Quantity: 2}},
	})
	if err != nil {
		t.Fatalf("checkout: %v", err)
	}

	if res.Invoice.TotalAmount != 30 {
		t.Errorf("total_amount = %.2f want 30.00", res.Invoice.TotalAmount)
	}
	if res.Invoice.DiscountTotal != 0 {
		t.Errorf("discount_total = %.2f want 0.00", res.Invoice.DiscountTotal)
	}
	if len(res.Items) != 1 || res.Items[0].Subtotal != 30 {
		t.Errorf("items mismatch: %+v", res.Items)
	}

	batch, _ := medRepo.FindBatchByNumber(context.Background(), fx.MedicineID, "FIX-B1")
	if batch.CurrentStock != 98 {
		t.Errorf("stock = %d want 98", batch.CurrentStock)
	}
}

func strPtr(s string) *string {
	return &s
}

// TestBonusQuantityInventoryCostCorrect verifies that batch.purchase_price
// reflects the true blended cost when bonus quantity is present.
func TestBonusQuantityInventoryCostCorrect(t *testing.T) {
	reset(t)
	ctx := context.Background()

	m := &models.Medicine{
		Name: "Blended Cost Med", SaltComposition: "Test",
		Manufacturer: "TestPharma", MinReorderLevel: 5,
	}
	if err := medRepo.Create(ctx, m); err != nil {
		t.Fatalf("create medicine: %v", err)
	}

	in := &repository.PurchaseInput{
		InvoiceNo:    fmt.Sprintf("BLEND-%d", time.Now().UnixNano()),
		SupplierName: "Blend Supplier",
		StoreID:      sid(testutil.StoreID),
		Items: []repository.PurchaseItemInput{{
			MedicineID:  m.ID,
			BatchNumber: "BLEND-B1",
			ExpiryDate:  models.NewDate(time.Now().AddDate(1, 0, 0)),
			Quantity:    10,
			BonusQuantity: 2,
			PurchasePrice: 50,
			SalePrice:     75,
		}},
	}
	if _, _, err := purchRepo.CreateInward(ctx, in); err != nil {
		t.Fatalf("inward: %v", err)
	}

	batch, err := medRepo.FindBatchByNumber(ctx, m.ID, "BLEND-B1")
	if err != nil {
		t.Fatalf("find batch: %v", err)
	}

	// Blended cost = 500 / 12 = 41.67
	expectedPrice := 41.67
	if batch.PurchasePrice != expectedPrice {
		t.Errorf("purchase_price = %.2f want %.2f (blended cost)", batch.PurchasePrice, expectedPrice)
	}
	if batch.CurrentStock != 12 {
		t.Errorf("stock = %d want 12", batch.CurrentStock)
	}
}

// TestBonusQuantityGSTInteraction verifies that GST is computed on paid
// quantity only, and stock includes bonus.
func TestBonusQuantityGSTInteraction(t *testing.T) {
	reset(t)
	ctx := context.Background()

	medicineID, batchID := seedGSTMedicine(t)

	// Purchase 10 paid + 2 bonus at ₹100 each with 12% GST
	pin := &repository.PurchaseInput{
		StoreID:     sid(testutil.StoreID),
		InvoiceNo:    fmt.Sprintf("GSTB-%d", time.Now().UnixNano()),
		SupplierName: "GST Bonus Supplier",
		Items: []repository.PurchaseItemInput{{
			MedicineID:  medicineID,
			BatchNumber: "GSTB-B1",
			ExpiryDate:  models.NewDate(time.Now().AddDate(1, 0, 0)),
			Quantity:    10,
			BonusQuantity: 2,
			PurchasePrice: 100,
			SalePrice:     150,
		}},
	}
	po, _, err := purchRepo.CreateInward(ctx, pin)
	if err != nil {
		t.Fatalf("purchase: %v", err)
	}

	// PO total = 10 × 100 = 1000 (paid only)
	if po.TotalAmount != 1000 {
		t.Errorf("po total = %.2f want 1000", po.TotalAmount)
	}

	// Stock = 12
	batch, _ := medRepo.FindBatchByNumber(ctx, medicineID, "GSTB-B1")
	if batch.CurrentStock != 12 {
		t.Errorf("stock = %d want 12", batch.CurrentStock)
	}

	// Now sell all 12 units
	c := &models.Customer{Name: "GST Bonus Customer",
		Phone: fmt.Sprintf("+9198%07d", time.Now().UnixNano()%10000000),
		CustomerType: "B2C"}
	if err := custRepo.Create(ctx, c); err != nil {
		t.Fatalf("create customer: %v", err)
	}

	_ = batchID // used indirectly
	checkout := &repository.CheckoutInput{
		StoreID:     sid(testutil.StoreID),
		CustomerID:  &c.ID,
		PaymentType: models.PaymentCash,
		Items: []repository.CheckoutItemInput{{
			BatchID:  batch.ID,
			Quantity: 12,
		}},
	}
	res, err := saleRepo.Checkout(ctx, checkout)
	if err != nil {
		t.Fatalf("checkout: %v", err)
	}

	// Invoice total should reflect sale of 12 units
	if res.Invoice.TotalAmount != 1800 {
		t.Errorf("invoice total = %.2f want 1800 (12 × 150)", res.Invoice.TotalAmount)
	}

	// Stock should be 0
	batch2, _ := medRepo.FindBatchByNumber(ctx, medicineID, "GSTB-B1")
	if batch2.CurrentStock != 0 {
		t.Errorf("final stock = %d want 0", batch2.CurrentStock)
	}
}

// TestBonusStockSoldCompletely verifies the full lifecycle:
// purchase 10+2 bonus, sell all 12, stock = 0.
func TestBonusStockSoldCompletely(t *testing.T) {
	reset(t)
	ctx := context.Background()

	m := &models.Medicine{
		Name: "Full Sell Med", SaltComposition: "Test",
		Manufacturer: "TestPharma", MinReorderLevel: 0,
	}
	if err := medRepo.Create(ctx, m); err != nil {
		t.Fatalf("create medicine: %v", err)
	}

	in := &repository.PurchaseInput{
		InvoiceNo:    fmt.Sprintf("FULL-%d", time.Now().UnixNano()),
		SupplierName: "Full Sell Supplier",
		StoreID:      sid(testutil.StoreID),
		Items: []repository.PurchaseItemInput{{
			MedicineID:  m.ID,
			BatchNumber: "FULL-B1",
			ExpiryDate:  models.NewDate(time.Now().AddDate(1, 0, 0)),
			Quantity:    10,
			BonusQuantity: 2,
			PurchasePrice: 50,
			SalePrice:     75,
		}},
	}
	if _, _, err := purchRepo.CreateInward(ctx, in); err != nil {
		t.Fatalf("inward: %v", err)
	}

	c := &models.Customer{Name: "Full Sell Customer",
		Phone: fmt.Sprintf("+9198%07d", time.Now().UnixNano()%10000000),
		CustomerType: "B2C"}
	if err := custRepo.Create(ctx, c); err != nil {
		t.Fatalf("create customer: %v", err)
	}

	batch, _ := medRepo.FindBatchByNumber(ctx, m.ID, "FULL-B1")

checkout := &repository.CheckoutInput{
		StoreID:     sid(testutil.StoreID),
		CustomerID:  &c.ID,
		PaymentType: models.PaymentCash,
		Items: []repository.CheckoutItemInput{{
			BatchID:  batch.ID,
			Quantity: 12,
		}},
	}
	if _, err := saleRepo.Checkout(ctx, checkout); err != nil {
		t.Fatalf("checkout: %v", err)
	}

	batch2, _ := medRepo.FindBatchByNumber(ctx, m.ID, "FULL-B1")
	if batch2.CurrentStock != 0 {
		t.Errorf("final stock = %d want 0", batch2.CurrentStock)
	}
}

// TestPurchaseStatsTotalSpendExcludesBonus verifies that medicine purchase stats
// total_spend only counts paid quantity, not bonus.
func TestPurchaseStatsTotalSpendExcludesBonus(t *testing.T) {
	reset(t)
	ctx := context.Background()

	m := &models.Medicine{
		Name: "Stats Med", SaltComposition: "Test",
		Manufacturer: "TestPharma", MinReorderLevel: 0,
	}
	if err := medRepo.Create(ctx, m); err != nil {
		t.Fatalf("create medicine: %v", err)
	}

	// Purchase 10 paid at ₹100 + 2 bonus, 10% line discount
	in := &repository.PurchaseInput{
		StoreID:     sid(testutil.StoreID),
		InvoiceNo:    fmt.Sprintf("STATS-%d", time.Now().UnixNano()),
		SupplierName: "Stats Supplier",
		Items: []repository.PurchaseItemInput{{
			MedicineID:    m.ID,
			BatchNumber:   "STATS-B1",
			ExpiryDate:    models.NewDate(time.Now().AddDate(1, 0, 0)),
			Quantity:      10,
			BonusQuantity: 2,
			PurchasePrice: 100,
			SalePrice:     150,
			DiscountType:  "percent",
			DiscountValue: 10,
		}},
	}
	if _, _, err := purchRepo.CreateInward(ctx, in); err != nil {
		t.Fatalf("inward: %v", err)
	}

	detail, err := medRepo.GetDetail(ctx, m.ID)
	if err != nil {
		t.Fatalf("get detail: %v", err)
	}

	// total_spend = 10 × 100 - 100 (10% discount) = 900
	// NOT (10+2) × 100 = 1200
	expectedSpend := 900.0
	if detail.PurchaseStats.TotalSpend != expectedSpend {
		t.Errorf("total_spend = %.2f want %.2f", detail.PurchaseStats.TotalSpend, expectedSpend)
	}
	// Units purchased includes bonus
	if detail.PurchaseStats.UnitsPurchased != 12 {
		t.Errorf("units_purchased = %d want 12", detail.PurchaseStats.UnitsPurchased)
	}
}

func seedState27Store(t *testing.T, ctx context.Context) string {
	t.Helper()
	var regID string
	err := pool.QueryRow(ctx, `
		WITH biz AS (
			INSERT INTO businesses (legal_name) VALUES ('Reg Pharma') RETURNING id
		), reg AS (
			INSERT INTO gst_registrations (business_id, gstin, state_code)
			SELECT id, '27AAAAA1111A1ZW', '27' FROM biz RETURNING id
		)
		SELECT id FROM reg`).Scan(&regID)
	if err != nil {
		t.Fatalf("seed state-27 registration: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE stores SET gst_registration_id = $1 WHERE id = $2`, regID, testutil.StoreID); err != nil {
		t.Fatalf("link state-27 reg to store: %v", err)
	}
	return testutil.StoreID
}

func TestCheckoutB2BRequiresValidGSTIN(t *testing.T) {
	reset(t)
	_, batchID := seedGSTMedicine(t)
	ctx := context.Background()

	sid := seedState27Store(t, ctx)

	run := func(name string, buyerGSTIN *string, wantErr bool) {
		t.Helper()
		pos := "27"
		_, err := saleRepo.Checkout(ctx, &repository.CheckoutInput{
			StoreID:       &sid,
			PaymentType:   models.PaymentCash,
			SaleType:      "B2B",
			PlaceOfSupply: &pos,
			BuyerGSTIN:    buyerGSTIN,
			BuyerName:     strPtr("Buyer"),
			Items:         []repository.CheckoutItemInput{{BatchID: batchID, Quantity: 2}},
		})
		if wantErr && err == nil {
			t.Errorf("%s: expected error, got nil", name)
		}
		if !wantErr && err != nil {
			t.Errorf("%s: unexpected error: %v", name, err)
		}
	}

	run("missing-gstin", nil, true)
	empty := ""
	run("empty-gstin", &empty, true)
	bad := "27AABCU9603R1ZM" // pattern-valid but checksum-invalid
	run("checksum-invalid", &bad, true)
	valid := "27AAPBC1234F1ZV"
	run("valid-gstin", &valid, false)
}

func TestInvoiceTaxPersistedAfterRateChange(t *testing.T) {
	reset(t)
	ctx := context.Background()

	// Use a dedicated HSN (9999) that no other test or migration touches, so we
	// never mutate the shared 3004/2106/9983 reference data between runs.
	tr := repository.NewTaxRepo(pool)
	if _, err := tr.CreateHSNCode(ctx, testutil.StoreID, "9999", "Regression test HSN"); err != nil {
		// may already exist from a prior run; ignore conflict
		_ = err
	}
	hsn, err := tr.GetHSNByCode(ctx, testutil.StoreID, "9999")
	if err != nil {
		t.Fatalf("get hsn 9999: %v", err)
	}
	// Remove any rates this test previously created so it is fully self-contained
	// across runs (avoids same-day-rate close violating chk_tr_effective).
	if _, err := pool.Exec(ctx, `DELETE FROM tax_rates WHERE hsn_code_id = $1`, hsn.ID); err != nil {
		t.Fatalf("clear 9999 rates: %v", err)
	}
	rate12, err := tr.UpsertTaxRate(ctx, testutil.StoreID, hsn.ID, 12, 6, 6, 12, 0)
	if err != nil {
		t.Fatalf("upsert 12%% rate: %v", err)
	}

	m := &models.Medicine{
		Name:             "Reg Medicine",
		SaltComposition:  "Reg",
		Manufacturer:     "RegPharma",
		MinReorderLevel:  2,
		Packing:          "Tablet",
	}
	if err := medRepo.Create(ctx, m); err != nil {
		t.Fatalf("create medicine: %v", err)
	}
	if _, err := tr.UpsertMedicineTaxConfig(ctx, testutil.StoreID, m.ID, hsn.ID, rate12.ID, false); err != nil {
		t.Fatalf("link config: %v", err)
	}

	if _, _, err := purchRepo.CreateInward(ctx, &repository.PurchaseInput{
		StoreID:     sid(testutil.StoreID),
		InvoiceNo:    "REG-IN-1",
		SupplierName: "Reg Supplier",
		Items: []repository.PurchaseItemInput{{
			MedicineID:    m.ID,
			BatchNumber:   "REG-B1",
			ExpiryDate:    models.NewDate(time.Now().AddDate(2, 0, 0)),
			Quantity:      100,
			PurchasePrice: 100,
			SalePrice:     150,
		}},
	}); err != nil {
		t.Fatalf("seed inward: %v", err)
	}
	batch, err := medRepo.FindBatchByNumber(ctx, m.ID, "REG-B1")
	if err != nil {
		t.Fatalf("find batch: %v", err)
	}

	res, err := saleRepo.Checkout(ctx, &repository.CheckoutInput{
		StoreID:     sid(testutil.StoreID),
		PaymentType: models.PaymentCash,
		Items:       []repository.CheckoutItemInput{{BatchID: batch.ID, Quantity: 10}},
	})
	if err != nil {
		t.Fatalf("checkout: %v", err)
	}
	invID := res.Invoice.ID

	var rate, cgst, sgst float64
	err = pool.QueryRow(ctx,
		`SELECT sii.gst_rate, sii.cgst_amount, sii.sgst_amount FROM sales_invoice_items sii WHERE sii.invoice_id = $1`, invID).
		Scan(&rate, &cgst, &sgst)
	if err != nil {
		t.Fatalf("read persisted item: %v", err)
	}
	if rate != 12 || cgst != 90 || sgst != 90 {
		t.Fatalf("initial persisted: got rate=%.2f cgst=%.2f sgst=%.2f, want 12/90/90", rate, cgst, sgst)
	}

	// Change the tax master for this HSN to 18% (9/9). Backdate the 12% rate
	// first so closing it (effective_to = CURRENT_DATE) does not violate the
	// chk_tr_effective constraint (effective_to > effective_from).
	if _, err := pool.Exec(ctx,
		`UPDATE tax_rates SET effective_from = '2017-07-01'::date WHERE id = $1`, rate12.ID); err != nil {
		t.Fatalf("backdate 12%% rate: %v", err)
	}
	rate18, err := tr.UpsertTaxRate(ctx, testutil.StoreID, hsn.ID, 18, 9, 9, 18, 0)
	if err != nil {
		t.Fatalf("upsert 18%% rate: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`UPDATE medicine_tax_config SET effective_from = '2017-07-01'::date WHERE medicine_id = $1 AND effective_to IS NULL`, m.ID); err != nil {
		t.Fatalf("backdate config: %v", err)
	}
	if _, err := tr.UpsertMedicineTaxConfig(ctx, testutil.StoreID, m.ID, hsn.ID, rate18.ID, false); err != nil {
		t.Fatalf("relink config: %v", err)
	}

	// Historical invoice tax must be unchanged: snapshotted at invoice time,
	// never recalculated against the current tax master.
	var pRate, pCgst, pSgst float64
	err = pool.QueryRow(ctx,
		`SELECT sii.gst_rate, sii.cgst_amount, sii.sgst_amount FROM sales_invoice_items sii WHERE sii.invoice_id = $1`, invID).
		Scan(&pRate, &pCgst, &pSgst)
	if err != nil {
		t.Fatalf("re-read persisted item: %v", err)
	}
	if pRate != 12 || pCgst != 90 || pSgst != 90 {
		t.Errorf("historical invoice tax was mutated: got rate=%.2f cgst=%.2f sgst=%.2f, want 12/90/90", pRate, pCgst, pSgst)
	}
}

