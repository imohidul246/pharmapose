package gst_test

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mohi/pms-marg-inspired/internal/database"
	"github.com/mohi/pms-marg-inspired/internal/gst"
	"github.com/mohi/pms-marg-inspired/internal/models"
	"github.com/mohi/pms-marg-inspired/internal/repository"
	"github.com/mohi/pms-marg-inspired/internal/testutil"
)

var (
	pool      *pgxpool.Pool
	medRepo   *repository.MedicineRepo
	saleRepo  *repository.SaleRepo
	purchRepo *repository.PurchaseRepo
	builder   *gst.GSTR1Builder
)

func TestMain(m *testing.M) {
	ctx := context.Background()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		url = "postgres://postgres:postgres@localhost:5432/pms_test?sslmode=disable"
	}

	var err error
	pool, err = database.Connect(ctx, url)
	if err != nil {
		fmt.Fprintf(os.Stderr, "connect test db: %v\n", err)
		os.Exit(1)
	}
	if err := database.Migrate(ctx, pool); err != nil {
		fmt.Fprintf(os.Stderr, "migrate: %v\n", err)
		os.Exit(1)
	}
	if err := testutil.SeedStore(ctx, pool); err != nil {
		fmt.Fprintf(os.Stderr, "seed store: %v\n", err)
		os.Exit(1)
	}

	medRepo = repository.NewMedicineRepo(pool, testutil.StoreID)
	saleRepo = repository.NewSaleRepo(pool)
	purchRepo = repository.NewPurchaseRepo(pool)
	builder = gst.NewGSTR1Builder(pool)

	code := m.Run()
	pool.Close()
	os.Exit(code)
}

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

// seedGSTMedicine creates a medicine with 12% GST (6% CGST + 6% SGST) HSN 3004.
func seedGSTMedicine(t *testing.T) (medicineID string, batchID string) {
	t.Helper()
	ctx := context.Background()

	m := &models.Medicine{
		Name:            "GSTR1 Paracetamol 500mg",
		SaltComposition: "Paracetamol 500mg",
		Manufacturer:    "GSTPharma",
		MinReorderLevel: 10,
		Packing:         "Strip of 10",
		UQC:             "TAB",
	}
	if err := medRepo.Create(ctx, m); err != nil {
		t.Fatalf("create medicine: %v", err)
	}
	medicineID = m.ID

	var hsnCount int
	pool.QueryRow(ctx, `SELECT COUNT(*) FROM hsn_codes WHERE code = '3004' AND store_id = $1`, testutil.StoreID).Scan(&hsnCount)
	if hsnCount == 0 {
		_, err := pool.Exec(ctx, `
			INSERT INTO hsn_codes (store_id, code, description) VALUES
				($1, '3004', 'Medicaments for therapeutic or prophylactic uses, packed for retail sale')
			ON CONFLICT (store_id, code) DO NOTHING`, testutil.StoreID)
		if err != nil {
			t.Fatalf("seed hsn: %v", err)
		}
	}

	var taxRateCount int
	pool.QueryRow(ctx, `SELECT COUNT(*) FROM tax_rates WHERE store_id = $1`, testutil.StoreID).Scan(&taxRateCount)
	if taxRateCount == 0 {
		_, err := pool.Exec(ctx, `
			INSERT INTO tax_rates (store_id, hsn_code_id, gst_rate, cgst_rate, sgst_rate, igst_rate, cess_rate, effective_from)
			SELECT $1, h.id, 12.00, 6.00, 6.00, 12.00, 0.00, '2017-07-01'::date
			FROM hsn_codes h WHERE h.code = '3004'
			ON CONFLICT DO NOTHING`, testutil.StoreID)
		if err != nil {
			t.Fatalf("seed tax_rates: %v", err)
		}
	}

	_, err := pool.Exec(ctx, `
		INSERT INTO medicine_tax_config (store_id, medicine_id, hsn_code_id, tax_rate_id, price_includes_tax, effective_from)
		SELECT $1, $2, h.id, tr.id, false, '2017-07-01'::date
		FROM hsn_codes h
		JOIN tax_rates tr ON tr.hsn_code_id = h.id
		WHERE h.code = '3004' AND tr.effective_to IS NULL
		ON CONFLICT DO NOTHING`, testutil.StoreID, medicineID)
	if err != nil {
		t.Fatalf("link tax config: %v", err)
	}

	in := &repository.PurchaseInput{
		InvoiceNo:    "GSTR1-INWARD-1",
		SupplierName: "GSTR1 Supplier",
		StoreID:      testutil.StoreIDPtr(),
		Items: []repository.PurchaseItemInput{{
			MedicineID:    medicineID,
			BatchNumber:   "GSTR1-B1",
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

	batch, err := medRepo.FindBatchByNumber(ctx, medicineID, "GSTR1-B1")
	if err != nil {
		t.Fatalf("find batch: %v", err)
	}
	batchID = batch.ID
	return medicineID, batchID
}

func TestBuildGSTR1_B2BIntraState(t *testing.T) {
	reset(t)
	_, batchID := seedGSTMedicine(t)
	ctx := context.Background()

	var sid string = testutil.StoreID

	// B2B: same state → intra-state
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

	// Set customer_gstin on the invoice to make it a B2B invoice
	_, err = pool.Exec(ctx, `
		UPDATE sales_invoices SET customer_gstin = '27AABCU9603R1ZM' WHERE id = $1`, res.Invoice.ID)
	if err != nil {
		t.Fatalf("update gstin: %v", err)
	}

	start := time.Now().Truncate(24 * time.Hour)
	end := start.AddDate(0, 1, 0)
	gstr, err := builder.BuildGSTR1(ctx, gst.GSTR1Request{
		StoreID:   sid,
		GSTIN:     "27AABCU9603R1ZM",
		StartDate: start,
		EndDate:   end,
	})
	if err != nil {
		t.Fatalf("build GSTR1: %v", err)
	}

	if len(gstr.B2B) != 1 {
		t.Fatalf("B2B entries = %d want 1", len(gstr.B2B))
	}

	entry := gstr.B2B[0]
	if entry.Ctin != "27AABCU9603R1ZM" {
		t.Errorf("recipient gstin (ctin) = %s want 27AABCU9603R1ZM", entry.Ctin)
	}
	if len(entry.Inv) != 1 {
		t.Fatalf("B2B invoices = %d want 1", len(entry.Inv))
	}

	b2b := entry.Inv[0]
	if b2b.Inum == "" {
		t.Error("inum is empty")
	}
	if b2b.Idt == "" {
		t.Error("idt is empty")
	}
	// DD-MM-YYYY required by GSTN.
	if _, err := time.Parse("02-01-2006", b2b.Idt); err != nil {
		t.Errorf("idt %q is not DD-MM-YYYY", b2b.Idt)
	}
	if b2b.InvTyp != "R" {
		t.Errorf("inv_typ = %q want R", b2b.InvTyp)
	}
	if b2b.Rchrg != "N" {
		t.Errorf("rchrg = %q want N", b2b.Rchrg)
	}
	// 10 × 150 = 1500
	if b2b.Val != 1680 {
		t.Errorf("val = %v want 1680", b2b.Val)
	}
	if b2b.Pos != "27" {
		t.Errorf("pos = %s want 27", b2b.Pos)
	}
	if len(b2b.Itms) != 1 {
		t.Fatalf("items = %d want 1", len(b2b.Itms))
	}
	item := b2b.Itms[0]
	if item.Num != 1 {
		t.Errorf("item num = %d want 1", item.Num)
	}
	if item.ItmDet.Txval != 1500 {
		t.Errorf("txval = %v want 1500", item.ItmDet.Txval)
	}
	if item.ItmDet.Rt != 12 {
		t.Errorf("rt = %v want 12", item.ItmDet.Rt)
	}
	// CGST = 6% of 1500 = 90, SGST = 90
	if item.ItmDet.Camt != 90 {
		t.Errorf("camt = %v want 90", item.ItmDet.Camt)
	}
	if item.ItmDet.Samt != 90 {
		t.Errorf("samt = %v want 90", item.ItmDet.Samt)
	}
	if item.ItmDet.Iamt != 0 {
		t.Errorf("iamt = %v want 0", item.ItmDet.Iamt)
	}
	if len(gstr.B2CL) != 0 {
		t.Errorf("b2cl should be empty, got %d", len(gstr.B2CL))
	}

	// Period-level metadata.
	if gstr.Fp != fmt.Sprintf("%02d%d", int(start.Month()), start.Year()) {
		t.Errorf("fp = %s want %s", gstr.Fp, fmt.Sprintf("%02d%d", int(start.Month()), start.Year()))
	}
	if gstr.Gt != 1680 {
		t.Errorf("gt = %v want 1680", gstr.Gt)
	}
	if gstr.CurtGt != 1680 {
		t.Errorf("cur_gt = %v want 1680", gstr.CurtGt)
	}
}

func TestBuildGSTR1_B2CS(t *testing.T) {
	reset(t)
	_, batchID := seedGSTMedicine(t)
	ctx := context.Background()

	// B2C: no GSTIN, intra-state, small amount (< 250000)
	pos := "27"
	_, err := saleRepo.Checkout(ctx, &repository.CheckoutInput{
		PaymentType:   models.PaymentCash,
		StoreID:       testutil.StoreIDPtr(),
		PlaceOfSupply: &pos,
		Items:         []repository.CheckoutItemInput{{BatchID: batchID, Quantity: 5}},
	})
	if err != nil {
		t.Fatalf("checkout: %v", err)
	}

	start := time.Now().Truncate(24 * time.Hour)
	end := start.AddDate(0, 1, 0)
	gstr, err := builder.BuildGSTR1(ctx, gst.GSTR1Request{
		StartDate: start,
		EndDate:   end,
	})
	if err != nil {
		t.Fatalf("build GSTR1: %v", err)
	}

	if len(gstr.B2CS) != 1 {
		t.Fatalf("B2CS items = %d want 1", len(gstr.B2CS))
	}
	b2cs := gstr.B2CS[0]
	if b2cs.Pos != "27" {
		t.Errorf("pos = %s want 27", b2cs.Pos)
	}
	// 5 × 150 = 750
	if b2cs.Txval != 750 {
		t.Errorf("txval = %v want 750", b2cs.Txval)
	}
	if b2cs.SplyTy != "INTRA" {
		t.Errorf("sply_ty = %s want INTRA", b2cs.SplyTy)
	}
	if b2cs.Rt != 12 {
		t.Errorf("rt = %v want 12", b2cs.Rt)
	}
}

func TestBuildGSTR1_HSN(t *testing.T) {
	reset(t)
	_, batchID := seedGSTMedicine(t)
	ctx := context.Background()

	pos := "27"
	_, err := saleRepo.Checkout(ctx, &repository.CheckoutInput{
		PaymentType:   models.PaymentCash,
		StoreID:       testutil.StoreIDPtr(),
		PlaceOfSupply: &pos,
		Items:         []repository.CheckoutItemInput{{BatchID: batchID, Quantity: 8}},
	})
	if err != nil {
		t.Fatalf("checkout: %v", err)
	}

	start := time.Now().Truncate(24 * time.Hour)
	end := start.AddDate(0, 1, 0)
	gstr, err := builder.BuildGSTR1(ctx, gst.GSTR1Request{
		StartDate: start,
		EndDate:   end,
	})
	if err != nil {
		t.Fatalf("build GSTR1: %v", err)
	}

	// B2C checkout (no GSTIN) lands in the Table 12 HSN summary.
	if len(gstr.Hsn.Data) != 1 {
		t.Fatalf("HSN summaries = %d want 1", len(gstr.Hsn.Data))
	}
	hsn := gstr.Hsn.Data[0]
	if hsn.HSNCode != "3004" {
		t.Errorf("hsn_sc = %s want 3004", hsn.HSNCode)
	}
	if hsn.Qty != 8 {
		t.Errorf("qty = %v want 8", hsn.Qty)
	}
	// 8 × 150 = 1200
	if hsn.Txval != 1200 {
		t.Errorf("txval = %v want 1200", hsn.Txval)
	}
	if hsn.Camt != 72 {
		t.Errorf("camt = %v want 72", hsn.Camt)
	}
	if hsn.Samt != 72 {
		t.Errorf("samt = %v want 72", hsn.Samt)
	}
	if hsn.UQC != "TAB" {
		t.Errorf("uqc = %s want TAB", hsn.UQC)
	}
	if hsn.Desc == "" {
		t.Error("desc should be populated from hsn_codes")
	}
}

func TestBuildGSTR1_DocSummary(t *testing.T) {
	reset(t)
	_, batchID := seedGSTMedicine(t)
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		_, err := saleRepo.Checkout(ctx, &repository.CheckoutInput{
			PaymentType: models.PaymentCash,
			StoreID:     testutil.StoreIDPtr(),
			Items:       []repository.CheckoutItemInput{{BatchID: batchID, Quantity: 1}},
		})
		if err != nil {
			t.Fatalf("checkout %d: %v", i, err)
		}
	}

	start := time.Now().Truncate(24 * time.Hour)
	end := start.AddDate(0, 1, 0)
	gstr, err := builder.BuildGSTR1(ctx, gst.GSTR1Request{
		StartDate: start,
		EndDate:   end,
	})
	if err != nil {
		t.Fatalf("build GSTR1: %v", err)
	}

	if len(gstr.DocIssue.DocDet) != 1 {
		t.Fatalf("document series = %d want 1", len(gstr.DocIssue.DocDet))
	}
	invDet := gstr.DocIssue.DocDet[0]
	if invDet.DocTyp != "Invoices for outward supply" {
		t.Errorf("doc_typ = %s want Invoices for outward supply", invDet.DocTyp)
	}
	var issued float64
	for _, doc := range invDet.Docs {
		issued += doc.TotNum
	}
	if issued != 3 {
		t.Errorf("issued = %v want 3", issued)
	}
	for _, doc := range invDet.Docs {
		if doc.NetIssue != doc.TotNum {
			t.Errorf("net_issue %v != totnum %v (no cancelled documents)", doc.NetIssue, doc.TotNum)
		}
		if doc.From == "" || doc.To == "" {
			t.Error("document series from/to must not be empty")
		}
	}
}

func TestBuildGSTR1_EmptyRange(t *testing.T) {
	reset(t)
	ctx := context.Background()

	start := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2020, 2, 1, 0, 0, 0, 0, time.UTC)
	gstr, err := builder.BuildGSTR1(ctx, gst.GSTR1Request{
		StartDate: start,
		EndDate:   end,
	})
	if err != nil {
		t.Fatalf("build GSTR1: %v", err)
	}
	if len(gstr.B2B) != 0 {
		t.Errorf("b2b should be empty, got %d", len(gstr.B2B))
	}
	if len(gstr.B2CS) != 0 {
		t.Errorf("b2cs should be empty, got %d", len(gstr.B2CS))
	}
	if len(gstr.Hsn.Data) != 0 {
		t.Errorf("hsn should be empty, got %d", len(gstr.Hsn.Data))
	}
	if len(gstr.DocIssue.DocDet) != 0 {
		t.Errorf("doc_issue should be empty, got %d", len(gstr.DocIssue.DocDet))
	}
	if gstr.Gt != 0 {
		t.Errorf("gt = %v want 0", gstr.Gt)
	}
}

func TestPreviewSummary(t *testing.T) {
	reset(t)
	_, batchID := seedGSTMedicine(t)
	ctx := context.Background()

	pos := "27"
	_, err := saleRepo.Checkout(ctx, &repository.CheckoutInput{
		PaymentType:   models.PaymentCash,
		StoreID:       testutil.StoreIDPtr(),
		PlaceOfSupply: &pos,
		Items:         []repository.CheckoutItemInput{{BatchID: batchID, Quantity: 10}},
	})
	if err != nil {
		t.Fatalf("checkout: %v", err)
	}

	start := time.Now().Truncate(24 * time.Hour)
	end := start.AddDate(0, 1, 0)
	summary, err := builder.PreviewSummary(ctx, gst.GSTR1Request{
		StartDate: start,
		EndDate:   end,
	})
	if err != nil {
		t.Fatalf("preview: %v", err)
	}

	// 10 × 150 = 1500
	if summary.TaxableValue != 1500 {
		t.Errorf("taxable_value = %.0f want 1500", summary.TaxableValue)
	}
	if summary.CGSTTotal != 90 {
		t.Errorf("cgst_total = %.0f want 90", summary.CGSTTotal)
	}
	if summary.SGSTTotal != 90 {
		t.Errorf("sgst_total = %.0f want 90", summary.SGSTTotal)
	}
	if summary.B2CCount != 1 {
		t.Errorf("b2c_count = %d want 1", summary.B2CCount)
	}
	if summary.B2BCount != 0 {
		t.Errorf("b2b_count = %d want 0", summary.B2BCount)
	}
}
