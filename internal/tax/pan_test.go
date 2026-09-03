package tax

import "testing"

func TestValidatePAN(t *testing.T) {
	valid := []string{
		"AAAAA0000A",
		"ABCDF1234Z",
		// A GSTIN-derived PAN substring is itself a structurally valid PAN.
		"ABCFD1234Z",
	}
	for _, s := range valid {
		if !ValidatePAN(s) {
			t.Errorf("expected %q to be a valid PAN", s)
		}
	}

	invalid := []string{
		"12345",        // too short / all digits
		"AAAAA000a",    // lowercase last char
		"AAAAA0000",    // missing last letter
		"",             // empty is not a provided value
		"aaaaa0000a",   // all lowercase
		"AAAA1A000A",   // digit in letter slot
	}
	for _, s := range invalid {
		if ValidatePAN(s) {
			t.Errorf("expected %q to be an invalid PAN", s)
		}
	}
}
