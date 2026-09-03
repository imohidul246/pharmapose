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

func strPtr(s string) *string { return &s }
func boolPtr(b bool) *bool    { return &b }

// seed3BShell attaches a fresh, active GST registration (state 27) to the
// shared test store so cru-repos bound to testutil.StoreID resolve.
func seed3BShell(t *testing.T) (storeID, gstin string) {
	t.Helper()
	ctx := context.Background()
	var businessID, regID string
	mustOK := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	mustOK(pool.QueryRow(ctx, `
		INSERT INTO businesses (legal_name, trade_name) VALUES ('3B Test Biz', '3B')
		RETURNING id::text`).Scan(&businessID))
	mustOK(pool.QueryRow(ctx, `
		INSERT INTO gst_registrations (business_id, gstin, legal_name, trade_name, pan, state_code, state_name, is_active)
		VALUES ($1, '27AABCU9603R1ZM', '3B Test Biz', '3B', 'ABCTY1234A', '27', 'Maharashtra', true)
		RETURNING id::text`, businessID).Scan(&regID))
	storeID = testutil.StoreID
	ct, err := pool.Exec(ctx, `
		UPDATE stores SET gst_registration_id = $1 WHERE id = $2`, regID, storeID)
	mustOK(err)
	if ct.RowsAffected() != 1 {
		t.Fatalf("expected 1 store row updated, got %d", ct.RowsAffected())
	}
	return storeID, "27AABCU9603R1ZM"
}

// seed3BMedicine creates a medicine with a 12% (6%+6%) tax config on HSN 3004.
func seed3BMedicine(t *testing.T, name string) string {
	t.Helper()
	ctx := context.Background()
	m := &models.Medicine{
		Name:            name,
		SaltComposition: "Paracetamol 500mg",
		Manufacturer:    "3B Pharma",
		MinReorderLevel: 5,
		Packing:         "Strip of 10",
		UQC:             "TAB",
	}
	if err := medRepo.Create(ctx, m); err != nil {
		t.Fatalf("create medicine: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO hsn_codes (store_id, code, description) VALUES ($1, '3004', 'Medicaments')
		ON CONFLICT (store_id, code) DO NOTHING`, testutil.StoreID); err != nil {
		t.Fatalf("seed hsn: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO tax_rates (store_id, hsn_code_id, gst_rate, cgst_rate, sgst_rate, igst_rate, cess_rate, effective_from)
		SELECT $1, h.id, 12, 6, 6, 12, 0, '2017-07-01' FROM hsn_codes h WHERE h.code = '3004'
		ON CONFLICT DO NOTHING`, testutil.StoreID); err != nil {
		t.Fatalf("seed tax rate: %v", err)
	}
	_, err := pool.Exec(ctx, `
		INSERT INTO medicine_tax_config (store_id, medicine_id, hsn_code_id, tax_rate_id, price_includes_tax, effective_from)
		SELECT $1, $2, h.id, tr.id, false, '2017-07-01'
		FROM hsn_codes h
		JOIN tax_rates tr ON tr.hsn_code_id = h.id
		WHERE h.code = '3004' AND tr.effective_to IS NULL
		ON CONFLICT DO NOTHING`, testutil.StoreID, m.ID)
	if err != nil {
		t.Fatalf("link tax config: %v", err)
	}
	return m.ID
}

// TestGSTR3B_IntraInterRCIneligible exercises the full GSTR-3B aggregation
// against every purchase/sale flavour and verifies the statutory set-off.
func TestGSTR3B_IntraInterRCIneligible(t *testing.T) {
	reset(t)
	ctx := context.Background()
	storeID, gstin := seed3BShell(t)

	when := time.Date(time.Now().Year(), time.Now().Month(), 10, 12, 0, 0, 0, time.UTC)
	whenStr := when.Format("2006-01-02")
	period := when.Format("2006-01")

	// 1. Intra-state purchase 100 × 100 → taxable 10000, CGST 600 + SGST 600.
	intra := seed3BMedicine(t, "3B Intra Med")
	if _, _, err := purchRepo.CreateInward(ctx, &repository.PurchaseInput{
		InvoiceNo:     "3B-IN-1",
		InvoiceDate:   &whenStr,
		SupplierName:  "Intra Distributor",
		SupplierGSTIN: strPtr("27AAECS9876F1ZS"),
		SupplierState: strPtr("27"),
		StoreID:       &storeID,
		PlaceOfSupply: strPtr("27"),
		ITCEligible:   boolPtr(true),
		Items:         []repository.PurchaseItemInput{{MedicineID: intra, BatchNumber: "3B-B-1", Quantity: 100, PurchasePrice: 100, SalePrice: 150, ExpiryDate: models.NewDate(time.Date(2099, 1, 1, 0, 0, 0, 0, time.UTC))}},
	}); err != nil {
		t.Fatalf("intra purchase: %v", err)
	}
	var intraBatchID string
	if err := pool.QueryRow(ctx, `SELECT id::text FROM batches WHERE batch_number = '3B-B-1'`).Scan(&intraBatchID); err != nil {
		t.Fatalf("find intra batch: %v", err)
	}

	// 2. Inter-state purchase 100 × 200 → taxable 20000, IGST 2400.
	if _, _, err := purchRepo.CreateInward(ctx, &repository.PurchaseInput{
		InvoiceNo:     "3B-IN-2",
		InvoiceDate:   &whenStr,
		SupplierName:  "Inter Distributor",
		SupplierGSTIN: strPtr("06AACDD3456G1ZP"),
		SupplierState: strPtr("06"),
		StoreID:       &storeID,
		PlaceOfSupply: strPtr("06"),
		ITCEligible:   boolPtr(true),
		Items:         []repository.PurchaseItemInput{{MedicineID: seed3BMedicine(t, "3B Inter Med"), BatchNumber: "3B-B-2", Quantity: 100, PurchasePrice: 200, SalePrice: 280, ExpiryDate: models.NewDate(time.Date(2099, 2, 1, 0, 0, 0, 0, time.UTC))}},
	}); err != nil {
		t.Fatalf("inter purchase: %v", err)
	}

	// 3. Reverse-charge purchase, not ITC-claimable → 3.1(b) + 4(B) only.
	if _, _, err := purchRepo.CreateInward(ctx, &repository.PurchaseInput{
		InvoiceNo:     "3B-RC-1",
		InvoiceDate:   &whenStr,
		SupplierName:  "RC Transport",
		SupplierGSTIN: strPtr("27AAHFS2345K1ZY"),
		SupplierState: strPtr("27"),
		StoreID:       &storeID,
		PlaceOfSupply: strPtr("27"),
		ReverseCharge: true,
		ITCEligible:   boolPtr(false),
		Items:         []repository.PurchaseItemInput{{MedicineID: seed3BMedicine(t, "3B RC Med"), BatchNumber: "3B-B-3", Quantity: 100, PurchasePrice: 100, SalePrice: 140, ExpiryDate: models.NewDate(time.Date(2099, 3, 1, 0, 0, 0, 0, time.UTC))}},
	}); err != nil {
		t.Fatalf("reverse charge purchase: %v", err)
	}

	// 4. Ineligible ITC purchase 50 × 100 → CGST 300 + SGST 300.
	if _, _, err := purchRepo.CreateInward(ctx, &repository.PurchaseInput{
		InvoiceNo:     "3B-IN-3",
		InvoiceDate:   &whenStr,
		SupplierName:  "NoCredit Supplier",
		SupplierGSTIN: strPtr("27AAECS9876F1ZS"),
		SupplierState: strPtr("27"),
		StoreID:       &storeID,
		PlaceOfSupply: strPtr("27"),
		ITCEligible:   boolPtr(false),
		Items:         []repository.PurchaseItemInput{{MedicineID: seed3BMedicine(t, "3B NoCredit Med"), BatchNumber: "3B-B-4", Quantity: 50, PurchasePrice: 100, SalePrice: 140, ExpiryDate: models.NewDate(time.Date(2099, 4, 1, 0, 0, 0, 0, time.UTC))}},
	}); err != nil {
		t.Fatalf("ineligible purchase: %v", err)
	}

	// 5. Retail sale 10 × 150 → taxable 1500, CGST 90 + SGST 90.
	pos := "27"
	res, err := saleRepo.Checkout(ctx, &repository.CheckoutInput{
		PaymentType:   models.PaymentCash,
		StoreID:       &storeID,
		PlaceOfSupply: &pos,
		Items:         []repository.CheckoutItemInput{{BatchID: intraBatchID, Quantity: 10}},
	})
	if err != nil {
		t.Fatalf("checkout: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE sales_invoices SET invoice_date = $1 WHERE id = $2`, when, res.Invoice.ID); err != nil {
		t.Fatalf("backdate sale: %v", err)
	}

	svc := gst.NewGSTR3BService(pool)
	g3b, err := svc.Build(ctx, gst.GSTR3BRequest{
		StoreID: storeID,
		Period:  period,
		GSTIN:   gstin,
	})
	if err != nil {
		t.Fatalf("build GSTR-3B: %v", err)
	}

	if g3b.GSTIN != gstin {
		t.Errorf("gstin = %s want %s", g3b.GSTIN, gstin)
	}
	if g3b.Period != period {
		t.Errorf("period = %s want %s", g3b.Period, period)
	}

	// 3.1(a) outward taxable supplies (net sales).
	if g3b.OutwardTaxableSupply.TaxableValue != 1500 {
		t.Errorf("outward taxable = %v want 1500", g3b.OutwardTaxableSupply.TaxableValue)
	}
	if g3b.OutwardTaxableSupply.CGST != 90 || g3b.OutwardTaxableSupply.SGST != 90 {
		t.Errorf("outward CGST/SGST = %v/%v want 90/90", g3b.OutwardTaxableSupply.CGST, g3b.OutwardTaxableSupply.SGST)
	}

	// 3.1(b) reverse charge outward.
	if g3b.ReverseChargeSupply.TaxableValue != 10000 {
		t.Errorf("reverse charge taxable = %v want 10000", g3b.ReverseChargeSupply.TaxableValue)
	}
	if g3b.ReverseChargeSupply.CGST != 600 || g3b.ReverseChargeSupply.SGST != 600 {
		t.Errorf("reverse charge CGST/SGST = %v/%v want 600/600", g3b.ReverseChargeSupply.CGST, g3b.ReverseChargeSupply.SGST)
	}

	// 4(A) eligible ITC: intra CGST/SGST 600, inter IGST 2400.
	if g3b.EligibleITC.CGST != 600 || g3b.EligibleITC.SGST != 600 {
		t.Errorf("eligible ITC CGST/SGST = %v/%v want 600/600", g3b.EligibleITC.CGST, g3b.EligibleITC.SGST)
	}
	if g3b.EligibleITC.IGST != 2400 {
		t.Errorf("eligible ITC IGST = %v want 2400", g3b.EligibleITC.IGST)
	}

	// 4(B) ineligible ITC: RC purchase 600+600 plus no-credit 300+300.
	if g3b.IneligibleITC.CGST != 900 || g3b.IneligibleITC.SGST != 900 {
		t.Errorf("ineligible ITC CGST/SGST = %v/%v want 900/900", g3b.IneligibleITC.CGST, g3b.IneligibleITC.SGST)
	}

	// Net liability after set-off: fully covered by credit.
	if g3b.NetLiability.Payable.Total != 0 {
		t.Errorf("net payable = %v want 0", g3b.NetLiability.Payable.Total)
	}
	// Liability: CGST 690 (90 sales + 600 RC), SGST 690.
	if g3b.NetLiability.Liability.CGST != 690 || g3b.NetLiability.Liability.SGST != 690 {
		t.Errorf("liability CGST/SGST = %v/%v want 690/690", g3b.NetLiability.Liability.CGST, g3b.NetLiability.Liability.SGST)
	}

	// No GSTR-2B imported → at-risk ITC is not asserted.
	if g3b.ITCAtRisk != 0 {
		t.Errorf("itc_at_risk = %v want 0 without 2B import", g3b.ITCAtRisk)
	}
}
