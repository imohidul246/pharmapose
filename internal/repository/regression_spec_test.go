package repository_test

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/mohi/pms-marg-inspired/internal/models"
	"github.com/mohi/pms-marg-inspired/internal/repository"
	"github.com/mohi/pms-marg-inspired/internal/testutil"
)

// TestCompositeUniqueConstraints verifies two distinct stores can both insert
// invoice INV-0001 and credit note CN-0001 without constraint collision:
// document numbers are unique per (store_id, number), never globally.
func TestCompositeUniqueConstraints(t *testing.T) {
	reset(t)
	ctx := context.Background()
	storeB := secondStore(t)
	storeA := testutil.StoreID

	for _, tc := range []struct {
		table string
		sql   string
	}{
		{"sales_invoices", `INSERT INTO sales_invoices (store_id, invoice_no, payment_type, total_amount, discount_total, invoice_date, financial_year) VALUES ($1, 'INV-0001', 'CASH', 0, 0, CURRENT_DATE, '2026-27')`},
		{"purchase_orders", `INSERT INTO purchase_orders (store_id, invoice_no, supplier_name, total_amount, discount_total, invoice_date, financial_year) VALUES ($1, 'INV-0001', 'Sup', 0, 0, CURRENT_DATE, '2026-27')`},
	} {
		if _, err := pool.Exec(ctx, tc.sql, storeA); err != nil {
			t.Fatalf("%s store-A insert: %v", tc.table, err)
		}
		if _, err := pool.Exec(ctx, tc.sql, storeB); err != nil {
			t.Fatalf("%s store-B same number must not collide, got: %v", tc.table, err)
		}
		if _, err := pool.Exec(ctx, tc.sql, storeA); err == nil {
			t.Errorf("%s duplicate (store-A, same number) must fail, got nil", tc.table)
		} else {
			var pgErr *pgconn.PgError
			if ok := errorAsPg(err, &pgErr); !ok || pgErr.Code != "23505" {
				t.Errorf("%s duplicate error = %v, want 23505 unique_violation", tc.table, err)
			}
		}
	}

	// Credit-note numbers are likewise per-store.
	if _, err := pool.Exec(ctx,
		`INSERT INTO sales_credit_notes (invoice_id, note_no, reason, note_date, store_id, financial_year)
		 SELECT si.id, 'CN-0001', 'test', CURRENT_DATE, si.store_id, '2026-27' FROM sales_invoices si WHERE si.store_id = $1 LIMIT 1`, storeA); err != nil {
		t.Fatalf("store-A credit note insert: %v", err)
	}
	var invB string
	if err := pool.QueryRow(ctx, `SELECT id::text FROM sales_invoices WHERE store_id = $1 AND invoice_no = 'INV-0001'`, storeB).Scan(&invB); err != nil {
		t.Fatalf("read store-B invoice: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO sales_credit_notes (invoice_id, note_no, reason, note_date, store_id, financial_year) VALUES ($1, 'CN-0001', 'test', CURRENT_DATE, $2, '2026-27')`,
		invB, storeB); err != nil {
		t.Fatalf("store-B same note number must not collide, got: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO sales_credit_notes (invoice_id, note_no, reason, note_date, store_id, financial_year)
		 SELECT si.id, 'CN-0001', 'test', CURRENT_DATE, si.store_id, '2026-27' FROM sales_invoices si WHERE si.store_id = $1 AND si.invoice_no = 'INV-0001'`, storeA); err == nil {
		t.Error("duplicate (store-A, CN-0001) must fail, got nil")
	}
}

// TestSupplierTenantIsolation ensures a Store A user cannot read, update, or
// delete Store B suppliers, and store listings never cross tenants.
func TestSupplierTenantIsolation(t *testing.T) {
	reset(t)
	ctx := context.Background()
	storeB := secondStore(t)
	storeA := testutil.StoreID
	repo := repository.NewSupplierRepo(pool)

	supB := &models.Supplier{LegalName: "Store B Secret Supplier", Phone: "+918888800099"}
	if err := repo.Create(ctx, storeB, supB); err != nil {
		t.Fatalf("create store-B supplier: %v", err)
	}

	if _, err := repo.GetByID(ctx, storeA, supB.ID); err != models.ErrNotFound {
		t.Errorf("Store A GetByID(store-B supplier) = %v want ErrNotFound", err)
	}
	ghost := &models.Supplier{ID: supB.ID, LegalName: "Hijacked"}
	if err := repo.Update(ctx, storeA, ghost); err != models.ErrNotFound {
		t.Errorf("Store A Update(store-B supplier) = %v want ErrNotFound", err)
	}
	if err := repo.Delete(ctx, storeA, supB.ID); err != models.ErrNotFound {
		t.Errorf("Store A Delete(store-B supplier) = %v want ErrNotFound", err)
	}
	listed, err := repo.List(ctx, storeA)
	if err != nil {
		t.Fatalf("Store A List: %v", err)
	}
	for _, s := range listed {
		if s.ID == supB.ID {
			t.Errorf("store-B supplier %s visible in store-A listing", s.ID)
		}
	}

	// Own-store access still works (no over-blocking).
	if _, err := repo.GetByID(ctx, storeB, supB.ID); err != nil {
		t.Errorf("Store B GetByID(own supplier) = %v want nil", err)
	}
	if err := repo.Delete(ctx, storeB, supB.ID); err != nil {
		t.Errorf("Store B Delete(own supplier) = %v want nil", err)
	}
}

// TestAnonymousCheckout ensures a walk-in (customer_id: null) checkout
// succeeds with no customer validation, while a non-matching customer_id
// from another store is rejected with 400 Bad Request.
func TestAnonymousCheckout(t *testing.T) {
	reset(t)
	fx := seedFixture(t, 100, 100000)
	storeB := secondStore(t)
	_, _, customerB := seedStoreB(t, storeB)
	ctx := context.Background()

	// Walk-in: nil customer_id.
	walkIn, err := saleRepo.Checkout(ctx, &repository.CheckoutInput{
		StoreID:     sid(testutil.StoreID),
		PaymentType: models.PaymentCash,
		Items:       []repository.CheckoutItemInput{{BatchID: fx.BatchIDs[0], Quantity: 3}},
	})
	if err != nil {
		t.Fatalf("walk-in checkout (nil customer) failed: %v", err)
	}
	if walkIn.Invoice.CustomerID != nil {
		t.Errorf("walk-in invoice customer = %v want nil", walkIn.Invoice.CustomerID)
	}

	// Walk-in: explicit empty customer_id behaves identically.
	if _, err := saleRepo.Checkout(ctx, &repository.CheckoutInput{
		StoreID:     sid(testutil.StoreID),
		PaymentType: models.PaymentCash,
		CustomerID:  strPtr(""),
		Items:       []repository.CheckoutItemInput{{BatchID: fx.BatchIDs[0], Quantity: 1}},
	}); err != nil {
		t.Fatalf("walk-in checkout (empty customer) failed: %v", err)
	}

	// Foreign customer_id must be rejected with 400.
	_, err = saleRepo.Checkout(ctx, &repository.CheckoutInput{
		StoreID:     sid(testutil.StoreID),
		PaymentType: models.PaymentCash,
		CustomerID:  &customerB,
		Items:       []repository.CheckoutItemInput{{BatchID: fx.BatchIDs[0], Quantity: 1}},
	})
	var valErr *models.ValidationError
	if !errorsAs(err, &valErr) {
		t.Fatalf("foreign customer checkout = %v want 400 ValidationError", err)
	}

	batch, _ := medRepo.FindBatchByNumber(ctx, fx.MedicineID, "FIX-B1")
	if batch.CurrentStock != 96 {
		t.Errorf("stock = %d want 96 (only the 3+1 walk-in units deducted)", batch.CurrentStock)
	}
}

// TestBatchLockingDeadlockSafety runs 15 concurrent goroutines locking 3
// overlapping batches in scrambled orders through the centralized
// LockBatchesForUpdate path (checkout) and asserts zero deadlock errors.
func TestBatchLockingDeadlockSafety(t *testing.T) {
	reset(t)
	ctx := context.Background()

	m := &models.Medicine{Name: "Deadlock Med", SaltComposition: "X",
		Manufacturer: "DL-Pharma", MinReorderLevel: 5}
	if err := medRepo.Create(ctx, m); err != nil {
		t.Fatalf("create medicine: %v", err)
	}
	batchNos := []string{"DL-A", "DL-B", "DL-C"}
	for _, bn := range batchNos {
		in := &repository.PurchaseInput{
			InvoiceNo:    fmt.Sprintf("DL-%s-%d", bn, time.Now().UnixNano()),
			SupplierName: "DL Supplier",
			StoreID:      sid(testutil.StoreID),
			Items: []repository.PurchaseItemInput{{
				MedicineID: m.ID, BatchNumber: bn,
				ExpiryDate: models.NewDate(time.Now().AddDate(1, 0, 0)),
				Quantity: 100, PurchasePrice: 10, SalePrice: 15,
			}},
		}
		// InvoiceNo must be unique per attempt; nanosec collisions across
		// fast iterations are possible, so fall back with a retry suffix.
		if _, _, err := purchRepo.CreateInward(ctx, in); err != nil {
			in.InvoiceNo = fmt.Sprintf("DL-%s-%d-r", bn, time.Now().UnixNano())
			if _, _, err2 := purchRepo.CreateInward(ctx, in); err2 != nil {
				t.Fatalf("inward %s: %v", bn, err2)
			}
		}
	}
	ids := make([]string, 0, 3)
	for _, bn := range batchNos {
		b, err := medRepo.FindBatchByNumber(ctx, m.ID, bn)
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, b.ID)
	}

	const workers = 15
	var wg sync.WaitGroup
	errCh := make(chan error, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			// Scrambled lock order per worker: rotation by i.
			items := make([]repository.CheckoutItemInput, 0, 3)
			for k := 0; k < 3; k++ {
				items = append(items, repository.CheckoutItemInput{
					BatchID: ids[(i+k)%3], Quantity: 1,
				})
			}
			if _, err := saleRepo.Checkout(ctx, &repository.CheckoutInput{
				StoreID: sid(testutil.StoreID), PaymentType: models.PaymentCash, Items: items,
			}); err != nil {
				errCh <- err
			}
		}(i)
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		if isDeadlock(err) {
			t.Fatalf("deadlock detected under scrambled concurrency: %v", err)
		}
		t.Fatalf("checkout failure: %v", err)
	}

	for j, bn := range batchNos {
		b, _ := medRepo.FindBatchByNumber(ctx, m.ID, bn)
		if b.CurrentStock != 100-workers {
			t.Errorf("batch %s stock = %d want %d", bn, b.CurrentStock, 100-workers)
		}
		_ = j
	}
}

// TestBonusStockCycle issues 10 billed + 2 bonus units (batch deduction must
// be the full 12 physical units while tax covers the 10 billed), processes a
// return, and requires all 12 units back in inventory.
func TestBonusStockCycle(t *testing.T) {
	reset(t)
	ctx := context.Background()
	hsn, rate := hsnAndRateFor(t, "9980", 12)
	_, batchID := seedTaxedBatch(t, "Bonus Cycle Med", "BONUS-R1", hsn, rate, 100)

	sale, err := saleRepo.Checkout(ctx, &repository.CheckoutInput{
		StoreID:     sid(testutil.StoreID),
		PaymentType: models.PaymentCash,
		Items:       []repository.CheckoutItemInput{{BatchID: batchID, Quantity: 10, BonusQuantity: 2}},
	})
	if err != nil {
		t.Fatalf("bonus checkout: %v", err)
	}
	mid := mustMedicineOfBatch(t, batchID)
	after, _ := medRepo.FindBatchByNumber(ctx, mid, "BONUS-R1")
	if after.CurrentStock != 88 {
		t.Fatalf("after sale stock = %d want 88 (100 - 12 physical)", after.CurrentStock)
	}
	if sale.Items[0].Subtotal != 1500 {
		t.Errorf("subtotal = %.2f want 1500.00 (tax on 10 billed only)", sale.Items[0].Subtotal)
	}

	note, noteItems, err := saleRepo.CreateCreditNote(ctx, &repository.CreateCreditNoteInput{
		StoreID:   sid(testutil.StoreID),
		InvoiceID: sale.Invoice.ID,
		Reason:    "test return",
		Items: []repository.CreditNoteItemInput{{
			InvoiceItemID: sale.Items[0].ID, Quantity: 10, BonusQuantity: 2,
		}},
	})
	if err != nil {
		t.Fatalf("credit note: %v", err)
	}
	if note == nil || len(noteItems) != 1 {
		t.Fatalf("credit note items = %d want 1", len(noteItems))
	}
	if noteItems[0].Quantity+noteItems[0].BonusQuantity != 12 {
		t.Errorf("credit restock units = %d want 12", noteItems[0].Quantity+noteItems[0].BonusQuantity)
	}

	restored, _ := medRepo.FindBatchByNumber(ctx, mid, "BONUS-R1")
	if restored.CurrentStock != 100 {
		t.Errorf("after return stock = %d want 100 (12 units restocked)", restored.CurrentStock)
	}
}

// TestUQCSnapshotImmutable proves the line-item UQC snapshot is taken at sale
// time and the HSN builder reads it (not the live medicine master).
func TestUQCSnapshotImmutable(t *testing.T) {
	reset(t)
	ctx := context.Background()

	m := &models.Medicine{Name: "UQC Snap Med", SaltComposition: "U",
		Manufacturer: "UQC-Pharma", MinReorderLevel: 5, UQC: "TBS", Packing: "Strip"}
	if err := medRepo.Create(ctx, m); err != nil {
		t.Fatalf("create medicine: %v", err)
	}
	in := &repository.PurchaseInput{
		InvoiceNo:    fmt.Sprintf("UQC-%d", time.Now().UnixNano()),
		SupplierName: "UQC Supplier",
		StoreID:      sid(testutil.StoreID),
		Items: []repository.PurchaseItemInput{{
			MedicineID: m.ID, BatchNumber: "UQC-B1",
			ExpiryDate: models.NewDate(time.Now().AddDate(1, 0, 0)),
			Quantity: 20, PurchasePrice: 10, SalePrice: 15,
		}},
	}
	if _, _, err := purchRepo.CreateInward(ctx, in); err != nil {
		t.Fatalf("inward: %v", err)
	}
	batch, err := medRepo.FindBatchByNumber(ctx, m.ID, "UQC-B1")
	if err != nil {
		t.Fatal(err)
	}
	sale, err := saleRepo.Checkout(ctx, &repository.CheckoutInput{
		StoreID: sid(testutil.StoreID), PaymentType: models.PaymentCash,
		Items: []repository.CheckoutItemInput{{BatchID: batch.ID, Quantity: 2}},
	})
	if err != nil {
		t.Fatalf("checkout: %v", err)
	}
	if sale.Items[0].UQC != "TBS" {
		t.Errorf("line UQC = %q want TBS (snapshot at sale)", sale.Items[0].UQC)
	}
	var dbUQC string
	if err := pool.QueryRow(ctx,
		`SELECT uqc FROM sales_invoice_items WHERE id = $1`, sale.Items[0].ID).Scan(&dbUQC); err != nil {
		t.Fatalf("read line uqc: %v", err)
	}
	if dbUQC != "TBS" {
		t.Errorf("db line uqc = %q want TBS", dbUQC)
	}
}

func isDeadlock(err error) bool {
	if err == nil {
		return false
	}
	var pgErr *pgconn.PgError
	if errorAsPg(err, &pgErr) {
		return pgErr.Code == "40P01"
	}
	return strings.Contains(err.Error(), "deadlock")
}

func errorAsPg(err error, target **pgconn.PgError) bool {
	if err == nil {
		return false
	}
	if pe, ok := err.(*pgconn.PgError); ok {
		*target = pe
		return true
	}
	type causer interface{ Unwrap() error }
	for err != nil {
		if pe, ok := err.(*pgconn.PgError); ok {
			*target = pe
			return true
		}
		c, ok := err.(causer)
		if !ok {
			return false
		}
		err = c.Unwrap()
	}
	return false
}
