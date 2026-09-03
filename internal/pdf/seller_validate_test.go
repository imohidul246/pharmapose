package pdf

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/mohi/pms-marg-inspired/internal/repository"
)

// validDetail returns a minimal invoice detail for generation tests.
func validDetail() repository.SalesInvoiceDetail {
	return repository.SalesInvoiceDetail{
		Invoice: baseInvoice(),
		Items:   []repository.SalesInvoiceItemDetail{sampleItem("Dolo 650", "30049099", 5, 2.5, 2.5, 0)},
	}
}

// TestSellerValidateComplete accepts a fully configured seller block.
func TestSellerValidateComplete(t *testing.T) {
	s := SellerInfo{
		Name: "ABC Pharmacy", GSTIN: "18ABCDE1234F1Z5",
		StateCode: "18", StateName: "Assam",
	}
	if err := s.Validate(); err != nil {
		t.Fatalf("complete seller rejected: %v", err)
	}
}

// TestSellerValidateMissingFields names every absent mandatory field and
// matches the sentinel, so handlers can map it to a clean 4xx.
func TestSellerValidateMissingFields(t *testing.T) {
	err := SellerInfo{}.Validate()
	if !errors.Is(err, ErrSellerIncomplete) {
		t.Fatalf("empty seller err = %v want ErrSellerIncomplete", err)
	}
	for _, want := range []string{"trade name", "GSTIN", "state code"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q must name missing %q", err, want)
		}
	}

	// A state NAME satisfies the state requirement when no code is stored.
	partial := SellerInfo{Name: "ABC", GSTIN: "18X", StateName: "Assam"}
	if err := partial.Validate(); err != nil {
		t.Errorf("state name should satisfy state requirement: %v", err)
	}
}

// TestGeneratePDFRejectsIncompleteSeller proves generation fails fast with a
// validation error (no half-written output, no panic) when the store profile
// or business registration is missing.
func TestGeneratePDFRejectsIncompleteSeller(t *testing.T) {
	d := InvoiceData{
		Invoice: validDetail(),
		Seller:  SellerInfo{},
		Buyer:   BuyerInfo{Name: "Hosp"},
	}
	var buf bytes.Buffer
	err := GenerateInvoicePDF(&buf, d)
	if !errors.Is(err, ErrSellerIncomplete) {
		t.Fatalf("GenerateInvoicePDF err = %v want ErrSellerIncomplete", err)
	}
	if buf.Len() != 0 {
		t.Errorf("no bytes may be written on validation failure, got %d", buf.Len())
	}
}

// TestGeneratePDFCompleteSellerSucceeds pins the happy path: embedded fonts
// load from memory and a valid seller block produces a real PDF.
func TestGeneratePDFCompleteSellerSucceeds(t *testing.T) {
	d := InvoiceData{
		Invoice: validDetail(),
		Seller: SellerInfo{
			Name: "ABC Pharmacy", Address: "Guwahati",
			GSTIN: "18ABCDE1234F1Z5", PAN: "ABCDE1234F",
			Phone: "+91 98765 43210", StateCode: "18", StateName: "Assam",
		},
		Buyer: BuyerInfo{Name: "City Hospital", GSTIN: "18XXXXX0000X1Z5", Address: "Delhi"},
	}
	var buf bytes.Buffer
	if err := GenerateInvoicePDF(&buf, d); err != nil {
		t.Fatalf("GenerateInvoicePDF: %v", err)
	}
	if !bytes.HasPrefix(buf.Bytes(), []byte("%PDF")) {
		t.Fatal("output does not start with %PDF magic")
	}
}

// TestEmbeddedFontsPresent ensures the singleton font buffers are non-empty
// so no request ever falls back to disk reads on a normal build.
func TestEmbeddedFontsPresent(t *testing.T) {
	if len(fontRegularTTF) == 0 {
		t.Error("embedded NotoSans-Regular.ttf is empty")
	}
	if len(fontBoldTTF) == 0 {
		t.Error("embedded NotoSans-Bold.ttf is empty")
	}
}
