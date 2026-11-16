// Package phone defines the shared Nigerian mobile-number standard.
package phone

import (
	"strings"
)

// Normalize accepts common display separators and returns international digits
// (234 plus ten subscriber digits). Letters and other punctuation are invalid.
func Normalize(raw string) string {
	var value strings.Builder
	for _, r := range raw {
		switch {
		case r >= '0' && r <= '9':
			value.WriteRune(r)
		case r == '+' && value.Len() == 0:
			value.WriteRune(r)
		case r == ' ' || (r >= '\t' && r <= '\r') || r == '(' || r == ')' || r == '-':
		default:
			return ""
		}
	}
	digits := value.String()
	if strings.HasPrefix(digits, "+") {
		if !strings.HasPrefix(digits, "+234") {
			return ""
		}
		digits = digits[1:]
	}
	digits = strings.TrimPrefix(digits, "00")
	digits = strings.TrimPrefix(digits, "234")
	digits = strings.TrimPrefix(digits, "0")
	if len(digits) != 10 || digits[0] < '7' || digits[0] > '9' {
		return ""
	}
	return "234" + digits
}
