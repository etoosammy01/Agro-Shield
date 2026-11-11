// Package policy is the single KYC policy for money movement.
//
// The matrix is data, and Check is a pure function of (matrix, request): it
// reads no database and no clock, so the same rules apply identically at the web
// handlers, the webhook, USSD and the marketplace, and can be tested without any
// of them. The caller supplies the facts that need a database (the account's
// tier, today's running totals, the balance) from inside the same transaction
// that holds the wallet row lock, so two concurrent requests cannot both pass.
//
// Everything fails closed: an unknown tier, an unknown operation, an invalid
// matrix, or an amount that is not positive is refused.
package policy

import (
	"errors"
	"fmt"

	"backend/internal/money"
)

// Tier is a KYC level. Tier 0 is an account with no verified identity: it may
// sign in and look, and nothing else. It is the default for every account, so a
// missing or unreadable KYC record can only ever mean "no access to money".
type Tier int

const (
	Tier0 Tier = 0
	Tier1 Tier = 1
	Tier2 Tier = 2
	Tier3 Tier = 3
)

func (t Tier) String() string { return fmt.Sprintf("Tier %d", int(t)) }

// Operation is a kind of money movement that the policy governs.
type Operation string

const (
	// Money coming in. These are limited by the receiver's tier.
	OpDeposit    Operation = "deposit"     // card or bank-transfer funding
	OpTransferIn Operation = "transfer_in" // approved wallet-to-wallet credit
	OpSaleCredit Operation = "sale_credit" // marketplace proceeds to a seller
	// Money going out. These are limited by the sender's tier.
	OpWithdrawal  Operation = "withdrawal"
	OpTransferOut Operation = "transfer_out"
	OpPurchase    Operation = "purchase"
)

// Refunds (withdrawal_refund, transfer_refund, deposit reversal) are not
// operations here on purpose. They return money that already belonged to the
// account, so refusing one for a limit would strand a customer's funds. They are
// exempt from limits but still pass through money.Credit, so they cannot
// overflow the column.

func (o Operation) incoming() bool {
	return o == OpDeposit || o == OpTransferIn || o == OpSaleCredit
}

func (o Operation) outgoing() bool {
	return o == OpWithdrawal || o == OpTransferOut || o == OpPurchase
}

// Limits is one tier's row of the matrix.
type Limits struct {
	// Allowed lists the operations the tier may perform. Anything not listed is
	// refused, including operations added to the code later.
	Allowed map[Operation]bool

	// PerTransaction caps a single movement, in either direction.
	PerTransaction money.Kobo

	// DailyOut caps the day's total of withdrawals, transfers out and purchases.
	DailyOut money.Kobo

	// DailyIn caps the day's total of deposits, transfers in and sale credits.
	DailyIn money.Kobo

	// MaxBalance is the most the wallet may hold after a credit.
	MaxBalance money.Kobo

	// Grantable says whether this process can legitimately put an account on this
	// tier. A tier whose evidence the platform does not collect (for example a
	// verified address) stays defined but ungrantable, so it cannot be assigned
	// by mistake and its limits are never reachable.
	Grantable bool

	// Evidence describes what must be verified before the tier is granted.
	Evidence string
}

// Matrix maps each tier to its limits.
type Matrix map[Tier]Limits

// Policy is a validated matrix.
type Policy struct {
	matrix Matrix
}

var (
	ErrUnknownTier      = errors.New("this account's verification level is not recognised")
	ErrUnknownOperation = errors.New("this kind of transaction is not recognised")
	ErrInvalidMatrix    = errors.New("invalid KYC policy matrix")
)

// ErrTierNotPermitted means the account's tier may not perform the operation at
// all. Required is the lowest grantable tier that may.
type ErrTierNotPermitted struct {
	Have, Required Tier
	Op             Operation
}

func (e *ErrTierNotPermitted) Error() string {
	if e.Required == Tier0 {
		return "verify your identity before using your wallet"
	}
	return fmt.Sprintf("this needs %s verification; your account is %s", e.Required, e.Have)
}

// LimitError means an operation is allowed in principle but this amount, today,
// would exceed a limit. Remaining is how much would still have been allowed.
type LimitError struct {
	Kind      LimitKind
	Limit     money.Kobo
	Remaining money.Kobo
}

type LimitKind string

const (
	LimitPerTransaction LimitKind = "per_transaction"
	LimitDaily          LimitKind = "daily"
	LimitMaxBalance     LimitKind = "max_balance"
)

func (e *LimitError) Error() string {
	switch e.Kind {
	case LimitPerTransaction:
		return fmt.Sprintf("that is above the ₦%s limit for a single transaction on your account", e.Limit)
	case LimitDaily:
		return fmt.Sprintf("that would go over your ₦%s daily limit; you can move ₦%s more today", e.Limit, e.Remaining)
	default:
		return fmt.Sprintf("that would take your wallet above its ₦%s maximum balance", e.Limit)
	}
}

// New validates the matrix and returns a Policy. The service should refuse to
// start on an error: a policy that cannot be trusted must not be run.
func New(m Matrix) (*Policy, error) {
	if err := Validate(m); err != nil {
		return nil, err
	}
	copied := make(Matrix, len(m))
	for tier, limits := range m {
		allowed := make(map[Operation]bool, len(limits.Allowed))
		for op, ok := range limits.Allowed {
			allowed[op] = ok
		}
		limits.Allowed = allowed
		copied[tier] = limits
	}
	return &Policy{matrix: copied}, nil
}

// Validate checks the structural rules the acceptance criteria depend on.
func Validate(m Matrix) error {
	tiers := []Tier{Tier0, Tier1, Tier2, Tier3}
	for _, tier := range tiers {
		if _, ok := m[tier]; !ok {
			return fmt.Errorf("%w: %s is missing", ErrInvalidMatrix, tier)
		}
	}
	if len(m) != len(tiers) {
		return fmt.Errorf("%w: unsupported tier present", ErrInvalidMatrix)
	}
	zero := m[Tier0]
	for op, ok := range zero.Allowed {
		if ok {
			return fmt.Errorf("%w: Tier 0 must not allow %s", ErrInvalidMatrix, op)
		}
	}
	if zero.Grantable {
		return fmt.Errorf("%w: Tier 0 is the default and is not grantable", ErrInvalidMatrix)
	}
	var prev Limits
	for i, tier := range tiers {
		l := m[tier]
		for op := range l.Allowed {
			if !op.incoming() && !op.outgoing() {
				return fmt.Errorf("%w: %s lists unknown operation %q", ErrInvalidMatrix, tier, op)
			}
		}
		for name, v := range map[string]money.Kobo{
			"per-transaction": l.PerTransaction, "daily-out": l.DailyOut,
			"daily-in": l.DailyIn, "max-balance": l.MaxBalance,
		} {
			if v < 0 || v > money.MaxKobo {
				return fmt.Errorf("%w: %s %s is outside 0..%s", ErrInvalidMatrix, tier, name, money.MaxKobo)
			}
		}
		if l.PerTransaction > l.MaxBalance && l.MaxBalance > 0 && tier != Tier0 {
			// A single movement larger than the balance cap could never succeed.
			return fmt.Errorf("%w: %s per-transaction limit exceeds its maximum balance", ErrInvalidMatrix, tier)
		}
		if i > 0 {
			// A higher tier must never be stricter than a lower one: limits only
			// widen as more identity evidence is collected.
			if l.PerTransaction < prev.PerTransaction || l.DailyOut < prev.DailyOut ||
				l.DailyIn < prev.DailyIn || l.MaxBalance < prev.MaxBalance {
				return fmt.Errorf("%w: %s is stricter than the tier below it", ErrInvalidMatrix, tier)
			}
			for op, ok := range prev.Allowed {
				if ok && !l.Allowed[op] {
					return fmt.Errorf("%w: %s drops %s that the tier below allows", ErrInvalidMatrix, tier, op)
				}
			}
		}
		prev = l
	}
	return nil
}

// Request is everything Check needs. The caller reads these inside the
// transaction that locks the wallet row; Check itself touches nothing.
type Request struct {
	Tier   Tier
	Op     Operation
	Amount money.Kobo

	// Balance is the wallet balance before this movement.
	Balance money.Kobo

	// UsedOutToday and UsedInToday are the day's totals so far, excluding this
	// request, counting the same operation groups as DailyOut and DailyIn.
	UsedOutToday money.Kobo
	UsedInToday  money.Kobo
}

// Check returns nil only when the request is inside every limit that applies.
func (p *Policy) Check(r Request) error {
	limits, ok := p.matrix[r.Tier]
	if !ok {
		return ErrUnknownTier
	}
	if !r.Op.incoming() && !r.Op.outgoing() {
		return ErrUnknownOperation
	}
	if r.Amount <= 0 {
		return money.ErrNotPositive
	}
	if r.Balance < 0 || r.UsedOutToday < 0 || r.UsedInToday < 0 {
		return money.ErrNegativeAmount
	}
	if !limits.Allowed[r.Op] {
		return &ErrTierNotPermitted{Have: r.Tier, Required: p.lowestTierAllowing(r.Op), Op: r.Op}
	}
	if r.Amount > limits.PerTransaction {
		return &LimitError{Kind: LimitPerTransaction, Limit: limits.PerTransaction, Remaining: limits.PerTransaction}
	}

	if r.Op.outgoing() {
		return p.checkDaily(limits.DailyOut, r.UsedOutToday, r.Amount)
	}

	if err := p.checkDaily(limits.DailyIn, r.UsedInToday, r.Amount); err != nil {
		return err
	}
	if _, err := money.Credit(r.Balance, r.Amount, limits.MaxBalance); err != nil {
		if errors.Is(err, money.ErrExceedsMax) {
			room := limits.MaxBalance - r.Balance
			if room < 0 {
				room = 0
			}
			return &LimitError{Kind: LimitMaxBalance, Limit: limits.MaxBalance, Remaining: room}
		}
		return err
	}
	return nil
}

func (p *Policy) checkDaily(limit, used, amount money.Kobo) error {
	total, err := money.Add(used, amount)
	if err != nil {
		return err
	}
	if total > limit {
		remaining := limit - used
		if remaining < 0 {
			remaining = 0
		}
		return &LimitError{Kind: LimitDaily, Limit: limit, Remaining: remaining}
	}
	return nil
}

// lowestTierAllowing is the lowest grantable tier that may perform op, or
// Tier0 when none can (the message then asks only for verification).
func (p *Policy) lowestTierAllowing(op Operation) Tier {
	for _, tier := range []Tier{Tier1, Tier2, Tier3} {
		l := p.matrix[tier]
		if l.Grantable && l.Allowed[op] {
			return tier
		}
	}
	return Tier0
}

// Grantable reports whether an account may be put on the tier by this process.
func (p *Policy) Grantable(t Tier) bool {
	l, ok := p.matrix[t]
	return ok && l.Grantable
}

// LimitsFor returns a copy of a tier's limits, for display and the policy
// matrix report.
func (p *Policy) LimitsFor(t Tier) (Limits, bool) {
	l, ok := p.matrix[t]
	if !ok {
		return Limits{}, false
	}
	allowed := make(map[Operation]bool, len(l.Allowed))
	for op, v := range l.Allowed {
		allowed[op] = v
	}
	l.Allowed = allowed
	return l, true
}
