package tax

import "regexp"

// panPattern matches the structural layout of a Permanent Account Number (PAN):
// 5 uppercase letters, 4 digits, 1 uppercase letter. The third letter is the
// entity-type code (powered by [A-Z]) — we deliberately keep the validation
// structural only so genuine PANs are not rejected.
var panPattern = regexp.MustCompile(`^[A-Z]{5}[0-9]{4}[A-Z]{1}$`)

// ValidatePAN reports whether s is a structurally valid PAN. Empty strings are
// handled by callers as "not provided"; this helper only validates a provided
// value, so it returns false for an empty string.
func ValidatePAN(s string) bool {
	if s == "" {
		return false
	}
	return panPattern.MatchString(s)
}
