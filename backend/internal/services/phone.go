package services

import "backend/internal/otp"

// Nigerian phone numbers are written several ways in the wild — "08012345678",
// "+2348012345678", "234 801 234 5678" — but SMS gateways and wallet recipient
// lookups need one canonical form. NormalizeNigerianPhone collapses all of
// those to an international digits-only form, e.g. "2348012345678".
//
// It returns "" for anything that cannot be a Nigerian number, so callers can
// treat an empty result as "not a valid number" rather than sending a bad
// request downstream.
func NormalizeNigerianPhone(phone string) string {
	return otp.NormalizePhone(phone)
}
