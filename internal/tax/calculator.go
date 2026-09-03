package tax

import "github.com/shopspring/decimal"

var (
	one       = decimal.NewFromInt(1)
	oneHundred = decimal.NewFromInt(100)
	zero      = decimal.Zero
	two       = decimal.NewFromInt(2)
)

// CalculateLineTax computes GST for a single line item.
//
// For tax-inclusive pricing (price_includes_tax = true):
//
//	gross = quantity × unit_price
//	taxable = (gross - discount) / (1 + gst_rate/100)
//	tax = (gross - discount) - taxable
//
// For tax-exclusive pricing:
//
//	gross = quantity × unit_price
//	taxable = gross - discount
//	tax = taxable × gst_rate / 100
//
// CGST/SGST/IGST split is determined by supply type.
// All monetary values are rounded to 2 decimal places.
func CalculateLineTax(in TaxInput) TaxLineResult {
	gross := in.Quantity.Mul(in.UnitPrice)
	gross = RoundMoney(gross)

	discount := in.DiscountAmount
	if discount.GreaterThan(gross) {
		discount = gross
	}
	if discount.IsNegative() {
		discount = zero
	}

	net := RoundMoney(gross.Sub(discount))

	var taxableValue, taxAmount decimal.Decimal

	var cessAmount decimal.Decimal

	if in.PriceIncludesTax {
		// Tax-inclusive: extract tax from the discounted price
		// divisor includes both GST and cess so that MRP = taxable + GST + cess
		divisor := one.
			Add(in.TaxRate.GSTRate.Div(oneHundred)).
			Add(in.TaxRate.CessRate.Div(oneHundred))
		taxableValue = net.Div(divisor)
		taxableValue = RoundMoney(taxableValue)
		taxAmount = taxableValue.Mul(in.TaxRate.GSTRate).Div(oneHundred)
		taxAmount = RoundMoney(taxAmount)
		cessAmount = taxableValue.Mul(in.TaxRate.CessRate).Div(oneHundred)
		cessAmount = RoundMoney(cessAmount)
	} else {
		// Tax-exclusive: add tax on top of discounted price
		taxableValue = net
		taxAmount = taxableValue.Mul(in.TaxRate.GSTRate).Div(oneHundred)
		taxAmount = RoundMoney(taxAmount)
		cessAmount = taxableValue.Mul(in.TaxRate.CessRate).Div(oneHundred)
		cessAmount = RoundMoney(cessAmount)
	}

	// Split tax into components based on supply type
	cgstRate, sgstRate, igstRate := SplitTaxComponents(in.TaxRate.GSTRate, in.SupplyType)

	cgstAmount := zero
	sgstAmount := zero
	igstAmount := zero

	if in.SupplyType == SupplyTypeInterState {
		igstAmount = taxAmount
	} else {
		// Intra-state: split equally between CGST and SGST. CGST is rounded to
		// two decimals and SGST takes the remaining paise so the sum of the two
		// always equals the line tax exactly (preserving MRP exactness).
		cgstAmount = taxAmount.Div(two)
		cgstAmount = RoundMoney(cgstAmount)
		sgstAmount = taxAmount.Sub(cgstAmount)
		sgstAmount = RoundMoney(sgstAmount)
	}

	var lineTotal decimal.Decimal
	if in.PriceIncludesTax {
		// Tax is already embedded in net (MRP), so line total = net.
		// Tax amounts are stored for reporting but not added on top.
		lineTotal = net
	} else {
		lineTotal = net.Add(taxAmount).Add(cessAmount)
		lineTotal = RoundMoney(lineTotal)
	}

	return TaxLineResult{
		GrossAmount:  gross,
		Discount:     discount,
		TaxableValue: taxableValue,

		CGSTRate:   RoundRate(cgstRate),
		CGSTAmount: cgstAmount,

		SGSTRate:   RoundRate(sgstRate),
		SGSTAmount: sgstAmount,

		IGSTRate:   RoundRate(igstRate),
		IGSTAmount: igstAmount,

		CessRate:   RoundRate(in.TaxRate.CessRate),
		CessAmount: cessAmount,

		TaxAmount: taxAmount,
		LineTotal: lineTotal,
		HSNCode:   in.HSNCode,
	}
}

// CalculateInvoiceTax aggregates multiple line results into an invoice-level summary.
// The supplyType is passed explicitly rather than inferred from line data to handle
// nil-rated inter-state supplies correctly (where IGST rate is 0 but supply is inter-state).
func CalculateInvoiceTax(lines []TaxLineResult, supplyType SupplyType) TaxInvoiceResult {
	result := TaxInvoiceResult{
		Lines:      lines,
		SupplyType: supplyType,
	}

	for _, l := range lines {
		result.GrossAmount = result.GrossAmount.Add(l.GrossAmount)
		result.DiscountTotal = result.DiscountTotal.Add(l.Discount)
		result.TaxableAmount = result.TaxableAmount.Add(l.TaxableValue)
		result.CGSTTotal = result.CGSTTotal.Add(l.CGSTAmount)
		result.SGSTTotal = result.SGSTTotal.Add(l.SGSTAmount)
		result.IGSTTotal = result.IGSTTotal.Add(l.IGSTAmount)
		result.CessTotal = result.CessTotal.Add(l.CessAmount)
		result.TaxTotal = result.TaxTotal.Add(l.TaxAmount)
	}

	// Compute grand total from components
	result.GrandTotal = result.TaxableAmount.
		Add(result.CGSTTotal).
		Add(result.SGSTTotal).
		Add(result.IGSTTotal).
		Add(result.CessTotal)
	result.GrandTotal = RoundMoney(result.GrandTotal)

	// Compute round_off: difference between grand total and sum of components
	sumComponents := result.TaxableAmount.
		Add(result.CGSTTotal).
		Add(result.SGSTTotal).
		Add(result.IGSTTotal).
		Add(result.CessTotal)
	result.RoundOff = result.GrandTotal.Sub(sumComponents)
	result.RoundOff = RoundMoney(result.RoundOff)

	return result
}

// ZeroTaxInput returns a TaxInput with zero tax (for legacy/non-GST items).
func ZeroTaxInput(quantity, unitPrice, discountAmount decimal.Decimal, hsnCode string) TaxInput {
	return TaxInput{
		Quantity:         quantity,
		UnitPrice:        unitPrice,
		DiscountAmount:   discountAmount,
		TaxRate:          TaxRate{},
		PriceIncludesTax: false,
		SupplyType:       SupplyTypeIntraState,
		HSNCode:          hsnCode,
	}
}

// ZeroTaxResult returns a TaxLineResult with zero tax values (for legacy items).
func ZeroTaxResult(quantity, unitPrice, discountAmount decimal.Decimal, hsnCode string) TaxLineResult {
	gross := quantity.Mul(unitPrice)
	gross = RoundMoney(gross)
	discount := discountAmount
	if discount.GreaterThan(gross) {
		discount = gross
	}
	net := RoundMoney(gross.Sub(discount))
	return TaxLineResult{
		GrossAmount:  gross,
		Discount:     discount,
		TaxableValue: net,
		LineTotal:    net,
		HSNCode:      hsnCode,
	}
}
