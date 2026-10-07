package services

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"

	"backend/internal/models"
	"backend/internal/repository"
)

type WalletService struct {
	repo     *repository.WalletRepository
	farmers  *repository.FarmerRepository
	flutter  *FlutterwaveClient
	adminIDs map[int]struct{}
}

func NewWalletService(repo *repository.WalletRepository, farmers *repository.FarmerRepository, flutter *FlutterwaveClient, adminIDs []int) *WalletService {
	adminSet := make(map[int]struct{}, len(adminIDs))
	for _, id := range adminIDs {
		if id > 0 {
			adminSet[id] = struct{}{}
		}
	}
	return &WalletService{repo: repo, farmers: farmers, flutter: flutter, adminIDs: adminSet}
}

func ParseWalletAdminIDs(value string) ([]int, error) {
	seen := make(map[int]struct{})
	var ids []int
	for _, part := range strings.Split(value, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		id, err := strconv.Atoi(part)
		if err != nil || id <= 0 {
			return nil, fmt.Errorf("invalid user ID in WALLET_ADMIN_USER_IDS: %q", part)
		}
		if _, exists := seen[id]; !exists {
			ids = append(ids, id)
			seen[id] = struct{}{}
		}
	}
	return ids, nil
}

func (s *WalletService) IsAdmin(farmerID int) bool {
	_, ok := s.adminIDs[farmerID]
	return ok
}

func (s *WalletService) Wallet(ctx context.Context, farmerID int) (*models.Wallet, []models.WalletTransaction, error) {
	wallet, err := s.repo.GetWallet(ctx, farmerID)
	if err != nil {
		return nil, nil, err
	}
	transactions, err := s.repo.ListTransactions(ctx, farmerID)
	if err != nil {
		return nil, nil, err
	}
	return wallet, transactions, nil
}

func (s *WalletService) Withdrawals(ctx context.Context, farmerID int) ([]models.Withdrawal, error) {
	return s.repo.ListFarmerWithdrawals(ctx, farmerID)
}

func (s *WalletService) Deposits(ctx context.Context, farmerID int) ([]models.WalletDeposit, error) {
	return s.repo.ListDeposits(ctx, farmerID)
}

func normalizeWalletAmount(amount string, wholeUnits bool) (string, int64, error) {
	amount = strings.TrimSpace(amount)
	if amount == "" {
		return "", 0, errors.New("amount is required")
	}
	parts := strings.Split(amount, ".")
	if len(parts) > 2 || parts[0] == "" || len(parts) == 2 && (len(parts[1]) == 0 || len(parts[1]) > 2) {
		return "", 0, errors.New("enter a valid positive amount with up to two decimal places")
	}
	for _, part := range parts {
		for _, digit := range part {
			if digit < '0' || digit > '9' {
				return "", 0, errors.New("enter a valid positive amount")
			}
		}
	}
	whole, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || whole > 9999999999 {
		return "", 0, errors.New("amount exceeds wallet limits")
	}
	cents := int64(0)
	if len(parts) == 2 {
		fraction := parts[1]
		if len(fraction) == 1 {
			fraction += "0"
		}
		cents, err = strconv.ParseInt(fraction, 10, 64)
		if err != nil {
			return "", 0, errors.New("enter a valid positive amount")
		}
	}
	if wholeUnits && cents != 0 {
		return "", 0, errors.New("NGN withdrawals must be a whole naira amount")
	}
	totalCents := whole*100 + cents
	if totalCents <= 0 {
		return "", 0, errors.New("enter a valid positive amount")
	}
	return fmt.Sprintf("%d.%02d", whole, cents), whole, nil
}

func walletReference(prefix string, farmerID int) (string, error) {
	random := make([]byte, 12)
	if _, err := rand.Read(random); err != nil {
		return "", err
	}
	return fmt.Sprintf("%s-%d-%s", prefix, farmerID, hex.EncodeToString(random)), nil
}

type DepositResult struct {
	CheckoutURL string
	Reference   string
}

func (s *WalletService) InitiateDeposit(ctx context.Context, farmer *models.Farmer, amount string) (*DepositResult, error) {
	if farmer == nil || farmer.ID <= 0 {
		return nil, errors.New("invalid wallet owner")
	}
	amount, _, err := normalizeWalletAmount(amount, false)
	if err != nil {
		return nil, err
	}
	reference, err := walletReference("WALLET", farmer.ID)
	if err != nil {
		return nil, fmt.Errorf("generate deposit reference: %w", err)
	}
	if err := s.repo.CreateDeposit(ctx, farmer.ID, reference, amount); err != nil {
		return nil, fmt.Errorf("create wallet deposit: %w", err)
	}
	checkoutURL, err := s.flutter.InitializePayment(InitializePaymentRequest{
		Reference:     reference,
		Amount:        amount,
		Currency:      "NGN",
		CustomerEmail: farmer.Email,
		CustomerName:  farmer.FullName,
		Title:         "Agro-Shield Wallet Deposit",
		Description:   "Wallet deposit",
	})
	if err != nil {
		// The provider may have accepted the request even if its response timed out.
		// Keep the attempt pending so a delayed webhook can still reconcile it.
		return nil, fmt.Errorf("initialize wallet deposit: %w", err)
	}
	if strings.TrimSpace(checkoutURL) == "" {
		// A missing response field is not proof the provider did not create the attempt.
		return nil, errors.New("payment provider returned no checkout URL")
	}
	return &DepositResult{CheckoutURL: checkoutURL, Reference: reference}, nil
}

func (s *WalletService) ConfirmDeposit(ctx context.Context, reference, transactionID string, amount float64, currency, status string) error {
	switch strings.ToLower(status) {
	case "failed", "cancelled", "canceled":
		return s.repo.FailDeposit(ctx, reference)
	case "successful":
	default:
		return nil
	}
	if math.IsNaN(amount) || math.IsInf(amount, 0) || amount <= 0 {
		return errors.New("invalid verified deposit amount")
	}
	cents := amount * 100
	roundedCents := math.Round(cents)
	if math.Abs(cents-roundedCents) > 0.000001 {
		return errors.New("verified deposit amount has unsupported precision")
	}
	verifiedAmount, _, err := normalizeWalletAmount(strconv.FormatFloat(roundedCents/100, 'f', 2, 64), false)
	if err != nil {
		return fmt.Errorf("invalid verified deposit amount: %w", err)
	}
	if currency != "NGN" {
		return errors.New("wallet deposits must be paid in NGN")
	}
	if transactionID == "" || !strings.HasPrefix(reference, "WALLET-") {
		return errors.New("invalid wallet deposit verification")
	}
	return s.repo.CompleteDeposit(ctx, reference, transactionID, verifiedAmount, currency)
}

func (s *WalletService) RequestWithdrawal(ctx context.Context, farmerID int, amount string) error {
	if farmerID <= 0 {
		return errors.New("invalid wallet owner")
	}
	amount, _, err := normalizeWalletAmount(amount, true)
	if err != nil {
		return err
	}
	reference, err := walletReference("WITHDRAW", farmerID)
	if err != nil {
		return fmt.Errorf("generate withdrawal reference: %w", err)
	}
	return s.repo.CreateWithdrawal(ctx, farmerID, reference, amount)
}

func (s *WalletService) PayForOrder(ctx context.Context, buyerID, cropID int, quantity float64) (int64, error) {
	return s.repo.PayForOrder(ctx, buyerID, cropID, quantity)
}

func (s *WalletService) UpdateBankDetails(farmerID int, bankName, bankCode, accountName, accountNumber string) error {
	bankName = strings.TrimSpace(bankName)
	bankCode = strings.TrimSpace(bankCode)
	accountName = strings.TrimSpace(accountName)
	accountNumber = strings.TrimSpace(accountNumber)
	if bankName == "" || bankCode == "" || accountName == "" {
		return errors.New("bank name, bank code, and account name are required")
	}
	for _, digit := range bankCode {
		if digit < '0' || digit > '9' {
			return errors.New("use the Flutterwave bank code for the selected bank")
		}
	}
	if len(accountNumber) != 10 {
		return errors.New("Nigerian bank account numbers must contain 10 digits")
	}
	for _, digit := range accountNumber {
		if digit < '0' || digit > '9' {
			return errors.New("Nigerian bank account numbers must contain 10 digits")
		}
	}
	return s.farmers.UpdateBankDetails(farmerID, bankName, bankCode, accountName, accountNumber)
}

func (s *WalletService) Banks() ([]Bank, error) {
	return s.flutter.ListBanks("NG")
}

func (s *WalletService) AdminWithdrawals(ctx context.Context) ([]models.Withdrawal, error) {
	return s.repo.ListWithdrawals(ctx, true)
}

func (s *WalletService) ApproveWithdrawal(ctx context.Context, withdrawalID int64, adminID int) error {
	if !s.IsAdmin(adminID) {
		return errors.New("admin access required")
	}
	withdrawal, err := s.repo.BeginWithdrawal(ctx, withdrawalID, adminID)
	if err != nil {
		return err
	}
	_, transferAmount, err := normalizeWalletAmount(withdrawal.Amount, true)
	if err != nil {
		return fmt.Errorf("invalid stored withdrawal amount: %w", err)
	}
	transferID, err := s.flutter.CreateTransfer(CreateTransferRequest{
		BankCode:      withdrawal.BankCode,
		AccountNumber: withdrawal.AccountNumber,
		AccountName:   withdrawal.AccountName,
		Amount:        transferAmount,
		Currency:      withdrawal.Currency,
		Reference:     withdrawal.Reference,
	})
	if err != nil {
		return fmt.Errorf("start payout; withdrawal remains reserved for reconciliation: %w", err)
	}
	if err := s.repo.SetTransferID(ctx, withdrawal.Reference, transferID); err != nil {
		return fmt.Errorf("save payout reference %s for reconciliation: %w", withdrawal.Reference, err)
	}
	verified, err := s.flutter.VerifyTransfer(transferID)
	if err != nil {
		return fmt.Errorf("payout submitted to Flutterwave but confirmation is delayed; funds remain reserved: %w", err)
	}
	if verified.ID != transferID || verified.Reference != withdrawal.Reference || verified.Amount != transferAmount || verified.Currency != withdrawal.Currency {
		return errors.New("payout submitted but provider confirmation did not match; funds remain reserved")
	}
	if verified.Status == "SUCCESSFUL" || verified.Status == "FAILED" || verified.Status == "REVERSED" {
		return s.repo.CompleteTransfer(ctx, verified.Reference, verified.ID, verified.Status, verified.Amount, verified.Currency)
	}
	return nil
}

func (s *WalletService) RejectWithdrawal(ctx context.Context, withdrawalID int64, adminID int) error {
	if !s.IsAdmin(adminID) {
		return errors.New("admin access required")
	}
	return s.repo.RejectWithdrawal(ctx, withdrawalID, adminID)
}

func (s *WalletService) ReconcileWithdrawal(ctx context.Context, withdrawalID int64, adminID int) (string, error) {
	if !s.IsAdmin(adminID) {
		return "", errors.New("admin access required")
	}
	withdrawal, err := s.repo.GetWithdrawal(ctx, withdrawalID)
	if err != nil {
		return "", err
	}
	if withdrawal.Status != "processing" {
		return "", errors.New("only processing withdrawals can be reconciled")
	}
	if !withdrawal.TransferID.Valid {
		return "", fmt.Errorf("no provider transfer ID is saved; check Flutterwave using reference %s and do not approve again", withdrawal.Reference)
	}
	verified, err := s.flutter.VerifyTransfer(withdrawal.TransferID.String)
	if err != nil {
		return "", fmt.Errorf("verify provider transfer: %w", err)
	}
	_, amount, err := normalizeWalletAmount(withdrawal.Amount, true)
	if err != nil {
		return "", fmt.Errorf("invalid stored withdrawal amount: %w", err)
	}
	if verified.ID != withdrawal.TransferID.String || verified.Reference != withdrawal.Reference ||
		verified.Amount != amount || verified.Currency != withdrawal.Currency {
		return "", errors.New("provider transfer details do not match; funds remain reserved")
	}
	switch verified.Status {
	case "SUCCESSFUL", "FAILED", "REVERSED":
		if err := s.repo.CompleteTransfer(ctx, verified.Reference, verified.ID, verified.Status, verified.Amount, verified.Currency); err != nil {
			return "", err
		}
	}
	return verified.Status, nil
}

func (s *WalletService) ProcessTransferWebhook(ctx context.Context, body []byte, receivedHash string) (bool, error) {
	if !s.flutter.VerifyWebhookSignature(receivedHash) {
		return false, ErrUnauthorizedWebhook
	}
	var payload struct {
		Event string `json:"event"`
		Data  struct {
			ID     int64  `json:"id"`
			Status string `json:"status"`
			Ref    string `json:"reference"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return false, fmt.Errorf("invalid transfer webhook payload: %w", err)
	}
	if payload.Data.ID <= 0 || !strings.HasPrefix(payload.Data.Ref, "WITHDRAW-") {
		return false, errors.New("invalid transfer webhook identifiers")
	}
	verified, err := s.flutter.VerifyTransfer(strconv.FormatInt(payload.Data.ID, 10))
	if err != nil {
		return false, fmt.Errorf("verify transfer: %w", err)
	}
	if verified.ID != strconv.FormatInt(payload.Data.ID, 10) || verified.Reference != payload.Data.Ref || verified.Currency != "NGN" {
		return false, errors.New("transfer verification does not match webhook")
	}
	if verified.Status != "SUCCESSFUL" && verified.Status != "FAILED" && verified.Status != "REVERSED" {
		return false, nil
	}
	if err := s.repo.CompleteTransfer(ctx, verified.Reference, verified.ID, verified.Status, verified.Amount, verified.Currency); err != nil {
		return false, err
	}
	return true, nil
}
