package tax

import "github.com/shopspring/decimal"

// RoundMoney rounds a decimal to 2 decimal places using round-half-even
// (banker's rounding), the statutory convention for GST paise arithmetic.
// shopspring's Round is round-half-up, so RoundBank is used explicitly.
// This is the single authoritative rounding function for all monetary
// calculations: every rupee amount reaching an invoice passes through here.
// Intra-state CGST/SGST symmetry is guaranteed by computing CGST directly
// from the taxable value at half-rate and mirroring it to SGST
// (sgst == cgst, total == 2*cgst), so CGST == SGST always holds even on
// odd-paisa taxes where rounding the total first would split asymmetrically.
func RoundMoney(d decimal.Decimal) decimal.Decimal {
	return d.RoundBank(2)
}

// RoundToPaise is an alias for RoundMoney kept for call-site readability:
// it rounds a rupee amount to the nearest paisa (2 decimals).
func RoundToPaise(d decimal.Decimal) decimal.Decimal {
	return RoundMoney(d)
}

// RoundQuantity rounds a decimal to 0 decimal places (whole units) using
// round-half-even for consistency with monetary rounding.
func RoundQuantity(d decimal.Decimal) decimal.Decimal {
	return d.RoundBank(0)
}

// RoundRate rounds a tax rate to 2 decimal places using round-half-even.
func RoundRate(d decimal.Decimal) decimal.Decimal {
	return d.RoundBank(2)
}

// RoundOffDifference computes the difference between a sum of rounded line
// totals and the properly rounded grand total, distributing any rounding error.
func RoundOffDifference(lineTotals []decimal.Decimal) decimal.Decimal {
	sum := decimal.Zero
	for _, lt := range lineTotals {
		sum = sum.Add(RoundMoney(lt))
	}
	rounded := RoundMoney(sum)
	return rounded.Sub(sum)
}
