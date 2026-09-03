package repository_test

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mohi/pms-marg-inspired/internal/models"
	"github.com/mohi/pms-marg-inspired/internal/repository"
	"github.com/mohi/pms-marg-inspired/internal/testutil"
)

var (
	pool       *pgxpool.Pool
	medRepo    *repository.MedicineRepo
	custRepo   *repository.CustomerRepo
	saleRepo   *repository.SaleRepo
	purchRepo  *repository.PurchaseRepo
	reconRepo  *repository.ReconcileRepo
	reportRepo *repository.ReportRepo
)

func TestMain(m *testing.M) {
	ctx := context.Background()

	var err error
	pool, err = testutil.ConnectTestDB(ctx, "repository")
	if err != nil {
		fmt.Fprintf(os.Stderr, "connect test db: %v\n", err)
		os.Exit(1)
	}
	if err := testutil.SeedStore(ctx, pool); err != nil {
		fmt.Fprintf(os.Stderr, "seed store: %v\n", err)
		os.Exit(1)
	}

	medRepo = repository.NewMedicineRepo(pool)
	custRepo = repository.NewCustomerRepo(pool)
	saleRepo = repository.NewSaleRepo(pool)
	purchRepo = repository.NewPurchaseRepo(pool)
	reconRepo = repository.NewReconcileRepo(pool)
	reportRepo = repository.NewReportRepo(pool)
	authRepo = repository.NewAuthRepo(pool)
	prRepo = repository.NewPurchaseRequestRepo(pool)
	saRepo = repository.NewStockAuditRequestRepo(pool)

	code := m.Run()
	pool.Close()
	os.Exit(code)
}

// reset truncates every table so tests stay independent of each other.
// hsn_codes and tax_rates are preserved — they are reference data seeded by
// migration 021 and should not be wiped between tests.
func reset(t *testing.T) {
	t.Helper()
	_, err := pool.Exec(context.Background(), `
		TRUNCATE customer_ledger, reconciliation_items, reconciliation_journals,
		         sales_credit_notes,
		         sales_invoice_items, sales_invoices,
		         purchase_order_items, purchase_orders,
		         gstr2b_imports, gstr2b_import_batches,
		         medicine_tax_config,
		         gst_registrations, stores, businesses,
		         suppliers,
		         batches, customers, medicines CASCADE`)
	if err != nil {
		t.Fatalf("reset: %v", err)
	}
	if err := testutil.SeedStore(context.Background(), pool); err != nil {
		t.Fatalf("reset seed store: %v", err)
	}
}

type fixture struct {
	MedicineID string
	BatchIDs   []string
	CustomerID string
}

func seedFixture(t *testing.T, stock int, creditLimit float64) fixture {
	t.Helper()
	ctx := context.Background()

	m := &models.Medicine{Name: "Test Paracetamol 500mg",
		SaltComposition: "Paracetamol 500mg", Manufacturer: "TestPharma",
		MinReorderLevel: 10}
	if err := medRepo.Create(ctx, testutil.StoreID, m); err != nil {
		t.Fatalf("create medicine: %v", err)
	}

	in := &repository.PurchaseInput{
		InvoiceNo:    fmt.Sprintf("FIX-%d", time.Now().UnixNano()),
		SupplierName: "Fixture Supplier",
		StoreID:     sid(testutil.StoreID),
		Items: []repository.PurchaseItemInput{{
			MedicineID:    m.ID,
			BatchNumber:   "FIX-B1",
			ExpiryDate:    models.NewDate(time.Now().AddDate(1, 0, 0)),
			Quantity:      stock,
			PurchasePrice: 10,
			SalePrice:     15,
		}},
	}
	if _, _, err := purchRepo.CreateInward(ctx, in); err != nil {
		t.Fatalf("inward: %v", err)
	}

	batch, err := medRepo.FindBatchByNumber(ctx, testutil.StoreID, m.ID, "FIX-B1")
	if err != nil {
		t.Fatalf("find batch: %v", err)
	}

	c := &models.Customer{Name: "Test Customer", Phone: fmt.Sprintf("+9198%07d", time.Now().UnixNano()%10000000), CreditLimit: creditLimit, CustomerType: "B2C"}
	if err := custRepo.Create(ctx, testutil.StoreID, c); err != nil {
		t.Fatalf("create customer: %v", err)
	}
	return fixture{MedicineID: m.ID, BatchIDs: []string{batch.ID}, CustomerID: c.ID}
}

func TestCheckoutDecrementsBatchStock(t *testing.T) {
	reset(t)
	fx := seedFixture(t, 100, 1000)

	res, err := saleRepo.Checkout(context.Background(), &repository.CheckoutInput{
		StoreID:     sid(testutil.StoreID),
		PaymentType: models.PaymentCash,
		Items:       []repository.CheckoutItemInput{{BatchID: fx.BatchIDs[0], Quantity: 30}},
	})
	if err != nil {
		t.Fatalf("checkout: %v", err)
	}
	if res.Invoice.TotalAmount != 450 {
		t.Errorf("total = %.2f want 450.00", res.Invoice.TotalAmount)
	}
	if len(res.Items) != 1 || res.Items[0].Subtotal != 450 {
		t.Errorf("items mismatch: %+v", res.Items)
	}

	batch, _ := medRepo.FindBatchByNumber(context.Background(), testutil.StoreID, fx.MedicineID, "FIX-B1")
	if batch.CurrentStock != 70 {
		t.Errorf("stock after sale = %d want 70", batch.CurrentStock)
	}
}

func TestCheckoutRejectsOversellAtomically(t *testing.T) {
	reset(t)
	fx := seedFixture(t, 50, 1000)

	in := repository.CheckoutInput{
		StoreID:     sid(testutil.StoreID),
		PaymentType: models.PaymentCash,
		Items: []repository.CheckoutItemInput{
			{BatchID: fx.BatchIDs[0], Quantity: 40},
			{BatchID: fx.BatchIDs[0], Quantity: 20},
		},
	}
	_, err := saleRepo.Checkout(context.Background(), &in)

	var stockErr *models.InsufficientStockError
	if !errorsAs(err, &stockErr) {
		t.Fatalf("want InsufficientStockError, got %v", err)
	}
	if stockErr.AvailableStock != 50 || stockErr.RequestedQty != 60 {
		t.Errorf("merged demand wrong: %+v", stockErr)
	}

	var invoices int
	if err := pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM sales_invoices`).Scan(&invoices); err != nil {
		t.Fatal(err)
	}
	if invoices != 0 {
		t.Errorf("failed checkout must not leave invoice rows, found %d", invoices)
	}
}

func TestCheckoutConcurrencyNeverNegativeStock(t *testing.T) {
	reset(t)
	fx := seedFixture(t, 100, 100000)

	const workers = 12
	const perWorker = 15

	var wg sync.WaitGroup
	errCh := make(chan error, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < perWorker; j++ {
				_, err := saleRepo.Checkout(context.Background(), &repository.CheckoutInput{
		StoreID:     sid(testutil.StoreID),
					PaymentType: models.PaymentCash,
					Items:       []repository.CheckoutItemInput{{BatchID: fx.BatchIDs[0], Quantity: 1}},
				})
				if err != nil {
					var se *models.InsufficientStockError
					if !errorsAs(err, &se) {
						errCh <- err
						return
					}
				}
			}
		}()
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Fatalf("unexpected concurrent checkout failure: %v", err)
	}

	batch, _ := medRepo.FindBatchByNumber(context.Background(), testutil.StoreID, fx.MedicineID, "FIX-B1")
	if batch.CurrentStock != 0 {
		t.Errorf("oversold: stock = %d want exactly 0 (demand 180 > supply 100)", batch.CurrentStock)
	}

	var invoices int
	if err := pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM sales_invoices`).Scan(&invoices); err != nil {
		t.Fatal(err)
	}
	if invoices != 100 {
		t.Errorf("sold %d invoices want 100", invoices)
	}
}

func TestCreditLimitEnforced(t *testing.T) {
	reset(t)
	fx := seedFixture(t, 500, 100)

	customer, err := custRepo.GetByID(context.Background(), testutil.StoreID, fx.CustomerID)
	if err != nil {
		t.Fatal(err)
	}
	if customer.CurrentBalance != 0 {
		t.Fatalf("balance should start at 0, got %.2f", customer.CurrentBalance)
	}

	_, err = saleRepo.Checkout(context.Background(), &repository.CheckoutInput{
		StoreID:     sid(testutil.StoreID),
		PaymentType: models.PaymentCredit,
		CustomerID:  &fx.CustomerID,
		Items:       []repository.CheckoutItemInput{{BatchID: fx.BatchIDs[0], Quantity: 7}},
	})
	if !assertCreditError(err) {
		t.Fatalf("credit over limit must fail with CreditLimitExceededError, got %v", err)
	}

	customer, _ = custRepo.GetByID(context.Background(), testutil.StoreID, fx.CustomerID)
	if customer.CurrentBalance != 0 {
		t.Errorf("rejected credit sale mutated balance: %.2f", customer.CurrentBalance)
	}
	batch, _ := medRepo.FindBatchByNumber(context.Background(), testutil.StoreID, fx.MedicineID, "FIX-B1")
	if batch.CurrentStock != 500 {
		t.Errorf("rejected credit sale mutated stock: %d", batch.CurrentStock)
	}

	res, err := saleRepo.Checkout(context.Background(), &repository.CheckoutInput{
		StoreID:     sid(testutil.StoreID),
		PaymentType: models.PaymentCredit,
		CustomerID:  &fx.CustomerID,
		Items:       []repository.CheckoutItemInput{{BatchID: fx.BatchIDs[0], Quantity: 6}},
	})
	if err != nil {
		t.Fatalf("within-limit credit sale should pass: %v", err)
	}
	if res.Invoice.TotalAmount != 90 {
		t.Errorf("total = %.2f want 90.00", res.Invoice.TotalAmount)
	}

	_, err = saleRepo.Checkout(context.Background(), &repository.CheckoutInput{
		StoreID:     sid(testutil.StoreID),
		PaymentType: models.PaymentCredit,
		CustomerID:  &fx.CustomerID,
		Items:       []repository.CheckoutItemInput{{BatchID: fx.BatchIDs[0], Quantity: 1}},
	})
	if !assertCreditError(err) {
		t.Fatalf("sale past accumulated balance must fail, got %v", err)
	}

	customer, _ = custRepo.GetByID(context.Background(), testutil.StoreID, fx.CustomerID)
	if customer.CurrentBalance != 90 {
		t.Errorf("balance = %.2f want 90.00", customer.CurrentBalance)
	}
}

func TestCreditSaleRequiresCustomer(t *testing.T) {
	reset(t)
	fx := seedFixture(t, 100, 0)

	_, err := saleRepo.Checkout(context.Background(), &repository.CheckoutInput{
		StoreID:     sid(testutil.StoreID),
		PaymentType: models.PaymentCredit,
		Items:       []repository.CheckoutItemInput{{BatchID: fx.BatchIDs[0], Quantity: 1}},
	})
	if err == nil || err.Error() != "credit sale requires a customer" {
		t.Fatalf("want customer-required error, got %v", err)
	}
}

func TestPurchaseInwardMergesSameBatchNumber(t *testing.T) {
	reset(t)
	fx := seedFixture(t, 100, 0)

	in := &repository.PurchaseInput{
		InvoiceNo:    fmt.Sprintf("MERGE-%d", time.Now().UnixNano()),
		SupplierName: "Second Supplier",
		StoreID:     sid(testutil.StoreID),
		Items: []repository.PurchaseItemInput{{
			MedicineID:    fx.MedicineID,
			BatchNumber:   "FIX-B1",
			ExpiryDate:    models.NewDate(time.Now().AddDate(1, 6, 0)),
			Quantity:      25,
			PurchasePrice: 11,
			SalePrice:     16,
		}},
	}
	po, items, err := purchRepo.CreateInward(context.Background(), in)
	if err != nil {
		t.Fatalf("merge inward: %v", err)
	}
	if po.TotalAmount != 275 {
		t.Errorf("po total = %.2f want 275.00", po.TotalAmount)
	}
	if len(items) != 1 {
		t.Fatalf("items = %d want 1", len(items))
	}

	batch, _ := medRepo.FindBatchByNumber(context.Background(), testutil.StoreID, fx.MedicineID, "FIX-B1")
	if batch.CurrentStock != 125 {
		t.Errorf("merged stock = %d want 125", batch.CurrentStock)
	}
	if batch.SalePrice != 16 {
		t.Errorf("merged sale price = %.2f want 16.00", batch.SalePrice)
	}

	var batchCount int
	if err := pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM batches WHERE medicine_id = $1`, fx.MedicineID).Scan(&batchCount); err != nil {
		t.Fatal(err)
	}
	if batchCount != 1 {
		t.Errorf("same physical batch duplicated: %d rows", batchCount)
	}
}

func TestPurchaseInwardCreatesNewMedicineInline(t *testing.T) {
	reset(t)

	in := &repository.PurchaseInput{
		InvoiceNo:    fmt.Sprintf("NEWMED-%d", time.Now().UnixNano()),
		SupplierName: "First Stock Supplier",
		StoreID:     sid(testutil.StoreID),
		Items: []repository.PurchaseItemInput{{
			MedicineName:    "Azithral New 500",
			SaltComposition: "Azithromycin 500mg",
			Manufacturer:    "NewCo",
			Packing:         "Strip of 5",
			MinReorderLevel: 15,
			BatchNumber:     "AZ-500-A1",
			ExpiryDate:      models.NewDate(time.Now().AddDate(2, 0, 0)),
			Quantity:        40,
			PurchasePrice:   20,
			SalePrice:       28,
		}},
	}
	po, items, err := purchRepo.CreateInward(context.Background(), in)
	if err != nil {
		t.Fatalf("inline inward: %v", err)
	}
	if po.TotalAmount != 800 {
		t.Errorf("po total = %.2f want 800.00", po.TotalAmount)
	}
	if len(items) != 1 || items[0].MedicineID == "" {
		t.Fatalf("items mismatch: %+v", items)
	}

	m, err := medRepo.GetByID(context.Background(), testutil.StoreID, items[0].MedicineID)
	if err != nil {
		t.Fatalf("medicine not registered by inward: %v", err)
	}
	if m.Name != "Azithral New 500" || m.SaltComposition != "Azithromycin 500mg" ||
		m.Manufacturer != "NewCo" || m.MinReorderLevel != 15 {
		t.Errorf("registered medicine fields wrong: %+v", m)
	}

	batch, err := medRepo.FindBatchByNumber(context.Background(), testutil.StoreID, m.ID, "AZ-500-A1")
	if err != nil {
		t.Fatalf("batch not stocked: %v", err)
	}
	if batch.CurrentStock != 40 || batch.SalePrice != 28 {
		t.Errorf("inventory not updated by first inward: %+v", batch)
	}
}

func TestPurchaseInwardRequiresMedicineReference(t *testing.T) {
	reset(t)

	in := &repository.PurchaseInput{
		SupplierName: "Any Supplier",
		StoreID:     sid(testutil.StoreID),
		Items: []repository.PurchaseItemInput{{
			BatchNumber: "B-1",
			ExpiryDate:  models.NewDate(time.Now().AddDate(1, 0, 0)),
			Quantity:    1,
		}},
	}
	_, _, err := purchRepo.CreateInward(context.Background(), in)
	if err == nil || !strings.Contains(err.Error(), "medicine") {
		t.Fatalf("want missing-medicine-reference error, got %v", err)
	}
}

func TestPurchaseInwardBonusStock(t *testing.T) {
	reset(t)
	fx := seedFixture(t, 100, 0)

	in := &repository.PurchaseInput{
		InvoiceNo:    fmt.Sprintf("BONUS-%d", time.Now().UnixNano()),
		SupplierName: "Bonus Supplier",
		StoreID:     sid(testutil.StoreID),
		Items: []repository.PurchaseItemInput{{
			MedicineID:    fx.MedicineID,
			BatchNumber:   "BONUS-B1",
			ExpiryDate:    models.NewDate(time.Now().AddDate(1, 0, 0)),
			Quantity:      10,
			BonusQuantity: 2,
			PurchasePrice: 50,
			SalePrice:     75,
		}},
	}
	po, items, err := purchRepo.CreateInward(context.Background(), in)
	if err != nil {
		t.Fatalf("bonus inward: %v", err)
	}
	if po.TotalAmount != 500 {
		t.Errorf("po total = %.2f want 500.00 (10 paid × 50)", po.TotalAmount)
	}
	if len(items) != 1 || items[0].BonusQuantity != 2 {
		t.Fatalf("items mismatch: bonus_quantity=%d want 2", items[0].BonusQuantity)
	}

	batch, err := medRepo.FindBatchByNumber(context.Background(), testutil.StoreID, fx.MedicineID, "BONUS-B1")
	if err != nil {
		t.Fatalf("find batch: %v", err)
	}
	if batch.CurrentStock != 12 {
		t.Errorf("stock = %d want 12 (10 paid + 2 free)", batch.CurrentStock)
	}
}

func TestPurchaseInwardPerLineDiscount(t *testing.T) {
	reset(t)
	fx := seedFixture(t, 1, 0)

	in := &repository.PurchaseInput{
		InvoiceNo:    fmt.Sprintf("DISC-%d", time.Now().UnixNano()),
		SupplierName: "Discount Supplier",
		StoreID:     sid(testutil.StoreID),
		Items: []repository.PurchaseItemInput{{
			MedicineID:    fx.MedicineID,
			BatchNumber:   "DISC-B1",
			ExpiryDate:    models.NewDate(time.Now().AddDate(1, 0, 0)),
			Quantity:      20,
			PurchasePrice: 100,
			SalePrice:     150,
			DiscountType:  "percent",
			DiscountValue: 10,
		}},
	}
	po, items, err := purchRepo.CreateInward(context.Background(), in)
	if err != nil {
		t.Fatalf("discount inward: %v", err)
	}
	// 20 × 100 = 2000, 10% discount = 200, net = 1800
	if po.TotalAmount != 1800 {
		t.Errorf("po total = %.2f want 1800.00", po.TotalAmount)
	}
	if items[0].DiscountAmount != 200 {
		t.Errorf("line discount_amount = %.2f want 200.00", items[0].DiscountAmount)
	}

	batch, err := medRepo.FindBatchByNumber(context.Background(), testutil.StoreID, fx.MedicineID, "DISC-B1")
	if err != nil {
		t.Fatalf("find batch: %v", err)
	}
	// Effective purchase price = (2000 - 200) / 20 = 90
	if batch.PurchasePrice != 90 {
		t.Errorf("batch purchase_price = %.2f want 90.00 (net effective)", batch.PurchasePrice)
	}
}

func TestPurchaseInwardPOLevelDiscount(t *testing.T) {
	reset(t)
	fx := seedFixture(t, 1, 0)

	in := &repository.PurchaseInput{
		InvoiceNo:     fmt.Sprintf("PODISC-%d", time.Now().UnixNano()),
		SupplierName:  "PO Discount Supplier",
		StoreID:      sid(testutil.StoreID),
		DiscountTotal: 500,
		Items: []repository.PurchaseItemInput{{
			MedicineID:    fx.MedicineID,
			BatchNumber:   "PODISC-B1",
			ExpiryDate:    models.NewDate(time.Now().AddDate(1, 0, 0)),
			Quantity:      10,
			PurchasePrice: 100,
			SalePrice:     150,
		}},
	}
	po, _, err := purchRepo.CreateInward(context.Background(), in)
	if err != nil {
		t.Fatalf("po discount inward: %v", err)
	}
	// 10 × 100 = 1000, PO discount 500, net = 500
	if po.TotalAmount != 500 {
		t.Errorf("po total = %.2f want 500.00", po.TotalAmount)
	}
	if po.DiscountTotal != 500 {
		t.Errorf("po discount_total = %.2f want 500.00", po.DiscountTotal)
	}
}

func TestReconcileCorrectsStockAndLeavesSalesHistoryIntact(t *testing.T) {
	reset(t)
	fx := seedFixture(t, 100, 1000)

	if _, err := saleRepo.Checkout(context.Background(), &repository.CheckoutInput{
		StoreID:     sid(testutil.StoreID),
		PaymentType: models.PaymentCash,
		Items:       []repository.CheckoutItemInput{{BatchID: fx.BatchIDs[0], Quantity: 40}},
	}); err != nil {
		t.Fatal(err)
	}

	journal, items, err := reconRepo.Reconcile(context.Background(), testutil.StoreID, &repository.ReconcileInput{
		Notes: "monthly audit",
		Items: []repository.ReconcileItemInput{
			{BatchID: fx.BatchIDs[0], PhysicalCount: 55, Reason: "broken strips removed"},
		},
	})
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("reconcile items = %d want 1", len(items))
	}

	it := items[0]
	if it.SystemStock != 60 || it.PhysicalStock != 55 || it.VarianceQuantity != -5 {
		t.Errorf("variance math wrong: system=%d physical=%d variance=%d",
			it.SystemStock, it.PhysicalStock, it.VarianceQuantity)
	}
	if it.CostImpact != -50 {
		t.Errorf("cost impact = %.2f want -50.00 (5 x purchase 10)", it.CostImpact)
	}
	if journal.ID == "" {
		t.Error("journal not persisted")
	}

	batch, _ := medRepo.FindBatchByNumber(context.Background(), testutil.StoreID, fx.MedicineID, "FIX-B1")
	if batch.CurrentStock != 55 {
		t.Errorf("stock not force-corrected: %d want 55", batch.CurrentStock)
	}

	var invCount, itemCount int
	if err := pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM sales_invoices`).Scan(&invCount); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM sales_invoice_items`).Scan(&itemCount); err != nil {
		t.Fatal(err)
	}
	if invCount != 1 || itemCount != 1 {
		t.Errorf("historical sales corrupted by reconcile: invoices=%d items=%d", invCount, itemCount)
	}

	var totalQty int
	if err := pool.QueryRow(context.Background(),
		`SELECT SUM(quantity) FROM sales_invoice_items`).Scan(&totalQty); err != nil {
		t.Fatal(err)
	}
	if totalQty != 40 {
		t.Errorf("historical sold quantity changed: %d want 40", totalQty)
	}
}

func TestReportsEndToEnd(t *testing.T) {
	reset(t)
	fx := seedFixture(t, 200, 10000)

	ctx := context.Background()
	if _, err := saleRepo.Checkout(ctx, &repository.CheckoutInput{
		StoreID:     sid(testutil.StoreID),
		PaymentType: models.PaymentCash,
		Items:       []repository.CheckoutItemInput{{BatchID: fx.BatchIDs[0], Quantity: 4}},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := saleRepo.Checkout(ctx, &repository.CheckoutInput{
		StoreID:     sid(testutil.StoreID),
		PaymentType: models.PaymentCredit,
		CustomerID:  &fx.CustomerID,
		Items:       []repository.CheckoutItemInput{{BatchID: fx.BatchIDs[0], Quantity: 2}},
	}); err != nil {
		t.Fatal(err)
	}

	now := time.Now().UTC()
	start := now.AddDate(0, 0, -1)

	sales, err := reportRepo.Sales(ctx, testutil.StoreID, start, now.AddDate(0, 0, 1))
	if err != nil {
		t.Fatalf("sales report: %v", err)
	}
	if sales.NetSales != 90 || sales.NetUnits != 6 {
		t.Errorf("sales summary: net=%.2f units=%d want 90/6", sales.NetSales, sales.NetUnits)
	}

	purchases, err := reportRepo.Purchases(ctx, testutil.StoreID, start, now.AddDate(0, 0, 1))
	if err != nil {
		t.Fatalf("purchase report: %v", err)
	}
	if purchases.OrderCount != 1 || purchases.ItemCount != 1 || purchases.TotalSpend != 2000 {
		t.Errorf("purchase summary: orders=%d items=%d spend=%.2f want 1/1/2000.00",
			purchases.OrderCount, purchases.ItemCount, purchases.TotalSpend)
	}
	if len(purchases.Suppliers) != 1 || purchases.Suppliers[0].SupplierName != "Fixture Supplier" {
		t.Errorf("supplier breakdown wrong: %+v", purchases.Suppliers)
	}

	pl, err := reportRepo.ProfitLoss(ctx, testutil.StoreID, start, now.AddDate(0, 0, 1))
	if err != nil {
		t.Fatalf("p&l report: %v", err)
	}
	if pl.Revenue != 90 || pl.Cost != 60 || pl.Profit != 30 {
		t.Errorf("p&l: revenue=%.2f cost=%.2f profit=%.2f want 90/60/30", pl.Revenue, pl.Cost, pl.Profit)
	}

	expiryBatches, err := reportRepo.Expiry(ctx, testutil.StoreID, 24)
	if err != nil {
		t.Fatalf("expiry report: %v", err)
	}
	found := false
	for _, b := range expiryBatches {
		if b.BatchID == fx.BatchIDs[0] && b.CurrentStock == 194 {
			found = true
		}
	}
	if !found {
		t.Errorf("expiring batch missing or wrong stock")
	}

	low, err := reportRepo.LowStock(ctx, testutil.StoreID)
	if err != nil {
		t.Fatalf("low stock report: %v", err)
	}
	for _, it := range low {
		if it.MedicineID == fx.MedicineID && it.TotalStock >= it.MinReorderLevel {
			t.Errorf("healthy item flagged low-stock: %+v", it)
		}
	}
}

func errorsAs(err error, target any) bool {
	if err == nil {
		return false
	}
	switch e := target.(type) {
	case **models.InsufficientStockError:
		if se, ok := err.(*models.InsufficientStockError); ok {
			*e = se
			return true
		}
	case **models.CreditLimitExceededError:
		if ce, ok := err.(*models.CreditLimitExceededError); ok {
			*e = ce
			return true
		}
	case **models.ValidationError:
		if ve, ok := err.(*models.ValidationError); ok {
			*e = ve
			return true
		}
	}
	return false
}

func assertCreditError(err error) bool {
	var ce *models.CreditLimitExceededError
	return errorsAs(err, &ce)
}

func sid(storeID string) *string { return &storeID }
