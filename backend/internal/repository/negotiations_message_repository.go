package repository

import (
	"backend/internal/models"
	"database/sql"
	"errors"
)

// ErrOfferStatusConflict means the offer was not in the status the caller
// expected, usually because another participant accepted or rejected it first.
// Callers treat it as "this offer is no longer pending" rather than as a
// database failure.
var ErrOfferStatusConflict = errors.New("offer status has already changed")

// ============================================================
// NEGOTIATION MESSAGE REPOSITORY
//
// Responsibility:
// - Communicates directly with negotiation_messages table.
// - Stores normal chat messages.
// - Stores price offers.
// - Retrieves the complete conversation.
// - Retrieves individual messages/offers.
// - Updates the status of individual offers.
//
// IMPORTANT:
//
// A negotiation is the conversation.
//
// A negotiation message can be:
//
//   💬 normal chat
//   💰 price offer
//
// Accepting/rejecting an offer changes ONLY that offer.
// It does not delete or close the conversation history.
// ============================================================

type NegotiationMessageRepository struct {
	db *sql.DB
}

// ============================================================
// CREATE REPOSITORY
// ============================================================

func NewNegotiationMessageRepository(
	db *sql.DB,
) *NegotiationMessageRepository {

	return &NegotiationMessageRepository{
		db: db,
	}
}

// ============================================================
// CREATE MESSAGE
//
// Stores either:
//
//   1. Normal chat message
//   2. Price offer
//
// Example chat:
//
// MessageType = "chat"
// OfferPrice = 0
// OfferStatus = "none"
//
// Example offer:
//
// MessageType = "offer"
// OfferPrice = 50000
// OfferStatus = "pending"
// ============================================================

func (r *NegotiationMessageRepository) Create(
	m *models.NegotiationMessage,
) error {

	query := `
		INSERT INTO negotiation_messages (
			negotiation_id,
			sender_id,
			message_type,
			offer_price,
			message,
			offer_status
		)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id, created_at
	`

	err := r.db.QueryRow(
		query,
		m.NegotiationID,
		m.SenderID,
		m.MessageType,
		m.OfferPrice,
		m.Message,
		m.OfferStatus,
	).Scan(
		&m.ID,
		&m.CreatedAt,
	)

	return err
}

// ============================================================
// LIST NEGOTIATION MESSAGES
//
// Returns the COMPLETE conversation.
//
// Includes:
//
// 💬 chat messages
// 💰 pending offers
// ❌ rejected offers
// ✅ accepted offers
//
// Nothing is removed from the conversation history.
// ============================================================

func (r *NegotiationMessageRepository) ListByNegotiation(
	negotiationID int,
) ([]models.NegotiationMessage, error) {

	query := `
		SELECT
			id,
			negotiation_id,
			sender_id,
			message_type,
			offer_price,
			message,
			offer_status,
			created_at
		FROM negotiation_messages
		WHERE negotiation_id = $1
		ORDER BY created_at ASC, id ASC
	`

	rows, err := r.db.Query(
		query,
		negotiationID,
	)

	if err != nil {
		return nil, err
	}

	defer rows.Close()

	var messages []models.NegotiationMessage

	for rows.Next() {

		var m models.NegotiationMessage

		err := rows.Scan(
			&m.ID,
			&m.NegotiationID,
			&m.SenderID,
			&m.MessageType,
			&m.OfferPrice,
			&m.Message,
			&m.OfferStatus,
			&m.CreatedAt,
		)

		if err != nil {
			return nil, err
		}

		messages = append(messages, m)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return messages, nil
}

// ============================================================
// GET MESSAGE BY ID
//
// Retrieves one specific message.
//
// This can be:
//
// 💬 chat
// 💰 offer
//
// The service layer decides whether the message can be
// accepted or rejected.
// ============================================================

func (r *NegotiationMessageRepository) GetByID(
	messageID int,
) (*models.NegotiationMessage, error) {

	query := `
		SELECT
			id,
			negotiation_id,
			sender_id,
			message_type,
			offer_price,
			message,
			offer_status,
			created_at
		FROM negotiation_messages
		WHERE id = $1
	`

	var m models.NegotiationMessage

	err := r.db.QueryRow(
		query,
		messageID,
	).Scan(
		&m.ID,
		&m.NegotiationID,
		&m.SenderID,
		&m.MessageType,
		&m.OfferPrice,
		&m.Message,
		&m.OfferStatus,
		&m.CreatedAt,
	)

	if err == sql.ErrNoRows {
		return nil, nil
	}

	if err != nil {
		return nil, err
	}

	return &m, nil
}

// ============================================================
// UPDATE OFFER STATUS
//
// Changes the status of ONE offer.
//
// This function should only be used for messages where:
//
// MessageType == "offer"
//
// Possible statuses:
//
// pending
// accepted
// rejected
//
// IMPORTANT:
//
// This does NOT change the negotiation itself.
// ============================================================

// UpdateOfferStatus moves one offer from expectedStatus to status.
//
// The expected status is part of the UPDATE, so two participants accepting the
// same offer at once cannot both succeed: the second statement matches no row.
// That is what keeps an accepted deal from being added to the cart twice.
func (r *NegotiationMessageRepository) UpdateOfferStatus(
	offerID int,
	status string,
	expectedStatus string,
) error {

	query := `
		UPDATE negotiation_messages
		SET offer_status = $1
		WHERE id = $2
		AND message_type = 'offer'
		AND offer_status = $3
	`

	result, err := r.db.Exec(
		query,
		status,
		offerID,
		expectedStatus,
	)

	if err != nil {
		return err
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}

	if rowsAffected == 0 {
		return ErrOfferStatusConflict
	}

	return nil
}
