package tax

import (
	"math"

	"github.com/shopspring/decimal"
)

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

// RoundHalfEven rounds a float64 to the nearest integer using banker's
// rounding (ties to even), backed by math.RoundToEven. It is the integer
// core of all paise arithmetic: GSTN-facing tax splits are computed in
// integer paise so symmetry holds down to the single paisa.
func RoundHalfEven(x float64) int64 {
	return int64(math.RoundToEven(x))
}

// ToPaise converts a rupee decimal to integer paise with banker's rounding.
func ToPaise(d decimal.Decimal) int64 {
	return RoundHalfEven(d.Mul(decimal.NewFromInt(100)).InexactFloat64())
}

// FromPaise converts integer paise back to a rupee decimal (exact).
func FromPaise(paise int64) decimal.Decimal {
	return decimal.NewFromInt(paise).Div(decimal.NewFromInt(100))
}

// SplitIntraStatePaise computes the mirrored intra-state split from the
// taxable value in paise and the total GST rate in percent:
//
//	cgstPaise := RoundHalfEven(taxablePaise * (gstRate/2) / 100)
//	sgstPaise := cgstPaise // strictly mirrored
//	total     := cgstPaise + sgstPaise
//
// Mirroring (rather than splitting a rounded total, or rounding each half
// independently) is what keeps CGST == SGST on odd-paisa taxes.
func SplitIntraStatePaise(taxablePaise int64, gstRate float64) (cgstPaise, sgstPaise, totalPaise int64) {
	cgstPaise = RoundHalfEven(float64(taxablePaise) * (gstRate / 2.0) / 100.0)
	sgstPaise = cgstPaise
	return cgstPaise, sgstPaise, cgstPaise + sgstPaise
}

// IGSTPaise computes the inter-state tax in paise:
//
//	igstPaise := RoundHalfEven(taxablePaise * gstRate / 100)
func IGSTPaise(taxablePaise int64, gstRate float64) int64 {
	return RoundHalfEven(float64(taxablePaise) * gstRate / 100.0)
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
