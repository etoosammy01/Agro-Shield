package escrow

import (
	"errors"
	"math/rand"
	"testing"

	"backend/internal/money"
)

func naira(n int64) money.Kobo { return money.Kobo(n * 100) }

func TestSplitRefundWorkedExamples(t *testing.T) {
	paid, commission := naira(20_000), naira(1_000) // 5%
	cases := []struct {
		name             string
		already, refund  money.Kobo
		seller, platform money.Kobo
	}{
		{"one tuber of ten", 0, naira(2_000), naira(1_900), naira(100)},
		{"full refund", 0, naira(20_000), naira(19_000), naira(1_000)},
		{"second refund completes the order", naira(2_000), naira(18_000), naira(17_100), naira(900)},
		{"one kobo", 0, 1, 1, 0},
		{"half a kobo of commission rounds the cumulative total", 0, 20, 19, 1},
	}
	for _, c := range cases {
		got, err := SplitRefund(paid, commission, c.already, c.refund)
		if err != nil || got.Seller != c.seller || got.Commission != c.platform {
			t.Errorf("%s: got %+v, %v; want seller %d platform %d", c.name, got, err, c.seller, c.platform)
		}
	}
}

func TestSplitRefundRejectsInvalidInput(t *testing.T) {
	paid, commission := naira(20_000), naira(1_000)
	cases := []struct {
		name                         string
		paid, commission, already, r money.Kobo
		want                         error
	}{
		{"zero refund", paid, commission, 0, 0, money.ErrNotPositive},
		{"negative refund", paid, commission, 0, -1, money.ErrNotPositive},
		{"zero paid", 0, 0, 0, 1, money.ErrNotPositive},
		{"negative commission", paid, -1, 0, 1, money.ErrNegativeAmount},
		{"negative already refunded", paid, commission, -1, 1, money.ErrNegativeAmount},
		{"refund above the total", paid, commission, 0, paid + 1, ErrRefundExceedsPaid},
		{"refund above what is left", paid, commission, naira(19_000), naira(1_001), ErrRefundExceedsPaid},
		{"overflowing sum", paid, commission, money.Kobo(1<<63 - 1), 1, money.ErrOverflow},
	}
	for _, c := range cases {
		if _, err := SplitRefund(c.paid, c.commission, c.already, c.r); !errors.Is(err, c.want) {
			t.Errorf("%s: error = %v; want %v", c.name, err, c.want)
		}
	}
	if _, err := SplitRefund(paid, paid+1, 0, 1); err == nil {
		t.Error("commission above the amount paid was accepted")
	}
}

func TestSplitRefundWithNoCommission(t *testing.T) {
	got, err := SplitRefund(naira(5_000), 0, 0, naira(1_234))
	if err != nil || got.Seller != naira(1_234) || got.Commission != 0 {
		t.Fatalf("got %+v, %v", got, err)
	}
}

func TestSplitRefundWorksAtTheColumnCeiling(t *testing.T) {
	// Products around 1e24 do not fit in int64; the 128-bit path must hold.
	paid, commission := money.MaxKobo, money.MaxKobo/20
	got, err := SplitRefund(paid, commission, 0, paid)
	if err != nil || got.Commission != commission || got.Seller != paid-commission {
		t.Fatalf("full refund at the ceiling: %+v, %v", got, err)
	}
	half, err := SplitRefund(paid, commission, 0, paid/2)
	if err != nil || half.Seller+half.Commission != paid/2 {
		t.Fatalf("half refund at the ceiling: %+v, %v", half, err)
	}
}

// The properties the wallet depends on, over many random orders and any
// sequence of partial refunds that adds up to the whole order.
func TestSplitRefundInvariants(t *testing.T) {
	rng := rand.New(rand.NewSource(11))
	for i := 0; i < 20000; i++ {
		paid := money.Kobo(rng.Int63n(int64(money.MaxKobo)) + 1)
		var commission money.Kobo
		if rng.Intn(5) > 0 {
			commission = money.Kobo(rng.Int63n(int64(paid)/5 + 1)) // up to 20%
		}
		net := paid - commission

		var refunded, sellerSum, platformSum money.Kobo
		for refunded < paid {
			remaining := paid - refunded
			var part money.Kobo
			if rng.Intn(4) == 0 {
				part = remaining // finish the order
			} else {
				part = money.Kobo(rng.Int63n(int64(remaining))) + 1
			}
			got, err := SplitRefund(paid, commission, refunded, part)
			if err != nil {
				t.Fatalf("paid=%d commission=%d refunded=%d part=%d: %v", paid, commission, refunded, part, err)
			}
			if got.Seller < 0 || got.Commission < 0 {
				t.Fatalf("negative portion: %+v", got)
			}
			if got.Seller+got.Commission != part {
				t.Fatalf("portions %+v do not add up to the refund %d", got, part)
			}
			refunded += part
			sellerSum += got.Seller
			platformSum += got.Commission
			if sellerSum > net || platformSum > commission {
				t.Fatalf("over-reversed: seller %d/%d platform %d/%d", sellerSum, net, platformSum, commission)
			}
		}
		if sellerSum != net || platformSum != commission {
			t.Fatalf("order fully refunded but seller %d (net %d), platform %d (commission %d)",
				sellerSum, net, platformSum, commission)
		}
		if _, err := SplitRefund(paid, commission, refunded, 1); !errors.Is(err, ErrRefundExceedsPaid) {
			t.Fatalf("a refund after the order is fully refunded = %v", err)
		}
	}
}

// Refund requests in any order must give the same totals: splitting depends on
// the running total, not on how the refunds were grouped.
func TestSplitRefundIsOrderIndependentInTotal(t *testing.T) {
	paid, commission := naira(9_999), naira(499)
	oneShot, _ := SplitRefund(paid, commission, 0, naira(6_000))
	a, _ := SplitRefund(paid, commission, 0, naira(2_500))
	b, _ := SplitRefund(paid, commission, naira(2_500), naira(3_500))
	if a.Seller+b.Seller != oneShot.Seller || a.Commission+b.Commission != oneShot.Commission {
		t.Fatalf("two refunds %+v + %+v differ from one %+v", a, b, oneShot)
	}
}