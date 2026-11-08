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
	ErrDisputeOrderNotFound = errors.New("order not found")
	ErrDisputeNotPaid       = errors.New("order has no confirmed wallet payment")
	ErrDisputeNotBuyer      = errors.New("only the order buyer can open this dispute")
	ErrDisputeDelivery      = errors.New("delivery does not belong to this order")
	ErrDisputeAmount        = errors.New("requested refund amount is invalid")
	ErrDisputeNotFound      = errors.New("dispute not found")
	ErrDisputeNotSeller     = errors.New("only the order seller can respond")
	ErrDisputeState         = errors.New("dispute is not in a permitted state")
)

type OrderDisputeRepository struct {
	db *sql.DB
}

func NewOrderDisputeRepository(db *sql.DB) *OrderDisputeRepository {
	return &OrderDisputeRepository{db: db}
}

type rowScanner interface {
	Scan(dest ...any) error
}

const disputeColumns = `
	id, order_id, delivery_id, buyer_id, seller_id,
	reason_code, description, requested_amount::text, status,
	seller_response, resolution_note, resolved_by, resolved_at,
	created_at, updated_at
`

func scanDispute(row rowScanner) (*models.OrderDispute, error) {
	var d models.OrderDispute
	var deliveryID, resolvedBy sql.NullInt64
	var sellerResponse, resolutionNote sql.NullString
	var resolvedAt sql.NullTime

	err := row.Scan(
		&d.ID,
		&d.OrderID,
		&deliveryID,
		&d.BuyerID,
		&d.SellerID,
		&d.ReasonCode,
		&d.Description,
		&d.RequestedAmount,
		&d.Status,
		&sellerResponse,
		&resolutionNote,
		&resolvedBy,
		&resolvedAt,
		&d.CreatedAt,
		&d.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}

	if deliveryID.Valid {
		id := deliveryID.Int64
		d.DeliveryID = &id
	}
	if sellerResponse.Valid {
		value := sellerResponse.String
		d.SellerResponse = &value
	}
	if resolutionNote.Valid {
		value := resolutionNote.String
		d.ResolutionNote = &value
	}
	if resolvedBy.Valid {
		id := int(resolvedBy.Int64)
		d.ResolvedBy = &id
	}
	if resolvedAt.Valid {
		value := resolvedAt.Time
		d.ResolvedAt = &value
	}

	return &d, nil
}

// Create opens a dispute only for an order with a matching successful
// wallet payment. It locks the order so concurrent submissions cannot
// bypass the active-dispute unique index.
func (r *OrderDisputeRepository) Create(
	ctx context.Context,
	d *models.OrderDispute,
) error {
	if d == nil || d.OrderID <= 0 || d.BuyerID <= 0 {
		return fmt.Errorf("invalid dispute details")
	}

	d.ReasonCode = strings.TrimSpace(d.ReasonCode)
	d.Description = strings.TrimSpace(d.Description)
	d.RequestedAmount = strings.TrimSpace(d.RequestedAmount)

	if d.ReasonCode == "" || d.Description == "" || d.RequestedAmount == "" {
		return fmt.Errorf("reason, description and requested amount are required")
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var buyerID, sellerID int
	var orderTotal string
	var paid bool

	err = tx.QueryRowContext(ctx, `
		SELECT
			o.buyer_id,
			c.farmer_id,
			o.total_price::numeric(12,2)::text,
			EXISTS (
				SELECT 1
				FROM wallet_transactions wt
				WHERE wt.reference = 'order-payment:' || o.id::text
				  AND wt.type = 'order_payment'
				  AND wt.amount = o.total_price::numeric(12,2)
			)
		FROM orders o
		JOIN crops c ON c.id = o.crop_id
		WHERE o.id = $1
		FOR UPDATE OF o
	`, d.OrderID).Scan(&buyerID, &sellerID, &orderTotal, &paid)

	if errors.Is(err, sql.ErrNoRows) {
		return ErrDisputeOrderNotFound
	}
	if err != nil {
		return err
	}
	if buyerID != d.BuyerID {
		return ErrDisputeNotBuyer
	}
	if !paid {
		return ErrDisputeNotPaid
	}

	if d.DeliveryID != nil {
		var deliveryBuyerID, deliverySellerID int
		err = tx.QueryRowContext(ctx, `
			SELECT buyer_id, farmer_id
			FROM deliveries
			WHERE id = $1 AND order_id = $2
			FOR UPDATE
		`, *d.DeliveryID, d.OrderID).Scan(
			&deliveryBuyerID,
			&deliverySellerID,
		)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrDisputeDelivery
		}
		if err != nil {
			return err
		}
		if deliveryBuyerID != buyerID || deliverySellerID != sellerID {
			return ErrDisputeDelivery
		}
	}

	var amountValid bool
	err = tx.QueryRowContext(ctx, `
		SELECT
			$1::numeric > 0
			AND $1::numeric <= $2::numeric
			AND $1::numeric = round($1::numeric, 2)
	`, d.RequestedAmount, orderTotal).Scan(&amountValid)
	if err != nil {
		return fmt.Errorf("validate dispute amount: %w", err)
	}
	if !amountValid {
		return ErrDisputeAmount
	}

	d.SellerID = sellerID
	d.Status = models.DisputeSubmitted

	err = tx.QueryRowContext(ctx, `
		INSERT INTO order_disputes (
			order_id, delivery_id, buyer_id, seller_id,
			reason_code, description, requested_amount, status
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7::numeric(12,2), $8)
		RETURNING id, created_at, updated_at
	`,
		d.OrderID,
		d.DeliveryID,
		d.BuyerID,
		d.SellerID,
		d.ReasonCode,
		d.Description,
		d.RequestedAmount,
		d.Status,
	).Scan(&d.ID, &d.CreatedAt, &d.UpdatedAt)
	if err != nil {
		return err
	}

	_, err = tx.ExecContext(ctx, `
		INSERT INTO order_dispute_events (
			dispute_id, actor_id, actor_role, event_type,
			to_status, note
		)
		VALUES ($1, $2, 'buyer', 'dispute_submitted', $3, $4)
	`,
		d.ID,
		d.BuyerID,
		d.Status,
		d.Description,
	)
	if err != nil {
		return err
	}

	return tx.Commit()
}

func (r *OrderDisputeRepository) GetByID(
	ctx context.Context,
	id int64,
) (*models.OrderDispute, error) {
	if id <= 0 {
		return nil, ErrDisputeNotFound
	}

	query := `SELECT ` + disputeColumns + ` FROM order_disputes WHERE id = $1`
	d, err := scanDispute(r.db.QueryRowContext(ctx, query, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return d, err
}

func (r *OrderDisputeRepository) ListByBuyer(
	ctx context.Context,
	buyerID int,
) ([]models.OrderDispute, error) {
	return r.list(ctx, `
		SELECT `+disputeColumns+`
		FROM order_disputes
		WHERE buyer_id = $1
		ORDER BY created_at DESC
	`, buyerID)
}

func (r *OrderDisputeRepository) ListBySeller(
	ctx context.Context,
	sellerID int,
) ([]models.OrderDispute, error) {
	return r.list(ctx, `
		SELECT `+disputeColumns+`
		FROM order_disputes
		WHERE seller_id = $1
		ORDER BY created_at DESC
	`, sellerID)
}

func (r *OrderDisputeRepository) list(
	ctx context.Context,
	query string,
	userID int,
) ([]models.OrderDispute, error) {
	rows, err := r.db.QueryContext(ctx, query, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	disputes := make([]models.OrderDispute, 0)
	for rows.Next() {
		d, err := scanDispute(rows)
		if err != nil {
			return nil, err
		}
		disputes = append(disputes, *d)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return disputes, nil
}

// AddSellerResponse records the seller's response and its audit event
// atomically. A seller cannot respond to another seller's dispute.
func (r *OrderDisputeRepository) AddSellerResponse(
	ctx context.Context,
	disputeID int64,
	sellerID int,
	response string,
) error {
	response = strings.TrimSpace(response)
	if disputeID <= 0 || sellerID <= 0 || response == "" {
		return fmt.Errorf("dispute and response are required")
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var currentStatus string
	err = tx.QueryRowContext(ctx, `
		SELECT status
		FROM order_disputes
		WHERE id = $1 AND seller_id = $2
		FOR UPDATE
	`, disputeID, sellerID).Scan(&currentStatus)

	if errors.Is(err, sql.ErrNoRows) {
		return ErrDisputeNotFound
	}
	if err != nil {
		return err
	}
	if currentStatus != models.DisputeSubmitted {
		return ErrDisputeState
	}

	_, err = tx.ExecContext(ctx, `
		UPDATE order_disputes
		SET seller_response = $1,
		    status = $2,
		    updated_at = CURRENT_TIMESTAMP
		WHERE id = $3
	`, response, models.DisputeSellerResponded, disputeID)
	if err != nil {
		return err
	}

	_, err = tx.ExecContext(ctx, `
		INSERT INTO order_dispute_events (
			dispute_id, actor_id, actor_role, event_type,
			from_status, to_status, note
		)
		VALUES ($1, $2, 'seller', 'seller_responded', $3, $4, $5)
	`,
		disputeID,
		sellerID,
		currentStatus,
		models.DisputeSellerResponded,
		response,
	)
	if err != nil {
		return err
	}

	return tx.Commit()
}

// AddEvent appends an audit event. It deliberately does not update or delete
// existing events.
func (r *OrderDisputeRepository) AddEvent(
	ctx context.Context,
	event *models.OrderDisputeEvent,
) error {
	if event == nil || event.DisputeID <= 0 || event.EventType == "" || event.ActorRole == "" {
		return fmt.Errorf("invalid dispute event")
	}

	_, err := r.db.ExecContext(ctx, `
		INSERT INTO order_dispute_events (
			dispute_id, actor_id, actor_role, event_type,
			from_status, to_status, note, metadata
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, COALESCE($8::jsonb, '{}'::jsonb))
	`,
		event.DisputeID,
		event.ActorID,
		event.ActorRole,
		event.EventType,
		event.FromStatus,
		event.ToStatus,
		event.Note,
		nullableJSON(event.Metadata),
	)
	if err != nil {
		return err
	}

	return nil
}

func nullableJSON(value []byte) any {
	if len(value) == 0 {
		return nil
	}
	return string(value)
}

// ListEvents returns the immutable event history in chronological order.
func (r *OrderDisputeRepository) ListEvents(
	ctx context.Context,
	disputeID int64,
) ([]models.OrderDisputeEvent, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, dispute_id, actor_id, actor_role, event_type,
		       from_status, to_status, note, metadata, created_at
		FROM order_dispute_events
		WHERE dispute_id = $1
		ORDER BY created_at ASC, id ASC
	`, disputeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	events := make([]models.OrderDisputeEvent, 0)
	for rows.Next() {
		var event models.OrderDisputeEvent
		var actorID sql.NullInt64
		var fromStatus, toStatus, note sql.NullString

		if err := rows.Scan(
			&event.ID,
			&event.DisputeID,
			&actorID,
			&event.ActorRole,
			&event.EventType,
			&fromStatus,
			&toStatus,
			&note,
			&event.Metadata,
			&event.CreatedAt,
		); err != nil {
			return nil, err
		}

		if actorID.Valid {
			id := int(actorID.Int64)
			event.ActorID = &id
		}
		if fromStatus.Valid {
			value := fromStatus.String
			event.FromStatus = &value
		}
		if toStatus.Valid {
			value := toStatus.String
			event.ToStatus = &value
		}
		if note.Valid {
			value := note.String
			event.Note = &value
		}

		events = append(events, event)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}
	return events, nil
}

// Keep the time import available for model-compatible timestamp handling.
var _ = time.Time{}
