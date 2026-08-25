package models

import "time"

// ============================================================
// CHAT MESSAGE MODEL
//
// Responsibility:
// - Represents one normal message inside a conversation.
//
// IMPORTANT:
//
// This is NOT NegotiationMessage.
//
// NegotiationMessage:
//     Used for buyer/seller negotiation.
//
// ChatMessage:
//     Used for normal Agro-Shield communication.
//
// Example:
//
// "Good morning everyone."
//
// "Does anyone have fertilizer?"
//
// "The maize meeting is tomorrow."
// ============================================================

type ChatMessage struct {
	ID int `json:"id"`

	ConversationID int `json:"conversation_id"`

	SenderID   int    `json:"sender_id"`
	SenderName string `json:"sender_name"`

	Message string `json:"message"`

	CreatedAt time.Time `json:"created_at"`
}
