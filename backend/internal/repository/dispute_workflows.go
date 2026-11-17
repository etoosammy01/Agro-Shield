package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"backend/internal/models"
)

func (r *OrderDisputeRepository) ListForReview(ctx context.Context, actor int, status string, before int64) ([]models.OrderDispute, error) {
	if !r.review.IsAdmin(actor) {
		return nil, ErrAdminForbidden
	}
	if status != "" && status != "active" && status != models.DisputeSubmitted && status != models.DisputeSellerResponded && status != models.DisputeUnderReview && status != models.DisputeResolvedRefund && status != models.DisputeResolvedRejected && status != models.DisputeWithdrawn {
		return nil, ErrAdminInvalid
	}
	rows, err := r.db.QueryContext(ctx, `SELECT `+disputeColumns+` FROM order_disputes WHERE ($1='' OR status=$1 OR ($1='active' AND status IN ('submitted','seller_responded','under_review'))) AND ($2=0 OR id<$2) ORDER BY id DESC LIMIT 25`, status, before)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]models.OrderDispute, 0)
	for rows.Next() {
		d, err := scanDispute(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *d)
	}
	return items, rows.Err()
}

func (r *OrderDisputeRepository) GetRefundForDispute(ctx context.Context, id int64) (*models.Refund, error) {
	f, err := scanRefund(r.db.QueryRowContext(ctx, `SELECT `+refundColumns+` FROM refunds WHERE dispute_id=$1 ORDER BY id DESC LIMIT 1`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return f, err
}

func (r *OrderDisputeRepository) LatestForOrder(ctx context.Context, orderID int64, actor int) (*models.OrderDispute, error) {
	d, err := scanDispute(r.db.QueryRowContext(ctx, `SELECT `+disputeColumns+` FROM order_disputes WHERE order_id=$1 AND (buyer_id=$2 OR seller_id=$2) ORDER BY id DESC LIMIT 1`, orderID, actor))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return d, err
}

func (r *OrderDisputeRepository) WithdrawDispute(ctx context.Context, id int64, buyer int, version int64) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	orderID, err := lockDisputeOrder(ctx, tx, id)
	if err != nil {
		return err
	}
	var status string
	var actualVersion int64
	err = tx.QueryRowContext(ctx, `SELECT order_id,status,version FROM order_disputes WHERE id=$1 AND buyer_id=$2 FOR UPDATE`, id, buyer).Scan(&orderID, &status, &actualVersion)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrDisputeNotFound
	}
	if err != nil {
		return err
	}
	if version != actualVersion {
		return ErrAdminConflict
	}
	if status != models.DisputeSubmitted && status != models.DisputeSellerResponded {
		return ErrDisputeState
	}
	if _, err := lockEscrowOrder(ctx, tx, orderID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE order_disputes SET status='withdrawn',updated_at=clock_timestamp() WHERE id=$1`, id); err != nil {
		return err
	}
	if err := unfreezeEscrowTx(ctx, tx, orderID, buyer, fmt.Sprintf("dispute:%d", id)); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO order_dispute_events(dispute_id,actor_id,actor_role,event_type,from_status,to_status) VALUES($1,$2,'buyer','dispute_withdrawn',$3,'withdrawn')`, id, buyer, status); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *OrderDisputeRepository) AddEvidence(ctx context.Context, e *models.OrderDisputeEvidence) error {
	if e == nil || e.DisputeID <= 0 || e.UploadedBy <= 0 || e.SizeBytes <= 0 || e.SizeBytes > 20<<20 || len(e.StorageKey) != 68 || !strings.HasSuffix(e.StorageKey, ".dat") || len(e.ContentHash) != 64 {
		return ErrAdminInvalid
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var buyer, seller int
	var status string
	if err := tx.QueryRowContext(ctx, `SELECT buyer_id,seller_id,status FROM order_disputes WHERE id=$1 FOR UPDATE`, e.DisputeID).Scan(&buyer, &seller, &status); err != nil {
		return ErrDisputeNotFound
	}
	if e.UploadedBy != buyer && e.UploadedBy != seller {
		return ErrDisputeNotFound
	}
	if status != models.DisputeSubmitted && status != models.DisputeSellerResponded && status != models.DisputeUnderReview {
		return ErrDisputeState
	}
	var count int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM order_dispute_evidence WHERE dispute_id=$1`, e.DisputeID).Scan(&count); err != nil {
		return err
	}
	if count >= 10 {
		return ErrAdminInvalid
	}
	if err := tx.QueryRowContext(ctx, `INSERT INTO order_dispute_evidence(dispute_id,uploaded_by,storage_key,original_filename,content_type,size_bytes,content_hash,validated_at) VALUES($1,$2,$3,$4,$5,$6,$7,clock_timestamp()) RETURNING id,created_at`, e.DisputeID, e.UploadedBy, e.StorageKey, e.OriginalFilename, e.ContentType, e.SizeBytes, e.ContentHash).Scan(&e.ID, &e.CreatedAt); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE order_disputes SET updated_at=clock_timestamp() WHERE id=$1`, e.DisputeID); err != nil {
		return err
	}
	role := "buyer"
	if e.UploadedBy == seller {
		role = "seller"
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO order_dispute_events(dispute_id,actor_id,actor_role,event_type,metadata) VALUES($1,$2,$3,'evidence_uploaded',jsonb_build_object('evidence_id',$4::bigint))`, e.DisputeID, e.UploadedBy, role, e.ID); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *OrderDisputeRepository) ListEvidence(ctx context.Context, id int64) ([]models.OrderDisputeEvidence, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT id,dispute_id,uploaded_by,storage_key,original_filename,content_type,size_bytes,created_at,COALESCE(content_hash,'') FROM order_dispute_evidence WHERE dispute_id=$1 ORDER BY id LIMIT 10`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]models.OrderDisputeEvidence, 0)
	for rows.Next() {
		var e models.OrderDisputeEvidence
		if err := rows.Scan(&e.ID, &e.DisputeID, &e.UploadedBy, &e.StorageKey, &e.OriginalFilename, &e.ContentType, &e.SizeBytes, &e.CreatedAt, &e.ContentHash); err != nil {
			return nil, err
		}
		items = append(items, e)
	}
	return items, rows.Err()
}

type PaymentException struct {
	ID                                                                int64
	DisputeID                                                         int64
	Kind, Status, Reference, Amount, Currency, Detail, ResolutionNote string
	OrderID, RefundID                                                 sql.NullInt64
}

func (r *OrderDisputeRepository) PaymentExceptions(ctx context.Context, actor int) ([]PaymentException, error) {
	if !r.review.IsAdmin(actor) {
		return nil, ErrAdminForbidden
	}
	rows, err := r.db.QueryContext(ctx, `SELECT e.id,e.kind,e.status,e.reference,COALESCE(e.amount::text,''),COALESCE(e.currency,''),e.detail,e.order_id,e.refund_id,e.resolution_note,COALESCE(r.dispute_id,0) FROM payment_exceptions e LEFT JOIN refunds r ON r.id=e.refund_id WHERE e.order_id IS NOT NULL AND e.status IN ('open','investigating') ORDER BY e.id DESC LIMIT 100`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]PaymentException, 0)
	for rows.Next() {
		var e PaymentException
		if err := rows.Scan(&e.ID, &e.Kind, &e.Status, &e.Reference, &e.Amount, &e.Currency, &e.Detail, &e.OrderID, &e.RefundID, &e.ResolutionNote, &e.DisputeID); err != nil {
			return nil, err
		}
		items = append(items, e)
	}
	return items, rows.Err()
}
