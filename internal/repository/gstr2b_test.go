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

// seed2BPurchase books a purchase for a known supplier so GSTR-2B documents
// can be reconciled against it.
func seed2BPurchase(t *testing.T, invoiceNo, gstin string, qty int, price float64) float64 {
	t.Helper()
	ctx := context.Background()
	in := &repository.PurchaseInput{
		StoreID:     sid(testutil.StoreID),
		InvoiceNo:     invoiceNo,
		SupplierName:  "2B Supplier",
		SupplierGSTIN: &gstin,
		InvoiceDate:   ptrStr(time.Now().Format("2006-01-02")),
		Items: []repository.PurchaseItemInput{{
			MedicineID:    mustSeedMedicine(t),
			BatchNumber:   "2B-" + invoiceNo,
			ExpiryDate:    models.NewDate(time.Now().AddDate(1, 0, 0)),
			Quantity:      qty,
			PurchasePrice: price,
			SalePrice:     price * 1.5,
		}},
	}
	if _, _, err := purchRepo.CreateInward(ctx, in); err != nil {
		t.Fatalf("seed 2B purchase: %v", err)
	}
	var grand float64
	if err := pool.QueryRow(ctx,
		`SELECT grand_total::float8 FROM purchase_orders WHERE invoice_no = $1`, invoiceNo).Scan(&grand); err != nil {
		t.Fatalf("read grand total: %v", err)
	}
	return grand
}

func TestGSTR2BImportReconcilesAndMatches(t *testing.T) {
	reset(t)
	ctx := context.Background()
	period := time.Now().Format("2006-01")
	docDate := time.Now().Format("2006-01-02")
	const gstin = "27AAECS9876F1ZS"

	grandA := seed2BPurchase(t, "2B-A-1", gstin, 10, 5.00)
	grandB := seed2BPurchase(t, "2B-B-1", gstin, 5, 7.00)

	repo := repository.NewGSTR2BRepo(pool)
	in := &models.GSTR2BImportInput{
		Period: period,
		Source: "2B-test-01.json",
		Docs: []models.GSTR2BDocInput{
			// Exact match.
			{SupplierGSTIN: gstin, DocType: "INV", InvoiceNo: "2B-A-1", InvoiceDate: docDate,
				TaxableValue: grandA, TotalValue: grandA},
			// Supplier reports a different value than our books.
			{SupplierGSTIN: gstin, DocType: "INV", InvoiceNo: "2B-B-1", InvoiceDate: docDate,
				TaxableValue: grandB + 100, TotalValue: grandB + 100},
			// Invoice that never existed in our purchases.
			{SupplierGSTIN: gstin, DocType: "INV", InvoiceNo: "2B-X-1", InvoiceDate: docDate,
				TaxableValue: 500, TotalValue: 500},
			// Credit note — not matched against the purchase ledger yet.
			{SupplierGSTIN: gstin, DocType: "CRN", InvoiceNo: "2B-A-1-CRN", InvoiceDate: docDate,
				TaxableValue: 20, TotalValue: 20},
		},
	}

	rec, err := repo.Import(ctx, testutil.StoreID, in)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if rec.TotalDocs != 4 || rec.Matched != 2 || rec.Unmatched != 2 || rec.AmountMismatch != 1 {
		t.Errorf("reconciliation = %+v want total 4 matched 2 unmatched 2 mismatch 1", rec)
	}

	batch, err := repo.GetBatch(ctx, testutil.StoreID, rec.BatchID)
	if err != nil {
		t.Fatalf("get batch: %v", err)
	}
	if batch.Status != "RECONCILED" || batch.DocCount != 4 || batch.MatchedCount != 2 || batch.UnmatchedCount != 2 {
		t.Errorf("batch counts = %+v", batch)
	}

	docs, err := repo.BatchDocs(ctx, testutil.StoreID, rec.BatchID)
	if err != nil {
		t.Fatalf("batch docs: %v", err)
	}
	if len(docs) != 4 {
		t.Fatalf("docs = %d want 4", len(docs))
	}

	byInvoice := map[string]models.GSTR2BImport{}
	for _, d := range docs {
		byInvoice[d.InvoiceNo+"|"+d.DocType] = d
	}
	if m := byInvoice["2B-A-1|INV"]; m.MatchStatus != "MATCHED" || m.MatchedPurchaseID == nil {
		t.Errorf("exact doc not matched: %+v", m)
	}
	if m := byInvoice["2B-B-1|INV"]; m.MatchStatus != "AMOUNT_MISMATCH" || m.MatchedDifference == nil ||
		m.MatchedPurchaseID == nil || (m.MatchedDifference != nil && *m.MatchedDifference < 100 && *m.MatchedDifference > -100) {
		t.Errorf("mismatch doc not flagged: %+v", m)
	}
	if m := byInvoice["2B-X-1|INV"]; m.MatchStatus != "UNMATCHED" || m.Notes == "" {
		t.Errorf("missing doc not unmatched: %+v", m)
	}
	if m := byInvoice["2B-A-1-CRN|CRN"]; m.MatchStatus != "UNMATCHED" {
		t.Errorf("credit note should stay unmatched: %+v", m)
	}
}

func TestGSTR2BReimportReplacesActiveDocuments(t *testing.T) {
	reset(t)
	ctx := context.Background()
	period := time.Now().Format("2006-01")
	docDate := time.Now().Format("2006-01-02")
	const gstin = "06AACDD3456G1ZP"

	grand := seed2BPurchase(t, "2B-R-1", gstin, 8, 12.50)
	repo := repository.NewGSTR2BRepo(pool)

	first, err := repo.Import(ctx, testutil.StoreID, &models.GSTR2BImportInput{
		Period: period, Source: "first.json",
		Docs: []models.GSTR2BDocInput{
			{SupplierGSTIN: gstin, DocType: "INV", InvoiceNo: "2B-R-1", InvoiceDate: docDate,
				TaxableValue: grand, TotalValue: grand},
			{SupplierGSTIN: gstin, DocType: "INV", InvoiceNo: "2B-R-2", InvoiceDate: docDate,
				TaxableValue: 999, TotalValue: 999},
		},
	})
	if err != nil {
		t.Fatalf("first import: %v", err)
	}

	// Same supplier + period with a corrected file containing only one doc.
	second, err := repo.Import(ctx, testutil.StoreID, &models.GSTR2BImportInput{
		Period: period, Source: "corrected.json",
		Docs: []models.GSTR2BDocInput{
			{SupplierGSTIN: gstin, DocType: "INV", InvoiceNo: "2B-R-1", InvoiceDate: docDate,
				TaxableValue: grand, TotalValue: grand},
		},
	})
	if err != nil {
		t.Fatalf("re-import: %v", err)
	}
	if second.BatchID == first.BatchID {
		t.Error("re-import must create a new batch")
	}

	var active int
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM gstr2b_imports WHERE supplier_gstin = $1 AND period = $2`,
		gstin, period).Scan(&active); err != nil {
		t.Fatal(err)
	}
	if active != 1 {
		t.Errorf("active documents after re-import = %d want 1 (old docs replaced)", active)
	}

	docs, err := repo.BatchDocs(ctx, testutil.StoreID, second.BatchID)
	if err != nil {
		t.Fatalf("batch docs: %v", err)
	}
	if len(docs) != 1 || docs[0].InvoiceNo != "2B-R-1" || docs[0].MatchStatus != "MATCHED" {
		t.Errorf("re-imported docs wrong: %+v", docs)
	}

	batches, err := repo.ListBatches(ctx, testutil.StoreID)
	if err != nil {
		t.Fatalf("list batches: %v", err)
	}
	if len(batches) != 2 {
		t.Errorf("batches = %d want 2 (history kept)", len(batches))
	}
}

func TestGSTR2BImportValidation(t *testing.T) {
	reset(t)
	ctx := context.Background()
	period := time.Now().Format("2006-01")
	docDate := time.Now().Format("2006-01-02")
	repo := repository.NewGSTR2BRepo(pool)

	cases := []struct {
		name string
		in   *models.GSTR2BImportInput
	}{
		{"no docs", &models.GSTR2BImportInput{Period: period}},
		{"bad period", &models.GSTR2BImportInput{
			Period: "august", Docs: []models.GSTR2BDocInput{
				{SupplierGSTIN: "27AAECS9876F1ZS", DocType: "INV", InvoiceNo: "2B-V-1", InvoiceDate: docDate},
			}}},
		{"bad date", &models.GSTR2BImportInput{
			Period: period, Docs: []models.GSTR2BDocInput{
				{SupplierGSTIN: "27AAECS9876F1ZS", DocType: "INV", InvoiceNo: "2B-V-1", InvoiceDate: "yesterday"},
			}}},
		{"missing supplier", &models.GSTR2BImportInput{
			Period: period, Docs: []models.GSTR2BDocInput{
				{DocType: "INV", InvoiceNo: "2B-V-1", InvoiceDate: docDate},
			}}},
		{"duplicate docs", &models.GSTR2BImportInput{
			Period: period, Docs: []models.GSTR2BDocInput{
				{SupplierGSTIN: "27AAECS9876F1ZS", DocType: "INV", InvoiceNo: "2B-V-1", InvoiceDate: docDate},
				{SupplierGSTIN: "27AAECS9876F1ZS", DocType: "INV", InvoiceNo: "2B-V-1", InvoiceDate: docDate},
			}}},
	}
	for _, tc := range cases {
		if _, err := repo.Import(ctx, testutil.StoreID, tc.in); err == nil {
			t.Errorf("%s: want validation error, got nil", tc.name)
		}
	}
}

// mustSeedMedicine creates a medicine and returns its ID.
func mustSeedMedicine(t *testing.T) string {
	t.Helper()
	m := &models.Medicine{
		Name:         fmt.Sprintf("2B Med %d", time.Now().UnixNano()),
		Manufacturer: "2B Pharma",
	}
	if err := medRepo.Create(context.Background(), testutil.StoreID, m); err != nil {
		t.Fatalf("create medicine: %v", err)
	}
	return m.ID
}

func ptrStr(s string) *string { return &s }
