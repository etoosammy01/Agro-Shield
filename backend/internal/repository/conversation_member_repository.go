package repository

import (
	"backend/internal/models"
	"database/sql"
)

// ============================================================
// CONVERSATION MEMBER REPOSITORY
//
// Responsibility:
// - Communicates directly with conversation_members table.
// - Adds users to conversations.
// - Removes users from conversations.
// - Checks whether a user belongs to a conversation.
// - Gets members of a conversation.
//
// IMPORTANT:
//
// Conversation = the chat room.
//
// ConversationMember = the people inside the room.
//
// Example:
//
// Conversation:
// "Maize Farmers Association"
//
// Members:
// James
// John
// Peter
// Sarah
//
// The relationship is stored in conversation_members.
// ============================================================

type ConversationMemberRepository struct {
	db *sql.DB
}

// ============================================================
// CREATE REPOSITORY
// ============================================================

func NewConversationMemberRepository(
	db *sql.DB,
) *ConversationMemberRepository {

	return &ConversationMemberRepository{
		db: db,
	}
}

// ============================================================
// ADD MEMBER
//
// Adds a user to a conversation.
//
// Example:
//
// Conversation ID: 5
// User ID: 12
//
// This means:
//
// User 12 belongs to Conversation 5.
// ============================================================

func (r *ConversationMemberRepository) AddMember(
	member *models.ConversationMember,
) error {

	query := `
		INSERT INTO conversation_members (
			conversation_id,
			user_id
		)
		VALUES ($1, $2)
		RETURNING id, joined_at
	`

	return r.db.QueryRow(
		query,
		member.ConversationID,
		member.UserID,
	).Scan(
		&member.ID,
		&member.JoinedAt,
	)
}

// ============================================================
// REMOVE MEMBER
//
// Removes a user from a conversation.
//
// Example:
//
// John leaves:
//
// "Maize Farmers Association"
//
// His membership record is deleted.
// ============================================================

func (r *ConversationMemberRepository) RemoveMember(
	conversationID,
	userID int,
) error {

	query := `
		DELETE FROM conversation_members
		WHERE conversation_id = $1
		AND user_id = $2
	`

	result, err := r.db.Exec(
		query,
		conversationID,
		userID,
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

// ============================================================
// IS MEMBER
//
// Checks whether a user belongs to a conversation.
//
// Returns:
//
// true  → user belongs to the conversation
// false → user does not belong
//
// This will be VERY important for security.
//
// Example:
//
// User tries:
//
// /chat?id=10
//
// Before allowing the user to read messages:
//
// "Does this user belong to conversation 10?"
//
// If NO → access denied.
// ============================================================

func (r *ConversationMemberRepository) IsMember(
	conversationID,
	userID int,
) (bool, error) {

	query := `
		SELECT EXISTS (
			SELECT 1
			FROM conversation_members
			WHERE conversation_id = $1
			AND user_id = $2
		)
	`

	var exists bool

	err := r.db.QueryRow(
		query,
		conversationID,
		userID,
	).Scan(&exists)

	if err != nil {
		return false, err
	}

	return exists, nil
}

// ============================================================
// LIST MEMBERS
//
// Retrieves all users belonging to a conversation.
//
// This will later help us display:
//
// 👥 25 members
//
// or:
//
// James
// John
// Peter
// Sarah
//
// We use a JOIN with farmers because user information is stored
// in the farmers table.
// ============================================================

func (r *ConversationMemberRepository) ListMembers(
	conversationID int,
) ([]models.ConversationMember, error) {

	query := `
		SELECT
			cm.id,
			cm.conversation_id,
			cm.user_id,
			f.full_name,
			COALESCE(f.photo_url, ''),
			cm.joined_at
		FROM conversation_members cm
		JOIN farmers f ON f.id = cm.user_id
		WHERE cm.conversation_id = $1
		ORDER BY cm.joined_at ASC
	`

	rows, err := r.db.Query(
		query,
		conversationID,
	)

	if err != nil {
		return nil, err
	}

	defer rows.Close()

	var members []models.ConversationMember

	for rows.Next() {

		var member models.ConversationMember

		err := rows.Scan(
			&member.ID,
			&member.ConversationID,
			&member.UserID,
			&member.FullName,
			&member.PhotoURL,
			&member.JoinedAt,
		)

		if err != nil {
			return nil, err
		}

		members = append(
			members,
			member,
		)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return members, nil
}

// ============================================================
// COUNT MEMBERS
//
// Returns the number of people inside a conversation.
//
// Example:
//
// Maize Farmers Association
// 37 members
//
// This can later be displayed on the group page.
// ============================================================

func (r *ConversationMemberRepository) CountMembers(
	conversationID int,
) (int, error) {

	query := `
		SELECT COUNT(*)
		FROM conversation_members
		WHERE conversation_id = $1
	`

	var count int

	err := r.db.QueryRow(
		query,
		conversationID,
	).Scan(&count)

	if err != nil {
		return 0, err
	}

	return count, nil
}

func (r *ConversationMemberRepository) MarkRead(conversationID, userID int) error {
	_, err := r.db.Exec(`UPDATE conversation_members SET last_read_at = CURRENT_TIMESTAMP WHERE conversation_id = $1 AND user_id = $2`, conversationID, userID)
	return err
}
