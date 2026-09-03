package gst_test

import (
	"context"
	"testing"
	"time"

	"github.com/mohi/pms-marg-inspired/internal/gst"
	"github.com/mohi/pms-marg-inspired/internal/models"
	"github.com/mohi/pms-marg-inspired/internal/repository"
	"github.com/mohi/pms-marg-inspired/internal/testutil"
)

func TestBuildGSTR1_HSNCombinedSummary(t *testing.T) {
	reset(t)
	_, batchID := seedGSTMedicine(t)
	ctx := context.Background()

	// B2C invoice (8 units)
	pos := "27"
	if _, err := saleRepo.Checkout(ctx, &repository.CheckoutInput{
		PaymentType:   models.PaymentCash,
		StoreID:       testutil.StoreIDPtr(),
		PlaceOfSupply: &pos,
		Items:         []repository.CheckoutItemInput{{BatchID: batchID, Quantity: 8}},
	}); err != nil {
		t.Fatalf("b2c checkout: %v", err)
	}
	// B2B invoice (10 units)
	res, err := saleRepo.Checkout(ctx, &repository.CheckoutInput{
		PaymentType:   models.PaymentCash,
		StoreID:       testutil.StoreIDPtr(),
		PlaceOfSupply: &pos,
		Items:         []repository.CheckoutItemInput{{BatchID: batchID, Quantity: 10}},
	})
	if err != nil {
		t.Fatalf("b2b checkout: %v", err)
	}
	// use a checksum-valid GSTIN so the B2B classification holds
	_, err = pool.Exec(ctx, `UPDATE sales_invoices SET customer_gstin = '27AAPBC1234F1ZV' WHERE id = $1`, res.Invoice.ID)
	if err != nil {
		t.Fatalf("set gstin: %v", err)
	}

	start := time.Now().Truncate(24 * time.Hour)
	end := start.AddDate(0, 1, 0)
	gstr, err := builder.BuildGSTR1(ctx, gst.GSTR1Request{
		GSTIN:     "27AAAAA1111A1ZW",
		StartDate: start,
		EndDate:   end,
	})
	if err != nil {
		t.Fatalf("build: %v", err)
	}

	// Both invoices share HSN 3004 / UQC TAB / 12% so they aggregate into a
	// single Table 12 row: taxable = (8 + 10) × 150 = 2700.
	if len(gstr.Hsn.Data) != 1 {
		t.Fatalf("hsn rows = %d want 1 combined", len(gstr.Hsn.Data))
	}
	hsn := gstr.Hsn.Data[0]
	if hsn.HSNCode != "3004" {
		t.Errorf("hsn_sc = %s want 3004", hsn.HSNCode)
	}
	if hsn.Txval != 2700 {
		t.Errorf("txval = %v want 2700", hsn.Txval)
	}
	if hsn.Qty != 18 {
		t.Errorf("qty = %v want 18", hsn.Qty)
	}
	// One B2B and one B2C invoice were booked.
	if len(gstr.B2B) != 1 || len(gstr.B2B[0].Inv) != 1 {
		t.Fatalf("expected 1 B2B entry with 1 invoice")
	}
}

func TestBuildGSTR1_B2CSAggregation(t *testing.T) {
	reset(t)
	_, batchID := seedGSTMedicine(t)
	ctx := context.Background()

	pos := "27"
	for _, qty := range []int{5, 7, 10} {
		if _, err := saleRepo.Checkout(ctx, &repository.CheckoutInput{
			PaymentType:   models.PaymentCash,
			StoreID:       testutil.StoreIDPtr(),
			PlaceOfSupply: &pos,
			Items:         []repository.CheckoutItemInput{{BatchID: batchID, Quantity: qty}},
		}); err != nil {
			t.Fatalf("checkout qty %d: %v", qty, err)
		}
	}

	start := time.Now().Truncate(24 * time.Hour)
	end := start.AddDate(0, 1, 0)
	gstr, err := builder.BuildGSTR1(ctx, gst.GSTR1Request{
		StartDate: start,
		EndDate:   end,
	})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if len(gstr.B2CS) != 1 {
		t.Fatalf("b2cs = %d want 1 aggregated row", len(gstr.B2CS))
	}
	row := gstr.B2CS[0]
	// taxable = (5+7+10) * 150 = 3300
	if row.Txval != 3300 {
		t.Errorf("b2cs taxable = %v want 3300", row.Txval)
	}
	// tax 12%: CGST = round(3300*0.06) per persisted lines sum
	var dbTaxable, dbCgst, dbSgst float64
	err = pool.QueryRow(ctx, `
		SELECT ROUND(SUM(sii.taxable_value),2), ROUND(SUM(sii.cgst_amount),2), ROUND(SUM(sii.sgst_amount),2)
		FROM sales_invoices si JOIN sales_invoice_items sii ON sii.invoice_id = si.id
		WHERE si.customer_gstin IS NULL OR si.customer_gstin = ''`).
		Scan(&dbTaxable, &dbCgst, &dbSgst)
	if err != nil {
		t.Fatalf("db sum: %v", err)
	}
	if row.Txval != dbTaxable {
		t.Errorf("b2cs taxable %v != db %.2f", row.Txval, dbTaxable)
	}
	if row.Camt != dbCgst || row.Samt != dbSgst {
		t.Errorf("b2cs aggregation mismatch: got CGST=%v SGST=%v want DB %.2f/%.2f",
			row.Camt, row.Samt, dbCgst, dbSgst)
	}
}

// seedGSTMedicineInStore provisions a 12%-GST (HSN 3004) medicine plus one
// stocked batch entirely inside storeID: store-local HSN row, tax rate,
// medicine config and inward. Tenant isolation forbids selling another
// store's batch, so GSTR tests that bill a dedicated store must seed their
// fixtures there rather than in the shared test store.
func seedGSTMedicineInStore(t *testing.T, storeID, name, batchNo string, qty, purchasePrice, salePrice int) (medicineID, batchID string) {
	t.Helper()
	ctx := context.Background()

	storeMedRepo := repository.NewMedicineRepo(pool)
	m := &models.Medicine{
		Name:            name,
		SaltComposition: "Paracetamol 500mg",
		Manufacturer:    "GSTPharma",
		MinReorderLevel: 5,
		Packing:         "Strip of 10",
		UQC:             "TAB",
	}
	if err := storeMedRepo.Create(ctx, storeID, m); err != nil {
		t.Fatalf("create medicine in store %s: %v", storeID, err)
	}

	if _, err := pool.Exec(ctx, `
		INSERT INTO hsn_codes (store_id, code, description) VALUES
			($1, '3004', 'Medicaments for therapeutic or prophylactic uses, packed for retail sale')
		ON CONFLICT (store_id, code) DO NOTHING`, storeID); err != nil {
		t.Fatalf("seed hsn: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO tax_rates (store_id, hsn_code_id, gst_rate, cgst_rate, sgst_rate, igst_rate, cess_rate, effective_from)
		SELECT $1, h.id, 12.00, 6.00, 6.00, 12.00, 0.00, '2017-07-01'::date
		FROM hsn_codes h WHERE h.code = '3004' AND h.store_id = $1
		ON CONFLICT DO NOTHING`, storeID); err != nil {
		t.Fatalf("seed tax rate: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO medicine_tax_config (store_id, medicine_id, hsn_code_id, tax_rate_id, price_includes_tax, effective_from)
		SELECT $1, $2, h.id, tr.id, false, '2017-07-01'::date
		FROM hsn_codes h
		JOIN tax_rates tr ON tr.hsn_code_id = h.id AND tr.store_id = $1 AND tr.effective_to IS NULL
		WHERE h.code = '3004' AND h.store_id = $1
		ON CONFLICT DO NOTHING`, storeID, m.ID); err != nil {
		t.Fatalf("link tax config: %v", err)
	}

	in := &repository.PurchaseInput{
		InvoiceNo:    "GSTR1-" + batchNo,
		SupplierName: "GSTR1 Supplier",
		StoreID:      &storeID,
		Items: []repository.PurchaseItemInput{{
			MedicineID:    m.ID,
			BatchNumber:   batchNo,
			ExpiryDate:    models.NewDate(time.Now().AddDate(2, 0, 0)),
			Quantity:      qty,
			PurchasePrice: float64(purchasePrice),
			SalePrice:     float64(salePrice),
		}},
	}
	if _, _, err := purchRepo.CreateInward(ctx, in); err != nil {
		t.Fatalf("seed inward in store %s: %v", storeID, err)
	}
	batch, err := storeMedRepo.FindBatchByNumber(ctx, storeID, m.ID, batchNo)
	if err != nil {
		t.Fatalf("find batch: %v", err)
	}
	return m.ID, batch.ID
}

// seedHighValueMedicineInStore is the high-unit-price variant of
// seedGSTMedicineInStore for B2CL threshold tests (qty 1 × 150000 × 1.12).
func seedHighValueMedicineInStore(t *testing.T, storeID string) string {
	t.Helper()
	_, batchID := seedGSTMedicineInStore(t, storeID, "GSTR1 Surgical Kit", "GSTR1-HB1", 2000, 80000, 150000)
	return batchID
}

// seedStoreWithGSTState27 creates a store linked to a state-27 GST registration
// so inter/intra-state supply classification resolves the real seller state.
func seedStoreWithGSTState27(t *testing.T) string {
	t.Helper()
	var sid string
	err := pool.QueryRow(context.Background(), `
		WITH biz AS (
			INSERT INTO businesses (legal_name) VALUES ('B2CL Business') RETURNING id
		), reg AS (
			INSERT INTO gst_registrations (business_id, gstin, state_code)
			SELECT id, '27AAAAA1111A1ZW', '27' FROM biz RETURNING id
		)
		INSERT INTO stores (gst_registration_id, name, address, is_active)
		SELECT id, 'B2CL Store', 'Pune', true FROM reg
		RETURNING id::text`).Scan(&sid)
	if err != nil {
		t.Fatalf("store with registration: %v", err)
	}
	return sid
}

func TestBuildGSTR1_B2CLThreshold(t *testing.T) {
	reset(t)
	sid := seedStoreWithGSTState27(t)
	batchID := seedHighValueMedicineInStore(t, sid)
	ctx := context.Background()

	// Inter-state B2C (no GSTIN), invoice value > Rs 1,00,000 (qty 1 x 150000 x 1.12 = 168000)
	pos := "06"
	if _, err := saleRepo.Checkout(ctx, &repository.CheckoutInput{
		PaymentType:   models.PaymentCash,
		StoreID:       &sid,
		PlaceOfSupply: &pos,
		Items:         []repository.CheckoutItemInput{{BatchID: batchID, Quantity: 1}},
	}); err != nil {
		t.Fatalf("high checkout: %v", err)
	}

	start := time.Now().Truncate(24 * time.Hour)
	end := start.AddDate(0, 1, 0)
	gstr, err := builder.BuildGSTR1(ctx, gst.GSTR1Request{
		StoreID:   sid,
		StartDate: start,
		EndDate:   end,
	})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if len(gstr.B2CL) != 1 {
		t.Fatalf("b2cl = %d want 1 for above-threshold inter-state B2C", len(gstr.B2CL))
	}
	if len(gstr.B2CS) != 0 {
		t.Fatalf("b2cs should be empty for above-threshold B2C, got %d", len(gstr.B2CS))
	}
}

func TestBuildGSTR1_B2CS_InterStateBelowThreshold(t *testing.T) {
	reset(t)
	sid := seedStoreWithGSTState27(t)
	_, batchID := seedGSTMedicineInStore(t, sid, "GSTR1 B2CS Med", "GSTR1-B2CS1", 100, 100, 150)
	ctx := context.Background()

	// Inter-state B2C below Rs 1,00,000 must be consolidated in B2CS as INTER,
	// not reported invoice-wise in B2CL.
	pos := "06"
	if _, err := saleRepo.Checkout(ctx, &repository.CheckoutInput{
		PaymentType:   models.PaymentCash,
		StoreID:       &sid,
		PlaceOfSupply: &pos,
		Items:         []repository.CheckoutItemInput{{BatchID: batchID, Quantity: 1}},
	}); err != nil {
		t.Fatalf("below-threshold checkout: %v", err)
	}

	start := time.Now().Truncate(24 * time.Hour)
	end := start.AddDate(0, 1, 0)
	gstr, err := builder.BuildGSTR1(ctx, gst.GSTR1Request{
		StoreID:   sid,
		StartDate: start,
		EndDate:   end,
	})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if len(gstr.B2CL) != 0 {
		t.Fatalf("b2cl should be empty for below-threshold B2C, got %d", len(gstr.B2CL))
	}
	// The inter-state row must be present in B2CS, typed INTER.
	found := false
	for _, row := range gstr.B2CS {
		if row.SplyTy == "INTER" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected an INTER B2CS row for below-threshold inter-state B2C, got %+v", gstr.B2CS)
	}
}

func TestBuildGSTR1_DocSeries(t *testing.T) {
	reset(t)
	ctx := context.Background()

	var sid string
	if err := pool.QueryRow(ctx, `
		INSERT INTO stores (name, address, is_active) VALUES ('DocTest Store', 'Mumbai', true)
		RETURNING id::text`).Scan(&sid); err != nil {
		t.Fatalf("store: %v", err)
	}
	_, batchID := seedGSTMedicineInStore(t, sid, "GSTR1 Doc Med", "GSTR1-DOC1", 100, 100, 150)

	for i := 0; i < 3; i++ {
		if _, err := saleRepo.Checkout(ctx, &repository.CheckoutInput{
			PaymentType: models.PaymentCash,
			StoreID:     &sid,
			Items:       []repository.CheckoutItemInput{{BatchID: batchID, Quantity: 1}},
		}); err != nil {
			t.Fatalf("checkout %d: %v", i, err)
		}
	}

	start := time.Now().Truncate(24 * time.Hour)
	end := start.AddDate(0, 1, 0)
	gstr, err := builder.BuildGSTR1(ctx, gst.GSTR1Request{
		StoreID:   sid,
		StartDate: start,
		EndDate:   end,
	})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	var issued float64
	for _, det := range gstr.DocIssue.DocDet {
		for _, d := range det.Docs {
			issued += d.TotNum
			if d.From == "" || d.To == "" {
				t.Errorf("doc series incomplete: %+v", d)
			}
		}
	}
	if issued != 3 {
		t.Errorf("issued = %v want 3", issued)
	}
}
