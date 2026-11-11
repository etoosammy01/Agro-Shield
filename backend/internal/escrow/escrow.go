// Package escrow is the contract between the wallet (Member 4) and the order,
// dispute and refund code (Members 3 and 5): the request and result types, the
// errors, the interface the wallet implements, and the one piece of arithmetic
// that must be exactly right - splitting a refund between seller and platform.
//
// It has no database or wallet dependency, so the dispute and refund code can
// compile and test against it before the wallet implementation exists.
package escrow

import (
	"context"
	"errors"
	"math/bits"

	"backend/internal/money"
)

// Settler is what the wallet service implements. Every method is idempotent:
// repeating a call with the same identifiers changes nothing and reports the
// outcome of the first call.
type Settler interface {
	// ReleaseEscrow moves an order's held funds to the seller's spendable
	// balance. Member 3 calls it after the buyer confirms receipt. It fails with
	// ErrEscrowDisputed while the hold is frozen.
	ReleaseEscrow(ctx context.Context, orderID int64, actorID int) error

	// FreezeEscrow blocks release while a dispute is open. disputeRef is the
	// caller's dispute identifier (for example "dispute:12") and is the
	// idempotency key for the freeze.
	FreezeEscrow(ctx context.Context, orderID int64, actorID int, disputeRef string) error

	// UnfreezeEscrow clears a freeze when a dispute is dismissed or resolved
	// without a refund.
	UnfreezeEscrow(ctx context.Context, orderID int64, actorID int, disputeRef string) error

	// RefundOrder settles one approved refund from the order's held funds.
	RefundOrder(ctx context.Context, req RefundRequest) (*RefundResult, error)
}

// RefundRequest asks the wallet to pay an approved refund.
//
// Only the gross amount is passed. The seller and platform portions are always
// derived by the wallet from the order's recorded payment, commission and
// earlier refunds, so a caller cannot get them wrong and there is one place the
// rule lives.
type RefundRequest struct {
	// RefundID is the caller's refunds.id. It is the idempotency key.
	RefundID int64
	OrderID  int64

	// Amount is the approved GROSS amount to return to the buyer. The buyer
	// always receives exactly this.
	Amount money.Kobo

	// ActorID is the administrator who approved the refund.
	ActorID int
}

// RefundResult reports what a refund did. BuyerCredit always equals
// SellerReversal + CommissionReversal.
type RefundResult struct {
	BuyerCredit        money.Kobo
	SellerReversal     money.Kobo
	CommissionReversal money.Kobo

	// AlreadyApplied is true when this RefundID had been settled before, and the
	// result is the original one.
	AlreadyApplied bool
}

var (
	// ErrRefundRequiresReview means the refund cannot be settled automatically:
	// the held funds cannot cover the seller's portion, the hold is already
	// closed, or the records do not reconcile. No balance changed and a
	// payment_exceptions row was written. Set the refund to requires_review.
	ErrRefundRequiresReview = errors.New("refund requires administrator review")

	// ErrRefundExceedsPaid means the amount is more than is still refundable on
	// the order. Nothing was written.
	ErrRefundExceedsPaid = errors.New("refund exceeds the amount still refundable on the order")

	ErrEscrowNotFound = errors.New("no escrow hold exists for this order")

	// ErrEscrowDisputed means release was attempted while a dispute is open.
	ErrEscrowDisputed = errors.New("funds are frozen by an open dispute")

	// ErrEscrowClosed means the hold has already been fully released or refunded.
	ErrEscrowClosed = errors.New("escrow hold is already closed")
)

// Split is the result of dividing a gross refund.
type Split struct {
	Seller     money.Kobo
	Commission money.Kobo
}

// SplitRefund divides a gross refund between the seller's held proceeds and the
// platform's commission, in whole kobo.
//
//	paid            what the buyer paid for the order (gross)
//	commission      the platform's share of paid, as recorded at payment time
//	alreadyRefunded the gross amount refunded on this order so far
//	refund          the gross amount being refunded now
//
// The commission reversed after a refund is
//
//	cumulative(alreadyRefunded + refund) - cumulative(alreadyRefunded)
//
// where cumulative(n) = round_half_up(n * commission / paid). Computing it
// cumulatively, rather than rounding each refund on its own, guarantees:
//
//   - the platform portions of any number of partial refunds add up to exactly
//     the commission once the whole order is refunded, never a kobo more or less;
//   - a refund of the entire order reverses exactly the recorded commission and
//     exactly the seller's net;
//   - the seller portion is never negative, and Seller + Commission == refund.
//
// 20,000 paid with 1,000 commission, refunding 2,000: commission 100, seller 1,900.
func SplitRefund(paid, commission, alreadyRefunded, refund money.Kobo) (Split, error) {
	switch {
	case paid <= 0 || refund <= 0:
		return Split{}, money.ErrNotPositive
	case commission < 0 || alreadyRefunded < 0:
		return Split{}, money.ErrNegativeAmount
	case commission > paid:
		return Split{}, errors.New("commission exceeds the amount paid")
	}
	after, err := money.Add(alreadyRefunded, refund)
	if err != nil {
		return Split{}, err
	}
	if after > paid {
		return Split{}, ErrRefundExceedsPaid
	}
	before, err := cumulative(alreadyRefunded, commission, paid)
	if err != nil {
		return Split{}, err
	}
	now, err := cumulative(after, commission, paid)
	if err != nil {
		return Split{}, err
	}
	platform := now - before
	if platform < 0 || platform > refund {
		// Cannot happen for valid input (see the tests); refusing is safer than
		// moving money on a result that breaks the invariant.
		return Split{}, errors.New("refund split is inconsistent")
	}
	return Split{Seller: refund - platform, Commission: platform}, nil
}

// cumulative returns round_half_up(n * commission / paid), using a 128-bit
// intermediate so the product cannot overflow int64.
func cumulative(n, commission, paid money.Kobo) (money.Kobo, error) {
	if n == 0 || commission == 0 {
		return 0, nil
	}
	hi, lo := bits.Mul64(uint64(n), uint64(commission))
	// Add paid/2 for round-half-up.
	lo, carry := bits.Add64(lo, uint64(paid)/2, 0)
	hi += carry
	if hi >= uint64(paid) {
		return 0, money.ErrOverflow // quotient would not fit in 64 bits
	}
	quotient, _ := bits.Div64(hi, lo, uint64(paid))
	if quotient > uint64(commission) {
		return 0, money.ErrOverflow
	}
	return money.Kobo(quotient), nil
}