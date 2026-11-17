package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"backend/internal/escrow"
	"backend/internal/models"
	"backend/internal/money"
)

var _ escrow.Settler = (*WalletRepository)(nil)

type EscrowHold struct {
	ID, OrderID, BuyerWalletID, SellerWalletID                                       int64
	Gross, Commission, Seller, Released, Refunded, CommissionReversed, RefundedGross money.Kobo
	Status                                                                           string
}

func lockEscrowWallets(ctx context.Context, tx *sql.Tx, first, second int64) error {
	rows, err := tx.QueryContext(ctx, `SELECT id FROM wallets WHERE id IN ($1,$2) ORDER BY id FOR UPDATE`, first, second)
	if err != nil {
		return err
	}
	defer rows.Close()
	n := 0
	for rows.Next() {
		n++
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if n != 2 || first == second {
		return ErrWalletNotFound
	}
	return nil
}

// The existing checkout transaction credits both balance and held_balance.
// This helper records the payment snapshot and immutable hold event only.
func createEscrowHold(ctx context.Context, tx *sql.Tx, orderID, buyerWallet, sellerWallet, gross, commission int64, actor int) error {
	var id int64
	err := tx.QueryRowContext(ctx, `INSERT INTO escrow_holds(order_id,buyer_wallet_id,seller_wallet_id,gross_amount,commission_amount,amount)
 VALUES($1,$2,$3,$4::numeric,$5::numeric,$6::numeric) RETURNING id`, orderID, buyerWallet, sellerWallet, koboToAmount(gross), koboToAmount(commission), koboToAmount(gross-commission)).Scan(&id)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO escrow_events(escrow_hold_id,event,amount,actor_id,reference)
 VALUES($1,'held',$2::numeric,$3,$4)`, id, koboToAmount(gross-commission), actor, fmt.Sprintf("payment:%d", orderID))
	return err
}

func loadEscrowHold(ctx context.Context, tx *sql.Tx, orderID int64) (*EscrowHold, error) {
	h := &EscrowHold{}
	err := tx.QueryRowContext(ctx, `SELECT id,order_id,buyer_wallet_id,seller_wallet_id,
 (gross_amount*100)::bigint,(commission_amount*100)::bigint,(amount*100)::bigint,
 (released_amount*100)::bigint,(refunded_amount*100)::bigint,(commission_reversed*100)::bigint,(refunded_gross*100)::bigint,status
 FROM escrow_holds WHERE order_id=$1 FOR UPDATE`, orderID).Scan(&h.ID, &h.OrderID, &h.BuyerWalletID, &h.SellerWalletID, &h.Gross, &h.Commission, &h.Seller, &h.Released, &h.Refunded, &h.CommissionReversed, &h.RefundedGross, &h.Status)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, escrow.ErrEscrowNotFound
	}
	return h, err
}

func lockEscrowOrder(ctx context.Context, tx *sql.Tx, orderID int64) (int, error) {
	var buyer int
	err := tx.QueryRowContext(ctx, `SELECT buyer_id FROM orders WHERE id=$1 FOR UPDATE`, orderID).Scan(&buyer)
	return buyer, err
}

// Always lock the order before its dispute/refund. Submission and receipt
// confirmation use this order too, preventing a resolve/resubmit deadlock.
func lockDisputeOrder(ctx context.Context, tx *sql.Tx, disputeID int64) (int64, error) {
	var orderID int64
	err := tx.QueryRowContext(ctx, `SELECT order_id FROM order_disputes WHERE id=$1`, disputeID).Scan(&orderID)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrDisputeNotFound
	}
	if err != nil {
		return 0, err
	}
	_, err = lockEscrowOrder(ctx, tx, orderID)
	return orderID, err
}

func (r *WalletRepository) EscrowForOrder(ctx context.Context, orderID int64) (*EscrowHold, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	h, err := loadEscrowHold(ctx, tx, orderID)
	if errors.Is(err, escrow.ErrEscrowNotFound) {
		return nil, nil
	}
	return h, err
}

// Only the purchasing buyer can confirm receipt. Order locking serializes
// confirmation with dispute creation, approval and settlement.
func (r *WalletRepository) ReleaseEscrow(ctx context.Context, orderID int64, actorID int) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	buyer, err := lockEscrowOrder(ctx, tx, orderID)
	if err != nil {
		return err
	}
	if buyer != actorID {
		return ErrAdminForbidden
	}
	h, err := loadEscrowHold(ctx, tx, orderID)
	if err != nil {
		return err
	}
	var released bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM escrow_events WHERE escrow_hold_id=$1 AND event='released')`, h.ID).Scan(&released); err != nil {
		return err
	}
	if released {
		return tx.Commit()
	}
	var active bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM order_disputes WHERE order_id=$1 AND status IN ('submitted','seller_responded','under_review'))`, orderID).Scan(&active); err != nil {
		return err
	}
	if active || h.Status == "disputed" {
		return escrow.ErrEscrowDisputed
	}
	if h.Status == "closed" {
		return escrow.ErrEscrowClosed
	}
	if err := lockEscrowWallets(ctx, tx, h.BuyerWalletID, h.SellerWalletID); err != nil {
		return err
	}
	remaining := h.Seller - h.Released - h.Refunded
	result, err := tx.ExecContext(ctx, `UPDATE wallets SET held_balance=held_balance-$1::numeric,updated_at=clock_timestamp() WHERE id=$2 AND held_balance >= $1::numeric`, remaining.String(), h.SellerWalletID)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return escrow.ErrRefundRequiresReview
	}
	reason := "released"
	if h.RefundedGross > 0 {
		reason = "split"
	}
	if _, err := tx.ExecContext(ctx, `UPDATE escrow_holds SET released_amount=released_amount+$1::numeric,status='closed',closed_reason=$2,closed_at=clock_timestamp(),updated_at=clock_timestamp() WHERE id=$3`, remaining.String(), reason, h.ID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO escrow_events(escrow_hold_id,event,amount,actor_id,reference) VALUES($1,'released',$2::numeric,$3,$4)`, h.ID, remaining.String(), actorID, fmt.Sprintf("release:%d", orderID)); err != nil {
		return err
	}
	return tx.Commit()
}

func freezeEscrowTx(ctx context.Context, tx *sql.Tx, orderID int64, actorID int, reference string) error {
	h, err := loadEscrowHold(ctx, tx, orderID)
	// Legacy payments are not retroactively converted into held funds.
	if errors.Is(err, escrow.ErrEscrowNotFound) || (err == nil && h.Status == "closed") {
		return nil
	}
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE escrow_holds SET status='disputed',updated_at=clock_timestamp() WHERE id=$1`, h.ID); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO escrow_events(escrow_hold_id,event,actor_id,reference) VALUES($1,'disputed',$2,$3) ON CONFLICT DO NOTHING`, h.ID, actorID, reference)
	return err
}

func unfreezeEscrowTx(ctx context.Context, tx *sql.Tx, orderID int64, actorID int, reference string) error {
	h, err := loadEscrowHold(ctx, tx, orderID)
	if errors.Is(err, escrow.ErrEscrowNotFound) || (err == nil && h.Status == "closed") {
		return nil
	}
	if err != nil {
		return err
	}
	var active bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM order_disputes WHERE order_id=$1 AND status IN ('submitted','seller_responded','under_review'))`, orderID).Scan(&active); err != nil {
		return err
	}
	if active {
		return escrow.ErrEscrowDisputed
	}
	if _, err = tx.ExecContext(ctx, `UPDATE escrow_holds SET status='held',updated_at=clock_timestamp() WHERE id=$1`, h.ID); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO escrow_events(escrow_hold_id,event,actor_id,reference) VALUES($1,'dispute_cleared',$2,$3) ON CONFLICT DO NOTHING`, h.ID, actorID, reference)
	return err
}

// The public compatibility interface validates the dispute reference against
// persisted ownership; submission/rejection use the same helpers inside their tx.
func (r *WalletRepository) FreezeEscrow(ctx context.Context, orderID int64, actorID int, reference string) error {
	return r.changeFreeze(ctx, orderID, actorID, reference, true)
}
func (r *WalletRepository) UnfreezeEscrow(ctx context.Context, orderID int64, actorID int, reference string) error {
	return r.changeFreeze(ctx, orderID, actorID, reference, false)
}
func (r *WalletRepository) changeFreeze(ctx context.Context, orderID int64, actorID int, reference string, freeze bool) error {
	var disputeID int64
	if _, err := fmt.Sscanf(reference, "dispute:%d", &disputeID); err != nil || reference != fmt.Sprintf("dispute:%d", disputeID) {
		return ErrDisputeNotFound
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	buyer, err := lockEscrowOrder(ctx, tx, orderID)
	if err != nil {
		return err
	}
	if buyer != actorID {
		return ErrAdminForbidden
	}
	var status string
	if err := tx.QueryRowContext(ctx, `SELECT status FROM order_disputes WHERE id=$1 AND order_id=$2 AND buyer_id=$3`, disputeID, orderID, actorID).Scan(&status); err != nil {
		return ErrDisputeNotFound
	}
	if freeze {
		if status != models.DisputeSubmitted && status != models.DisputeSellerResponded && status != models.DisputeUnderReview {
			return ErrDisputeState
		}
		err = freezeEscrowTx(ctx, tx, orderID, actorID, reference)
	} else {
		err = unfreezeEscrowTx(ctx, tx, orderID, actorID, reference)
	}
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (r *WalletRepository) RefundOrder(ctx context.Context, req escrow.RefundRequest) (*escrow.RefundResult, error) {
	if req.RefundID <= 0 || req.OrderID <= 0 || req.Amount <= 0 {
		return nil, ErrRefundInvalidAmount
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err := r.review.AuthorizeTx(ctx, tx, req.ActorID); err != nil {
		return nil, err
	}
	if _, err := lockEscrowOrder(ctx, tx, req.OrderID); err != nil {
		return nil, err
	}
	var orderID, disputeID int64
	var buyer, seller int
	var approved money.Kobo
	var approver sql.NullInt64
	var status, disputeStatus string
	var version int64
	err = tx.QueryRowContext(ctx, `SELECT r.order_id,r.dispute_id,r.buyer_id,r.seller_id,COALESCE((r.approved_amount*100)::bigint,0),r.approved_by,r.status,d.status,d.version
 FROM refunds r JOIN order_disputes d ON d.id=r.dispute_id WHERE r.id=$1 FOR UPDATE OF r,d`, req.RefundID).Scan(&orderID, &disputeID, &buyer, &seller, &approved, &approver, &status, &disputeStatus, &version)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrRefundNotFound
	}
	if err != nil {
		return nil, err
	}
	if orderID != req.OrderID || approved != req.Amount {
		return nil, ErrRefundInvalidAmount
	}
	if req.ActorID == buyer || req.ActorID == seller {
		return nil, ErrSelfApproval
	}
	if _, err := lockEscrowOrder(ctx, tx, orderID); err != nil {
		return nil, err
	}
	ref := fmt.Sprintf("refund:%d", req.RefundID)
	var applied escrow.RefundResult
	err = tx.QueryRowContext(ctx, `SELECT (b.amount*100)::bigint,COALESCE((s.amount*100)::bigint,0),
 COALESCE((SELECT SUM(amount)*100 FROM platform_revenue_reversals WHERE reference=$3),0)::bigint
 FROM wallet_transactions b LEFT JOIN wallet_transactions s ON s.reference=$2
 WHERE b.reference=$1 AND b.type='order_refund' AND (s.type IS NULL OR s.type='sale_reversal') AND EXISTS(SELECT 1 FROM escrow_events WHERE event='refunded' AND reference=$3)`, fmt.Sprintf("order-refund:%d", req.RefundID), fmt.Sprintf("sale-reversal:%d", req.RefundID), ref).Scan(&applied.BuyerCredit, &applied.SellerReversal, &applied.CommissionReversal)
	if err == nil {
		applied.AlreadyApplied = true
		return &applied, tx.Commit()
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	if req.ExpectedVersion != nil && *req.ExpectedVersion != version {
		return nil, ErrAdminConflict
	}
	if status == models.RefundSucceeded {
		return nil, recordRefundException(ctx, tx, req, disputeID, "Historical settlement lacks matching wallet ledger evidence.")
	}
	if disputeStatus != models.DisputeUnderReview {
		return nil, ErrRefundDisputeNotUnderReview
	}
	h, err := loadEscrowHold(ctx, tx, orderID)
	if errors.Is(err, escrow.ErrEscrowNotFound) || (err == nil && (h.Status == "closed" || !approver.Valid)) {
		return nil, recordRefundException(ctx, tx, req, disputeID, "The payment has no available verified escrow hold or approving actor.")
	}
	if err != nil {
		return nil, err
	}
	split, err := escrow.SplitRefund(h.Gross, h.Commission, h.RefundedGross, req.Amount)
	if err != nil {
		return nil, err
	}
	if h.Seller-h.Released-h.Refunded < split.Seller {
		return nil, recordRefundException(ctx, tx, req, disputeID, "The order's held proceeds cannot cover this refund.")
	}
	if err := lockEscrowWallets(ctx, tx, h.BuyerWalletID, h.SellerWalletID); err != nil {
		return nil, err
	}
	var funded bool
	if err := tx.QueryRowContext(ctx, `SELECT bw.farmer_id=$3 AND sw.farmer_id=$4 AND sw.held_balance >= $5::numeric
 AND bw.balance+$6::numeric <= 9999999999.99
 AND sw.held_balance=COALESCE((SELECT SUM(amount-released_amount-refunded_amount) FROM escrow_holds WHERE seller_wallet_id=sw.id AND status<>'closed'),0)
 AND EXISTS(SELECT 1 FROM orders o WHERE o.id=$7 AND o.total_price::numeric(12,2)=$8::numeric)
 AND (EXISTS(SELECT 1 FROM wallet_transactions p WHERE p.reference='order-payment:'||$7::text AND p.type='order_payment' AND p.wallet_id=bw.id AND p.amount=$8::numeric)
      OR EXISTS(SELECT 1 FROM payments p WHERE p.order_id=$7 AND p.status='successful' AND p.amount=$8::numeric AND p.currency='NGN'))
 FROM wallets bw, wallets sw WHERE bw.id=$1 AND sw.id=$2`, h.BuyerWalletID, h.SellerWalletID, buyer, seller, split.Seller.String(), req.Amount.String(), orderID, h.Gross.String()).Scan(&funded); err != nil {
		return nil, err
	}
	if !funded {
		return nil, recordRefundException(ctx, tx, req, disputeID, "Wallet ownership, held funds or balance bounds require review.")
	}
	if _, err := tx.ExecContext(ctx, `UPDATE wallets SET balance=balance-$1::numeric,held_balance=held_balance-$1::numeric,updated_at=clock_timestamp() WHERE id=$2`, split.Seller.String(), h.SellerWalletID); err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE wallets SET balance=balance+$1::numeric,updated_at=clock_timestamp() WHERE id=$2`, req.Amount.String(), h.BuyerWalletID); err != nil {
		return nil, err
	}
	// Zero seller reversals are valid for a commission-only final partial refund.
	if split.Seller > 0 {
		if _, err := tx.ExecContext(ctx, `INSERT INTO wallet_transactions(wallet_id,reference,type,amount) VALUES($1,$2,'sale_reversal',$3::numeric)`, h.SellerWalletID, fmt.Sprintf("sale-reversal:%d", req.RefundID), split.Seller.String()); err != nil {
			return nil, err
		}
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO wallet_transactions(wallet_id,reference,type,amount) VALUES($1,$2,'order_refund',$3::numeric)`, h.BuyerWalletID, fmt.Sprintf("order-refund:%d", req.RefundID), req.Amount.String()); err != nil {
		return nil, err
	}
	if split.Commission > 0 {
		if _, err := tx.ExecContext(ctx, `INSERT INTO platform_revenue_reversals(order_id,refund_id,amount,reference) VALUES($1,$2,$3::numeric,$4)`, orderID, req.RefundID, split.Commission.String(), ref); err != nil {
			return nil, err
		}
	}
	full := h.RefundedGross+req.Amount == h.Gross
	if _, err := tx.ExecContext(ctx, `UPDATE escrow_holds SET refunded_amount=refunded_amount+$1::numeric,commission_reversed=commission_reversed+$2::numeric,refunded_gross=refunded_gross+$3::numeric,
 status=CASE WHEN $4 THEN 'closed' ELSE 'held' END,closed_reason=CASE WHEN $4 THEN 'refunded' ELSE NULL END,closed_at=CASE WHEN $4 THEN clock_timestamp() ELSE NULL END,updated_at=clock_timestamp() WHERE id=$5`, split.Seller.String(), split.Commission.String(), req.Amount.String(), full, h.ID); err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO escrow_events(escrow_hold_id,event,amount,actor_id,reference) VALUES($1,'refunded',$2::numeric,$3,$4)`, h.ID, req.Amount.String(), req.ActorID, ref); err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE refunds SET status='succeeded',failure_reason=NULL,processed_at=clock_timestamp(),updated_at=clock_timestamp() WHERE id=$1`, req.RefundID); err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE order_disputes SET status='resolved_refund',resolved_by=$2,resolved_at=clock_timestamp(),updated_at=clock_timestamp() WHERE id=$1`, disputeID, approver); err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO order_dispute_events(dispute_id,actor_id,actor_role,event_type,from_status,to_status,metadata)
 VALUES($1,$2,'wallet','refund_succeeded','under_review','resolved_refund',jsonb_build_object('refund_id',$3::bigint))`, disputeID, req.ActorID, req.RefundID); err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE payment_exceptions SET status='resolved',resolution_note='Settled from verified escrow',resolved_by=$2,resolved_at=clock_timestamp(),updated_at=clock_timestamp() WHERE refund_id=$1 AND status IN ('open','investigating')`, req.RefundID, req.ActorID); err != nil {
		return nil, err
	}
	return &escrow.RefundResult{BuyerCredit: req.Amount, SellerReversal: split.Seller, CommissionReversal: split.Commission}, tx.Commit()
}

func recordRefundException(ctx context.Context, tx *sql.Tx, req escrow.RefundRequest, disputeID int64, detail string) error {
	if _, err := tx.ExecContext(ctx, `INSERT INTO payment_exceptions(kind,reference,order_id,refund_id,amount,currency,detail)
 VALUES('refund_unfunded',$1,$2,$3,$4::numeric,'NGN',$5) ON CONFLICT DO NOTHING`, fmt.Sprintf("refund:%d", req.RefundID), req.OrderID, req.RefundID, req.Amount.String(), detail); err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, `UPDATE refunds SET status='requires_review',failure_reason=$2,updated_at=clock_timestamp() WHERE id=$1 AND status<>'succeeded' AND (status<>'requires_review' OR failure_reason IS DISTINCT FROM $2)`, req.RefundID, detail)
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed > 0 {
		if _, err := tx.ExecContext(ctx, `UPDATE order_disputes SET updated_at=clock_timestamp() WHERE id=$1`, disputeID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO order_dispute_events(dispute_id,actor_id,actor_role,event_type,metadata)
 VALUES($1,$2,'wallet','refund_requires_review',jsonb_build_object('refund_id',$3::bigint))`, disputeID, req.ActorID, req.RefundID); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	return escrow.ErrRefundRequiresReview
}
