// Package money is the one place where naira amounts are parsed, added,
// subtracted and compared.
//
// Every amount is a whole number of kobo held in an int64. There is no float
// anywhere in a decision: a provider's float is converted once, at the edge, by
// its shortest decimal text, and rejected if that text is not an exact number
// of kobo. Every operation that can overflow reports it instead of wrapping.
package money

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
)

// Kobo is an amount in kobo (1/100 naira).
type Kobo int64

// MaxKobo is the largest amount the database can hold: NUMERIC(12,2) tops out at
// 9,999,999,999.99 naira. Keeping the application ceiling equal to the column
// ceiling means a credit is refused here, cleanly, instead of failing inside a
// transaction as "numeric field overflow" - which a webhook would retry forever.
const MaxKobo Kobo = 999_999_999_999

var (
	ErrInvalidAmount  = errors.New("enter a valid positive amount")
	ErrPrecision      = errors.New("enter a valid positive amount with up to two decimal places")
	ErrTooLarge       = errors.New("amount exceeds wallet limits")
	ErrOverflow       = errors.New("amount arithmetic overflow")
	ErrExceedsMax     = errors.New("amount would take the balance above its maximum")
	ErrInsufficient   = errors.New("insufficient wallet balance")
	ErrNotPositive    = errors.New("amount must be greater than zero")
	ErrNegativeAmount = errors.New("amount must not be negative")
)

// Parse reads "1234", "1234.5" or "1234.56". It accepts digits and at most one
// decimal point, so a sign, exponent, separator or whitespace inside the number
// cannot sneak through. Zero parses; use ParsePositive when zero is an error.
func Parse(text string) (Kobo, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return 0, ErrInvalidAmount
	}
	whole, fraction, hasPoint := strings.Cut(text, ".")
	if strings.Contains(fraction, ".") {
		return 0, ErrInvalidAmount
	}
	if whole == "" || (hasPoint && (fraction == "" || len(fraction) > 2)) {
		return 0, ErrPrecision
	}
	if !digitsOnly(whole) || (hasPoint && !digitsOnly(fraction)) {
		return 0, ErrInvalidAmount
	}
	// 12 digits is the NUMERIC(12,2) integer ceiling, and keeps whole*100 far
	// from int64 overflow before the range check below.
	if len(strings.TrimLeft(whole, "0")) > 10 {
		return 0, ErrTooLarge
	}
	w, err := strconv.ParseInt(whole, 10, 64)
	if err != nil {
		return 0, ErrTooLarge
	}
	var f int64
	if hasPoint {
		if len(fraction) == 1 {
			fraction += "0"
		}
		f, err = strconv.ParseInt(fraction, 10, 64)
		if err != nil {
			return 0, ErrInvalidAmount
		}
	}
	k := Kobo(w*100 + f)
	if k > MaxKobo {
		return 0, ErrTooLarge
	}
	return k, nil
}

// ParsePositive is Parse that also refuses zero.
func ParsePositive(text string) (Kobo, error) {
	k, err := Parse(text)
	if err != nil {
		return 0, err
	}
	if k <= 0 {
		return 0, ErrInvalidAmount
	}
	return k, nil
}

func digitsOnly(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return s != ""
}

// FromProviderFloat converts a provider-reported amount, which arrives as a JSON
// number, into kobo. It formats the float by its shortest round-trip decimal and
// parses that text, so 19.99 is exactly 1999 kobo with no multiplication, no
// tolerance and no rounding. A value with more than two decimals, a non-finite
// value, zero, a negative, or anything above MaxKobo is rejected.
func FromProviderFloat(f float64) (Kobo, error) {
	if math.IsNaN(f) || math.IsInf(f, 0) || f <= 0 {
		return 0, ErrInvalidAmount
	}
	return Parse(strconv.FormatFloat(f, 'f', -1, 64))
}

// String renders the "1234.56" form the NUMERIC columns and the provider take.
func (k Kobo) String() string {
	if k < 0 {
		// Never stored, but a debug print should not look like a positive amount.
		return "-" + Kobo(-k).String()
	}
	return fmt.Sprintf("%d.%02d", int64(k)/100, int64(k)%100)
}

// Add returns a+b, or ErrOverflow if the int64 sum would wrap.
func Add(a, b Kobo) (Kobo, error) {
	if (b > 0 && a > Kobo(math.MaxInt64)-b) || (b < 0 && a < Kobo(math.MinInt64)-b) {
		return 0, ErrOverflow
	}
	return a + b, nil
}

// Sub returns a-b, or ErrOverflow if the int64 difference would wrap. It does
// not refuse a negative result; Debit is the balance-aware version.
func Sub(a, b Kobo) (Kobo, error) {
	if (b < 0 && a > Kobo(math.MaxInt64)+b) || (b > 0 && a < Kobo(math.MinInt64)+b) {
		return 0, ErrOverflow
	}
	return a - b, nil
}

// MulInt returns a*n, or ErrOverflow.
func MulInt(a Kobo, n int64) (Kobo, error) {
	if a == 0 || n == 0 {
		return 0, nil
	}
	p := int64(a) * n
	if p/n != int64(a) || (int64(a) == math.MinInt64 && n == -1) || (n == math.MinInt64 && int64(a) == -1) {
		return 0, ErrOverflow
	}
	return Kobo(p), nil
}

// BasisPoints returns amount*bps/10000, rounded half up, with every step
// checked. 250 bps is 2.5%.
func BasisPoints(amount Kobo, bps int64) (Kobo, error) {
	if amount < 0 || bps < 0 {
		return 0, ErrNegativeAmount
	}
	scaled, err := MulInt(amount, bps)
	if err != nil {
		return 0, err
	}
	scaled, err = Add(scaled, 5000)
	if err != nil {
		return 0, err
	}
	return scaled / 10000, nil
}

// Credit returns balance+amount and refuses the result if it exceeds limit. The
// limit is clamped to MaxKobo, so even a policy that says "no cap" cannot push a
// balance past what the column can store.
func Credit(balance, amount, limit Kobo) (Kobo, error) {
	if balance < 0 {
		return 0, ErrNegativeAmount
	}
	if amount <= 0 {
		return 0, ErrNotPositive
	}
	if limit > MaxKobo || limit < 0 {
		limit = MaxKobo
	}
	sum, err := Add(balance, amount)
	if err != nil {
		return 0, err
	}
	if sum > limit {
		return 0, ErrExceedsMax
	}
	return sum, nil
}

// Debit returns balance-amount, or ErrInsufficient. The balance never goes
// negative.
func Debit(balance, amount Kobo) (Kobo, error) {
	if balance < 0 {
		return 0, ErrNegativeAmount
	}
	if amount <= 0 {
		return 0, ErrNotPositive
	}
	if balance < amount {
		return 0, ErrInsufficient
	}
	return balance - amount, nil
}
