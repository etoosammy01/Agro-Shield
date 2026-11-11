package policy

import "backend/internal/money"

// naira converts whole naira to kobo for the matrix below.
func naira(n int64) money.Kobo { return money.Kobo(n * 100) }

// DefaultMatrix is the shipped policy.
//
// THE NUMBERS BELOW ARE NOT CONFIRMED. They come from two published summaries of
// the CBN tiered KYC framework, and those summaries disagree with each other on
// the daily figures (one gives Tier 1 as N30,000 a day, another N50,000; one
// gives Tier 2 as N500,000 a day, another N200,000). The balance caps agree
// (N300,000 and N500,000). Where sources disagree the lower figure is used, so
// the default can only be stricter than the rule, never looser.
//
// Before go-live, compliance must confirm each figure against the CBN circular
// that governs this licence category and adjust it here. Nothing else in the code
// needs to change: limits are data, and Validate re-checks the matrix at startup.
//
// Tier 3 is defined but not grantable: it needs a verified residential address,
// which this platform does not collect. Defining it keeps the matrix complete;
// leaving it ungrantable keeps its limits unreachable.
func DefaultMatrix() Matrix {
	allOps := map[Operation]bool{
		OpDeposit: true, OpTransferIn: true, OpSaleCredit: true,
		OpWithdrawal: true, OpTransferOut: true, OpPurchase: true,
	}
	return Matrix{
		Tier0: {
			Allowed:  map[Operation]bool{},
			Evidence: "None. Default for every account; no money movement.",
		},
		Tier1: {
			Allowed:        allOps,
			PerTransaction: naira(30_000),
			DailyOut:       naira(30_000),
			DailyIn:        naira(30_000),
			MaxBalance:     naira(300_000),
			Grantable:      true,
			Evidence:       "Full name, date of birth and a BVN or NIN confirmed by a reviewer.",
		},
		Tier2: {
			Allowed:        allOps,
			PerTransaction: naira(100_000),
			DailyOut:       naira(200_000),
			DailyIn:        naira(200_000),
			MaxBalance:     naira(500_000),
			Grantable:      true,
			Evidence:       "Tier 1 plus a government-issued photo ID confirmed by a reviewer.",
		},
		Tier3: {
			Allowed:        allOps,
			PerTransaction: naira(5_000_000),
			DailyOut:       naira(5_000_000),
			DailyIn:        naira(5_000_000),
			MaxBalance:     money.MaxKobo,
			Grantable:      false,
			Evidence:       "Tier 2 plus a verified residential address. Not collected yet, so not grantable.",
		},
	}
}
