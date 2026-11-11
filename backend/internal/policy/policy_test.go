package policy

import (
	"errors"
	"math/rand"
	"testing"

	"backend/internal/money"
)

var allOperations = []Operation{OpDeposit, OpTransferIn, OpSaleCredit, OpWithdrawal, OpTransferOut, OpPurchase}

func mustPolicy(t *testing.T) *Policy {
	t.Helper()
	p, err := New(DefaultMatrix())
	if err != nil {
		t.Fatalf("default matrix is invalid: %v", err)
	}
	return p
}

func TestDefaultMatrixIsValid(t *testing.T) {
	if err := Validate(DefaultMatrix()); err != nil {
		t.Fatal(err)
	}
}

func TestTier0CannotMoveMoney(t *testing.T) {
	p := mustPolicy(t)
	for _, op := range allOperations {
		err := p.Check(Request{Tier: Tier0, Op: op, Amount: 1})
		var denied *ErrTierNotPermitted
		if !errors.As(err, &denied) {
			t.Errorf("Tier 0 %s: error = %v; want ErrTierNotPermitted", op, err)
			continue
		}
		if denied.Required != Tier1 {
			t.Errorf("Tier 0 %s: required = %s; want Tier 1", op, denied.Required)
		}
	}
}

func TestUnknownTierAndOperationFailClosed(t *testing.T) {
	p := mustPolicy(t)
	for _, tier := range []Tier{-1, 4, 99} {
		if err := p.Check(Request{Tier: tier, Op: OpDeposit, Amount: 1}); !errors.Is(err, ErrUnknownTier) {
			t.Errorf("tier %d = %v; want ErrUnknownTier", tier, err)
		}
	}
	if err := p.Check(Request{Tier: Tier1, Op: "refund", Amount: 1}); !errors.Is(err, ErrUnknownOperation) {
		t.Errorf("unknown op = %v", err)
	}
	if err := p.Check(Request{Tier: Tier1, Op: "", Amount: 1}); !errors.Is(err, ErrUnknownOperation) {
		t.Errorf("empty op = %v", err)
	}
}

func TestNonPositiveAndNegativeInputsRefused(t *testing.T) {
	p := mustPolicy(t)
	for _, amount := range []money.Kobo{0, -1, -100} {
		if err := p.Check(Request{Tier: Tier1, Op: OpDeposit, Amount: amount}); !errors.Is(err, money.ErrNotPositive) {
			t.Errorf("amount %d = %v", amount, err)
		}
	}
	if err := p.Check(Request{Tier: Tier1, Op: OpDeposit, Amount: 1, Balance: -1}); !errors.Is(err, money.ErrNegativeAmount) {
		t.Errorf("negative balance = %v", err)
	}
	if err := p.Check(Request{Tier: Tier1, Op: OpWithdrawal, Amount: 1, UsedOutToday: -1}); !errors.Is(err, money.ErrNegativeAmount) {
		t.Errorf("negative usage = %v", err)
	}
}

// Every limit is tested at the exact boundary: the limit itself passes and one
// kobo more fails. Off-by-one at a limit is the bug this guards against.
func TestBoundaries(t *testing.T) {
	p := mustPolicy(t)
	for _, tier := range []Tier{Tier1, Tier2} {
		l, _ := p.LimitsFor(tier)

		if err := p.Check(Request{Tier: tier, Op: OpWithdrawal, Amount: l.PerTransaction}); err != nil && !isDaily(err) {
			t.Errorf("%s: a single movement of exactly the per-transaction limit = %v", tier, err)
		}
		assertLimit(t, p.Check(Request{Tier: tier, Op: OpTransferOut, Amount: l.PerTransaction + 1}), LimitPerTransaction, tier.String()+" per-txn+1")

		// Daily out: fill the day to one kobo below the limit.
		used := l.DailyOut - 1
		if err := p.Check(Request{Tier: tier, Op: OpPurchase, Amount: 1, UsedOutToday: used}); err != nil {
			t.Errorf("%s: last kobo of the daily out limit = %v", tier, err)
		}
		assertLimit(t, p.Check(Request{Tier: tier, Op: OpPurchase, Amount: 2, UsedOutToday: used}), LimitDaily, tier.String()+" daily-out+1")

		usedIn := l.DailyIn - 1
		if err := p.Check(Request{Tier: tier, Op: OpDeposit, Amount: 1, UsedInToday: usedIn}); err != nil {
			t.Errorf("%s: last kobo of the daily in limit = %v", tier, err)
		}
		assertLimit(t, p.Check(Request{Tier: tier, Op: OpDeposit, Amount: 2, UsedInToday: usedIn}), LimitDaily, tier.String()+" daily-in+1")

		// Max balance: one kobo of room left.
		bal := l.MaxBalance - 1
		if err := p.Check(Request{Tier: tier, Op: OpSaleCredit, Amount: 1, Balance: bal}); err != nil {
			t.Errorf("%s: credit to exactly the max balance = %v", tier, err)
		}
		assertLimit(t, p.Check(Request{Tier: tier, Op: OpTransferIn, Amount: 2, Balance: bal}), LimitMaxBalance, tier.String()+" balance+1")
	}
}

func isDaily(err error) bool {
	var le *LimitError
	return errors.As(err, &le) && le.Kind == LimitDaily
}

func assertLimit(t *testing.T, err error, kind LimitKind, label string) {
	t.Helper()
	var le *LimitError
	if !errors.As(err, &le) {
		t.Errorf("%s: error = %v; want a LimitError", label, err)
		return
	}
	if le.Kind != kind {
		t.Errorf("%s: kind = %s; want %s", label, le.Kind, kind)
	}
}

// Outgoing money is not held to the balance cap, and incoming money is not held
// to the out-going daily total: the two directions are tracked separately.
func TestDirectionsAreIndependent(t *testing.T) {
	p := mustPolicy(t)
	l, _ := p.LimitsFor(Tier1)
	if err := p.Check(Request{Tier: Tier1, Op: OpWithdrawal, Amount: 100, Balance: l.MaxBalance}); err != nil {
		t.Errorf("withdrawal from a full wallet = %v", err)
	}
	if err := p.Check(Request{Tier: Tier1, Op: OpDeposit, Amount: 100, UsedOutToday: l.DailyOut}); err != nil {
		t.Errorf("deposit after the out limit is used = %v", err)
	}
}

func TestLimitErrorReportsRemaining(t *testing.T) {
	p := mustPolicy(t)
	err := p.Check(Request{Tier: Tier1, Op: OpWithdrawal, Amount: naira(10_000), UsedOutToday: naira(25_000)})
	var le *LimitError
	if !errors.As(err, &le) || le.Remaining != naira(5_000) {
		t.Fatalf("got %v; want 5,000 naira remaining", err)
	}
	err = p.Check(Request{Tier: Tier1, Op: OpDeposit, Amount: naira(20_000), Balance: naira(290_000)})
	if !errors.As(err, &le) || le.Kind != LimitMaxBalance || le.Remaining != naira(10_000) {
		t.Fatalf("got %v; want max balance with 10,000 naira room", err)
	}
	// A balance already above the cap (for example after a limit was lowered)
	// reports zero room rather than a negative number.
	err = p.Check(Request{Tier: Tier1, Op: OpDeposit, Amount: 1, Balance: naira(400_000)})
	if !errors.As(err, &le) || le.Remaining != 0 {
		t.Fatalf("over-cap balance: got %v; want zero room", err)
	}
}

func TestUsageOverflowIsReportedNotWrapped(t *testing.T) {
	p := mustPolicy(t)
	err := p.Check(Request{Tier: Tier2, Op: OpWithdrawal, Amount: 100, UsedOutToday: money.Kobo(1<<63 - 1)})
	if !errors.Is(err, money.ErrOverflow) {
		t.Fatalf("got %v; want ErrOverflow", err)
	}
}

func TestTier3IsNotGrantable(t *testing.T) {
	p := mustPolicy(t)
	if p.Grantable(Tier0) || !p.Grantable(Tier1) || !p.Grantable(Tier2) || p.Grantable(Tier3) || p.Grantable(7) {
		t.Fatal("grantable set is wrong")
	}
}

func TestValidateRejectsBrokenMatrices(t *testing.T) {
	mutate := func(f func(m Matrix)) Matrix {
		m := DefaultMatrix()
		f(m)
		return m
	}
	set := func(m Matrix, tier Tier, f func(l *Limits)) {
		l := m[tier]
		f(&l)
		m[tier] = l
	}
	cases := map[string]Matrix{
		"missing tier": mutate(func(m Matrix) { delete(m, Tier2) }),
		"extra tier":   mutate(func(m Matrix) { m[Tier(4)] = m[Tier3] }),
		"tier 0 allows": mutate(func(m Matrix) {
			set(m, Tier0, func(l *Limits) { l.Allowed = map[Operation]bool{OpDeposit: true} })
		}),
		"tier 0 grantable": mutate(func(m Matrix) { set(m, Tier0, func(l *Limits) { l.Grantable = true }) }),
		"unknown operation": mutate(func(m Matrix) {
			set(m, Tier1, func(l *Limits) { l.Allowed = map[Operation]bool{"mint": true} })
		}),
		"negative limit":   mutate(func(m Matrix) { set(m, Tier1, func(l *Limits) { l.DailyOut = -1 }) }),
		"above column max": mutate(func(m Matrix) { set(m, Tier3, func(l *Limits) { l.MaxBalance = money.MaxKobo + 1 }) }),
		"higher tier stricter": mutate(func(m Matrix) {
			set(m, Tier2, func(l *Limits) { l.DailyOut = 1 })
		}),
		"higher tier drops op": mutate(func(m Matrix) {
			set(m, Tier2, func(l *Limits) {
				l.Allowed = map[Operation]bool{OpDeposit: true}
			})
		}),
		"per-txn above balance cap": mutate(func(m Matrix) {
			set(m, Tier1, func(l *Limits) { l.PerTransaction = l.MaxBalance + 1 })
		}),
	}
	for name, m := range cases {
		if err := Validate(m); !errors.Is(err, ErrInvalidMatrix) {
			t.Errorf("%s: Validate = %v; want ErrInvalidMatrix", name, err)
		}
		if _, err := New(m); err == nil {
			t.Errorf("%s: New accepted an invalid matrix", name)
		}
	}
}

func TestNewCopiesTheMatrix(t *testing.T) {
	m := DefaultMatrix()
	p, err := New(m)
	if err != nil {
		t.Fatal(err)
	}
	m[Tier1].Allowed[OpDeposit] = false
	delete(m[Tier1].Allowed, OpWithdrawal)
	if err := p.Check(Request{Tier: Tier1, Op: OpDeposit, Amount: 100}); err != nil {
		t.Fatalf("editing the caller's matrix changed the policy: %v", err)
	}
	l, _ := p.LimitsFor(Tier1)
	l.Allowed[OpDeposit] = false
	if err := p.Check(Request{Tier: Tier1, Op: OpDeposit, Amount: 100}); err != nil {
		t.Fatalf("editing LimitsFor's copy changed the policy: %v", err)
	}
}

// The acceptance property: whatever the inputs, a request that Check allows is
// inside every limit of its tier, and Tier 0 and unknown tiers never pass.
func TestNoTierCanExceedItsLimits(t *testing.T) {
	p := mustPolicy(t)
	rng := rand.New(rand.NewSource(42))
	pick := func() money.Kobo {
		switch rng.Intn(4) {
		case 0:
			return money.Kobo(rng.Int63n(1000))
		case 1:
			return money.Kobo(rng.Int63n(int64(naira(600_000))))
		case 2:
			return money.Kobo(rng.Int63n(int64(naira(20_000_000))))
		default:
			return money.Kobo(rng.Int63n(int64(money.MaxKobo)))
		}
	}
	allowed := 0
	for i := 0; i < 300000; i++ {
		r := Request{
			Tier: Tier(rng.Intn(6) - 1), Op: allOperations[rng.Intn(len(allOperations))],
			Amount: pick() + 1, Balance: pick(), UsedOutToday: pick(), UsedInToday: pick(),
		}
		if p.Check(r) != nil {
			continue
		}
		allowed++
		l, ok := p.LimitsFor(r.Tier)
		if !ok || r.Tier == Tier0 {
			t.Fatalf("allowed a request for tier %d: %+v", r.Tier, r)
		}
		if r.Amount > l.PerTransaction {
			t.Fatalf("per-transaction limit exceeded: %+v vs %+v", r, l)
		}
		if r.Op.outgoing() && r.UsedOutToday+r.Amount > l.DailyOut {
			t.Fatalf("daily out limit exceeded: %+v vs %+v", r, l)
		}
		if r.Op.incoming() {
			if r.UsedInToday+r.Amount > l.DailyIn {
				t.Fatalf("daily in limit exceeded: %+v vs %+v", r, l)
			}
			if r.Balance+r.Amount > l.MaxBalance || r.Balance+r.Amount > money.MaxKobo {
				t.Fatalf("balance cap exceeded: %+v vs %+v", r, l)
			}
		}
	}
	if allowed == 0 {
		t.Fatal("the property test never allowed a request, so it proved nothing")
	}
}
