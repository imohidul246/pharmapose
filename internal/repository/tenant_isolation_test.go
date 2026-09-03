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

// secondStore creates an independent store (tenant B) and returns its ID.
// reset() wipes stores, so every test calls this after reset.
func secondStore(t *testing.T) string {
	t.Helper()
	var id string
	if err := pool.QueryRow(context.Background(), `
		INSERT INTO stores (name, address, max_employees)
		VALUES ('Store B (isolation probe)', '', 2)
		RETURNING id::text`).Scan(&id); err != nil {
		t.Fatalf("create second store: %v", err)
	}
	return id
}

// seedStoreB provisions one medicine + batch + customer inside store B and
// returns their IDs. All writes go through store-B-scoped inputs.
func seedStoreB(t *testing.T, storeB string) (medicineID, batchID, customerID string) {
	t.Helper()
	ctx := context.Background()

	medB := repository.NewMedicineRepo(pool, storeB)
	m := &models.Medicine{Name: "Store B Only Med", SaltComposition: "B-Salt",
		Manufacturer: "B-Pharma", MinReorderLevel: 5}
	if err := medB.Create(ctx, m); err != nil {
		t.Fatalf("create store-B medicine: %v", err)
	}

	in := &repository.PurchaseInput{
		InvoiceNo:    fmt.Sprintf("B-IN-%d", time.Now().UnixNano()),
		SupplierName: "Store B Supplier",
		StoreID:      &storeB,
		Items: []repository.PurchaseItemInput{{
			MedicineID:    m.ID,
			BatchNumber:   "B-B1",
			ExpiryDate:    models.NewDate(time.Now().AddDate(1, 0, 0)),
			Quantity:      50,
			PurchasePrice: 10,
			SalePrice:     15,
		}},
	}
	if _, _, err := purchRepo.CreateInward(ctx, in); err != nil {
		t.Fatalf("store-B inward: %v", err)
	}
	batch, err := medB.FindBatchByNumber(ctx, m.ID, "B-B1")
	if err != nil {
		t.Fatalf("find store-B batch: %v", err)
	}

	custB := repository.NewCustomerRepo(pool, storeB)
	c := &models.Customer{Name: "Store B Customer", Phone: "+918888800001",
		CreditLimit: 5000, CustomerType: "B2C"}
	if err := custB.Create(ctx, c); err != nil {
		t.Fatalf("create store-B customer: %v", err)
	}
	return m.ID, batch.ID, c.ID
}

// TestCrossStoreBatchCheckoutRejected proves store A cannot touch store B's
// inventory: the tenant-scoped batch lock matches nothing, so checkout fails
// instead of decrementing foreign stock.
func TestCrossStoreBatchCheckoutRejected(t *testing.T) {
	reset(t)
	_ = seedFixture(t, 100, 1000)
	storeB := secondStore(t)
	_, batchB, _ := seedStoreB(t, storeB)

	_, err := saleRepo.Checkout(context.Background(), &repository.CheckoutInput{
		StoreID:     sid(testutil.StoreID), // store A
		PaymentType: models.PaymentCash,
		Items:       []repository.CheckoutItemInput{{BatchID: batchB, Quantity: 5}},
	})
	if err == nil {
		t.Fatal("cross-store checkout must fail, got nil error")
	}
	var stockErr *models.InsufficientStockError
	if !errorsAs(err, &stockErr) {
		t.Fatalf("want InsufficientStockError for foreign batch, got %v", err)
	}

	// Store B's stock is untouched.
	medB := repository.NewMedicineRepo(pool, storeB)
	batch, err := medB.FindBatchByNumber(context.Background(), mustMedicineOfBatch(t, batchB), "B-B1")
	if err != nil {
		t.Fatalf("re-read store-B batch: %v", err)
	}
	if batch.CurrentStock != 50 {
		t.Errorf("store-B stock = %d want 50 (untouched)", batch.CurrentStock)
	}

	// No invoice row leaked into store A (or anywhere).
	var invoices int
	if err := pool.QueryRow(context.Background(), `SELECT COUNT(*) FROM sales_invoices`).Scan(&invoices); err != nil {
		t.Fatal(err)
	}
	if invoices != 0 {
		t.Errorf("cross-store checkout left %d invoice rows, want 0", invoices)
	}
}

func mustMedicineOfBatch(t *testing.T, batchID string) string {
	t.Helper()
	var medID string
	if err := pool.QueryRow(context.Background(),
		`SELECT medicine_id::text FROM batches WHERE id = $1`, batchID).Scan(&medID); err != nil {
		t.Fatalf("resolve batch medicine: %v", err)
	}
	return medID
}

// TestCrossStoreCustomerNotVisible proves customers are invisible across
// stores: reads, payments and credit checkouts against a foreign customer ID
// all fail with ErrNotFound instead of leaking balances or extending credit.
func TestCrossStoreCustomerNotVisible(t *testing.T) {
	reset(t)
	fx := seedFixture(t, 100, 100000)
	storeB := secondStore(t)
	_, _, customerB := seedStoreB(t, storeB)

	ctx := context.Background()

	if _, err := custRepo.GetByID(ctx, customerB); err != models.ErrNotFound {
		t.Errorf("GetByID foreign customer = %v want ErrNotFound", err)
	}
	if _, _, err := custRepo.RecordPayment(ctx, customerB, 10, "probe"); err != models.ErrNotFound {
		t.Errorf("RecordPayment foreign customer = %v want ErrNotFound", err)
	}

	// A credit checkout naming store B's customer from store A must fail.
	_, err := saleRepo.Checkout(ctx, &repository.CheckoutInput{
		StoreID:     sid(testutil.StoreID),
		PaymentType: models.PaymentCredit,
		CustomerID:  &customerB,
		Items:       []repository.CheckoutItemInput{{BatchID: fx.BatchIDs[0], Quantity: 1}},
	})
	if err != models.ErrNotFound {
		t.Errorf("credit checkout with foreign customer = %v want ErrNotFound", err)
	}

	// Store B's customer balance is untouched.
	custB := repository.NewCustomerRepo(pool, storeB)
	c, err := custB.GetByID(ctx, customerB)
	if err != nil {
		t.Fatalf("re-read store-B customer: %v", err)
	}
	if c.CurrentBalance != 0 {
		t.Errorf("store-B balance = %.2f want 0 (untouched)", c.CurrentBalance)
	}
}

// TestCrossStoreMedicineNotVisible proves the catalogue is tenant-scoped for
// reads and writes.
func TestCrossStoreMedicineNotVisible(t *testing.T) {
	reset(t)
	storeB := secondStore(t)
	medB, _, _ := seedStoreB(t, storeB)

	if _, err := medRepo.GetByID(context.Background(), medB); err != models.ErrNotFound {
		t.Errorf("GetByID foreign medicine = %v want ErrNotFound", err)
	}
	ghost := &models.Medicine{ID: medB, Name: "Hijacked"}
	if err := medRepo.Update(context.Background(), ghost); err != models.ErrNotFound {
		t.Errorf("Update foreign medicine = %v want ErrNotFound", err)
	}
	if err := medRepo.SoftDelete(context.Background(), medB); err != models.ErrNotFound {
		t.Errorf("SoftDelete foreign medicine = %v want ErrNotFound", err)
	}
}

// TestCrossStoreInvoiceNotVisible proves invoice history cannot be pulled
// across stores by ID enumeration.
func TestCrossStoreInvoiceNotVisible(t *testing.T) {
	reset(t)
	storeB := secondStore(t)
	_, batchB, _ := seedStoreB(t, storeB)

	res, err := saleRepo.Checkout(context.Background(), &repository.CheckoutInput{
		StoreID:     &storeB,
		PaymentType: models.PaymentCash,
		Items:       []repository.CheckoutItemInput{{BatchID: batchB, Quantity: 2}},
	})
	if err != nil {
		t.Fatalf("store-B checkout: %v", err)
	}

	if _, err := saleRepo.GetInvoice(context.Background(), testutil.StoreID, res.Invoice.ID); err != models.ErrNotFound {
		t.Errorf("GetInvoice foreign invoice = %v want ErrNotFound", err)
	}

	rows, err := saleRepo.ListInvoices(context.Background(), testutil.StoreID,
		time.Now().AddDate(0, 0, -1), time.Now().AddDate(0, 0, 1), "")
	if err != nil {
		t.Fatalf("ListInvoices: %v", err)
	}
	for _, r := range rows {
		if r.ID == res.Invoice.ID {
			t.Errorf("store-B invoice %s visible in store-A listing", r.ID)
		}
	}

	// The invoice IS visible from its own store (no over-blocking).
	if _, err := saleRepo.GetInvoice(context.Background(), storeB, res.Invoice.ID); err != nil {
		t.Errorf("GetInvoice own-store invoice = %v want nil", err)
	}
}
