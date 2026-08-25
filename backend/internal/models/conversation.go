package models

import "time"

// ============================================================
// CONVERSATION MODEL
//
// Responsibility:
// - Represents a general Agro-Shield chat room.
//
// A conversation can be:
//
// private → one user chatting with another user
//
// group → multiple users communicating in one room
//
// IMPORTANT:
// This model is for GENERAL CHAT.
// It is completely separate from negotiations.
// ============================================================

type Conversation struct {
	ID int `json:"id"`

	// Name is mainly used for group conversations.
	//
	// Example:
	//
	// "Maize Farmers Association"
	//
	// Private conversations may not need a name.
	Name      string `json:"name"`
	OtherName string `json:"other_name"`

	// Type determines what kind of conversation this is.
	//
	// Possible values:
	//
	// "private"
	// "group"
	Type string `json:"type"`

	// CreatedBy is the user who created the conversation.
	CreatedBy int `json:"created_by"`

	CreatedAt time.Time `json:"created_at"`

	UpdatedAt time.Time `json:"updated_at"`
}
