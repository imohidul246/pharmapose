package tax

// ValidatedUQCs are the GSTN Unit Quantity Codes relevant to a pharmacy.
// The authoritative list is maintained by GSTN; the codes below are the
// common ones for medicinal products. "TBS" (Tablets) is the correct code
// for tablets — "TAB" is not a valid GSTN UQC.
var validUQCs = map[string]bool{
	"BAG": true, "BAL": true, "BDL": true, "BGM": true, "BTL": true,
	"BOX": true, "CAN": true, "CTN": true, "DZN": true, "GMS": true,
	"KGS": true, "KIT": true, "LTR": true, "MTR": true, "MLT": true,
	"MTS": true, "NOS": true, "PAC": true, "PCS": true, "ROL": true,
	"SET": true, "SQF": true, "TBS": true, "THD": true, "TON": true,
	"UNT": true, "OTH": true,
}

// ValidateUQC returns true if uqc is a recognised GSTN Unit Quantity Code.
func ValidateUQC(uqc string) bool {
	return validUQCs[uqc]
}
