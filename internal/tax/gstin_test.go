package tax

import "testing"

func TestValidateGSTIN(t *testing.T) {
	valid := []string{
		// Verified against real registered GSTIN 33AAACC1206D1ZN (Central Warehousing Corp)
		"33AAACC1206D1ZN",
		// Generated valid fixtures (checksum computed with the official algorithm)
		"27AAPBC1234F1ZV", "27AACCC5678K1Z7", "06AADCM4321P1ZC",
		"27AAECS9876F1ZS", "27AAHFS2345K1ZY", "27AAAAA1111A1ZW",
	}
	for _, g := range valid {
		if !ValidateGSTIN(g) {
			t.Errorf("expected %s to be a valid GSTIN", g)
		}
	}

	invalid := []string{
		// Pattern-valid but bad checksum
		"33AAACC1206D1ZM", "27AAAAA0000A1Z5", "27AAACC1111C1Z7",
		"06AADCM1234P1Z2", "27AAECS1234F1Z3", "27AAHFS5678K1Z9",
		"27AABCU9603R1ZM",
		// Structural violations
		"", "27AAAAA0000A1", "27AAAAA0000A1Z", "27AAAAA0000A1Z5X",
		"27AAAAA0000A1Z#", "27AAAAA0000A1z5",
	}
	for _, g := range invalid {
		if ValidateGSTIN(g) {
			t.Errorf("expected %s to be an invalid GSTIN", g)
		}
	}
}

func TestValidateUQC(t *testing.T) {
	if !ValidateUQC("TBS") {
		t.Error("TBS should be a valid UQC for tablets")
	}
	if !ValidateUQC("BOX") || !ValidateUQC("NOS") || !ValidateUQC("PCS") {
		t.Error("BOX/NOS/PCS should be valid UQCs")
	}
	if ValidateUQC("TAB") {
		t.Error("TAB is NOT a valid GSTN UQC (must be TBS)")
	}
	if ValidateUQC("") || ValidateUQC("XX") {
		t.Error("empty/unknown UQC should be invalid")
	}
}
