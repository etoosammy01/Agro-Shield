package money

import (
	"errors"
	"math"
	"math/rand"
	"testing"
)

func TestParse(t *testing.T) {
	good := map[string]Kobo{
		"0": 0, "1": 100, "1.5": 150, "1.50": 150, "0.01": 1, "5000": 500000,
		"9999999999.99": MaxKobo, " 12.30 ": 1230, "007": 700,
	}
	for in, want := range good {
		got, err := Parse(in)
		if err != nil || got != want {
			t.Errorf("Parse(%q) = %d, %v; want %d", in, got, err, want)
		}
	}
	bad := map[string]error{
		"":                        ErrInvalidAmount,
		"   ":                     ErrInvalidAmount,
		"-1":                      ErrInvalidAmount,
		"+1":                      ErrInvalidAmount,
		"1e3":                     ErrInvalidAmount,
		"1,000":                   ErrInvalidAmount,
		"abc":                     ErrInvalidAmount,
		"1 0":                     ErrInvalidAmount,
		".5":                      ErrPrecision,
		"5.":                      ErrPrecision,
		"1.234":                   ErrPrecision,
		"1.2.3":                   ErrInvalidAmount,
		"10000000000":             ErrTooLarge,
		"9999999999.995":          ErrPrecision,
		"99999999999999999999999": ErrTooLarge,
	}
	for in, want := range bad {
		if _, err := Parse(in); !errors.Is(err, want) {
			t.Errorf("Parse(%q) error = %v; want %v", in, err, want)
		}
	}
}

func TestParsePositiveRejectsZero(t *testing.T) {
	for _, in := range []string{"0", "0.00", "000"} {
		if _, err := ParsePositive(in); !errors.Is(err, ErrInvalidAmount) {
			t.Errorf("ParsePositive(%q) = %v; want ErrInvalidAmount", in, err)
		}
	}
}

func TestStringRoundTrip(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	for i := 0; i < 20000; i++ {
		k := Kobo(rng.Int63n(int64(MaxKobo) + 1))
		got, err := Parse(k.String())
		if err != nil || got != k {
			t.Fatalf("round trip of %d via %q = %d, %v", k, k.String(), got, err)
		}
	}
	for _, k := range []Kobo{0, 1, 99, 100, MaxKobo} {
		if got, err := Parse(k.String()); err != nil || got != k {
			t.Fatalf("boundary %d via %q = %d, %v", k, k.String(), got, err)
		}
	}
	if Kobo(-150).String() != "-1.50" {
		t.Errorf("negative String = %q", Kobo(-150).String())
	}
}

func TestFromProviderFloat(t *testing.T) {
	good := map[float64]Kobo{
		19.99: 1999, 0.1: 10, 0.07: 7, 1234567890.12: 123456789012, 5000: 500000,
		1.005*0 + 0.29: 29, 4.35: 435, 9999999999.99: MaxKobo,
	}
	for in, want := range good {
		got, err := FromProviderFloat(in)
		if err != nil || got != want {
			t.Errorf("FromProviderFloat(%v) = %d, %v; want %d", in, got, err, want)
		}
	}
	// The classic float traps: these products are not exact in binary, and the
	// old amount*100 approach needed a tolerance to cope with them.
	for _, in := range []float64{0.07, 0.29, 1.15, 4.35, 8.2, 19.99, 100.07} {
		k, err := FromProviderFloat(in)
		if err != nil {
			t.Errorf("FromProviderFloat(%v) unexpectedly failed: %v", in, err)
			continue
		}
		back, err := Parse(k.String())
		if err != nil || back != k {
			t.Errorf("FromProviderFloat(%v) = %d does not round trip", in, k)
		}
	}
	bad := []float64{0, -1, -0.01, 0.001, 1.234, math.NaN(), math.Inf(1), math.Inf(-1),
		1e10, 1e21, math.MaxFloat64, math.SmallestNonzeroFloat64}
	for _, in := range bad {
		if k, err := FromProviderFloat(in); err == nil {
			t.Errorf("FromProviderFloat(%v) = %d; want an error", in, k)
		}
	}
}

func TestAddSubOverflow(t *testing.T) {
	max := Kobo(math.MaxInt64)
	min := Kobo(math.MinInt64)
	if _, err := Add(max, 1); !errors.Is(err, ErrOverflow) {
		t.Error("Add(max,1) did not overflow")
	}
	if _, err := Add(min, -1); !errors.Is(err, ErrOverflow) {
		t.Error("Add(min,-1) did not overflow")
	}
	if got, err := Add(max-1, 1); err != nil || got != max {
		t.Errorf("Add(max-1,1) = %d, %v", got, err)
	}
	if _, err := Sub(min, 1); !errors.Is(err, ErrOverflow) {
		t.Error("Sub(min,1) did not overflow")
	}
	if _, err := Sub(max, -1); !errors.Is(err, ErrOverflow) {
		t.Error("Sub(max,-1) did not overflow")
	}
	if got, err := Sub(5, 7); err != nil || got != -2 {
		t.Errorf("Sub(5,7) = %d, %v", got, err)
	}
}

func TestMulInt(t *testing.T) {
	cases := []struct {
		a    Kobo
		n    int64
		want Kobo
		err  error
	}{
		{100, 3, 300, nil}, {0, math.MaxInt64, 0, nil}, {5, 0, 0, nil},
		{Kobo(math.MaxInt64), 2, 0, ErrOverflow},
		{Kobo(math.MaxInt64/2 + 1), 2, 0, ErrOverflow},
		{Kobo(math.MinInt64), -1, 0, ErrOverflow},
		{-1, math.MinInt64, 0, ErrOverflow},
		{Kobo(math.MaxInt64 / 3), 3, Kobo(math.MaxInt64/3) * 3, nil},
	}
	for _, c := range cases {
		got, err := MulInt(c.a, c.n)
		if !errors.Is(err, c.err) || got != c.want {
			t.Errorf("MulInt(%d,%d) = %d, %v; want %d, %v", c.a, c.n, got, err, c.want, c.err)
		}
	}
}

func TestBasisPoints(t *testing.T) {
	cases := []struct {
		amount Kobo
		bps    int64
		want   Kobo
	}{
		{100000, 250, 2500}, // 2.5% of N1,000.00
		{1, 250, 0},         // below half a kobo rounds down
		{20, 250, 1},        // exactly half a kobo rounds up
		{199, 5000, 100},    // 50% of 1.99 = 0.995 -> 1.00
		{0, 250, 0}, {12345, 0, 0},
	}
	for _, c := range cases {
		got, err := BasisPoints(c.amount, c.bps)
		if err != nil || got != c.want {
			t.Errorf("BasisPoints(%d,%d) = %d, %v; want %d", c.amount, c.bps, got, err, c.want)
		}
	}
	if _, err := BasisPoints(Kobo(math.MaxInt64), 10000); !errors.Is(err, ErrOverflow) {
		t.Error("BasisPoints did not detect overflow")
	}
	if _, err := BasisPoints(-1, 250); !errors.Is(err, ErrNegativeAmount) {
		t.Error("BasisPoints accepted a negative amount")
	}
}

func TestCredit(t *testing.T) {
	if got, err := Credit(100, 50, 150); err != nil || got != 150 {
		t.Errorf("credit to exactly the limit = %d, %v", got, err)
	}
	if _, err := Credit(100, 51, 150); !errors.Is(err, ErrExceedsMax) {
		t.Errorf("credit one kobo over the limit = %v", err)
	}
	if _, err := Credit(0, 0, 100); !errors.Is(err, ErrNotPositive) {
		t.Errorf("zero credit = %v", err)
	}
	if _, err := Credit(-1, 5, 100); !errors.Is(err, ErrNegativeAmount) {
		t.Errorf("negative balance = %v", err)
	}
	// A policy that says "no cap" is still held to the column ceiling.
	if _, err := Credit(MaxKobo, 1, math.MaxInt64); !errors.Is(err, ErrExceedsMax) {
		t.Errorf("credit past the column ceiling = %v", err)
	}
	if got, err := Credit(MaxKobo-1, 1, math.MaxInt64); err != nil || got != MaxKobo {
		t.Errorf("credit to the column ceiling = %d, %v", got, err)
	}
	// int64 overflow is reported before the limit is even compared.
	if _, err := Credit(Kobo(math.MaxInt64), 1, MaxKobo); !errors.Is(err, ErrOverflow) {
		t.Errorf("int64 overflow on credit = %v", err)
	}
}

func TestDebit(t *testing.T) {
	if got, err := Debit(100, 100); err != nil || got != 0 {
		t.Errorf("debit to zero = %d, %v", got, err)
	}
	if _, err := Debit(100, 101); !errors.Is(err, ErrInsufficient) {
		t.Errorf("overdraw = %v", err)
	}
	if _, err := Debit(100, 0); !errors.Is(err, ErrNotPositive) {
		t.Errorf("zero debit = %v", err)
	}
	if _, err := Debit(100, -5); !errors.Is(err, ErrNotPositive) {
		t.Errorf("negative debit = %v", err)
	}
}

// A long random sequence of credits and debits must never leave a balance
// outside [0, MaxKobo], and must agree with a second, independent computation.
func TestBalanceNeverLeavesBounds(t *testing.T) {
	rng := rand.New(rand.NewSource(7))
	balance := Kobo(0)
	for i := 0; i < 200000; i++ {
		amount := Kobo(rng.Int63n(int64(MaxKobo)/4) + 1)
		var next Kobo
		var err error
		if rng.Intn(2) == 0 {
			next, err = Credit(balance, amount, MaxKobo)
			if err == nil && next != balance+amount {
				t.Fatalf("credit %d+%d = %d", balance, amount, next)
			}
		} else {
			next, err = Debit(balance, amount)
			if err == nil && next != balance-amount {
				t.Fatalf("debit %d-%d = %d", balance, amount, next)
			}
		}
		if err == nil {
			balance = next
		}
		if balance < 0 || balance > MaxKobo {
			t.Fatalf("balance %d left [0, %d] at step %d", balance, MaxKobo, i)
		}
	}
}
