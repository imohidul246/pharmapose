package tax

import "github.com/shopspring/decimal"

// SupplyType determines whether CGST+SGST or IGST applies.
type SupplyType int

const (
	SupplyTypeIntraState SupplyType = iota
	SupplyTypeInterState
)

func (s SupplyType) String() string {
	switch s {
	case SupplyTypeIntraState:
		return "INTRA_STATE"
	case SupplyTypeInterState:
		return "INTER_STATE"
	default:
		return "UNKNOWN"
	}
}

func ParseSupplyType(s string) SupplyType {
	switch s {
	case "INTRA_STATE", "intra_state":
		return SupplyTypeIntraState
	case "INTER_STATE", "inter_state":
		return SupplyTypeInterState
	default:
		return SupplyTypeIntraState
	}
}

// TaxRate holds the effective-dated tax configuration for an HSN code.
type TaxRate struct {
	GSTRate  decimal.Decimal
	CGSTRate decimal.Decimal
	SGSTRate decimal.Decimal
	IGSTRate decimal.Decimal
	CessRate decimal.Decimal
}

// TaxInput is the input to the tax calculator for a single line item.
type TaxInput struct {
	Quantity         decimal.Decimal
	UnitPrice        decimal.Decimal // MRP / selling price
	DiscountAmount   decimal.Decimal // absolute rupee discount (already computed)
	TaxRate          TaxRate
	PriceIncludesTax bool   // true if UnitPrice is tax-inclusive MRP
	SupplyType       SupplyType
	HSNCode          string
}

// TaxLineResult is the output for a single line item.
type TaxLineResult struct {
	GrossAmount  decimal.Decimal
	Discount     decimal.Decimal
	TaxableValue decimal.Decimal

	CGSTRate   decimal.Decimal
	CGSTAmount decimal.Decimal

	SGSTRate   decimal.Decimal
	SGSTAmount decimal.Decimal

	IGSTRate   decimal.Decimal
	IGSTAmount decimal.Decimal

	CessRate   decimal.Decimal
	CessAmount decimal.Decimal

	TaxAmount decimal.Decimal
	LineTotal decimal.Decimal
	HSNCode   string
}

// TaxInvoiceResult is the aggregated result for an entire invoice.
type TaxInvoiceResult struct {
	Lines         []TaxLineResult
	GrossAmount   decimal.Decimal
	DiscountTotal decimal.Decimal
	TaxableAmount decimal.Decimal
	CGSTTotal     decimal.Decimal
	SGSTTotal     decimal.Decimal
	IGSTTotal     decimal.Decimal
	CessTotal     decimal.Decimal
	TaxTotal      decimal.Decimal
	RoundOff      decimal.Decimal
	GrandTotal    decimal.Decimal
	SupplyType    SupplyType
}
