package tax_test

import (
	"testing"

	"github.com/mohi/pms-marg-inspired/internal/tax"
)

// TestRoundMoneyHalfEven locks round-half-even (banker's) rounding for every
// monetary amount: exact ties round to the nearest even paisa instead of
// always rounding up, so large GST aggregations do not drift upward.
func TestRoundMoneyHalfEven(t *testing.T) {
	cases := map[string]string{
		"2.665": "2.66", // tie, 6 even → stay
		"2.675": "2.68", // tie, 7 odd → up
		"0.125": "0.12",
		"0.135": "0.14",
		"0.145": "0.14",
		"1.015": "1.02",
		"1.025": "1.02",
		"2.625": "2.62",
	}
	for in, want := range cases {
		got := tax.RoundMoney(d(in)).String()
		if got != want {
			t.Errorf("RoundMoney(%s) = %s want %s (banker's rounding)", in, got, want)
		}
	}
}

// TestCGSTSGSTSplitSumsToTotalGST is the fractional-paise invariant: for any
// line tax, CGST + SGST must equal the total GST exactly — no drift, no lost
// paisa — including odd-paisa taxes where the half-paisa split itself ties.
func TestCGSTSGSTSplitSumsToTotalGST(t *testing.T) {
	// 5% of 1.00 = 0.05 total GST → 0.025 each half (an exact tie).
	result := tax.CalculateLineTax(tax.TaxInput{
		Quantity:         d("1"),
		UnitPrice:        d("1.00"),
		DiscountAmount:   d("0"),
		TaxRate:          tax.TaxRate{GSTRate: d("5")},
		PriceIncludesTax: false,
		SupplyType:       tax.SupplyTypeIntraState,
		HSNCode:          "3004",
	})
	if result.TaxAmount.Cmp(d("0.05")) != 0 {
		t.Fatalf("tax = %s want 0.05", result.TaxAmount)
	}
	sum := result.CGSTAmount.Add(result.SGSTAmount)
	if sum.Cmp(result.TaxAmount) != 0 {
		t.Errorf("cgst(%s) + sgst(%s) = %s != total GST %s",
			result.CGSTAmount, result.SGSTAmount, sum, result.TaxAmount)
	}

	// Sweep a range of fractional totals through the invoice aggregator and
	// require the component sums to reconcile with the grand total.
	lines := []tax.TaxLineResult{
		tax.CalculateLineTax(tax.TaxInput{
			Quantity: d("1"), UnitPrice: d("1.00"), DiscountAmount: d("0"),
			TaxRate: tax.TaxRate{GSTRate: d("5")}, PriceIncludesTax: false,
			SupplyType: tax.SupplyTypeIntraState,
		}),
		tax.CalculateLineTax(tax.TaxInput{
			Quantity: d("3"), UnitPrice: d("33.33"), DiscountAmount: d("0"),
			TaxRate: tax.TaxRate{GSTRate: d("18")}, PriceIncludesTax: false,
			SupplyType: tax.SupplyTypeIntraState,
		}),
		tax.CalculateLineTax(tax.TaxInput{
			Quantity: d("7"), UnitPrice: d("9.99"), DiscountAmount: d("1.05"),
			TaxRate: tax.TaxRate{GSTRate: d("12")}, PriceIncludesTax: false,
			SupplyType: tax.SupplyTypeIntraState,
		}),
	}
	inv := tax.CalculateInvoiceTax(lines, tax.SupplyTypeIntraState)
	if inv.CGSTTotal.Add(inv.SGSTTotal).Cmp(inv.TaxTotal) != 0 {
		t.Errorf("invoice cgst(%s)+sgst(%s) != tax total %s",
			inv.CGSTTotal, inv.SGSTTotal, inv.TaxTotal)
	}
	recomputed := inv.TaxableAmount.
		Add(inv.CGSTTotal).
		Add(inv.SGSTTotal).
		Add(inv.IGSTTotal).
		Add(inv.CessTotal).
		Add(inv.RoundOff)
	if recomputed.Cmp(inv.GrandTotal) != 0 {
		t.Errorf("components + round_off = %s != grand total %s",
			recomputed, inv.GrandTotal)
	}
}

// TestRoundRateHalfEven ensures GST rate snapshots use the same convention.
func TestRoundRateHalfEven(t *testing.T) {
	if got := tax.RoundRate(d("2.675")).String(); got != "2.68" {
		t.Errorf("RoundRate(2.675) = %s want 2.68", got)
	}
	if got := tax.RoundRate(d("2.665")).String(); got != "2.66" {
		t.Errorf("RoundRate(2.665) = %s want 2.66", got)
	}
}
