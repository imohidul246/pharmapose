package tax

import "regexp"

// gstinPattern matches the structural layout of a GSTIN:
//
//	[0-9]{2}  : state code
//	[A-Z]{5}  : first 5 chars of PAN
//	[0-9]{4}  : last 4 chars of PAN
//	[A-Z]{1}  : last char of PAN
//	[1-9A-Z]{1} : entity code (1-9, A-Z)
//	Z          : reserved/default char
//	[0-9A-Z]{1} : checksum char
var gstinPattern = regexp.MustCompile(`^[0-9]{2}[A-Z]{5}[0-9]{4}[A-Z]{1}[1-9A-Z]{1}Z[0-9A-Z]{1}$`)

// gstinAlphabet is the ISO 7064 MOD 37,36 alphabet: digits 0-9 then A-Z.
// The checksum digit is derived from the first 14 characters.
const gstinAlphabet = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ"

// ValidateGSTIN checks both the structural pattern and the ISO 7064
// MOD 37,36 checksum digit (the 15th character). A GSTIN that fails the
// checksum is not a structurally valid registered GSTIN even if it matches
// the layout. The algorithm is the official GSTN one: alternate weights of
// 1 and 2 over the first 14 characters, summing the base-36 digit-sums of
// each weighted product, then (36 - sum % 36) % 36 as the check character.
//
// It was verified against a real, currently-registered GSTIN
// (33AAACC1206D1ZN) which reproduces the expected 'N' check character.
func ValidateGSTIN(gstin string) bool {
	if !gstinPattern.MatchString(gstin) {
		return false
	}
	return gstinChecksum(gstin)
}

func gstinChecksum(gstin string) bool {
	sum := 0
	factor := 1
	for i := 0; i < 14; i++ {
		value := indexOf(gstinAlphabet, gstin[i])
		if value < 0 {
			return false
		}
		product := value * factor
		sum += product/36 + product%36
		if factor == 1 {
			factor = 2
		} else {
			factor = 1
		}
	}
	checkIndex := (36 - sum%36) % 36
	return gstinAlphabet[checkIndex] == gstin[14]
}

func indexOf(s string, b byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == b {
			return i
		}
	}
	return -1
}
