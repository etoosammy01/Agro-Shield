package services

import (
	"context"
	"errors"
	"strings"
	"testing"

	"backend/internal/models"
	"backend/internal/repository"
)

func TestRecipientPhoneForms(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want []string
	}{
		{
			name: "local trunk form",
			in:   "08012345678",
			want: []string{"2348012345678", "08012345678", "8012345678"},
		},
		{
			name: "international form",
			in:   "+2348012345678",
			want: []string{"2348012345678", "08012345678", "8012345678"},
		},
		{
			name: "already normalized",
			in:   "2348012345678",
			want: []string{"2348012345678", "08012345678", "8012345678"},
		},
		{
			name: "spaced",
			in:   "0801 234 5678",
			want: []string{"2348012345678", "08012345678", "8012345678"},
		},
		{name: "invalid returns nothing", in: "12345", want: nil},
		{name: "empty returns nothing", in: "", want: nil},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := recipientPhoneForms(test.in)
			if len(got) != len(test.want) {
				t.Fatalf("recipientPhoneForms(%q) = %v, want %v", test.in, got, test.want)
			}
			for i := range got {
				if got[i] != test.want[i] {
					t.Fatalf("recipientPhoneForms(%q) = %v, want %v", test.in, got, test.want)
				}
			}
		})
	}
}

func TestNormalizeTransferNote(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    string
		wantErr bool
	}{
		{name: "plain text", in: "Payment for maize", want: "Payment for maize"},
		{name: "trimmed", in: "  seed money  ", want: "seed money"},
		{name: "empty", in: "", want: ""},
		{name: "newline becomes space", in: "first\nsecond", want: "first second"},
		{name: "tab becomes space", in: "first\tsecond", want: "first second"},
		{name: "control characters dropped", in: "a\x00b\x07c", want: "abc"},
		{name: "at the limit", in: strings.Repeat("a", 140), want: strings.Repeat("a", 140)},
		{name: "past the limit", in: strings.Repeat("a", 141), wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := normalizeTransferNote(test.in)
			if (err != nil) != test.wantErr {
				t.Fatalf("normalizeTransferNote() error = %v, wantErr %v", err, test.wantErr)
			}
			if err == nil && got != test.want {
				t.Fatalf("normalizeTransferNote(%q) = %q, want %q", test.in, got, test.want)
			}
		})
	}
}

// TestNormalizeTransferNoteCountsRunes guards against rejecting a note by byte
// length, which would cut multilingual notes short of the stated 140 characters.
func TestNormalizeTransferNoteCountsRunes(t *testing.T) {
	note := strings.Repeat("₦", 140) // 3 bytes each, 140 characters
	got, err := normalizeTransferNote(note)
	if err != nil {
		t.Fatalf("normalizeTransferNote() rejected 140 multi-byte characters: %v", err)
	}
	if got != note {
		t.Fatal("normalizeTransferNote() altered a note within the limit")
	}
}

type transferFarmerLookup struct {
	byPhone     map[string]*models.Farmer
	byID        map[int]*models.Farmer
	byWallet    map[string]*models.Farmer
	byAccount   map[string][]*models.Farmer
	phoneHits   int
	walletHits  int
	accountHits int
}

func (f *transferFarmerLookup) GetByID(id int) (*models.Farmer, error) {
	return f.byID[id], nil
}

func (f *transferFarmerLookup) GetByPhone(phone string) (*models.Farmer, error) {
	f.phoneHits++
	return f.byPhone[phone], nil
}

func (f *transferFarmerLookup) GetByWalletNumber(walletNumber string) (*models.Farmer, error) {
	f.walletHits++
	return f.byWallet[walletNumber], nil
}

func (f *transferFarmerLookup) FindByAccountNumber(accountNumber string) ([]*models.Farmer, error) {
	f.accountHits++
	return f.byAccount[accountNumber], nil
}

func (f *transferFarmerLookup) UpdateBankDetails(int, string, string, string, string) error {
	return nil
}

type transferAccountLookup struct {
	walletStore
	account *models.WalletVirtualAccount
	err     error
}

func (f *transferAccountLookup) GetVirtualAccountByAccountNumber(context.Context, string) (*models.WalletVirtualAccount, error) {
	if f.err != nil {
		return nil, f.err
	}
	if f.account == nil {
		return nil, repository.ErrVirtualAccountNotFound
	}
	return f.account, nil
}

func TestResolveTransferRecipientByAgroShieldAccountNumber(t *testing.T) {
	accounts := &transferAccountLookup{account: &models.WalletVirtualAccount{
		FarmerID: 42, AccountNumber: "0106787237", Status: models.VirtualAccountActive,
	}}
	farmers := &transferFarmerLookup{byID: map[int]*models.Farmer{
		42: {ID: 42, FullName: "James Inalegwu"},
	}}

	recipient, err := (&WalletService{repo: accounts, farmers: farmers}).ResolveTransferRecipient(
		context.Background(), 7, "0106787237",
	)
	if err != nil {
		t.Fatalf("ResolveTransferRecipient() error = %v", err)
	}
	if recipient.ID != 42 || recipient.FullName != "James Inalegwu" {
		t.Fatalf("ResolveTransferRecipient() = %+v, want the account number's owner", recipient)
	}
	if farmers.phoneHits != 0 {
		t.Errorf("made %d phone lookups for an account number, want 0", farmers.phoneHits)
	}
}

// Most members never open a Flutterwave funding account, but they do save the
// bank account where payouts go. That saved number must find them too.
func TestResolveTransferRecipientBySavedAccountNumber(t *testing.T) {
	accounts := &transferAccountLookup{}
	farmers := &transferFarmerLookup{byAccount: map[string][]*models.Farmer{
		"0106787237": {{ID: 42, FullName: "James Inalegwu"}},
	}}

	recipient, err := (&WalletService{repo: accounts, farmers: farmers}).ResolveTransferRecipient(
		context.Background(), 7, "0106787237",
	)
	if err != nil {
		t.Fatalf("ResolveTransferRecipient() error = %v", err)
	}
	if recipient.ID != 42 || recipient.FullName != "James Inalegwu" {
		t.Fatalf("ResolveTransferRecipient() = %+v, want the saved account number's owner", recipient)
	}
}

// Every member has a permanent Agro-Shield wallet number, even without a
// Flutterwave funding account or saved bank details. It must find them on its
// own.
func TestResolveTransferRecipientByWalletNumber(t *testing.T) {
	accounts := &transferAccountLookup{}
	farmers := &transferFarmerLookup{byWallet: map[string]*models.Farmer{
		"8000000042": {ID: 42, FullName: "Ada Obi", WalletNumber: "8000000042"},
	}}

	recipient, err := (&WalletService{repo: accounts, farmers: farmers}).ResolveTransferRecipient(
		context.Background(), 7, "8000 000 042",
	)
	if err != nil {
		t.Fatalf("ResolveTransferRecipient() error = %v", err)
	}
	if recipient.ID != 42 || recipient.FullName != "Ada Obi" {
		t.Fatalf("ResolveTransferRecipient() = %+v, want the wallet number's owner", recipient)
	}
	if farmers.walletHits != 1 {
		t.Errorf("made %d wallet number lookups, want 1", farmers.walletHits)
	}
	if farmers.accountHits != 0 || farmers.phoneHits != 0 {
		t.Errorf("fell through after a wallet match: %d account, %d phone lookups", farmers.accountHits, farmers.phoneHits)
	}
}

// A wallet number is issued by Agro-Shield and belongs to exactly one member,
// so it wins over a bank account number with the same digits.
func TestResolveTransferRecipientPrefersWalletNumber(t *testing.T) {
	accounts := &transferAccountLookup{account: &models.WalletVirtualAccount{
		FarmerID: 43, AccountNumber: "8000000042", Status: models.VirtualAccountActive,
	}}
	farmers := &transferFarmerLookup{
		byWallet: map[string]*models.Farmer{
			"8000000042": {ID: 42, FullName: "Wallet Holder"},
		},
		byID: map[int]*models.Farmer{43: {ID: 43, FullName: "Account Holder"}},
		byAccount: map[string][]*models.Farmer{
			"8000000042": {{ID: 44, FullName: "Saved Account Holder"}},
		},
	}

	recipient, err := (&WalletService{repo: accounts, farmers: farmers}).ResolveTransferRecipient(
		context.Background(), 7, "8000000042",
	)
	if err != nil {
		t.Fatalf("ResolveTransferRecipient() error = %v", err)
	}
	if recipient.ID != 42 {
		t.Fatalf("ResolveTransferRecipient() chose %d, want wallet number owner 42", recipient.ID)
	}
}

// A ten-digit value that is not a wallet number still has to work as a phone
// or a saved bank account number.
func TestResolveTransferRecipientFallsThroughAnUnknownWalletNumber(t *testing.T) {
	accounts := &transferAccountLookup{}
	farmers := &transferFarmerLookup{
		byWallet: map[string]*models.Farmer{},
		byPhone: map[string]*models.Farmer{
			"08012345678": {ID: 42, FullName: "Ada Obi"},
		},
	}

	recipient, err := (&WalletService{repo: accounts, farmers: farmers}).ResolveTransferRecipient(
		context.Background(), 7, "8012345678",
	)
	if err != nil {
		t.Fatalf("ResolveTransferRecipient() error = %v", err)
	}
	if recipient.ID != 42 {
		t.Fatalf("ResolveTransferRecipient() = %+v, want the phone number's owner", recipient)
	}
	if farmers.walletHits != 1 || farmers.phoneHits == 0 {
		t.Errorf("wallet hits = %d, phone hits = %d, want a wallet miss followed by a phone match", farmers.walletHits, farmers.phoneHits)
	}
}

// A saved account number can exist at more than one bank, so the lookup must
// refuse to guess rather than sending money to the wrong member.
func TestResolveTransferRecipientRefusesAnAmbiguousAccountNumber(t *testing.T) {
	accounts := &transferAccountLookup{}
	farmers := &transferFarmerLookup{byAccount: map[string][]*models.Farmer{
		"0106787237": {
			{ID: 42, FullName: "James Inalegwu"},
			{ID: 43, FullName: "Another Member"},
		},
	}}

	_, err := (&WalletService{repo: accounts, farmers: farmers}).ResolveTransferRecipient(
		context.Background(), 7, "0106787237",
	)
	if !errors.Is(err, ErrTransferRecipientAmbiguous) {
		t.Fatalf("ResolveTransferRecipient() error = %v, want ErrTransferRecipientAmbiguous", err)
	}
}

func TestResolveTransferRecipientFallsBackToPhoneNumber(t *testing.T) {
	accounts := &transferAccountLookup{}
	farmers := &transferFarmerLookup{byPhone: map[string]*models.Farmer{
		"08012345678": {ID: 42, FullName: "Ada Obi"},
	}}

	recipient, err := (&WalletService{repo: accounts, farmers: farmers}).ResolveTransferRecipient(
		context.Background(), 7, "0801 234 5678",
	)
	if err != nil {
		t.Fatalf("ResolveTransferRecipient() error = %v", err)
	}
	if recipient.ID != 42 {
		t.Fatalf("ResolveTransferRecipient() = %+v, want the phone number's owner", recipient)
	}
}

func TestResolveTransferRecipientPrefersAnExistingTenDigitAccountNumber(t *testing.T) {
	accounts := &transferAccountLookup{account: &models.WalletVirtualAccount{
		FarmerID: 42, AccountNumber: "8012345678", Status: models.VirtualAccountActive,
	}}
	farmers := &transferFarmerLookup{
		byID:    map[int]*models.Farmer{42: {ID: 42, FullName: "Account Holder"}},
		byPhone: map[string]*models.Farmer{"2348012345678": {ID: 99, FullName: "Phone Holder"}},
	}

	recipient, err := (&WalletService{repo: accounts, farmers: farmers}).ResolveTransferRecipient(
		context.Background(), 7, "8012345678",
	)
	if err != nil {
		t.Fatalf("ResolveTransferRecipient() error = %v", err)
	}
	if recipient.ID != 42 {
		t.Fatalf("ResolveTransferRecipient() chose account %d, want account number 42", recipient.ID)
	}
}

func TestResolveTransferRecipientReportsAnUnknownIdentifier(t *testing.T) {
	accounts := &transferAccountLookup{}
	farmers := &transferFarmerLookup{}

	_, err := (&WalletService{repo: accounts, farmers: farmers}).ResolveTransferRecipient(
		context.Background(), 7, "0106787237",
	)
	if !errors.Is(err, ErrTransferRecipientNotFound) {
		t.Fatalf("ResolveTransferRecipient() error = %v, want ErrTransferRecipientNotFound", err)
	}
}
