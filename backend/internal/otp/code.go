package otp

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"time"
)

const (
	codeLength            = 6
	codeTTL               = 5 * time.Minute
	maxAttempts           = 5
	issueWindow           = 15 * time.Minute
	maxIssuesPerWindow    = 3
)

// generateCode returns a cryptographically secure random 6-digit numeric string.
func generateCode() (string, error) {
	limit := big.NewInt(1)
	for i := 0; i < codeLength; i++ {
		limit.Mul(limit, big.NewInt(10))
	}
	n, err := rand.Int(rand.Reader, limit)
	if err != nil {
		return "", fmt.Errorf("generate one-time passcode: %w", err)
	}
	return fmt.Sprintf("%0*d", codeLength, n.Int64()), nil
}

// isWellFormedCode checks whether the code is exactly 6 numeric digits.
func isWellFormedCode(code string) bool {
	if len(code) != codeLength {
		return false
	}
	for _, r := range code {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
