package repository

import (
	"backend/internal/models"
	"database/sql"
)

// ============================================================
// CHAT MESSAGE REPOSITORY
//
// Responsibility:
// - Communicates directly with the chat_messages table.
// - Saves messages.
// - Retrieves messages from a conversation.
// - Finds one message.
// - Keeps messages in chronological order.
//
// IMPORTANT:
//
// This repository does NOT decide:
//
// - Who can send a message.
// - Who belongs to a conversation.
// - Whether a conversation is private or a group.
// - Whether a user is allowed to access a conversation.
//
// Those rules belong to the ChatService.
//
// The repository's job is simply:
//
//        SERVICE
//           ↓
//      REPOSITORY
//           ↓
//       DATABASE
// ============================================================

type ChatMessageRepository struct {
	db *sql.DB
}

// ============================================================
// CREATE REPOSITORY
// ============================================================
//
// Connects the repository to PostgreSQL.
//

func NewChatMessageRepository(
	db *sql.DB,
) *ChatMessageRepository {

	return &ChatMessageRepository{
		db: db,
	}
}

// ============================================================
// CREATE MESSAGE
// ============================================================
//
// Stores a new chat message.
//
// Example:
//
// Conversation: 5
// Sender:       12
// Message:      "Good morning everyone."
//
// After PostgreSQL creates the record, the database returns:
//
// - ID
// - CreatedAt
// ============================================================

func (r *ChatMessageRepository) Create(
	message *models.ChatMessage,
) error {

	query := `
		INSERT INTO chat_messages (
			conversation_id,
			sender_id,
			message
		)
		VALUES ($1, $2, $3)
		RETURNING id, created_at
	`

	return r.db.QueryRow(
		query,
		message.ConversationID,
		message.SenderID,
		message.Message,
	).Scan(
		&message.ID,
		&message.CreatedAt,
	)
}

// ============================================================
// GET MESSAGE BY ID
// ============================================================
//
// Finds one specific chat message.
//
// This can later be useful when we add features such as:
//
// - Delete message
// - Edit message
// - Report message
//
// We don't need those features yet, but having this method
// available gives the repository a clean foundation.
// ============================================================

func (r *ChatMessageRepository) GetByID(
	messageID int,
) (*models.ChatMessage, error) {

	query := `
		SELECT
			id,
			conversation_id,
			sender_id,
			message,
			created_at
		FROM chat_messages
		WHERE id = $1
	`

	var message models.ChatMessage

	err := r.db.QueryRow(
		query,
		messageID,
	).Scan(
		&message.ID,
		&message.ConversationID,
		&message.SenderID,
		&message.Message,
		&message.CreatedAt,
	)

	if err == sql.ErrNoRows {
		return nil, nil
	}

	if err != nil {
		return nil, err
	}

	return &message, nil
}

// ============================================================
// LIST MESSAGES
// ============================================================
//
// Retrieves messages belonging to one conversation.
//
// IMPORTANT:
//
// Messages are returned in chronological order.
//
// Oldest
//   ↓
// Message 1
// Message 2
// Message 3
//   ↓
// Newest
//
// This allows the frontend to display the conversation naturally.
// ============================================================

func (r *ChatMessageRepository) ListByConversation(
	conversationID int,
) ([]models.ChatMessage, error) {

	query := `
		SELECT
			id,
			conversation_id,
			sender_id,
				m.message,
				m.created_at,
				f.full_name
			FROM chat_messages m
			JOIN farmers f ON f.id = m.sender_id
			WHERE m.conversation_id = $1
			ORDER BY m.created_at ASC, m.id ASC
	`

	rows, err := r.db.Query(
		query,
		conversationID,
	)

	if err != nil {
		return nil, err
	}

	defer rows.Close()

	var messages []models.ChatMessage

	for rows.Next() {

		var message models.ChatMessage

		err := rows.Scan(
			&message.ID,
			&message.ConversationID,
			&message.SenderID,
			&message.Message,
			&message.CreatedAt,
			&message.SenderName,
		)

		if err != nil {
			return nil, err
		}

		messages = append(
			messages,
			message,
		)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return messages, nil
}

// ============================================================
// DELETE MESSAGE
// ============================================================
//
// Deletes one message.
//
// IMPORTANT:
//
// The repository does NOT check whether the person deleting
// the message is the sender.
//
// That security/business rule belongs to ChatService.
//
// The service will first check:
//
// "Is this user allowed to delete this message?"
//
// Then the repository performs the database operation.
// ============================================================

func (r *ChatMessageRepository) Delete(
	messageID int,
) error {

	query := `
		DELETE FROM chat_messages
		WHERE id = $1
	`

	result, err := r.db.Exec(
		query,
		messageID,
	)

	if err != nil {
		return err
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}

	if rowsAffected == 0 {
		return sql.ErrNoRows
	}

	return nil
}
