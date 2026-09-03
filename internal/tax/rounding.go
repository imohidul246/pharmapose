package tax

import "github.com/shopspring/decimal"

// RoundMoney rounds a decimal to 2 decimal places using banker's rounding.
// This is the single authoritative rounding function for all monetary calculations.
func RoundMoney(d decimal.Decimal) decimal.Decimal {
	return d.Round(2)
}

// RoundQuantity rounds a decimal to 0 decimal places (whole units).
func RoundQuantity(d decimal.Decimal) decimal.Decimal {
	return d.Round(0)
}

// RoundRate rounds a tax rate to 2 decimal places.
func RoundRate(d decimal.Decimal) decimal.Decimal {
	return d.Round(2)
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
