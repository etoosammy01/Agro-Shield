package repository

import (
	"backend/internal/models"
	"database/sql"
)

// ============================================================
// CONVERSATION REPOSITORY
//
// Responsibility:
// - Communicates directly with the conversations table.
// - Creates private and group conversations.
// - Finds conversations.
// - Retrieves conversations belonging to a user.
//
// IMPORTANT:
//
// The repository only handles DATABASE operations.
//
// It does NOT decide:
// - who is allowed to create a group
// - who can join
// - who can send messages
//
// Those decisions belong to the Service layer.
// ============================================================

type ConversationRepository struct {
	db *sql.DB
}

// ============================================================
// CREATE REPOSITORY
// ============================================================
//
// Connects the repository to PostgreSQL.
//

func NewConversationRepository(
	db *sql.DB,
) *ConversationRepository {

	return &ConversationRepository{
		db: db,
	}
}

// ============================================================
// CREATE CONVERSATION
// ============================================================
//
// Creates a new conversation.
//
// Examples:
//
// Private conversation:
//   Type = "private"
//   Name = ""
//
// Group conversation:
//   Type = "group"
//   Name = "Maize Farmers Association"
//
// The database returns the new conversation ID and timestamps.
// ============================================================

func (r *ConversationRepository) Create(
	conversation *models.Conversation,
) error {

	query := `
		INSERT INTO conversations (
			name,
			type,
			created_by
		)
		VALUES ($1, $2, $3)
		RETURNING id, created_at, updated_at
	`

	return r.db.QueryRow(
		query,
		conversation.Name,
		conversation.Type,
		conversation.CreatedBy,
	).Scan(
		&conversation.ID,
		&conversation.CreatedAt,
		&conversation.UpdatedAt,
	)
}

// ============================================================
// GET CONVERSATION BY ID
// ============================================================
//
// Finds one conversation using its ID.
//
// Example:
//
// /chat?id=15
//
// The service can then check whether the logged-in user
// belongs to this conversation.
// ============================================================

func (r *ConversationRepository) GetByID(
	conversationID int,
) (*models.Conversation, error) {

	query := `
		SELECT
			id,
			name,
			type,
			created_by,
			created_at,
			updated_at
		FROM conversations
		WHERE id = $1
	`

	var conversation models.Conversation

	err := r.db.QueryRow(
		query,
		conversationID,
	).Scan(
		&conversation.ID,
		&conversation.Name,
		&conversation.Type,
		&conversation.CreatedBy,
		&conversation.CreatedAt,
		&conversation.UpdatedAt,
	)

	if err == sql.ErrNoRows {
		return nil, nil
	}

	if err != nil {
		return nil, err
	}

	return &conversation, nil
}

// FindPrivateBetween returns the existing one-to-one chat for two users.
func (r *ConversationRepository) FindPrivateBetween(firstUserID, secondUserID int) (*models.Conversation, error) {
	query := `
		SELECT c.id, c.name, c.type, c.created_by, c.created_at, c.updated_at
		FROM conversations c
		JOIN conversation_members first_member ON first_member.conversation_id = c.id AND first_member.user_id = $1
		JOIN conversation_members second_member ON second_member.conversation_id = c.id AND second_member.user_id = $2
		WHERE c.type = 'private'
		  AND (SELECT COUNT(*) FROM conversation_members cm WHERE cm.conversation_id = c.id) = 2
		ORDER BY c.id ASC LIMIT 1`
	var conversation models.Conversation
	err := r.db.QueryRow(query, firstUserID, secondUserID).Scan(&conversation.ID, &conversation.Name, &conversation.Type, &conversation.CreatedBy, &conversation.CreatedAt, &conversation.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &conversation, nil
}

// ============================================================
// LIST USER CONVERSATIONS
// ============================================================
//
// Returns every conversation that a user belongs to.
//
// This uses conversation_members because the members are stored
// separately from the conversation itself.
//
// Example:
//
// James belongs to:
//
// - John
// - Maize Farmers Association
// - Farm Tools Sellers
//
// All three conversations will be returned.
// ============================================================

func (r *ConversationRepository) ListForUser(
	userID int,
) ([]models.Conversation, error) {

	query := `
		SELECT
			c.id,
			c.name,
			COALESCE((SELECT f.full_name FROM conversation_members cm JOIN farmers f ON f.id = cm.user_id WHERE cm.conversation_id = c.id AND cm.user_id <> $1 LIMIT 1), '') AS other_name,
			COALESCE((SELECT cm.user_id FROM conversation_members cm WHERE cm.conversation_id = c.id AND cm.user_id <> $1 LIMIT 1), 0) AS other_user_id,
			COALESCE((SELECT f.photo_url FROM conversation_members cm JOIN farmers f ON f.id = cm.user_id WHERE cm.conversation_id = c.id AND cm.user_id <> $1 LIMIT 1), '') AS other_photo_url,
			c.type,
			c.created_by,
			c.created_at,
			c.updated_at
			, (SELECT COUNT(*) FROM chat_messages msg WHERE msg.conversation_id = c.id AND msg.sender_id <> $1 AND msg.created_at > COALESCE(cm.last_read_at, cm.joined_at)) AS unread_count
		FROM conversations c
		INNER JOIN conversation_members cm
			ON cm.conversation_id = c.id
		WHERE cm.user_id = $1
		ORDER BY c.updated_at DESC
	`

	rows, err := r.db.Query(
		query,
		userID,
	)

	if err != nil {
		return nil, err
	}

	defer rows.Close()

	var conversations []models.Conversation

	for rows.Next() {

		var conversation models.Conversation

		err := rows.Scan(
			&conversation.ID,
			&conversation.Name,
			&conversation.OtherName,
			&conversation.OtherUserID,
			&conversation.OtherPhotoURL,
			&conversation.Type,
			&conversation.CreatedBy,
			&conversation.CreatedAt,
			&conversation.UpdatedAt,
			&conversation.UnreadCount,
		)

		if err != nil {
			return nil, err
		}

		conversations = append(
			conversations,
			conversation,
		)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return conversations, nil
}

// ============================================================
// UPDATE CONVERSATION TIMESTAMP
// ============================================================
//
// Updates the conversation's updated_at value.
//
// We will use this whenever a new chat message is sent.
//
// This allows the chat list to show the most recently active
// conversations first.
// ============================================================

func (r *ConversationRepository) Touch(
	conversationID int,
) error {

	query := `
		UPDATE conversations
		SET updated_at = CURRENT_TIMESTAMP
		WHERE id = $1
	`

	_, err := r.db.Exec(
		query,
		conversationID,
	)

	return err
}
