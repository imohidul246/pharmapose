package handlers

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/mohi/pms-marg-inspired/internal/models"
	"github.com/mohi/pms-marg-inspired/internal/repository"
	"github.com/mohi/pms-marg-inspired/internal/testutil"
)

// seedB2BInvoice stocks one batch and raises a B2B invoice against it,
// returning the invoice ID for PDF tests.
func seedB2BInvoice(t *testing.T) string {
	t.Helper()
	ctx := context.Background()

	medRepo := repository.NewMedicineRepo(testPoolDB, testutil.StoreID)
	m := &models.Medicine{Name: "B2B PDF Med", SaltComposition: "Rx",
		Manufacturer: "PDFPharma", MinReorderLevel: 5}
	if err := medRepo.Create(ctx, m); err != nil {
		t.Fatalf("create medicine: %v", err)
	}
	purchRepo := repository.NewPurchaseRepo(testPoolDB)
	if _, _, err := purchRepo.CreateInward(ctx, &repository.PurchaseInput{
		InvoiceNo:    fmt.Sprintf("PDF-IN-%d", time.Now().UnixNano()),
		SupplierName: "PDF Supplier",
		StoreID:      testutil.StoreIDPtr(),
		Items: []repository.PurchaseItemInput{{
			MedicineID:    m.ID,
			BatchNumber:   "PDF-B1",
			ExpiryDate:    models.NewDate(time.Now().AddDate(2, 0, 0)),
			Quantity:      50,
			PurchasePrice: 100,
			SalePrice:     150,
		}},
	}); err != nil {
		t.Fatalf("inward: %v", err)
	}
	batch, err := medRepo.FindBatchByNumber(ctx, m.ID, "PDF-B1")
	if err != nil {
		t.Fatalf("find batch: %v", err)
	}

	saleRepo := repository.NewSaleRepo(testPoolDB)
	// Checksum-valid GSTIN (same fixture as the GSTR-1 regression tests).
	buyerGSTIN := "27AAAAA1111A1ZW"
	buyerName := "City Hospital"
	buyerAddr := "Mumbai"
	res, err := saleRepo.Checkout(ctx, &repository.CheckoutInput{
		PaymentType:  models.PaymentCash,
		StoreID:      testutil.StoreIDPtr(),
		SaleType:     "B2B",
		BuyerName:    &buyerName,
		BuyerGSTIN:   &buyerGSTIN,
		BuyerAddress: &buyerAddr,
		Items:        []repository.CheckoutItemInput{{BatchID: batch.ID, Quantity: 5}},
	})
	if err != nil {
		t.Fatalf("b2b checkout: %v", err)
	}
	return res.Invoice.ID
}

// TestB2BPDFRejectsIncompleteSeller proves a store without a GST
// registration gets a clean 400 (not a panic, not a corrupt PDF) when
// requesting a B2B invoice PDF.
func TestB2BPDFRejectsIncompleteSeller(t *testing.T) {
	ctx := context.Background()
	if _, err := testPoolDB.Exec(ctx,
		`UPDATE stores SET gst_registration_id = NULL WHERE id = $1`, testutil.StoreID); err != nil {
		t.Fatalf("unlink registration: %v", err)
	}
	invID := seedB2BInvoice(t)

	rec := doJSONAs(t, http.MethodGet, "/api/sales/invoices/"+invID+"/pdf", nil, ownerRawToken)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("pdf without seller registration = %d want 400, body %s",
			rec.Code, rec.Body.String())
	}
	if msg := errMessage(t, rec); msg == "" {
		t.Error("expected a descriptive validation error message")
	}
}

// TestB2BPDFStreamsWithCompleteSeller proves the happy path: with store +
// GST registration configured, the endpoint streams a real PDF document.
func TestB2BPDFStreamsWithCompleteSeller(t *testing.T) {
	ctx := context.Background()
	var bizID string
	if err := testPoolDB.QueryRow(ctx,
		`INSERT INTO businesses (legal_name, trade_name) VALUES ('PDF Biz', 'PDF Biz') RETURNING id::text`).Scan(&bizID); err != nil {
		t.Fatalf("insert business: %v", err)
	}
	var regID string
	if err := testPoolDB.QueryRow(ctx, `
		INSERT INTO gst_registrations (business_id, gstin, legal_name, trade_name, pan, state_code, state_name, address, is_active)
		VALUES ($1, '18ABCDE1234F1Z5', 'PDF Biz Pvt Ltd', 'PDF Biz', 'ABCDE1234F', '18', 'Assam', 'Guwahati', true)
		RETURNING id::text`, bizID).Scan(&regID); err != nil {
		t.Fatalf("insert registration: %v", err)
	}
	if _, err := testPoolDB.Exec(ctx,
		`UPDATE stores SET gst_registration_id = $1 WHERE id = $2`, regID, testutil.StoreID); err != nil {
		t.Fatalf("link registration: %v", err)
	}

	invID := seedB2BInvoice(t)
	rec := doJSONAs(t, http.MethodGet, "/api/sales/invoices/"+invID+"/pdf", nil, ownerRawToken)
	if rec.Code != http.StatusOK {
		t.Fatalf("pdf with complete seller = %d want 200, body %s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/pdf" {
		t.Errorf("content-type = %q want application/pdf", ct)
	}
	if !bytes.HasPrefix(rec.Body.Bytes(), []byte("%PDF")) {
		t.Error("response body is not a PDF document")
	}
}
