package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"backend/internal/models"
)

var (
	ErrRefundNotFound              = errors.New("refund not found")
	ErrRefundAlreadyExists         = errors.New("refund already exists for this dispute")
	ErrRefundInvalidAmount         = errors.New("approved refund amount is invalid")
	ErrRefundVideoEvidenceRequired = errors.New("video evidence is required for quality refunds")
	ErrRefundDisputeNotUnderReview = errors.New("dispute must be under administrator review")
	ErrRefundInvalidTransition     = errors.New("invalid refund status transition")
)

type refundScanner interface {
	Scan(dest ...any) error
}

const refundColumns = `
	id, dispute_id, order_id, buyer_id, seller_id,
	reason_code, requested_amount::text, approved_amount::text,
	currency, status, idempotency_key, reference,
	requested_by, approved_by, approval_note, failure_reason,
	processed_at, created_at, updated_at
`

func scanRefund(row refundScanner) (*models.Refund, error) {
	var refund models.Refund
	var approvedAmount sql.NullString
	var approvedBy sql.NullInt64
	var approvalNote, failureReason sql.NullString
	var processedAt sql.NullTime

	err := row.Scan(
		&refund.ID,
		&refund.DisputeID,
		&refund.OrderID,
		&refund.BuyerID,
		&refund.SellerID,
		&refund.ReasonCode,
		&refund.RequestedAmount,
		&approvedAmount,
		&refund.Currency,
		&refund.Status,
		&refund.IdempotencyKey,
		&refund.Reference,
		&refund.RequestedBy,
		&approvedBy,
		&approvalNote,
		&failureReason,
		&processedAt,
		&refund.CreatedAt,
		&refund.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}

	if approvedAmount.Valid {
		value := approvedAmount.String
		refund.ApprovedAmount = &value
	}
	if approvedBy.Valid {
		value := int(approvedBy.Int64)
		refund.ApprovedBy = &value
	}
	if approvalNote.Valid {
		value := approvalNote.String
		refund.ApprovalNote = &value
	}
	if failureReason.Valid {
		value := failureReason.String
		refund.FailureReason = &value
	}
	if processedAt.Valid {
		value := processedAt.Time
		refund.ProcessedAt = &value
	}

	return &refund, nil
}

// ApproveRefund creates a refund record after administrator approval.
//
// It does not move money. Member 4's RefundOrder operation handles settlement.
func (r *OrderDisputeRepository) ApproveRefund(
	ctx context.Context,
	disputeID int64,
	adminID int,
	approvedAmount string,
	approvalNote string,
) (*models.Refund, error) {
	approvedAmount = strings.TrimSpace(approvedAmount)
	approvalNote = strings.TrimSpace(approvalNote)

	if disputeID <= 0 || adminID <= 0 ||
		approvedAmount == "" || approvalNote == "" ||
		len(approvalNote) > 3000 {
		return nil, ErrRefundInvalidAmount
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	var orderID int64
	var buyerID, sellerID int
	var reasonCode, disputeStatus string
	var requestedAmount, orderTotal string

	// Lock the dispute and order. This serializes refund approvals
	// involving the same order.
	err = tx.QueryRowContext(ctx, `
		SELECT
			d.order_id,
			d.buyer_id,
			d.seller_id,
			d.reason_code,
			d.status,
			d.requested_amount::text,
			o.total_price::numeric(12,2)::text
		FROM order_disputes d
		JOIN orders o ON o.id = d.order_id
		WHERE d.id = $1
		FOR UPDATE OF d, o
	`, disputeID).Scan(
		&orderID,
		&buyerID,
		&sellerID,
		&reasonCode,
		&disputeStatus,
		&requestedAmount,
		&orderTotal,
	)

	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrDisputeNotFound
	}
	if err != nil {
		return nil, err
	}

	if disputeStatus != models.DisputeUnderReview {
		return nil, ErrRefundDisputeNotUnderReview
	}

	// A quality refund requires a video uploaded by the buyer.
	// The upload layer must separately validate the actual file contents.
	if reasonCode == models.DisputeReasonQualityIssue {
		var hasVideo bool
		err = tx.QueryRowContext(ctx, `
			SELECT EXISTS (
				SELECT 1
				FROM order_dispute_evidence
				WHERE dispute_id = $1
				  AND uploaded_by = $2
				  AND content_type ILIKE 'video/%'
			)
		`, disputeID, buyerID).Scan(&hasVideo)

		if err != nil {
			return nil, err
		}
		if !hasVideo {
			return nil, ErrRefundVideoEvidenceRequired
		}
	}

	var amountValid bool
	err = tx.QueryRowContext(ctx, `
		SELECT
			$1::numeric > 0
			AND $1::numeric = round($1::numeric, 2)
			AND $1::numeric <= $2::numeric
			AND $1::numeric <= $3::numeric
	`, approvedAmount, requestedAmount, orderTotal).Scan(&amountValid)

	if err != nil {
		return nil, fmt.Errorf("validate approved refund amount: %w", err)
	}
	if !amountValid {
		return nil, ErrRefundInvalidAmount
	}

	// One refund record per dispute. A failed settlement must be retried
	// using the existing record, not by creating a second refund.
	var existing bool
	err = tx.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM refunds WHERE dispute_id = $1
		)
	`, disputeID).Scan(&existing)

	if err != nil {
		return nil, err
	}
	if existing {
		return nil, ErrRefundAlreadyExists
	}

	// Do not approve more than the original order's remaining refundable
	// value across all active and successful refund records.
	var previouslyReserved string
	err = tx.QueryRowContext(ctx, `
		SELECT COALESCE(SUM(approved_amount), 0)::numeric(12,2)::text
		FROM refunds
		WHERE order_id = $1
		  AND status IN (
			'requested',
			'processing',
			'succeeded',
			'requires_review'
		  )
	`, orderID).Scan(&previouslyReserved)

	if err != nil {
		return nil, err
	}

	var withinOrderLimit bool
	err = tx.QueryRowContext(ctx, `
		SELECT $1::numeric + $2::numeric <= $3::numeric
	`, approvedAmount, previouslyReserved, orderTotal).Scan(&withinOrderLimit)

	if err != nil {
		return nil, err
	}
	if !withinOrderLimit {
		return nil, ErrRefundInvalidAmount
	}

	idempotencyKey := fmt.Sprintf("order-dispute-refund:%d", disputeID)
	reference := fmt.Sprintf("AGRO-RFD-%d", disputeID)

	var refundID int64
	err = tx.QueryRowContext(ctx, `
		INSERT INTO refunds (
			dispute_id,
			order_id,
			buyer_id,
			seller_id,
			reason_code,
			requested_amount,
			approved_amount,
			currency,
			status,
			idempotency_key,
			reference,
			requested_by,
			approved_by,
			approval_note
		)
		VALUES (
			$1, $2, $3, $4, $5,
			$6::numeric, $7::numeric, 'NGN',
			'requested', $8, $9, $3, $10, $11
		)
		RETURNING id
	`,
		disputeID,
		orderID,
		buyerID,
		sellerID,
		reasonCode,
		requestedAmount,
		approvedAmount,
		idempotencyKey,
		reference,
		adminID,
		approvalNote,
	).Scan(&refundID)

	if err != nil {
		return nil, err
	}

	// Approval is audited, but the dispute stays under review until
	// Member 4 confirms that the wallet settlement actually succeeded.
	_, err = tx.ExecContext(ctx, `
		INSERT INTO order_dispute_events (
			dispute_id,
			actor_id,
			actor_role,
			event_type,
			from_status,
			to_status,
			note,
			metadata
		)
		VALUES (
			$1, $2, 'admin', 'refund_approved',
			$3, $3, $4,
			jsonb_build_object('refund_id', $5)
		)
	`,
		disputeID,
		adminID,
		models.DisputeUnderReview,
		approvalNote,
		refundID,
	)

	if err != nil {
		return nil, err
	}

	refund, err := scanRefund(tx.QueryRowContext(ctx,
		`SELECT `+refundColumns+` FROM refunds WHERE id = $1`,
		refundID,
	))
	if err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	return refund, nil
}

// UpdateRefundStatus records the result returned by the wallet settlement
// integration. Only a successful settlement resolves the dispute.
func (r *OrderDisputeRepository) UpdateRefundStatus(
	ctx context.Context,
	refundID int64,
	newStatus string,
	detail string,
) error {
	if refundID <= 0 {
		return ErrRefundNotFound
	}

	newStatus = strings.TrimSpace(newStatus)
	detail = strings.TrimSpace(detail)

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var disputeID int64
	var currentRefundStatus, currentDisputeStatus string
	var approvedBy sql.NullInt64

	err = tx.QueryRowContext(ctx, `
		SELECT
			r.dispute_id,
			r.status,
			d.status,
			r.approved_by
		FROM refunds r
		JOIN order_disputes d ON d.id = r.dispute_id
		WHERE r.id = $1
		FOR UPDATE OF r, d
	`, refundID).Scan(
		&disputeID,
		&currentRefundStatus,
		&currentDisputeStatus,
		&approvedBy,
	)

	if errors.Is(err, sql.ErrNoRows) {
		return ErrRefundNotFound
	}
	if err != nil {
		return err
	}

	allowed := false
	switch currentRefundStatus {
	case models.RefundRequested:
		allowed = newStatus == models.RefundProcessing ||
			newStatus == models.RefundFailed ||
			newStatus == models.RefundRequiresReview

	case models.RefundProcessing:
		allowed = newStatus == models.RefundSucceeded ||
			newStatus == models.RefundFailed ||
			newStatus == models.RefundRequiresReview
	}

	if !allowed {
		return ErrRefundInvalidTransition
	}

	if newStatus == models.RefundSucceeded &&
		currentDisputeStatus != models.DisputeUnderReview {
		return ErrRefundDisputeNotUnderReview
	}

	var approver any
	if approvedBy.Valid {
		approver = approvedBy.Int64
	}

	_, err = tx.ExecContext(ctx, `
		UPDATE refunds
		SET status = $1,
		    failure_reason = CASE
				WHEN $1 IN ('failed', 'requires_review') THEN NULLIF($2, '')
				ELSE NULL
		    END,
		    processed_at = CASE
				WHEN $1 IN ('succeeded', 'failed') THEN CURRENT_TIMESTAMP
				ELSE NULL
		    END,
		    updated_at = CURRENT_TIMESTAMP
		WHERE id = $3
	`, newStatus, detail, refundID)

	if err != nil {
		return err
	}

	if newStatus == models.RefundSucceeded {
		if !approvedBy.Valid {
			return fmt.Errorf("successful refund has no approving administrator")
		}

		result, err := tx.ExecContext(ctx, `
			UPDATE order_disputes
			SET status = $1,
			    resolved_by = $2,
			    resolved_at = CURRENT_TIMESTAMP,
			    updated_at = CURRENT_TIMESTAMP
			WHERE id = $3 AND status = $4
		`,
			models.DisputeResolvedRefund,
			approver,
			disputeID,
			models.DisputeUnderReview,
		)

		if err != nil {
			return err
		}

		affected, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if affected != 1 {
			return ErrRefundDisputeNotUnderReview
		}

		_, err = tx.ExecContext(ctx, `
			INSERT INTO order_dispute_events (
				dispute_id, actor_role, event_type,
				from_status, to_status, note,
				metadata
			)
			VALUES (
				$1, 'wallet', 'refund_succeeded',
				$2, $3, $4,
				jsonb_build_object('refund_id', $5)
			)
		`,
			disputeID,
			models.DisputeUnderReview,
			models.DisputeResolvedRefund,
			detail,
			refundID,
		)
		if err != nil {
			return err
		}
	} else {
		_, err = tx.ExecContext(ctx, `
			INSERT INTO order_dispute_events (
				dispute_id, actor_role, event_type,
				from_status, to_status, note,
				metadata
			)
			VALUES (
				$1, 'wallet', $2,
				$3, $3, $4,
				jsonb_build_object('refund_id', $5)
			)
		`,
			disputeID,
			"refund_"+newStatus,
			currentDisputeStatus,
			detail,
			refundID,
		)
		if err != nil {
			return err
		}
	}

	return tx.Commit()
}

// GetRefundByID returns a refund record without modifying its financial state.
func (r *OrderDisputeRepository) GetRefundByID(
	ctx context.Context,
	refundID int64,
) (*models.Refund, error) {
	if refundID <= 0 {
		return nil, ErrRefundNotFound
	}

	refund, err := scanRefund(r.db.QueryRowContext(
		ctx,
		`SELECT `+refundColumns+` FROM refunds WHERE id = $1`,
		refundID,
	))

	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return refund, err
}

// Keep the time import explicit for database timestamp compatibility.
var _ = time.Time{}
