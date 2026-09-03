package tax_test

import (
	"testing"

	"github.com/mohi/pms-marg-inspired/internal/tax"
)

// TestIntraStateGSTSymmetryOddValue is the GSTN portal compliance regression:
// for intra-state sales the line-item CGST MUST strictly equal SGST, even on
// odd-paisa taxes where rounding the total first would split asymmetrically.
func TestIntraStateGSTSymmetryOddValue(t *testing.T) {
	// Rs 13.75 at 5% GST: half-rate 2.5% -> 13.75*2.5/100 = 0.34375 -> 0.34.
	result := tax.CalculateLineTax(tax.TaxInput{
		Quantity:         d("1"),
		UnitPrice:        d("13.75"),
		DiscountAmount:   d("0"),
		TaxRate:          tax.TaxRate{GSTRate: d("5")},
		PriceIncludesTax: false,
		SupplyType:       tax.SupplyTypeIntraState,
		HSNCode:          "3004",
	})
	if result.CGSTAmount.Cmp(result.SGSTAmount) != 0 {
		t.Fatalf("CGST(%s) != SGST(%s) for Rs 13.75 @ 5%%: portal would reject",
			result.CGSTAmount, result.SGSTAmount)
	}
	sum := result.CGSTAmount.Add(result.SGSTAmount)
	if sum.Cmp(result.TaxAmount) != 0 {
		t.Fatalf("CGST+SGST(%s) != TaxAmount(%s)", sum, result.TaxAmount)
	}
	if result.IGSTAmount.Cmp(d("0")) != 0 {
		t.Errorf("IGST = %s want 0 for intra-state", result.IGSTAmount)
	}

	// Sweep several odd-paisa taxable values across rates: symmetry must hold
	// for every one.
	cases := []struct{ taxable, rate string }{
		{"13.75", "5"},
		{"1.00", "5"},
		{"0.50", "12"},
		{"99.99", "18"},
		{"7.33", "28"},
		{"10.01", "3"},
	}
	for _, tc := range cases {
		r := tax.CalculateLineTax(tax.TaxInput{
			Quantity:       d("1"),
			UnitPrice:      d(tc.taxable),
			DiscountAmount: d("0"),
			TaxRate:        tax.TaxRate{GSTRate: d(tc.rate)},
			SupplyType:     tax.SupplyTypeIntraState,
		})
		if r.CGSTAmount.Cmp(r.SGSTAmount) != 0 {
			t.Errorf("taxable %s @ %s%%: CGST(%s) != SGST(%s)",
				tc.taxable, tc.rate, r.CGSTAmount, r.SGSTAmount)
		}
		if r.CGSTAmount.Add(r.SGSTAmount).Cmp(r.TaxAmount) != 0 {
			t.Errorf("taxable %s @ %s%%: CGST+SGST != TaxAmount (%s vs %s)",
				tc.taxable, tc.rate, r.CGSTAmount.Add(r.SGSTAmount), r.TaxAmount)
		}
	}

	// Tax-inclusive path must also stay symmetric.
	incl := tax.CalculateLineTax(tax.TaxInput{
		Quantity:         d("1"),
		UnitPrice:        d("105"),
		DiscountAmount:   d("0"),
		TaxRate:          tax.TaxRate{GSTRate: d("5")},
		PriceIncludesTax: true,
		SupplyType:       tax.SupplyTypeIntraState,
	})
	if incl.CGSTAmount.Cmp(incl.SGSTAmount) != 0 {
		t.Errorf("tax-inclusive: CGST(%s) != SGST(%s)", incl.CGSTAmount, incl.SGSTAmount)
	}
}
