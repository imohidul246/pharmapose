package repository_test

import (
	"context"
	"sync"
	"testing"

	"github.com/mohi/pms-marg-inspired/internal/models"
	"github.com/mohi/pms-marg-inspired/internal/repository"
	"github.com/mohi/pms-marg-inspired/internal/testutil"
)

// TestConcurrentCheckoutInvoiceNumbersUnique proves the atomic invoice
// sequence never emits a duplicate number when many checkouts race: every
// committed invoice carries a distinct number even though all transactions
// increment the same (store, financial-year, prefix) sequence row.
func TestConcurrentCheckoutInvoiceNumbersUnique(t *testing.T) {
	const workers = 10
	const perWorker = 20
	const total = workers * perWorker

	reset(t)
	fx := seedFixture(t, total, 1000000)

	var mu sync.Mutex
	seen := make(map[string]bool, total)
	var wg sync.WaitGroup
	errCh := make(chan error, workers*perWorker)

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < perWorker; j++ {
				res, err := saleRepo.Checkout(context.Background(), &repository.CheckoutInput{
					StoreID:     sid(testutil.StoreID),
					PaymentType: models.PaymentCash,
					Items:       []repository.CheckoutItemInput{{BatchID: fx.BatchIDs[0], Quantity: 1}},
				})
				if err != nil {
					errCh <- err
					return
				}
				mu.Lock()
				if seen[res.Invoice.InvoiceNo] {
					errCh <- &models.InsufficientStockError{BatchID: "duplicate-invoice-no:" + res.Invoice.InvoiceNo}
				}
				seen[res.Invoice.InvoiceNo] = true
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Fatalf("concurrent checkout failure: %v", err)
	}

	if len(seen) != total {
		t.Errorf("unique invoice numbers = %d want %d (duplicates issued under concurrency)", len(seen), total)
	}

	var invoices int
	if err := pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM sales_invoices`).Scan(&invoices); err != nil {
		t.Fatal(err)
	}
	if invoices != total {
		t.Errorf("invoice rows = %d want %d", invoices, total)
	}

	batch, _ := medRepo.FindBatchByNumber(context.Background(), testutil.StoreID, fx.MedicineID, "FIX-B1")
	if batch.CurrentStock != 0 {
		t.Errorf("stock = %d want 0 (%d sold from %d)", batch.CurrentStock, total, total)
	}
}
