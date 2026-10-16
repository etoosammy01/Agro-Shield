package services

import "strings"

// Nigerian phone numbers are written several ways in the wild — "08012345678",
// "+2348012345678", "234 801 234 5678" — but SMS gateways and wallet recipient
// lookups need one canonical form. NormalizeNigerianPhone collapses all of
// those to the international digits-only form Termii expects, e.g.
// "2348012345678".
//
// It returns "" for anything that cannot be a Nigerian number, so callers can
// treat an empty result as "not a valid number" rather than sending a bad
// request downstream.
func NormalizeNigerianPhone(phone string) string {
	var digits strings.Builder
	for _, r := range phone {
		if r >= '0' && r <= '9' {
			digits.WriteRune(r)
		}
	}
	value := digits.String()

	switch {
	case value == "":
		return ""
	case strings.HasPrefix(value, "234"):
		// Already international. 234 + 10 subscriber digits.
		if len(value) == 13 {
			return value
		}
		return ""
	case strings.HasPrefix(value, "0"):
		// Local trunk form: 0 + 10 subscriber digits.
		if len(value) == 11 {
			return "234" + value[1:]
		}
		return ""
	default:
		// Bare subscriber number with the trunk zero dropped.
		if len(value) == 10 {
			return "234" + value
		}
		return ""
	}
}
