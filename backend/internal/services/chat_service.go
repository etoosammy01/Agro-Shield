package services

import (
	"errors"
	"strings"

	"backend/internal/models"
	"backend/internal/repository"
)

// ============================================================
// CHAT SERVICE
//
// Responsibility:
// - Controls the business rules for Agro-Shield chat.
// - Creates private conversations.
// - Creates group conversations.
// - Adds and removes members.
// - Sends messages.
// - Loads conversations.
// - Loads messages.
// - Checks membership before allowing access.
//
// IMPORTANT:
//
// Handler
//    ↓
// ChatService
//    ↓
// Repository
//    ↓
// PostgreSQL
//
// The service is where we protect the chat system.
// ============================================================

type ChatService struct {
	conversationRepo *repository.ConversationRepository
	memberRepo       *repository.ConversationMemberRepository
	messageRepo      *repository.ChatMessageRepository
}

// ============================================================
// CREATE CHAT SERVICE
// ============================================================

func NewChatService(
	conversationRepo *repository.ConversationRepository,
	memberRepo *repository.ConversationMemberRepository,
	messageRepo *repository.ChatMessageRepository,
) *ChatService {

	return &ChatService{
		conversationRepo: conversationRepo,
		memberRepo:       memberRepo,
		messageRepo:      messageRepo,
	}
}

// ============================================================
// CREATE GROUP
//
// Creates a new group conversation.
//
// Example:
//
// Name:
// "Maize Farmers Association"
//
// Creator:
// James
//
// The creator automatically becomes the first member.
//
// ============================================================

func (s *ChatService) CreateGroup(
	creatorID int,
	name string,
) (*models.Conversation, error) {

	name = strings.TrimSpace(name)

	if name == "" {
		return nil, errors.New(
			"group name cannot be empty",
		)
	}

	if len(name) > 100 {
		return nil, errors.New(
			"group name is too long",
		)
	}

	conversation := &models.Conversation{
		Name:      name,
		Type:      "group",
		CreatedBy: creatorID,
	}

	if err := s.conversationRepo.Create(
		conversation,
	); err != nil {
		return nil, err
	}

	// The creator automatically joins the group.
	member := &models.ConversationMember{
		ConversationID: conversation.ID,
		UserID:         creatorID,
	}

	if err := s.memberRepo.AddMember(member); err != nil {
		return nil, err
	}

	return conversation, nil
}

// ============================================================
// CREATE PRIVATE CHAT
//
// Creates a one-to-one conversation.
//
// Example:
//
// James wants to chat with John.
//
// The resulting conversation contains:
//
// James
// John
//
// ============================================================

func (s *ChatService) CreatePrivateChat(
	userID,
	otherUserID int,
) (*models.Conversation, error) {

	if userID <= 0 || otherUserID <= 0 {
		return nil, errors.New(
			"invalid user ID",
		)
	}

	if userID == otherUserID {
		return nil, errors.New(
			"you cannot create a private chat with yourself",
		)
	}

	existing, err := s.conversationRepo.FindPrivateBetween(userID, otherUserID)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return existing, nil
	}

	conversation := &models.Conversation{
		Name:      "",
		Type:      "private",
		CreatedBy: userID,
	}

	if err := s.conversationRepo.Create(
		conversation,
	); err != nil {
		return nil, err
	}

	// Add first user.
	firstMember := &models.ConversationMember{
		ConversationID: conversation.ID,
		UserID:         userID,
	}

	if err := s.memberRepo.AddMember(
		firstMember,
	); err != nil {
		return nil, err
	}

	// Add second user.
	secondMember := &models.ConversationMember{
		ConversationID: conversation.ID,
		UserID:         otherUserID,
	}

	if err := s.memberRepo.AddMember(
		secondMember,
	); err != nil {
		return nil, err
	}

	return conversation, nil
}

// ============================================================
// ADD MEMBER
//
// Adds a user to a group.
//
// IMPORTANT:
//
// Only GROUP conversations can have members added.
//
// Private chats already have their two members.
//
// The caller must already be a member of the group.
//
// ============================================================

func (s *ChatService) AddMember(
	conversationID,
	requesterID,
	userID int,
) error {

	conversation, err := s.conversationRepo.GetByID(
		conversationID,
	)

	if err != nil {
		return err
	}

	if conversation == nil {
		return errors.New(
			"conversation not found",
		)
	}

	if conversation.Type != "group" {
		return errors.New(
			"members can only be added to groups",
		)
	}

	// Check requester membership.
	isMember, err := s.memberRepo.IsMember(
		conversationID,
		requesterID,
	)

	if err != nil {
		return err
	}

	if !isMember {
		return errors.New(
			"you are not a member of this group",
		)
	}

	// Check if user is already a member.
	alreadyMember, err := s.memberRepo.IsMember(
		conversationID,
		userID,
	)

	if err != nil {
		return err
	}

	if alreadyMember {
		return errors.New(
			"user is already a member of this group",
		)
	}

	member := &models.ConversationMember{
		ConversationID: conversationID,
		UserID:         userID,
	}

	return s.memberRepo.AddMember(member)
}

// ============================================================
// REMOVE MEMBER
//
// Removes a member from a group.
//
// The group creator cannot be removed through this function.
//
// The creator should leave only after we later implement
// ownership transfer.
//
// ============================================================

func (s *ChatService) RemoveMember(
	conversationID,
	requesterID,
	userID int,
) error {

	conversation, err := s.conversationRepo.GetByID(
		conversationID,
	)

	if err != nil {
		return err
	}

	if conversation == nil {
		return errors.New(
			"conversation not found",
		)
	}

	if conversation.Type != "group" {
		return errors.New(
			"members can only be removed from groups",
		)
	}

	// Only the group creator can remove members.
	if conversation.CreatedBy != requesterID {
		return errors.New(
			"only the group creator can remove members",
		)
	}

	// Protect the creator.
	if userID == conversation.CreatedBy {
		return errors.New(
			"group creator cannot be removed",
		)
	}

	return s.memberRepo.RemoveMember(
		conversationID,
		userID,
	)
}

// ============================================================
// LEAVE GROUP
//
// Allows a normal member to leave a group.
//
// The creator cannot leave yet because we have not implemented
// group ownership transfer.
//
// ============================================================

func (s *ChatService) LeaveGroup(
	conversationID,
	userID int,
) error {

	conversation, err := s.conversationRepo.GetByID(
		conversationID,
	)

	if err != nil {
		return err
	}

	if conversation == nil {
		return errors.New(
			"conversation not found",
		)
	}

	if conversation.Type != "group" {
		return errors.New(
			"this is not a group",
		)
	}

	if conversation.CreatedBy == userID {
		return errors.New(
			"group creator cannot leave yet",
		)
	}

	isMember, err := s.memberRepo.IsMember(
		conversationID,
		userID,
	)

	if err != nil {
		return err
	}

	if !isMember {
		return errors.New(
			"you are not a member of this group",
		)
	}

	return s.memberRepo.RemoveMember(
		conversationID,
		userID,
	)
}

// ============================================================
// SEND MESSAGE
//
// Sends a normal chat message.
//
// Before saving:
//
// 1. Conversation must exist.
// 2. User must belong to conversation.
// 3. Message cannot be empty.
//
// Then:
//
// Message → Database
//
// And the conversation's updated_at timestamp is refreshed.
// ============================================================

func (s *ChatService) SendMessage(
	conversationID,
	senderID int,
	message string,
) (*models.ChatMessage, error) {

	message = strings.TrimSpace(message)

	if message == "" {
		return nil, errors.New(
			"message cannot be empty",
		)
	}

	if len(message) > 5000 {
		return nil, errors.New(
			"message is too long",
		)
	}

	conversation, err := s.conversationRepo.GetByID(
		conversationID,
	)

	if err != nil {
		return nil, err
	}

	if conversation == nil {
		return nil, errors.New(
			"conversation not found",
		)
	}

	isMember, err := s.memberRepo.IsMember(
		conversationID,
		senderID,
	)

	if err != nil {
		return nil, err
	}

	if !isMember {
		return nil, errors.New(
			"you are not a member of this conversation",
		)
	}

	chatMessage := &models.ChatMessage{
		ConversationID: conversationID,
		SenderID:       senderID,
		Message:        message,
	}

	if err := s.messageRepo.Create(
		chatMessage,
	); err != nil {
		return nil, err
	}

	// Update conversation activity.
	if err := s.conversationRepo.Touch(
		conversationID,
	); err != nil {
		return nil, err
	}

	return chatMessage, nil
}

// ============================================================
// GET USER CONVERSATIONS
//
// Returns all conversations belonging to the logged-in user.
//
// This will power:
//
// "My Chats"
//
// Example:
//
// James sees:
//
// 🌽 Maize Farmers Association
// 🛠️ Farm Tools Sellers
// 👤 John
// ============================================================

func (s *ChatService) MyConversations(
	userID int,
) ([]models.Conversation, error) {

	return s.conversationRepo.ListForUser(
		userID,
	)
}

// ============================================================
// GET CONVERSATION
//
// Retrieves one conversation.
//
// IMPORTANT:
//
// The user must be a member.
//
// This prevents someone from simply changing:
//
// /chat?id=5
//
// and reading another group's messages.
// ============================================================

func (s *ChatService) GetConversation(
	conversationID,
	userID int,
) (*models.Conversation, error) {

	conversation, err := s.conversationRepo.GetByID(
		conversationID,
	)

	if err != nil {
		return nil, err
	}

	if conversation == nil {
		return nil, errors.New(
			"conversation not found",
		)
	}

	isMember, err := s.memberRepo.IsMember(
		conversationID,
		userID,
	)

	if err != nil {
		return nil, err
	}

	if !isMember {
		return nil, errors.New(
			"you are not a member of this conversation",
		)
	}

	return conversation, nil
}

// ============================================================
// GET MESSAGES
//
// Retrieves messages after checking membership.
//
// This keeps the security rule in the service layer.
// ============================================================

func (s *ChatService) GetMessages(
	conversationID,
	userID int,
) ([]models.ChatMessage, error) {

	isMember, err := s.memberRepo.IsMember(
		conversationID,
		userID,
	)

	if err != nil {
		return nil, err
	}

	if !isMember {
		return nil, errors.New(
			"you are not a member of this conversation",
		)
	}

	return s.messageRepo.ListByConversation(
		conversationID,
	)
}

// ============================================================
// GET MEMBERS
//
// Returns the members of a conversation.
//
// The user requesting the list must already belong to the
// conversation.
// ============================================================

func (s *ChatService) GetMembers(
	conversationID,
	userID int,
) ([]models.ConversationMember, error) {

	isMember, err := s.memberRepo.IsMember(
		conversationID,
		userID,
	)

	if err != nil {
		return nil, err
	}

	if !isMember {
		return nil, errors.New(
			"you are not a member of this conversation",
		)
	}

	return s.memberRepo.ListMembers(
		conversationID,
	)
}

// ============================================================
// DELETE MESSAGE
//
// Only the person who sent the message can delete it.
//
// This is why we first:
//
// 1. Check conversation membership.
// 2. Find the message.
// 3. Check sender.
// 4. Delete it.
// ============================================================

func (s *ChatService) DeleteMessage(
	messageID,
	userID int,
) error {

	message, err := s.messageRepo.GetByID(
		messageID,
	)

	if err != nil {
		return err
	}

	if message == nil {
		return errors.New(
			"message not found",
		)
	}

	if message.SenderID != userID {
		return errors.New(
			"you can only delete your own messages",
		)
	}

	return s.messageRepo.Delete(
		messageID,
	)
}
