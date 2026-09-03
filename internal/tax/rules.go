package tax

import "github.com/shopspring/decimal"

// DetermineSupplyType determines whether CGST+SGST or IGST applies
// based on the seller's state code and the place of supply state code.
// Both state codes must be 2-digit strings (e.g. "27" for Maharashtra).
func DetermineSupplyType(sellerStateCode, placeOfSupplyStateCode string) SupplyType {
	if sellerStateCode == "" || placeOfSupplyStateCode == "" {
		return SupplyTypeIntraState
	}
	if sellerStateCode == placeOfSupplyStateCode {
		return SupplyTypeIntraState
	}
	return SupplyTypeInterState
}

// SplitTaxComponents splits a total GST rate into CGST+SGST (intra-state)
// or IGST (inter-state) components.
// For intra-state: CGST = rate/2, SGST = rate/2, IGST = 0
// For inter-state: CGST = 0, SGST = 0, IGST = rate
func SplitTaxComponents(totalRate decimal.Decimal, supplyType SupplyType) (cgstRate, sgstRate, igstRate decimal.Decimal) {
	half := totalRate.Div(two)
	switch supplyType {
	case SupplyTypeInterState:
		return decimal.Zero, decimal.Zero, totalRate
	default:
		return half, half, decimal.Zero
	}
}
