package repository

import (
	"context"
	"errors"
	"testing"

	"backend/internal/models"
)

func TestApproveRefundValidatesAmountAndPreventsDuplicate(t *testing.T) {
	db, buyerID, sellerID, orderID := createPaidOrderForDispute(t)
	repo := NewOrderDisputeRepository(db)
	ctx := context.Background()

	dispute := newTestDispute(orderID, buyerID)
	dispute.ReasonCode = models.DisputeReasonMissingQuantity

	if err := repo.Create(ctx, dispute); err != nil {
		t.Fatalf("create dispute: %v", err)
	}

	// Repository tests exercise persistence, not administrator authorization.
	// The seller ID is used here as a valid farmers foreign-key reference.
	if err := repo.StartAdminReview(ctx, dispute.ID, sellerID); err != nil {
		t.Fatalf("start review: %v", err)
	}

	_, err := repo.ApproveRefund(
		ctx, dispute.ID, sellerID, "500.01", "Missing quantity verified",
	)
	if !errors.Is(err, ErrRefundInvalidAmount) {
		t.Fatalf("excessive refund error = %v, want ErrRefundInvalidAmount", err)
	}

	refund, err := repo.ApproveRefund(
		ctx, dispute.ID, sellerID, "250.00", "One part of the order was not delivered",
	)
	if err != nil {
		t.Fatalf("approve partial refund: %v", err)
	}

	if refund.Status != models.RefundRequested {
		t.Fatalf("refund status = %q, want requested", refund.Status)
	}
	if refund.ApprovedAmount == nil || *refund.ApprovedAmount != "250.00" {
		t.Fatalf("approved amount = %v, want 250.00", refund.ApprovedAmount)
	}
	if refund.BuyerID != buyerID || refund.SellerID != sellerID {
		t.Fatalf("refund participants are incorrect: buyer=%d seller=%d",
			refund.BuyerID, refund.SellerID)
	}
	if refund.ApprovedBy == nil || *refund.ApprovedBy != sellerID {
		t.Fatal("approving actor was not recorded")
	}

	// A second approval must not create a second refund.
	_, err = repo.ApproveRefund(
		ctx, dispute.ID, sellerID, "100.00", "Duplicate approval",
	)
	if !errors.Is(err, ErrRefundAlreadyExists) {
		t.Fatalf("duplicate approval error = %v, want ErrRefundAlreadyExists", err)
	}

	// Approval alone must not resolve the dispute.
	got, err := repo.GetByID(ctx, dispute.ID)
	if err != nil {
		t.Fatalf("get dispute: %v", err)
	}
	if got.Status != models.DisputeUnderReview {
		t.Fatalf("dispute status = %q, want under_review", got.Status)
	}

	if balance := walletBalanceOf(t, db, buyerID); balance != "4000.00" {
		t.Fatalf("approval moved buyer wallet balance: %s", balance)
	}
}

func TestApproveQualityRefundRequiresBuyerVideoEvidence(t *testing.T) {
	db, buyerID, sellerID, orderID := createPaidOrderForDispute(t)
	repo := NewOrderDisputeRepository(db)
	ctx := context.Background()

	dispute := newTestDispute(orderID, buyerID)
	dispute.ReasonCode = models.DisputeReasonQualityIssue

	if err := repo.Create(ctx, dispute); err != nil {
		t.Fatalf("create quality dispute: %v", err)
	}

	if err := repo.StartAdminReview(ctx, dispute.ID, sellerID); err != nil {
		t.Fatalf("start review: %v", err)
	}

	_, err := repo.ApproveRefund(
		ctx, dispute.ID, sellerID, "150.00", "Quality complaint reviewed",
	)
	if !errors.Is(err, ErrRefundVideoEvidenceRequired) {
		t.Fatalf("approval without video error = %v, want ErrRefundVideoEvidenceRequired", err)
	}

	// This inserts a video evidence record for the repository test.
	// Actual file upload and content validation must be implemented separately.
	_, err = db.ExecContext(ctx, `
		INSERT INTO order_dispute_evidence (
			dispute_id, uploaded_by, storage_key,
			original_filename, content_type, size_bytes
		)
		VALUES ($1, $2, $3, $4, $5, $6)
	`,
		dispute.ID,
		buyerID,
		"private/test-quality-video.mp4",
		"quality-evidence.mp4",
		"video/mp4",
		1024,
	)
	if err != nil {
		t.Fatalf("insert video evidence record: %v", err)
	}

	refund, err := repo.ApproveRefund(
		ctx, dispute.ID, sellerID, "150.00", "Video evidence reviewed",
	)
	if err != nil {
		t.Fatalf("approve quality refund with video: %v", err)
	}

	if refund.Status != models.RefundRequested {
		t.Fatalf("refund status = %q, want requested", refund.Status)
	}
}

func TestSuccessfulRefundSettlementResolvesDispute(t *testing.T) {
	db, buyerID, sellerID, orderID := createPaidOrderForDispute(t)
	repo := NewOrderDisputeRepository(db)
	ctx := context.Background()

	dispute := newTestDispute(orderID, buyerID)
	dispute.ReasonCode = models.DisputeReasonNonDelivery
	dispute.RequestedAmount = "1000.00"

	if err := repo.Create(ctx, dispute); err != nil {
		t.Fatalf("create non-delivery dispute: %v", err)
	}

	if err := repo.StartAdminReview(ctx, dispute.ID, sellerID); err != nil {
		t.Fatalf("start review: %v", err)
	}

	refund, err := repo.ApproveRefund(
		ctx, dispute.ID, sellerID, "1000.00", "Complete non-delivery verified",
	)
	if err != nil {
		t.Fatalf("approve refund: %v", err)
	}

	// Approval does not mean settlement has happened.
	got, err := repo.GetByID(ctx, dispute.ID)
	if err != nil {
		t.Fatalf("get dispute after approval: %v", err)
	}
	if got.Status != models.DisputeUnderReview {
		t.Fatalf("status after approval = %q, want under_review", got.Status)
	}

	if err := repo.UpdateRefundStatus(
		ctx, refund.ID, models.RefundProcessing, "Wallet settlement started",
	); err != nil {
		t.Fatalf("mark refund processing: %v", err)
	}

	if err := repo.UpdateRefundStatus(
		ctx, refund.ID, models.RefundSucceeded, "Wallet settlement confirmed",
	); err != nil {
		t.Fatalf("mark refund succeeded: %v", err)
	}

	got, err = repo.GetByID(ctx, dispute.ID)
	if err != nil {
		t.Fatalf("get resolved dispute: %v", err)
	}
	if got.Status != models.DisputeResolvedRefund {
		t.Fatalf("final dispute status = %q, want resolved_refund", got.Status)
	}

	settled, err := repo.GetRefundByID(ctx, refund.ID)
	if err != nil {
		t.Fatalf("get settled refund: %v", err)
	}
	if settled.Status != models.RefundSucceeded {
		t.Fatalf("refund status = %q, want succeeded", settled.Status)
	}
	if settled.ProcessedAt == nil {
		t.Fatal("successful refund has no processed timestamp")
	}

	// A second success callback must not resolve or process it again.
	err = repo.UpdateRefundStatus(
		ctx, refund.ID, models.RefundSucceeded, "Duplicate callback",
	)
	if !errors.Is(err, ErrRefundInvalidTransition) {
		t.Fatalf("duplicate success error = %v, want ErrRefundInvalidTransition", err)
	}
}
