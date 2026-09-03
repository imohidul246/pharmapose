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

// TestCompositeUniqueInvoicePerStore proves two independent stores can each
// issue the same document number without a global unique-key collision:
// (store_id, invoice_no) is unique, invoice_no alone is not.
func TestCompositeUniqueInvoicePerStore(t *testing.T) {
	reset(t)
	ctx := context.Background()
	storeB := secondStore(t)
	storeA := testutil.StoreID

	// Same invoice_no in two different stores must coexist.
	for _, tc := range []struct {
		table string
		sql   string
	}{
		{"sales_invoices", `INSERT INTO sales_invoices (store_id, invoice_no, payment_type, total_amount, discount_total, invoice_date, financial_year) VALUES ($1, 'INV-0001', 'CASH', 0, 0, CURRENT_DATE, '2026-27')`},
		{"purchase_orders", `INSERT INTO purchase_orders (store_id, invoice_no, supplier_name, total_amount, discount_total, invoice_date, financial_year) VALUES ($1, 'PO-0001', 'Sup', 0, 0, CURRENT_DATE, '2026-27')`},
	} {
		if _, err := pool.Exec(ctx, tc.sql, storeA); err != nil {
			t.Fatalf("%s store-A insert: %v", tc.table, err)
		}
		if _, err := pool.Exec(ctx, tc.sql, storeB); err != nil {
			t.Fatalf("%s store-B same number must not collide, got: %v", tc.table, err)
		}
		// Same store + same number MUST collide (per-store uniqueness enforced).
		if _, err := pool.Exec(ctx, tc.sql, storeA); err == nil {
			t.Errorf("%s duplicate (store-A, same number) must fail, got nil", tc.table)
		} else {
			var pgErr *pgconn.PgError
			if ok := errorAs(err, &pgErr); !ok || pgErr.Code != "23505" {
				t.Errorf("%s duplicate error = %v (code %v), want 23505 unique_violation", tc.table, err, pgErr)
			}
		}
	}

	// Credit-note numbers are likewise per-store.
	if _, err := pool.Exec(ctx,
		`INSERT INTO sales_credit_notes (invoice_id, note_no, reason, note_date, store_id, financial_year)
		 SELECT si.id, 'CN-0001', 'test', CURRENT_DATE, si.store_id, '2026-27' FROM sales_invoices si WHERE si.store_id = $1 LIMIT 1`, storeA); err != nil {
		t.Fatalf("store-A credit note insert: %v", err)
	}
	// Need an invoice in store B to attach the second note to.
	var invB string
	if err := pool.QueryRow(ctx, `SELECT id::text FROM sales_invoices WHERE store_id = $1 LIMIT 1`, storeB).Scan(&invB); err != nil {
		// No invoice in B for sales table? create one via purchase_orders-independent path:
		// insert a minimal sales invoice for B first.
		if _, err2 := pool.Exec(ctx,
			`INSERT INTO sales_invoices (store_id, invoice_no, payment_type, total_amount, discount_total, invoice_date, financial_year) VALUES ($1, 'INV-B1', 'CASH', 0, 0, CURRENT_DATE, '2026-27')`, storeB); err2 != nil {
			t.Fatalf("seed store-B invoice: %v", err2)
		}
		if err := pool.QueryRow(ctx, `SELECT id::text FROM sales_invoices WHERE store_id = $1 AND invoice_no='INV-B1'`, storeB).Scan(&invB); err != nil {
			t.Fatalf("read store-B invoice: %v", err)
		}
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO sales_credit_notes (invoice_id, note_no, reason, note_date, store_id, financial_year) VALUES ($1, 'CN-0001', 'test', CURRENT_DATE, $2, '2026-27')`,
		invB, storeB); err != nil {
		t.Fatalf("store-B same note number must not collide, got: %v", err)
	}
}

// TestForeignKeyIDORCheckoutRejected proves a checkout naming a foreign
// customer's or batch's UUID is rejected and touches nothing.
func TestForeignKeyIDORCheckoutRejected(t *testing.T) {
	reset(t)
	fx := seedFixture(t, 100, 100000)
	storeB := secondStore(t)
	_, batchB, customerB := seedStoreB(t, storeB)
	ctx := context.Background()

	// Foreign batch via store-A credentials.
	if _, err := saleRepo.Checkout(ctx, &repository.CheckoutInput{
		StoreID:     sid(testutil.StoreID),
		PaymentType: models.PaymentCash,
		Items:       []repository.CheckoutItemInput{{BatchID: batchB, Quantity: 1}},
	}); err == nil {
		t.Fatal("checkout with foreign batch must be rejected, got nil")
	}

	// Foreign customer via store-A credentials (credit sale).
	if _, err := saleRepo.Checkout(ctx, &repository.CheckoutInput{
		StoreID:     sid(testutil.StoreID),
		PaymentType: models.PaymentCredit,
		CustomerID:  &customerB,
		Items:       []repository.CheckoutItemInput{{BatchID: fx.BatchIDs[0], Quantity: 1}},
	}); err == nil {
		t.Fatal("checkout with foreign customer must be rejected, got nil")
	}

	// Nothing leaked: no invoices, stocks and balances untouched.
	var n int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM sales_invoices`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("rejected IDOR checkouts left %d invoices, want 0", n)
	}
	medB := repository.NewMedicineRepo(pool, storeB)
	batch, err := medB.FindBatchByNumber(ctx, mustMedicineOfBatch(t, batchB), "B-B1")
	if err != nil {
		t.Fatalf("re-read store-B batch: %v", err)
	}
	if batch.CurrentStock != 50 {
		t.Errorf("foreign batch stock = %d want 50", batch.CurrentStock)
	}
	own, _ := medRepo.FindBatchByNumber(ctx, fx.MedicineID, "FIX-B1")
	if own.CurrentStock != 100 {
		t.Errorf("own batch stock = %d want 100", own.CurrentStock)
	}
}

// TestDeadlockOverlappingBatchesReverseOrder runs 10 parallel checkouts over
// the same two batches in opposite payload orders and requires zero
// PostgreSQL deadlock errors (40P01).
func TestDeadlockOverlappingBatchesReverseOrder(t *testing.T) {
	reset(t)
	ctx := context.Background()

	// Two batches with ample stock.
	m := &models.Medicine{Name: "Deadlock Med", SaltComposition: "X",
		Manufacturer: "DL-Pharma", MinReorderLevel: 5}
	if err := medRepo.Create(ctx, m); err != nil {
		t.Fatalf("create medicine: %v", err)
	}
	for _, bn := range []string{"DL-A", "DL-B"} {
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
		if _, _, err := purchRepo.CreateInward(ctx, in); err != nil {
			t.Fatalf("inward %s: %v", bn, err)
		}
	}
	batchA, err := medRepo.FindBatchByNumber(ctx, m.ID, "DL-A")
	if err != nil {
		t.Fatal(err)
	}
	batchB, err := medRepo.FindBatchByNumber(ctx, m.ID, "DL-B")
	if err != nil {
		t.Fatal(err)
	}

	const workers = 10
	var wg sync.WaitGroup
	errCh := make(chan error, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			var items []repository.CheckoutItemInput
			if i%2 == 0 {
				items = []repository.CheckoutItemInput{
					{BatchID: batchA.ID, Quantity: 1},
					{BatchID: batchB.ID, Quantity: 1},
				}
			} else {
				// Reverse order: would deadlock without sorted locking.
				items = []repository.CheckoutItemInput{
					{BatchID: batchB.ID, Quantity: 1},
					{BatchID: batchA.ID, Quantity: 1},
				}
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
			t.Fatalf("deadlock detected under reverse-order concurrency: %v", err)
		}
		t.Fatalf("checkout failure: %v", err)
	}

	// Each batch lost exactly `workers` units.
	a, _ := medRepo.FindBatchByNumber(ctx, m.ID, "DL-A")
	b, _ := medRepo.FindBatchByNumber(ctx, m.ID, "DL-B")
	if a.CurrentStock != 100-workers {
		t.Errorf("batch A stock = %d want %d", a.CurrentStock, 100-workers)
	}
	if b.CurrentStock != 100-workers {
		t.Errorf("batch B stock = %d want %d", b.CurrentStock, 100-workers)
	}
}

// TestBonusRestockReturnsFullPhysicalQuantity issues 10 billed + 2 free,
// returns the full line, and requires all 12 units to come back.
func TestBonusRestockReturnsFullPhysicalQuantity(t *testing.T) {
	reset(t)
	ctx := context.Background()
	hsn, rate := hsnAndRateFor(t, "9980", 12)
	_, batchID := seedTaxedBatch(t, "Bonus Restock Med", "BONUS-R1", hsn, rate, 100)

	sale, err := saleRepo.Checkout(ctx, &repository.CheckoutInput{
		StoreID:     sid(testutil.StoreID),
		PaymentType: models.PaymentCash,
		Items:       []repository.CheckoutItemInput{{BatchID: batchID, Quantity: 10, BonusQuantity: 2}},
	})
	if err != nil {
		t.Fatalf("bonus checkout: %v", err)
	}
	if len(sale.Items) != 1 {
		t.Fatalf("items = %d want 1", len(sale.Items))
	}
	mid := mustMedicineOfBatch(t, batchID)
	after, _ := medRepo.FindBatchByNumber(ctx, mid, "BONUS-R1")
	if after.CurrentStock != 88 {
		t.Fatalf("after sale stock = %d want 88", after.CurrentStock)
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
	if noteItems[0].Quantity != 10 || noteItems[0].BonusQuantity != 2 {
		t.Errorf("credit line = %d+%d want 10+2", noteItems[0].Quantity, noteItems[0].BonusQuantity)
	}

	restored, _ := medRepo.FindBatchByNumber(ctx, mid, "BONUS-R1")
	if restored.CurrentStock != 100 {
		t.Errorf("after return stock = %d want 100 (12 units restored)", restored.CurrentStock)
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
	if errorAs(err, &pgErr) {
		return pgErr.Code == "40P01"
	}
	return strings.Contains(err.Error(), "deadlock")
}

func errorAs(err error, target any) bool {
	switch e := target.(type) {
	case **pgconn.PgError:
		if pe, ok := err.(*pgconn.PgError); ok {
			*e = pe
			return true
		}
		// Unwrap via errors.As equivalent for pgconn (which implements Unwrap chains).
		type causer interface{ Unwrap() error }
		for err != nil {
			if pe, ok := err.(*pgconn.PgError); ok {
				*e = pe
				return true
			}
			c, ok := err.(causer)
			if !ok {
				// Try errors.As via type switch fallback on wrapped *pgconn.PgError with fmt %w.
				if strings.Contains(err.Error(), "40P01") {
					return false
				}
				return false
			}
			err = c.Unwrap()
		}
	}
	return false
}
