package repository

import (
	"context"
	"errors"
	"testing"

	"backend/internal/models"
)

// The wallet page polls /wallet/status to notice a credit that was committed
// after the page rendered. The snapshot must come from the database, be scoped
// to one farmer, move only when the ledger moves, and never write anything.
func TestWalletStatusReportsCommittedStateWithoutWriting(t *testing.T) {
	db := testDB(t)
	farmer := createTestUser(t, db)
	owner := createTestUser(t, db)
	repo := NewWalletRepository(db)
	ctx := context.Background()

	status, err := repo.GetStatus(ctx, farmer.ID)
	if err != nil {
		t.Fatalf("read initial status: %v", err)
	}
	if status.Balance != "0.00" || status.Currency != "NGN" {
		t.Fatalf("initial balance = %s %s, want 0.00 NGN", status.Balance, status.Currency)
	}
	if status.PendingDeposits != 0 || status.LatestTransactionID != 0 {
		t.Fatalf("initial status = %+v, want no pending deposits and no ledger rows", status)
	}

	reference := uniqueRef("STATUS")
	if err := repo.CreateDeposit(ctx, farmer.ID, models.ProviderFlutterwave, reference, "25.50"); err != nil {
		t.Fatalf("create pending deposit: %v", err)
	}

	pending, err := repo.GetStatus(ctx, farmer.ID)
	if err != nil {
		t.Fatalf("read pending status: %v", err)
	}
	if pending.PendingDeposits != 1 || pending.LatestDepositStatus != models.DepositPending {
		t.Fatalf("pending status = %+v, want one pending deposit", pending)
	}
	if pending.Balance != "0.00" {
		t.Fatalf("balance moved before provider confirmation: %s", pending.Balance)
	}

	if _, err := repo.CompleteDeposit(ctx, reference, "TX-"+reference, "25.50", "NGN"); err != nil {
		t.Fatalf("complete deposit: %v", err)
	}

	credited, err := repo.GetStatus(ctx, farmer.ID)
	if err != nil {
		t.Fatalf("read credited status: %v", err)
	}
	if credited.Balance != "25.50" {
		t.Fatalf("balance = %s, want 25.50", credited.Balance)
	}
	if credited.PendingDeposits != 0 || credited.LatestDepositStatus != models.DepositSuccessful {
		t.Fatalf("credited status = %+v, want a settled deposit", credited)
	}
	if credited.LatestTransactionID <= 0 || credited.LatestDepositID <= 0 {
		t.Fatalf("credited status lacks watermarks: %+v", credited)
	}

	// A second account must never see this balance through the status reader.
	other, err := repo.GetStatus(ctx, owner.ID)
	if err != nil {
		t.Fatalf("read other user's status: %v", err)
	}
	if other.Balance != "0.00" || other.LatestTransactionID != 0 {
		t.Fatalf("other user's status leaked another wallet: %+v", other)
	}

	// The reader is a snapshot: reading it twice changes neither ledger nor row.
	before := ledgerCountOf(t, db, farmer.ID)
	if _, err := repo.GetStatus(ctx, farmer.ID); err != nil {
		t.Fatalf("repeat status read: %v", err)
	}
	if after := ledgerCountOf(t, db, farmer.ID); after != before {
		t.Fatalf("status read wrote ledger rows: %d -> %d", before, after)
	}
	if balance := walletBalanceOf(t, db, farmer.ID); balance != "25.50" {
		t.Fatalf("status read changed balance: %s", balance)
	}
}

func TestWalletStatusReportsMissingWallet(t *testing.T) {
	db := testDB(t)
	farmer := createTestUser(t, db)

	if _, err := db.Exec(`DELETE FROM wallets WHERE farmer_id = $1`, farmer.ID); err != nil {
		t.Fatalf("delete wallet: %v", err)
	}

	if _, err := NewWalletRepository(db).GetStatus(context.Background(), farmer.ID); !errors.Is(err, ErrWalletNotFound) {
		t.Fatalf("error = %v, want ErrWalletNotFound", err)
	}
}
