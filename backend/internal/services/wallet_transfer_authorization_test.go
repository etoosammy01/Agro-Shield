package services

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"backend/internal/models"
	"backend/internal/otp"
	"backend/internal/repository"
)

type transferAuthMoneyStore struct {
	walletStore
	balance                   string
	writes, sender, recipient int
	amount, note              string
	events                    *[]string
}

func (s *transferAuthMoneyStore) GetWallet(_ context.Context, owner int) (*models.Wallet, error) {
	return &models.Wallet{ID: 99, FarmerID: owner, Balance: s.balance, Currency: "NGN"}, nil
}

func (s *transferAuthMoneyStore) CreateTransfer(_ context.Context, sender, recipient int, _, amount, note string) error {
	s.writes++
	s.sender, s.recipient, s.amount, s.note = sender, recipient, amount, note
	if s.events != nil {
		*s.events = append(*s.events, "reserve")
	}
	return nil
}

type transferAuthCodes struct {
	calls, owner  int
	purpose, code string
	err           error
	used          bool
	events        *[]string
}

func (c *transferAuthCodes) Verify(_ context.Context, owner int, purpose, code string) error {
	c.calls++
	c.owner, c.purpose, c.code = owner, purpose, code
	if c.events != nil {
		*c.events = append(*c.events, "OTP")
	}
	if c.err != nil {
		return c.err
	}
	if c.used {
		return otp.ErrUnavailable
	}
	if code != "012345" {
		return otp.ErrInvalid
	}
	c.used = true
	return nil
}

func transferAuthFixture() (*WalletService, *transferAuthMoneyStore, *pinTestStore, *transferAuthCodes, *[]string) {
	events := []string{}
	store := &transferAuthMoneyStore{balance: "1000.00", events: &events}
	pins := &pinTestStore{expectedPIN: "0456", events: &events}
	codes := &transferAuthCodes{events: &events}
	farmers := &fakeFarmerStore{farmer: &models.Farmer{ID: 7, FullName: "Bola Farms"}}
	service := NewWalletService(store, farmers, nil, nil, nil, 0, nil)
	service.SetTransferVerification(NewTransactionPINService(pins), codes)
	return service, store, pins, codes, &events
}

func TestTransferRequiresWalletPINBeforeOTPAndFundReservation(t *testing.T) {
	for _, tc := range []struct {
		name, pin    string
		pinErr, want error
	}{
		{"missing PIN", "", nil, ErrInvalidTransactionPIN},
		{"malformed PIN", "45ab", nil, ErrInvalidTransactionPIN},
		{"wrong PIN", "9999", nil, repository.ErrTransactionPINIncorrect},
		{"locked PIN", "0456", repository.ErrTransactionPINLocked, repository.ErrTransactionPINLocked},
		{"PIN not created", "0456", repository.ErrTransactionPINNotSet, repository.ErrTransactionPINNotSet},
		{"PIN storage failure", "0456", errors.New("database unavailable"), ErrTransactionPINUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			service, money, pins, codes, _ := transferAuthFixture()
			pins.err = tc.pinErr
			err := service.RequestTransfer(context.Background(), 7, 42, "25", "seeds", tc.pin, "012345")
			if !errors.Is(err, tc.want) || codes.calls != 0 || money.writes != 0 {
				t.Fatalf("transfer = %v, OTP calls = %d, writes = %d", err, codes.calls, money.writes)
			}
		})
	}
}

func TestTransferRequiresOTPAfterWalletPIN(t *testing.T) {
	for _, failure := range []error{otp.ErrInvalid, otp.ErrUnavailable, otp.ErrTooManyAttempts, errors.New("verification storage unavailable")} {
		service, money, _, codes, events := transferAuthFixture()
		codes.err = failure
		if err := service.RequestTransfer(context.Background(), 7, 42, "25", "seeds", "0456", "012345"); err == nil || money.writes != 0 {
			t.Fatal("OTP failure allowed a transfer")
		}
		if !reflect.DeepEqual(*events, []string{"PIN", "OTP"}) {
			t.Fatalf("verification order = %v", *events)
		}
	}
}

func TestTransferAuthorizesOnlySenderAndRejectsOTPReplay(t *testing.T) {
	service, money, pins, codes, events := transferAuthFixture()
	if err := service.RequestTransfer(context.Background(), 7, 42, "25", " seeds ", "0456", "012345"); err != nil {
		t.Fatal(err)
	}
	if pins.verifiedOwner != 7 || codes.owner != 7 || codes.purpose != models.OTPPurposeTransfer || codes.code != "012345" {
		t.Fatal("transfer verified credentials against another wallet or purpose")
	}
	if money.sender != 7 || money.recipient != 42 || money.amount != "25.00" || money.note != "seeds" ||
		!reflect.DeepEqual(*events, []string{"PIN", "OTP", "reserve"}) {
		t.Fatalf("incorrect transfer details or order: %+v, %v", money, *events)
	}
	if err := service.RequestTransfer(context.Background(), 7, 42, "25", "seeds", "0456", "012345"); !errors.Is(err, otp.ErrUnavailable) || money.writes != 1 {
		t.Fatal("reused OTP reserved funds twice")
	}
}

func TestTransferRejectsInvalidDetailsBeforeVerification(t *testing.T) {
	for _, tc := range []struct {
		name, amount string
		recipient    int
	}{
		{"self transfer", "25", 7},
		{"missing recipient", "25", 0},
		{"negative amount", "-1", 42},
		{"insufficient balance", "2000", 42},
	} {
		t.Run(tc.name, func(t *testing.T) {
			service, money, pins, codes, _ := transferAuthFixture()
			if err := service.RequestTransfer(context.Background(), 7, tc.recipient, tc.amount, "", "0456", "012345"); err == nil || pins.verifications != 0 || codes.calls != 0 || money.writes != 0 {
				t.Fatal("invalid transfer reached credentials or fund reservation")
			}
		})
	}
	service, money, _, _, _ := transferAuthFixture()
	service.SetTransferVerification(nil, nil)
	if err := service.RequestTransfer(context.Background(), 7, 42, "25", "", "0456", "012345"); !errors.Is(err, ErrTransactionPINUnavailable) || money.writes != 0 {
		t.Fatal("unconfigured transfer verification did not fail closed")
	}
}
