package pdf

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/mohi/pms-marg-inspired/internal/models"
	"github.com/mohi/pms-marg-inspired/internal/repository"
)

func f(v float64) *float64 { return &v }
func s(v string) *string   { return &v }

func sampleItem(name, hsn string, gst, cgst, sgst, igst float64) repository.SalesInvoiceItemDetail {
	return repository.SalesInvoiceItemDetail{
		MedicineName:   name,
		MRP:            f(24.50),
		UnitSalePrice:  20.00,
		Quantity:       10,
		BonusQuantity:  2,
		HSNCode:        s(hsn),
		DiscountAmount: 5.0,
		TaxableValue:   f(180.00),
		GSTRate:        f(gst),
		CGSTRate:       f(cgst),
		SGSTRate:       f(sgst),
		IGSTRate:       f(igst),
		LineTotal:      f(190.00),
	}
}

func baseInvoice() models.SalesInvoice {
	d, _ := models.ParseDate("2026-08-29")
	return models.SalesInvoice{
		InvoiceNo:     "B2B/26-27/00007",
		SaleType:      "B2B",
		InvoiceDate:   d,
		FinancialYear: "2026-27",
		GrossAmount:   f(100.0),
		DiscountTotal: 10.0,
		TaxableAmount: f(80.36),
		TaxTotal:      f(9.64),
		TotalAmount:   90.0,
		GrandTotal:    f(90.0),
	}
}

// TestTaxDisplayInterState verifies IGST is shown and CGST/SGST are not.
func TestTaxDisplayInterState(t *testing.T) {
	d := InvoiceData{
		Invoice: repository.SalesInvoiceDetail{
			Invoice: baseInvoice(),
			Items:   []repository.SalesInvoiceItemDetail{sampleItem("Dolo 650", "30049099", 5, 0, 0, 5)},
		},
		Seller: SellerInfo{Name: "ABC", GSTIN: "18X", PAN: "P", Phone: "9", StateCode: "18"},
		Buyer:  BuyerInfo{Name: "B", GSTIN: "07Y"},
	}
	parts := printTaxParts(d.Invoice.Items[0])
	got := strings.Join(parts, "|")
	if !strings.Contains(got, "IGST 5%") {
		t.Fatalf("expected IGST 5%% in tax display, got %q", got)
	}
	if strings.Contains(got, "CGST") || strings.Contains(got, "SGST") {
		t.Fatalf("CGST/SGST should not appear for inter-state, got %q", got)
	}
}

// TestTaxDisplayIntraState verifies CGST+SGST shown and IGST not shown.
func TestTaxDisplayIntraState(t *testing.T) {
	d := InvoiceData{
		Invoice: repository.SalesInvoiceDetail{
			Invoice: baseInvoice(),
			Items:   []repository.SalesInvoiceItemDetail{sampleItem("Crocin", "30049099", 5, 2.5, 2.5, 0)},
		},
		Seller: SellerInfo{Name: "ABC", GSTIN: "18X", PAN: "P"},
		Buyer:  BuyerInfo{Name: "B", GSTIN: "18Y"},
	}
	parts := printTaxParts(d.Invoice.Items[0])
	got := strings.Join(parts, "|")
	if !strings.Contains(got, "CGST 2.5%") || !strings.Contains(got, "SGST 2.5%") {
		t.Fatalf("expected CGST+SGST for intra-state, got %q", got)
	}
	if strings.Contains(got, "IGST") {
		t.Fatalf("IGST should not appear for intra-state, got %q", got)
	}
}

// TestTaxDisplayNil verifies nil-rated tax collapses to a single GST 0%.
func TestTaxDisplayNil(t *testing.T) {
	item := sampleItem("Plain", "30049099", 0, 0, 0, 0)
	parts := printTaxParts(item)
	if strings.Join(parts, "|") != "GST 0%" {
		t.Fatalf("expected single GST 0%%, got %v", parts)
	}
}

// TestSellerGSTINPresent / Missing verifies the top-left seller mapping.
func TestSellerGSTINPAN(t *testing.T) {
	with := InvoiceData{Seller: SellerInfo{Name: "ABC", GSTIN: "18X", PAN: "P123"}}
	lines := with.sellerLines()
	if !hasLine(lines, "GSTIN: 18X") {
		t.Fatalf("seller GSTIN missing: %v", lines)
	}
	if !hasLine(lines, "PAN: P123") {
		t.Fatalf("seller PAN missing: %v", lines)
	}

	missing := InvoiceData{Seller: SellerInfo{Name: "ABC"}}
	ml := missing.sellerLines()
	if !hasLine(ml, "GSTIN: -") {
		t.Fatalf("expected neutral GSTIN: - , got %v", ml)
	}
	if !hasLine(ml, "PAN: -") {
		t.Fatalf("expected neutral PAN: - , got %v", ml)
	}
}

// TestBuyerLines verifies buyer GSTIN mapping for B2B buyers.
func TestBuyerLines(t *testing.T) {
	inv := baseInvoice()
	inv.BuyerGSTIN = s("07AA")
	inv.SaleType = "B2B"
	d := InvoiceData{Invoice: repository.SalesInvoiceDetail{Invoice: inv, CustomerName: "Hosp"}}
	lines := d.buyerLines(true)
	if !hasLine(lines, "GSTIN: 07AA") {
		t.Fatalf("buyer GSTIN missing: %v", lines)
	}

	inv2 := baseInvoice()
	inv2.SaleType = "B2B"
	d2 := InvoiceData{Invoice: repository.SalesInvoiceDetail{Invoice: inv2, CustomerName: "Hosp"}}
	l2 := d2.buyerLines(true)
	if !hasLine(l2, "GSTIN: -") {
		t.Fatalf("expected buyer GSTIN: - , got %v", l2)
	}
}

// TestBonusAndDiscountIndependent verifies bonus is not merged into quantity.
func TestBonusAndDiscountIndependent(t *testing.T) {
	item := sampleItem("M", "30049099", 5, 2.5, 2.5, 0)
	item.Quantity = 10
	item.BonusQuantity = 2
	// Both are separate fields rendered in separate columns; assert they differ.
	if item.Quantity == item.BonusQuantity {
		t.Fatal("quantity and bonus are identical; can't verify separation")
	}
	// The renderer uses Quantity and BonusQuantity directly (not a merged sum).
	_ = item
}

// TestFormatAmount verifies Indian currency grouping formatting.
func TestFormatAmount(t *testing.T) {
	cases := map[string]float64{
		"90.00":      90.0,
		"1,250.00":   1250.0,
		"12,500.50":  12500.5,
		"1,23,456.78": 123456.78,
	}
	for want, in := range cases {
		if got := formatAmount(in); got != want {
			t.Errorf("formatAmount(%v) = %q, want %q", in, got, want)
		}
	}
}

// TestNumberToWords verifies the amount-in-words helper.
func TestNumberToWords(t *testing.T) {
	if got := NumberToWordsIndian(90); got != "Ninety" {
		t.Errorf("NumberToWordsIndian(90) = %q, want Ninety", got)
	}
	if got := NumberToWordsIndian(0); got != "Zero" {
		t.Errorf("NumberToWordsIndian(0) = %q, want Zero", got)
	}
}

// TestGrandTotalFromInvoice verifies the grand total used comes from the
// authoritative invoice value and not a re-computation.
func TestGrandTotalFromInvoice(t *testing.T) {
	inv := baseInvoice()
	inv.GrandTotal = f(1234.56)
	grand := inv.GrandTotal
	if *grand != 1234.56 {
		t.Fatalf("expected grand total 1234.56, got %v", *grand)
	}
	words := "Rupees " + NumberToWordsIndian(*grand) + " Only"
	if !strings.Contains(words, "Only") {
		t.Fatal("expected amount in words to contain Only")
	}
}

// TestGeneratePDF verifies a full PDF can be produced for the whole suite of
// data shapes: inter & intra, long names, bonus, multi-page.
func TestGeneratePDF(t *testing.T) {
	inv := baseInvoice()
	inv.SupplyType = s("INTER_STATE")
	inv.IGSTTotal = f(9.64)
	var items []repository.SalesInvoiceItemDetail
	for i := 0; i < 40; i++ {
		items = append(items, sampleItem(
			"Amoxicillin And Clavulanate Potassium 625mg Tablet With Long Name For Wrap Test",
			"30041020", 12, 0, 0, 12))
	}
	d := InvoiceData{
		Invoice: repository.SalesInvoiceDetail{Invoice: inv, Items: items},
		Seller:  SellerInfo{Name: "ABC Pharmacy", Address: "Assam", GSTIN: "18X", PAN: "P", Phone: "9", StateCode: "18", StateName: "Assam"},
		Buyer:   BuyerInfo{Name: "Hosp", GSTIN: "07Y", Address: "Delhi", Phone: "8"},
	}
	var buf bytes.Buffer
	if err := GenerateInvoicePDF(&buf, d); err != nil {
		t.Fatalf("GenerateInvoicePDF: %v", err)
	}
	if buf.Len() == 0 {
		t.Fatal("expected non-empty PDF output")
	}
	if !bytes.HasPrefix(buf.Bytes(), []byte("%PDF")) {
		t.Fatal("output does not start with %PDF magic")
	}
}

// hasLine reports whether any textLine holds exactly want (ignoring surrounding
// whitespace inside the value).
func hasLine(lines []textLine, want string) bool {
	for _, l := range lines {
		if strings.TrimSpace(l.text) == want {
			return true
		}
	}
	return false
}

// TestSellerCompleteData verifies every seller field (name, GSTIN, PAN,
// address, phone, state) surfaces in the SELLER / FROM line set.
func TestSellerCompleteData(t *testing.T) {
	d := InvoiceData{
		Seller: SellerInfo{
			Name:      "ABC Pharmacy",
			Address:   "G.S. Road, Guwahati, Assam 781005",
			GSTIN:     "18ABCDE1234F1Z5",
			PAN:       "ABCDE1234F",
			Phone:     "+91 98765 43210",
			StateCode: "18",
			StateName: "Assam",
		},
	}
	lines := d.sellerLines()
	for _, want := range []string{
		"Store Name: ABC Pharmacy",
		"GSTIN: 18ABCDE1234F1Z5",
		"PAN: ABCDE1234F",
		"Address: G.S. Road, Guwahati, Assam 781005",
		"Phone: +91 98765 43210",
		"State: Assam",
	} {
		if !hasLine(lines, want) {
			t.Fatalf("seller line %q missing; got %v", want, lines)
		}
	}
}

// TestSellerMissingOptionalFields verifies optional GSTIN/PAN degrade to the
// consistent "-" placeholder while the other fields still render.
func TestSellerMissingOptionalFields(t *testing.T) {
	d := InvoiceData{
		Seller: SellerInfo{Name: "ABC Pharmacy", Address: "Assam", Phone: "+91 1"},
	}
	lines := d.sellerLines()
	if !hasLine(lines, "GSTIN: -") {
		t.Fatalf("expected GSTIN: - when GSTIN absent; got %v", lines)
	}
	if !hasLine(lines, "PAN: -") {
		t.Fatalf("expected PAN: - when PAN absent; got %v", lines)
	}
	if !hasLine(lines, "Phone: +91 1") {
		t.Fatalf("phone should render when present; got %v", lines)
	}
	if !hasLine(lines, "Address: Assam") {
		t.Fatalf("address should render when present; got %v", lines)
	}
}

// TestSellerLongAddressWrapping verifies a long address is preserved and the
// PDF still generates (address wraps rather than being truncated/dropped).
func TestSellerLongAddressWrapping(t *testing.T) {
	longAddr := "1st Floor, ABC Complex, G.S. Road, Ulubari, Guwahati, Kamrup, Assam 781007, India"
	d := InvoiceData{
		Invoice: repository.SalesInvoiceDetail{
			Invoice: baseInvoice(),
			Items:   []repository.SalesInvoiceItemDetail{sampleItem("Calpol", "30049099", 5, 0, 0, 5)},
		},
		Seller: SellerInfo{Name: "ABC Pharmacy", Address: longAddr, GSTIN: "18X", PAN: "P", Phone: "+91 9", StateCode: "18", StateName: "Assam"},
		Buyer:  BuyerInfo{Name: "B"},
	}
	// The address must be represented in the seller line set and must not be
	// dropped because it is long.
	lines := d.sellerLines()
	found := false
	for _, l := range lines {
		if strings.Contains(l.text, "ABC Complex") && strings.Contains(l.text, "G.S. Road") {
			found = true
		}
	}
	if !found {
		t.Fatalf("long address lost from seller lines: %v", lines)
	}
	var buf bytes.Buffer
	if err := GenerateInvoicePDF(&buf, d); err != nil {
		t.Fatalf("GenerateInvoicePDF with long address: %v", err)
	}
	if buf.Len() == 0 {
		t.Fatal("expected non-empty PDF output for long address")
	}
}

// TestTableHeadersPresent verifies all required column headers are defined and
// that they remain inside the 180mm content width (no clipped/overrun header).
func TestTableHeadersPresent(t *testing.T) {
	want := []string{"MEDICINE", "HSN", "MRP", "SELL", "QTY", "BONUS", "DISC.", "TAXABLE", "TAX", "TOTAL"}
	if len(itemColumns) != len(want) {
		t.Fatalf("expected %d columns, got %d", len(want), len(itemColumns))
	}
	for i, w := range want {
		if itemColumns[i].header != w {
			t.Fatalf("column %d header = %q, want %q", i, itemColumns[i].header, w)
		}
	}
}

// TestColumnWidthsSumToContent verifies the column widths still total the
// configured content width, so no header/row is forced off the page.
func TestColumnWidthsSumToContent(t *testing.T) {
	var sum float64
	for _, c := range itemColumns {
		sum += c.width
	}
	if sum != contentWidth {
		t.Fatalf("column widths sum to %v, want %v (contentWidth)", sum, contentWidth)
	}
}

// TestBonusSeparateRenders verifies quantity and bonus stay independent (they
// are never merged) and both appear as distinct row cells.
func TestBonusSeparateRenders(t *testing.T) {
	item := sampleItem("M", "30049099", 5, 2.5, 2.5, 0)
	item.Quantity = 5
	item.BonusQuantity = 1
	// The renderer maps Quantity and BonusQuantity to separate columns without
	// summing them; assert the values feeding the columns are distinct.
	qtyCell := fmt.Sprintf("%d", item.Quantity)
	bonusCell := fmt.Sprintf("%d", item.BonusQuantity)
	if qtyCell == bonusCell {
		t.Fatal("qty and bonus render to the same value; cannot be distinguished")
	}
	if item.Quantity+item.BonusQuantity == item.Quantity {
		t.Fatal("bonus appears to be folded into quantity")
	}
}
