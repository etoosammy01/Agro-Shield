package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"backend/internal/database"
	"backend/internal/escrow"
	"backend/internal/models"
	"backend/internal/money"
)

func approveEscrowRefund(t *testing.T, repo *OrderDisputeRepository, orderID int64, buyer, admin int, amount string) *models.Refund {
	t.Helper()
	ctx := context.Background()
	d := newTestDispute(orderID, buyer)
	d.RequestedAmount = amount
	if err := repo.Create(ctx, d); err != nil {
		t.Fatal(err)
	}
	if err := repo.StartAdminReview(ctx, d.ID, admin, 0); err != nil {
		t.Fatal(err)
	}
	f, err := repo.ApproveRefund(ctx, d.ID, admin, amount, "Synthetic reviewed refund", 1)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func escrowWallet(t *testing.T, db *sql.DB, repo *OrderDisputeRepository) *WalletRepository {
	t.Helper()
	w := NewWalletRepository(db)
	w.ConfigureReviewAccess(repo.review)
	return w
}

func TestEscrowRefundConservesFundsAndConcurrentRetries(t *testing.T) {
	db, buyer, seller, order := createPaidOrderForDispute(t, 500)
	r, admin := disputeReviewFixture(t, db)
	w := escrowWallet(t, db, r)
	ctx := context.Background()
	wallet, err := w.GetWallet(ctx, seller)
	if err != nil || wallet.Balance != "950.00" || wallet.HeldBalance != "950.00" || wallet.AvailableBalance != "0.00" {
		t.Fatalf("seller hold: %+v %v", wallet, err)
	}
	// Held proceeds cannot be sent to another account.
	if err := w.CreateTransfer(ctx, seller, buyer, "held-transfer", "1.00", "test"); !errors.Is(err, ErrInsufficientBalance) {
		t.Fatalf("held transfer: %v", err)
	}
	f := approveEscrowRefund(t, r, order, buyer, admin, "250.00")
	if err := w.ReleaseEscrow(ctx, order, buyer); !errors.Is(err, escrow.ErrEscrowDisputed) {
		t.Fatalf("release during dispute: %v", err)
	}
	version := int64(2)
	req := escrow.RefundRequest{RefundID: f.ID, OrderID: order, ActorID: admin, Amount: 25000, ExpectedVersion: &version}
	result, err := w.RefundOrder(ctx, req)
	if err != nil || result.BuyerCredit != 25000 || result.SellerReversal != 23750 || result.CommissionReversal != 1250 {
		t.Fatalf("split: %+v %v", result, err)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			result, err := w.RefundOrder(ctx, req)
			if err == nil && !result.AlreadyApplied {
				err = fmt.Errorf("retry moved money")
			}
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if balance := walletBalanceOf(t, db, buyer); balance != "4250.00" {
		t.Fatal(balance)
	}
	if balance := walletBalanceOf(t, db, seller); balance != "712.50" {
		t.Fatal(balance)
	}
	if total, _, err := w.PlatformRevenueTotal(ctx); err != nil || total != "37.50" {
		t.Fatalf("revenue %s %v", total, err)
	}
	if err := w.ReleaseEscrow(ctx, order, seller); !errors.Is(err, ErrAdminForbidden) {
		t.Fatal("seller confirmed buyer receipt", err)
	}
	for i := 0; i < 2; i++ {
		if err := w.ReleaseEscrow(ctx, order, buyer); err != nil {
			t.Fatal(err)
		}
	}
	wallet, err = w.GetWallet(ctx, seller)
	if err != nil || wallet.HeldBalance != "0.00" || wallet.AvailableBalance != "712.50" {
		t.Fatalf("released: %+v %v", wallet, err)
	}
	var refunds, reversals, releases int
	if err := db.QueryRow(`SELECT (SELECT COUNT(*) FROM wallet_transactions WHERE type='order_refund'),(SELECT COUNT(*) FROM platform_revenue_reversals),(SELECT COUNT(*) FROM escrow_events WHERE event='released')`).Scan(&refunds, &reversals, &releases); err != nil || refunds != 1 || reversals != 1 || releases != 1 {
		t.Fatalf("duplicate financial records: %d %d %d %v", refunds, reversals, releases, err)
	}
}

func TestEscrowSequentialRefundsRespectRemainingValue(t *testing.T) {
	db, buyer, seller, order := createPaidOrderForDispute(t, 333)
	r, admin := disputeReviewFixture(t, db)
	w := escrowWallet(t, db, r)
	ctx := context.Background()
	for _, amount := range []string{"250.00", "500.00", "250.00"} {
		f := approveEscrowRefund(t, r, order, buyer, admin, amount)
		kobo, err := money.ParsePositive(amount)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.RefundOrder(ctx, escrow.RefundRequest{RefundID: f.ID, OrderID: order, ActorID: admin, Amount: kobo}); err != nil {
			t.Fatal(err)
		}
	}
	if walletBalanceOf(t, db, buyer) != "5000.00" || walletBalanceOf(t, db, seller) != "0.00" {
		t.Fatal("full refund did not conserve funds")
	}
	if total, _, err := w.PlatformRevenueTotal(ctx); err != nil || total != "0.00" {
		t.Fatal(total, err)
	}
	d := newTestDispute(order, buyer)
	if err := r.Create(ctx, d); err != nil {
		t.Fatal(err)
	}
	if err := r.StartAdminReview(ctx, d.ID, admin); err != nil {
		t.Fatal(err)
	}
	if _, err := r.ApproveRefund(ctx, d.ID, admin, "0.01", "Excess refund"); !errors.Is(err, ErrRefundInvalidAmount) {
		t.Fatalf("remaining limit: %v", err)
	}
}

func TestDisputeDecisionsRejectStaleSelfAndRevokedReviewers(t *testing.T) {
	db, buyer, seller, order := createPaidOrderForDispute(t)
	r, admin := disputeReviewFixture(t, db)
	ctx := context.Background()
	adminExec(t, db, `UPDATE farmers SET admin_role='administrator' WHERE id IN ($1,$2)`, buyer, seller)
	d := newTestDispute(order, buyer)
	if err := r.Create(ctx, d); err != nil {
		t.Fatal(err)
	}
	for _, actor := range []int{buyer, seller} {
		if err := r.StartAdminReview(ctx, d.ID, actor, 0); !errors.Is(err, ErrSelfApproval) {
			t.Fatal("self review", err)
		}
	}
	if err := r.AddSellerResponse(ctx, d.ID, seller, "Synthetic response"); err != nil {
		t.Fatal(err)
	}
	if err := r.StartAdminReview(ctx, d.ID, admin, 0); !errors.Is(err, ErrAdminConflict) {
		t.Fatal("stale review", err)
	}
	if err := r.StartAdminReview(ctx, d.ID, admin, 1); err != nil {
		t.Fatal(err)
	}
	e := &models.OrderDisputeEvidence{DisputeID: d.ID, UploadedBy: buyer, StorageKey: strings.Repeat("a", 64) + ".dat", ContentHash: strings.Repeat("b", 64), ContentType: "image/png", OriginalFilename: "fixture.png", SizeBytes: 100}
	if err := r.AddEvidence(ctx, e); err != nil {
		t.Fatal(err)
	}
	if _, err := r.ApproveRefund(ctx, d.ID, admin, "100.00", "Stale evidence", 2); !errors.Is(err, ErrAdminConflict) {
		t.Fatal("stale approval", err)
	}
	if err := r.Reject(ctx, d.ID, admin, "Stale evidence", 2); !errors.Is(err, ErrAdminConflict) {
		t.Fatal("stale rejection", err)
	}
	f, err := r.ApproveRefund(ctx, d.ID, admin, "100.00", "Current review", 3)
	if err != nil {
		t.Fatal(err)
	}
	w := escrowWallet(t, db, r)
	for _, actor := range []int{buyer, seller} {
		if _, err := w.RefundOrder(ctx, escrow.RefundRequest{RefundID: f.ID, OrderID: order, ActorID: actor, Amount: 10000}); !errors.Is(err, ErrSelfApproval) {
			t.Fatal("self settlement", err)
		}
	}
	reviewer := createTestUser(t, db)
	adminExec(t, db, `UPDATE farmers SET admin_role='administrator' WHERE id=$1`, reviewer.ID)
	adminExec(t, db, `UPDATE farmers SET admin_role='' WHERE id=$1`, reviewer.ID)
	if _, err := w.RefundOrder(ctx, escrow.RefundRequest{RefundID: f.ID, OrderID: order, ActorID: reviewer.ID, Amount: 10000}); !errors.Is(err, ErrAdminForbidden) {
		t.Fatal("revoked settlement", err)
	}
	if err := r.Reject(ctx, d.ID, admin, "Reject approved refund", 4); !errors.Is(err, ErrRefundAlreadyExists) {
		t.Fatal("approved refund rejected", err)
	}
}

func TestRefundSettlementRollsBackWithAuditAndCanRetry(t *testing.T) {
	db, buyer, seller, order := createPaidOrderForDispute(t)
	r, admin := disputeReviewFixture(t, db)
	w := escrowWallet(t, db, r)
	ctx := context.Background()
	f := approveEscrowRefund(t, r, order, buyer, admin, "250.00")
	adminExec(t, db, `CREATE FUNCTION fail_refund_audit() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'synthetic audit failure'; END $$; CREATE TRIGGER fail_refund_audit BEFORE INSERT ON admin_audit_events FOR EACH ROW EXECUTE FUNCTION fail_refund_audit()`)
	req := escrow.RefundRequest{RefundID: f.ID, OrderID: order, ActorID: admin, Amount: 25000}
	if _, err := w.RefundOrder(ctx, req); err == nil {
		t.Fatal("audit failure committed")
	}
	if walletBalanceOf(t, db, buyer) != "4000.00" || walletBalanceOf(t, db, seller) != "1000.00" {
		t.Fatal("failed audit moved money")
	}
	f, err := r.GetRefundByID(ctx, f.ID)
	if err != nil || f.Status != models.RefundRequested {
		t.Fatalf("approval lost %+v %v", f, err)
	}
	adminExec(t, db, `DROP TRIGGER fail_refund_audit ON admin_audit_events`)
	if _, err := w.RefundOrder(ctx, req); err != nil {
		t.Fatal(err)
	}
	var eventCount int
	adminQuery := `SELECT COUNT(*) FROM order_dispute_events`
	if err := db.QueryRow(adminQuery).Scan(&eventCount); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := database.RunMigration(db); err != nil {
			t.Fatal(err)
		}
	}
	var after int
	if err := db.QueryRow(adminQuery).Scan(&after); err != nil || after != eventCount {
		t.Fatal("migration rewrote history", err)
	}
}

func TestUnfundedRefundRemainsReviewableAndRetryIsDeduplicated(t *testing.T) {
	db, buyer, seller, order := createPaidOrderForDispute(t)
	r, admin := disputeReviewFixture(t, db)
	w := escrowWallet(t, db, r)
	ctx := context.Background()
	// A real receipt confirmation closes escrow before a later complaint.
	if err := w.ReleaseEscrow(ctx, order, buyer); err != nil {
		t.Fatal(err)
	}
	f := approveEscrowRefund(t, r, order, buyer, admin, "250.00")
	req := escrow.RefundRequest{RefundID: f.ID, OrderID: order, ActorID: admin, Amount: 25000}
	for i := 0; i < 2; i++ {
		if _, err := w.RefundOrder(ctx, req); !errors.Is(err, escrow.ErrRefundRequiresReview) {
			t.Fatal(err)
		}
	}
	f, err := r.GetRefundByID(ctx, f.ID)
	if err != nil || f.Status != models.RefundRequiresReview || f.FailureReason == nil {
		t.Fatalf("unfunded refund: %+v %v", f, err)
	}
	if walletBalanceOf(t, db, buyer) != "4000.00" || walletBalanceOf(t, db, seller) != "1000.00" {
		t.Fatal("unfunded refund took available funds")
	}
	exceptions, err := r.PaymentExceptions(ctx, admin)
	if err != nil || len(exceptions) != 1 || exceptions[0].DisputeID != f.DisputeID {
		t.Fatal("exception missing or duplicated", err)
	}
	var events int
	if err := db.QueryRow(`SELECT COUNT(*) FROM order_dispute_events WHERE event_type='refund_requires_review'`).Scan(&events); err != nil || events != 1 {
		t.Fatal("retry duplicated history", err)
	}
}

func TestRefundMigrationPreservesLegacyRecordsAndRejectsAmbiguity(t *testing.T) {
	db, buyer, _, order := createPaidOrderForDispute(t)
	r, admin := disputeReviewFixture(t, db)
	f := approveEscrowRefund(t, r, order, buyer, admin, "250.00")
	adminExec(t, db, `UPDATE refunds SET payment_method='flutterwave',amount=250,provider_refund_id='legacy-provider' WHERE id=$1`, f.ID)
	adminExec(t, db, `ALTER TABLE refunds RENAME TO order_refunds`)
	if err := database.RunMigration(db); err != nil {
		t.Fatal(err)
	}
	var provider, amount string
	var id int64
	if err := db.QueryRow(`SELECT id,provider_refund_id,amount::text FROM refunds WHERE id=$1`, f.ID).Scan(&id, &provider, &amount); err != nil || id != f.ID || provider != "legacy-provider" || amount != "250.00" {
		t.Fatal("legacy record changed", err)
	}
	adminExec(t, db, `CREATE TABLE order_refunds(id BIGINT)`)
	if err := database.RunRefundMigration(db); err == nil {
		t.Fatal("ambiguous tables silently merged")
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM refunds WHERE id=$1`, f.ID).Scan(&count); err != nil || count != 1 {
		t.Fatal("ambiguous migration lost refund", err)
	}
}
