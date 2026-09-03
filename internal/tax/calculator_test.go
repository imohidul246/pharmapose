package tax_test

import (
	"testing"

	"github.com/shopspring/decimal"

	"github.com/mohi/pms-marg-inspired/internal/tax"
)

func d(s string) decimal.Decimal {
	v, _ := decimal.NewFromString(s)
	return v
}

// Test 1: 5% GST, tax-inclusive price, no discount, intra-state
func TestTaxInclusive5PercentIntraState(t *testing.T) {
	result := tax.CalculateLineTax(tax.TaxInput{
		Quantity:         d("1"),
		UnitPrice:        d("105"),
		DiscountAmount:   d("0"),
		TaxRate:          tax.TaxRate{GSTRate: d("5"), CGSTRate: d("2.5"), SGSTRate: d("2.5")},
		PriceIncludesTax: true,
		SupplyType:       tax.SupplyTypeIntraState,
		HSNCode:          "3004",
	})

	if result.GrossAmount.Cmp(d("105")) != 0 {
		t.Errorf("gross = %s want 105", result.GrossAmount)
	}
	if result.TaxableValue.Cmp(d("100")) != 0 {
		t.Errorf("taxable = %s want 100", result.TaxableValue)
	}
	if result.CGSTAmount.Cmp(d("2.50")) != 0 {
		t.Errorf("cgst = %s want 2.50", result.CGSTAmount)
	}
	if result.SGSTAmount.Cmp(d("2.50")) != 0 {
		t.Errorf("sgst = %s want 2.50", result.SGSTAmount)
	}
	if result.IGSTAmount.Cmp(d("0")) != 0 {
		t.Errorf("igst = %s want 0", result.IGSTAmount)
	}
	if result.LineTotal.Cmp(d("105")) != 0 {
		t.Errorf("total = %s want 105", result.LineTotal)
	}
}

// Test 2: 5% GST, tax-inclusive price with discount
func TestTaxInclusive5PercentWithDiscount(t *testing.T) {
	result := tax.CalculateLineTax(tax.TaxInput{
		Quantity:         d("1"),
		UnitPrice:        d("105"),
		DiscountAmount:   d("5"),
		TaxRate:          tax.TaxRate{GSTRate: d("5")},
		PriceIncludesTax: true,
		SupplyType:       tax.SupplyTypeIntraState,
		HSNCode:          "3004",
	})

	// customer pays = 100
	if result.GrossAmount.Cmp(d("105")) != 0 {
		t.Errorf("gross = %s want 105", result.GrossAmount)
	}
	if result.Discount.Cmp(d("5")) != 0 {
		t.Errorf("discount = %s want 5", result.Discount)
	}
	// taxable = 100 / 1.05 = 95.238095... → rounded to 95.24
	expectedTaxable := d("95.24")
	if result.TaxableValue.Cmp(expectedTaxable) != 0 {
		t.Errorf("taxable = %s want %s", result.TaxableValue, expectedTaxable)
	}
	// tax = 100 - 95.24 = 4.76
	expectedTax := d("4.76")
	if result.TaxAmount.Cmp(expectedTax) != 0 {
		t.Errorf("tax = %s want %s", result.TaxAmount, expectedTax)
	}
	// total = 100
	if result.LineTotal.Cmp(d("100")) != 0 {
		t.Errorf("total = %s want 100", result.LineTotal)
	}
}

// Test 3: IGST (inter-state)
func TestIGSTInterState(t *testing.T) {
	result := tax.CalculateLineTax(tax.TaxInput{
		Quantity:         d("1"),
		UnitPrice:        d("100"),
		DiscountAmount:   d("0"),
		TaxRate:          tax.TaxRate{GSTRate: d("5")},
		PriceIncludesTax: false,
		SupplyType:       tax.SupplyTypeInterState,
		HSNCode:          "3004",
	})

	if result.TaxableValue.Cmp(d("100")) != 0 {
		t.Errorf("taxable = %s want 100", result.TaxableValue)
	}
	if result.CGSTAmount.Cmp(d("0")) != 0 {
		t.Errorf("cgst = %s want 0", result.CGSTAmount)
	}
	if result.SGSTAmount.Cmp(d("0")) != 0 {
		t.Errorf("sgst = %s want 0", result.SGSTAmount)
	}
	if result.IGSTAmount.Cmp(d("5")) != 0 {
		t.Errorf("igst = %s want 5", result.IGSTAmount)
	}
	if result.LineTotal.Cmp(d("105")) != 0 {
		t.Errorf("total = %s want 105", result.LineTotal)
	}
}

// Test 4: Intra-state (CGST+SGST)
func TestIntraStateCGSTSGST(t *testing.T) {
	result := tax.CalculateLineTax(tax.TaxInput{
		Quantity:         d("1"),
		UnitPrice:        d("100"),
		DiscountAmount:   d("0"),
		TaxRate:          tax.TaxRate{GSTRate: d("5")},
		PriceIncludesTax: false,
		SupplyType:       tax.SupplyTypeIntraState,
		HSNCode:          "3004",
	})

	if result.TaxableValue.Cmp(d("100")) != 0 {
		t.Errorf("taxable = %s want 100", result.TaxableValue)
	}
	if result.CGSTAmount.Cmp(d("2.50")) != 0 {
		t.Errorf("cgst = %s want 2.50", result.CGSTAmount)
	}
	if result.SGSTAmount.Cmp(d("2.50")) != 0 {
		t.Errorf("sgst = %s want 2.50", result.SGSTAmount)
	}
	if result.IGSTAmount.Cmp(d("0")) != 0 {
		t.Errorf("igst = %s want 0", result.IGSTAmount)
	}
	if result.LineTotal.Cmp(d("105")) != 0 {
		t.Errorf("total = %s want 105", result.LineTotal)
	}
}

// Test 5: Nil-rated product (0% GST)
func TestNilRatedProduct(t *testing.T) {
	result := tax.CalculateLineTax(tax.TaxInput{
		Quantity:         d("1"),
		UnitPrice:        d("100"),
		DiscountAmount:   d("0"),
		TaxRate:          tax.TaxRate{GSTRate: d("0")},
		PriceIncludesTax: false,
		SupplyType:       tax.SupplyTypeIntraState,
		HSNCode:          "3004",
	})

	if result.TaxableValue.Cmp(d("100")) != 0 {
		t.Errorf("taxable = %s want 100", result.TaxableValue)
	}
	if result.CGSTAmount.Cmp(d("0")) != 0 {
		t.Errorf("cgst = %s want 0", result.CGSTAmount)
	}
	if result.SGSTAmount.Cmp(d("0")) != 0 {
		t.Errorf("sgst = %s want 0", result.SGSTAmount)
	}
	if result.IGSTAmount.Cmp(d("0")) != 0 {
		t.Errorf("igst = %s want 0", result.IGSTAmount)
	}
	if result.TaxAmount.Cmp(d("0")) != 0 {
		t.Errorf("tax = %s want 0", result.TaxAmount)
	}
	if result.LineTotal.Cmp(d("100")) != 0 {
		t.Errorf("total = %s want 100", result.LineTotal)
	}
}

// Test 6: Percentage discount on tax-exclusive calculation
func TestPercentageDiscountTaxExclusive(t *testing.T) {
	result := tax.CalculateLineTax(tax.TaxInput{
		Quantity:         d("10"),
		UnitPrice:        d("100"),
		DiscountAmount:   d("100"), // 10% of 1000
		TaxRate:          tax.TaxRate{GSTRate: d("5")},
		PriceIncludesTax: false,
		SupplyType:       tax.SupplyTypeIntraState,
		HSNCode:          "3004",
	})

	// gross = 1000, discount = 100, taxable = 900, GST = 45
	if result.GrossAmount.Cmp(d("1000")) != 0 {
		t.Errorf("gross = %s want 1000", result.GrossAmount)
	}
	if result.Discount.Cmp(d("100")) != 0 {
		t.Errorf("discount = %s want 100", result.Discount)
	}
	if result.TaxableValue.Cmp(d("900")) != 0 {
		t.Errorf("taxable = %s want 900", result.TaxableValue)
	}
	if result.TaxAmount.Cmp(d("45")) != 0 {
		t.Errorf("tax = %s want 45", result.TaxAmount)
	}
	if result.LineTotal.Cmp(d("945")) != 0 {
		t.Errorf("total = %s want 945", result.LineTotal)
	}
}

// Test 7: Flat discount on tax-exclusive calculation
func TestFlatDiscountTaxExclusive(t *testing.T) {
	result := tax.CalculateLineTax(tax.TaxInput{
		Quantity:         d("10"),
		UnitPrice:        d("100"),
		DiscountAmount:   d("100"),
		TaxRate:          tax.TaxRate{GSTRate: d("5")},
		PriceIncludesTax: false,
		SupplyType:       tax.SupplyTypeIntraState,
		HSNCode:          "3004",
	})

	if result.TaxableValue.Cmp(d("900")) != 0 {
		t.Errorf("taxable = %s want 900", result.TaxableValue)
	}
	if result.TaxAmount.Cmp(d("45")) != 0 {
		t.Errorf("tax = %s want 45", result.TaxAmount)
	}
	if result.LineTotal.Cmp(d("945")) != 0 {
		t.Errorf("total = %s want 945", result.LineTotal)
	}
}

// Test 8: Discount cannot make amount negative (clamping)
func TestDiscountClamping(t *testing.T) {
	result := tax.CalculateLineTax(tax.TaxInput{
		Quantity:         d("1"),
		UnitPrice:        d("100"),
		DiscountAmount:   d("500"),
		TaxRate:          tax.TaxRate{GSTRate: d("5")},
		PriceIncludesTax: false,
		SupplyType:       tax.SupplyTypeIntraState,
		HSNCode:          "3004",
	})

	if result.Discount.Cmp(d("100")) != 0 {
		t.Errorf("discount = %s want 100 (clamped to gross)", result.Discount)
	}
	if result.TaxableValue.Cmp(d("0")) != 0 {
		t.Errorf("taxable = %s want 0", result.TaxableValue)
	}
	if result.TaxAmount.Cmp(d("0")) != 0 {
		t.Errorf("tax = %s want 0", result.TaxAmount)
	}
	if result.LineTotal.Cmp(d("0")) != 0 {
		t.Errorf("total = %s want 0", result.LineTotal)
	}
}

func TestDetermineSupplyType(t *testing.T) {
	tests := []struct {
		seller  string
		buyer   string
		want    tax.SupplyType
	}{
		{"27", "27", tax.SupplyTypeIntraState},
		{"27", "24", tax.SupplyTypeInterState},
		{"", "", tax.SupplyTypeIntraState},
		{"27", "", tax.SupplyTypeIntraState},
		{"", "27", tax.SupplyTypeIntraState},
	}
	for _, tt := range tests {
		got := tax.DetermineSupplyType(tt.seller, tt.buyer)
		if got != tt.want {
			t.Errorf("DetermineSupplyType(%q, %q) = %v want %v", tt.seller, tt.buyer, got, tt.want)
		}
	}
}

func TestCalculateInvoiceTax(t *testing.T) {
	lines := []tax.TaxLineResult{
		tax.CalculateLineTax(tax.TaxInput{
			Quantity:         d("2"),
			UnitPrice:        d("100"),
			DiscountAmount:   d("0"),
			TaxRate:          tax.TaxRate{GSTRate: d("5")},
			PriceIncludesTax: false,
			SupplyType:       tax.SupplyTypeIntraState,
		}),
		tax.CalculateLineTax(tax.TaxInput{
			Quantity:         d("1"),
			UnitPrice:        d("200"),
			DiscountAmount:   d("0"),
			TaxRate:          tax.TaxRate{GSTRate: d("5")},
			PriceIncludesTax: false,
			SupplyType:       tax.SupplyTypeIntraState,
		}),
	}

	inv := tax.CalculateInvoiceTax(lines, tax.SupplyTypeIntraState)

	// Line 1: 200 + 10 tax = 210
	// Line 2: 200 + 10 tax = 210
	// Total: 420
	if inv.GrossAmount.Cmp(d("400")) != 0 {
		t.Errorf("gross = %s want 400", inv.GrossAmount)
	}
	if inv.TaxableAmount.Cmp(d("400")) != 0 {
		t.Errorf("taxable = %s want 400", inv.TaxableAmount)
	}
	if inv.TaxTotal.Cmp(d("20")) != 0 {
		t.Errorf("tax = %s want 20", inv.TaxTotal)
	}
	if inv.GrandTotal.Cmp(d("420")) != 0 {
		t.Errorf("grand total = %s want 420", inv.GrandTotal)
	}
	if inv.SupplyType != tax.SupplyTypeIntraState {
		t.Errorf("supply type = %v want IntraState", inv.SupplyType)
	}
}

// Test: Qty > 1, tax-inclusive, 5%
func TestTaxInclusiveQtyGreaterThanOne(t *testing.T) {
	result := tax.CalculateLineTax(tax.TaxInput{
		Quantity:         d("3"),
		UnitPrice:        d("105"),
		DiscountAmount:   d("0"),
		TaxRate:          tax.TaxRate{GSTRate: d("5")},
		PriceIncludesTax: true,
		SupplyType:       tax.SupplyTypeIntraState,
		HSNCode:          "3004",
	})

	// gross = 315, taxable = 315 / 1.05 = 300, tax = 15
	if result.GrossAmount.Cmp(d("315")) != 0 {
		t.Errorf("gross = %s want 315", result.GrossAmount)
	}
	if result.TaxableValue.Cmp(d("300")) != 0 {
		t.Errorf("taxable = %s want 300", result.TaxableValue)
	}
	if result.TaxAmount.Cmp(d("15")) != 0 {
		t.Errorf("tax = %s want 15", result.TaxAmount)
	}
	if result.LineTotal.Cmp(d("315")) != 0 {
		t.Errorf("total = %s want 315", result.LineTotal)
	}
}

// Test: Cess calculation, tax-exclusive
func TestCessCalculation(t *testing.T) {
	result := tax.CalculateLineTax(tax.TaxInput{
		Quantity:         d("1"),
		UnitPrice:        d("100"),
		DiscountAmount:   d("0"),
		TaxRate:          tax.TaxRate{GSTRate: d("5"), CessRate: d("12")},
		PriceIncludesTax: false,
		SupplyType:       tax.SupplyTypeIntraState,
		HSNCode:          "3004",
	})

	// taxable = 100, GST = 5, Cess = 12
	if result.TaxableValue.Cmp(d("100")) != 0 {
		t.Errorf("taxable = %s want 100", result.TaxableValue)
	}
	if result.TaxAmount.Cmp(d("5")) != 0 {
		t.Errorf("tax = %s want 5", result.TaxAmount)
	}
	if result.CessAmount.Cmp(d("12")) != 0 {
		t.Errorf("cess = %s want 12", result.CessAmount)
	}
	// lineTotal = 100 + 5 + 12 = 117
	if result.LineTotal.Cmp(d("117")) != 0 {
		t.Errorf("total = %s want 117", result.LineTotal)
	}
}

// Test: Tax-inclusive with cess
func TestTaxInclusiveWithCess(t *testing.T) {
	result := tax.CalculateLineTax(tax.TaxInput{
		Quantity:         d("1"),
		UnitPrice:        d("118"),
		DiscountAmount:   d("0"),
		TaxRate:          tax.TaxRate{GSTRate: d("12"), CessRate: d("10")},
		PriceIncludesTax: true,
		SupplyType:       tax.SupplyTypeIntraState,
		HSNCode:          "3004",
	})

	// divisor = 1 + (12+10)/100 = 1.22
	// taxable = 118 / 1.22 = 96.72 (rounded)
	// CGST = 96.72 * 6% = 5.80, SGST = CGST (mirrored for portal symmetry),
	// GST total = 11.60 (mirrored sum; 1p less than Round(96.72*12%) = 11.61
	// which would split asymmetrically 5.80/5.81 and be rejected).
	// Cess = 96.72 * 10% = 9.67 (rounded)
	if result.TaxableValue.Cmp(d("96.72")) != 0 {
		t.Errorf("taxable = %s want 96.72", result.TaxableValue)
	}
	if result.TaxAmount.Cmp(d("11.60")) != 0 {
		t.Errorf("tax = %s want 11.60", result.TaxAmount)
	}
	if result.CGSTAmount.Cmp(result.SGSTAmount) != 0 {
		t.Errorf("cgst(%s) != sgst(%s): must be symmetric", result.CGSTAmount, result.SGSTAmount)
	}
	if result.CessAmount.Cmp(d("9.67")) != 0 {
		t.Errorf("cess = %s want 9.67", result.CessAmount)
	}
	if result.LineTotal.Cmp(d("118")) != 0 {
		t.Errorf("total = %s want 118", result.LineTotal)
	}
}

// Test: Multiple lines with different GST rates
func TestMultipleLinesDifferentGSTRates(t *testing.T) {
	lines := []tax.TaxLineResult{
		tax.CalculateLineTax(tax.TaxInput{
			Quantity: d("1"), UnitPrice: d("100"), DiscountAmount: d("0"),
			TaxRate: tax.TaxRate{GSTRate: d("5")}, PriceIncludesTax: false,
			SupplyType: tax.SupplyTypeIntraState,
		}),
		tax.CalculateLineTax(tax.TaxInput{
			Quantity: d("1"), UnitPrice: d("200"), DiscountAmount: d("0"),
			TaxRate: tax.TaxRate{GSTRate: d("12")}, PriceIncludesTax: false,
			SupplyType: tax.SupplyTypeIntraState,
		}),
	}

	inv := tax.CalculateInvoiceTax(lines, tax.SupplyTypeIntraState)

	// Line 1: taxable=100, tax=5. Line 2: taxable=200, tax=24
	if inv.TaxableAmount.Cmp(d("300")) != 0 {
		t.Errorf("taxable = %s want 300", inv.TaxableAmount)
	}
	if inv.TaxTotal.Cmp(d("29")) != 0 {
		t.Errorf("tax = %s want 29", inv.TaxTotal)
	}
	// grand = 300 + 29 = 329
	if inv.GrandTotal.Cmp(d("329")) != 0 {
		t.Errorf("grand = %s want 329", inv.GrandTotal)
	}
}

// Test: Rounding edge cases with fractional paise
func TestRoundingEdgeCasesFractionalPaise(t *testing.T) {
	result := tax.CalculateLineTax(tax.TaxInput{
		Quantity:         d("3"),
		UnitPrice:        d("33.33"),
		DiscountAmount:   d("0"),
		TaxRate:          tax.TaxRate{GSTRate: d("18")},
		PriceIncludesTax: false,
		SupplyType:       tax.SupplyTypeIntraState,
		HSNCode:          "3004",
	})

	// gross = 99.99, taxable = 99.99, tax = 17.9982 → 18.00
	if result.TaxAmount.Cmp(d("18")) != 0 {
		t.Errorf("tax = %s want 18", result.TaxAmount)
	}
	// CGST = 9.00, SGST = 9.00
	if result.CGSTAmount.Cmp(d("9")) != 0 {
		t.Errorf("cgst = %s want 9", result.CGSTAmount)
	}
	if result.SGSTAmount.Cmp(d("9")) != 0 {
		t.Errorf("sgst = %s want 9", result.SGSTAmount)
	}
}

// Test: Nil-rated inter-state supply type is preserved
func TestCalculateInvoiceTaxNilRatedInterState(t *testing.T) {
	lines := []tax.TaxLineResult{
		tax.CalculateLineTax(tax.TaxInput{
			Quantity: d("1"), UnitPrice: d("100"), DiscountAmount: d("0"),
			TaxRate: tax.TaxRate{GSTRate: d("0")}, PriceIncludesTax: false,
			SupplyType: tax.SupplyTypeInterState,
		}),
	}

	inv := tax.CalculateInvoiceTax(lines, tax.SupplyTypeInterState)

	// Supply type should be InterState even though IGST rate is 0
	if inv.SupplyType != tax.SupplyTypeInterState {
		t.Errorf("supply type = %v want InterState", inv.SupplyType)
	}
	if inv.GrandTotal.Cmp(d("100")) != 0 {
		t.Errorf("grand = %s want 100", inv.GrandTotal)
	}
}

// Test: Round-off computation
func TestRoundOffComputation(t *testing.T) {
	// 3 lines with amounts that produce rounding drift
	lines := []tax.TaxLineResult{
		tax.CalculateLineTax(tax.TaxInput{
			Quantity: d("1"), UnitPrice: d("33.33"), DiscountAmount: d("0"),
			TaxRate: tax.TaxRate{GSTRate: d("18")}, PriceIncludesTax: false,
			SupplyType: tax.SupplyTypeIntraState,
		}),
		tax.CalculateLineTax(tax.TaxInput{
			Quantity: d("1"), UnitPrice: d("33.33"), DiscountAmount: d("0"),
			TaxRate: tax.TaxRate{GSTRate: d("18")}, PriceIncludesTax: false,
			SupplyType: tax.SupplyTypeIntraState,
		}),
		tax.CalculateLineTax(tax.TaxInput{
			Quantity: d("1"), UnitPrice: d("33.34"), DiscountAmount: d("0"),
			TaxRate: tax.TaxRate{GSTRate: d("18")}, PriceIncludesTax: false,
			SupplyType: tax.SupplyTypeIntraState,
		}),
	}

	inv := tax.CalculateInvoiceTax(lines, tax.SupplyTypeIntraState)

	// taxable = 99.99+0.01 rounding should = 100.00
	// The round_off should capture any drift between sum-of-components and grand total
	sumComponents := inv.TaxableAmount.Add(inv.CGSTTotal).Add(inv.SGSTTotal).Add(inv.IGSTTotal).Add(inv.CessTotal)
	expectedGrand := sumComponents.Add(inv.RoundOff)
	if inv.GrandTotal.Cmp(expectedGrand) != 0 {
		t.Errorf("grand total %s != sum+roundoff %s", inv.GrandTotal, expectedGrand)
	}
}
