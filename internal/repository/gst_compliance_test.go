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

// seedTaxedBatch creates a medicine linked to the given HSN/rate and stocks
// one batch of `stock` units (purchase 100, sale 150, tax-exclusive).
func seedTaxedBatch(t *testing.T, name, batchNo string, hsn *models.HSNCode, rate *models.TaxRate, stock int) (medicineID, batchID string) {
	t.Helper()
	ctx := context.Background()

	m := &models.Medicine{Name: name, SaltComposition: "Rx",
		Manufacturer: "CompliancePharma", MinReorderLevel: 5}
	if err := medRepo.Create(ctx, m); err != nil {
		t.Fatalf("create medicine: %v", err)
	}
	tr := repository.NewTaxRepo(pool)
	if _, err := tr.UpsertMedicineTaxConfig(ctx, testutil.StoreID, m.ID, hsn.ID, rate.ID, false); err != nil {
		t.Fatalf("link tax config: %v", err)
	}
	in := &repository.PurchaseInput{
		InvoiceNo:    fmt.Sprintf("TAX-%s-%d", batchNo, time.Now().UnixNano()),
		SupplierName: "Compliance Supplier",
		StoreID:      sid(testutil.StoreID),
		Items: []repository.PurchaseItemInput{{
			MedicineID:    m.ID,
			BatchNumber:   batchNo,
			ExpiryDate:    models.NewDate(time.Now().AddDate(2, 0, 0)),
			Quantity:      stock,
			PurchasePrice: 100,
			SalePrice:     150,
		}},
	}
	if _, _, err := purchRepo.CreateInward(ctx, in); err != nil {
		t.Fatalf("inward: %v", err)
	}
	batch, err := medRepo.FindBatchByNumber(ctx, m.ID, batchNo)
	if err != nil {
		t.Fatalf("find batch: %v", err)
	}
	return m.ID, batch.ID
}

// TestBonusQuantityTaxAndStockDeduction locks the statutory bonus/scheme
// treatment: the FULL physical quantity (billed + bonus) leaves the batch,
// while taxable value and GST are computed strictly on the billed commercial
// amount — bonus units must not inflate GSTR-1/GSTR-3B turnover.
func TestBonusQuantityTaxAndStockDeduction(t *testing.T) {
	reset(t)
	hsn, rate := hsnAndRateFor(t, "9975", 12)
	_, batchID := seedTaxedBatch(t, "Bonus Compliance Med", "BONUS-C1", hsn, rate, 100)

	ctx := context.Background()
	bonusQty := 2
	res, err := saleRepo.Checkout(ctx, &repository.CheckoutInput{
		StoreID:     sid(testutil.StoreID),
		PaymentType: models.PaymentCash,
		Items:       []repository.CheckoutItemInput{{BatchID: batchID, Quantity: 10, BonusQuantity: bonusQty}},
	})
	if err != nil {
		t.Fatalf("bonus checkout: %v", err)
	}
	if len(res.Items) != 1 {
		t.Fatalf("items = %d want 1", len(res.Items))
	}
	li := res.Items[0]
	if li.Quantity != 10 || li.BonusQuantity != 2 {
		t.Errorf("line qty/bonus = %d/%d want 10/2", li.Quantity, li.BonusQuantity)
	}

	// Physical deduction covers billed + bonus.
	batch, _ := medRepo.FindBatchByNumber(ctx, mustMedicineOfBatch(t, batchID), "BONUS-C1")
	if batch.CurrentStock != 88 {
		t.Errorf("stock = %d want 88 (100 - 10 billed - 2 bonus)", batch.CurrentStock)
	}

	// Commercial math covers billed units only: 10 × 150, no discount.
	if li.Subtotal != 1500 {
		t.Errorf("subtotal = %.2f want 1500.00 (bonus adds no commercial value)", li.Subtotal)
	}
	mustFloat := func(name string, v *float64, want float64) {
		t.Helper()
		if v == nil {
			t.Errorf("%s is nil, want %.2f", name, want)
			return
		}
		if *v != want {
			t.Errorf("%s = %.2f want %.2f", name, *v, want)
		}
	}
	mustFloat("gross_amount", li.GrossAmount, 1500)
	mustFloat("taxable_value", li.TaxableValue, 1500)
	// 12% of 1500 = 180 total GST, split 90/90 intra-state.
	mustFloat("gst_rate", li.GSTRate, 12)
	if li.CGSTAmount == nil || li.SGSTAmount == nil {
		t.Fatal("cgst/sgst amounts must be snapshotted")
	}
	if *li.CGSTAmount+*li.SGSTAmount != 180 {
		t.Errorf("cgst+sgst = %.2f want 180.00 (no tax on bonus units)", *li.CGSTAmount+*li.SGSTAmount)
	}

	// A plain 10-unit checkout of the same batch must produce IDENTICAL tax:
	// bonus units are invisible to the tax engine.
	plain, err := saleRepo.Checkout(ctx, &repository.CheckoutInput{
		StoreID:     sid(testutil.StoreID),
		PaymentType: models.PaymentCash,
		Items:       []repository.CheckoutItemInput{{BatchID: batchID, Quantity: 10}},
	})
	if err != nil {
		t.Fatalf("plain checkout: %v", err)
	}
	pl := plain.Items[0]
	if pl.TaxableValue == nil || li.TaxableValue == nil || *pl.TaxableValue != *li.TaxableValue {
		t.Errorf("bonus taxable %v != plain taxable %v (must match)", li.TaxableValue, pl.TaxableValue)
	}
	if *pl.CGSTAmount+*pl.SGSTAmount != *li.CGSTAmount+*li.SGSTAmount {
		t.Errorf("bonus GST != plain GST (bonus must not be taxed)")
	}

	batch, _ = medRepo.FindBatchByNumber(ctx, mustMedicineOfBatch(t, batchID), "BONUS-C1")
	if batch.CurrentStock != 78 {
		t.Errorf("stock = %d want 78 (88 - 10 plain)", batch.CurrentStock)
	}
}

// TestHSNReassignmentPreservesHistoricalInvoices proves line-item tax
// snapshots are immutable: after a medicine is reassigned to a new HSN (and
// new rate), previously recorded sales AND purchase invoices still read back
// the original HSN code and rates instead of the reassigned master values.
func TestHSNReassignmentPreservesHistoricalInvoices(t *testing.T) {
	reset(t)
	ctx := context.Background()

	hsnOld, rateOld := hsnAndRateFor(t, "9976", 12)
	hsnNew, rateNew := hsnAndRateFor(t, "9977", 18)

	medID, batchID := seedTaxedBatch(t, "HSN Snapshot Med", "HSN-S1", hsnOld, rateOld, 100)

	sale, err := saleRepo.Checkout(ctx, &repository.CheckoutInput{
		StoreID:     sid(testutil.StoreID),
		PaymentType: models.PaymentCash,
		Items:       []repository.CheckoutItemInput{{BatchID: batchID, Quantity: 2}},
	})
	if err != nil {
		t.Fatalf("sale checkout: %v", err)
	}

	po, _, err := purchRepo.CreateInward(ctx, &repository.PurchaseInput{
		InvoiceNo:    fmt.Sprintf("HSN-SNAP-%d", time.Now().UnixNano()),
		SupplierName: "Snapshot Supplier",
		StoreID:      sid(testutil.StoreID),
		Items: []repository.PurchaseItemInput{{
			MedicineID:    medID,
			BatchNumber:   "HSN-S2",
			ExpiryDate:    models.NewDate(time.Now().AddDate(2, 0, 0)),
			Quantity:      10,
			PurchasePrice: 100,
			SalePrice:     150,
		}},
	})
	if err != nil {
		t.Fatalf("purchase inward: %v", err)
	}

	// Reassign the medicine to the new HSN + rate (same-day, worst case).
	tr := repository.NewTaxRepo(pool)
	if _, err := tr.UpsertMedicineTaxConfig(ctx, testutil.StoreID, medID, hsnNew.ID, rateNew.ID, false); err != nil {
		t.Fatalf("reassign HSN: %v", err)
	}
	cfg, err := tr.GetMedicineTaxConfigByMedicine(ctx, testutil.StoreID, medID)
	if err != nil || cfg == nil || cfg.HSNCode != hsnNew.Code {
		t.Fatalf("current config = %+v err=%v want HSN %s", cfg, err, hsnNew.Code)
	}

	// Historical SALE invoice still carries the original snapshot.
	got, err := saleRepo.GetInvoice(ctx, testutil.StoreID, sale.Invoice.ID)
	if err != nil {
		t.Fatalf("get historical sale invoice: %v", err)
	}
	if len(got.Items) != 1 {
		t.Fatalf("sale items = %d want 1", len(got.Items))
	}
	si := got.Items[0]
	if si.HSNCode == nil || *si.HSNCode != hsnOld.Code {
		t.Errorf("historical sale HSN = %v want %s (must not follow reassignment)", si.HSNCode, hsnOld.Code)
	}
	if si.GSTRate == nil || *si.GSTRate != 12 {
		t.Errorf("historical sale gst_rate = %v want 12", si.GSTRate)
	}

	// Historical PURCHASE invoice still carries the original snapshot.
	poGot, err := purchRepo.GetInvoice(ctx, testutil.StoreID, po.ID)
	if err != nil {
		t.Fatalf("get historical purchase invoice: %v", err)
	}
	if len(poGot.Items) != 1 {
		t.Fatalf("purchase items = %d want 1", len(poGot.Items))
	}
	pi := poGot.Items[0]
	if pi.HSNCode == nil || *pi.HSNCode != hsnOld.Code {
		t.Errorf("historical purchase HSN = %v want %s (must not follow reassignment)", pi.HSNCode, hsnOld.Code)
	}
	if pi.GSTRate == nil || *pi.GSTRate != 12 {
		t.Errorf("historical purchase gst_rate = %v want 12", pi.GSTRate)
	}

	// And a NEW sale after reassignment picks up the new HSN.
	after, err := saleRepo.Checkout(ctx, &repository.CheckoutInput{
		StoreID:     sid(testutil.StoreID),
		PaymentType: models.PaymentCash,
		Items:       []repository.CheckoutItemInput{{BatchID: batchID, Quantity: 1}},
	})
	if err != nil {
		t.Fatalf("post-reassignment checkout: %v", err)
	}
	if after.Items[0].HSNCode == nil || *after.Items[0].HSNCode != hsnNew.Code {
		t.Errorf("new sale HSN = %v want %s", after.Items[0].HSNCode, hsnNew.Code)
	}
}
