package handlers

import (
	"encoding/json"
	"html/template"
	"log"
	"net/http"
	"strconv"
	"strings"

	"backend/internal/models"
	"backend/internal/repository"
	"backend/internal/services"
	"backend/middleware"
)

// ============================================================
// CHAT HANDLER
//
// Responsibility:
// - Handles Agro-Shield general chat.
// - Displays the user's conversations.
// - Opens private/group conversations.
// - Sends messages.
// - Creates groups.
// - Adds members.
// - Removes members.
// - Allows members to leave groups.
// - Deletes messages.
//
// IMPORTANT:
//
// This handler is DIFFERENT from NegotiationHandler.
//
// NegotiationHandler:
//     Buyer ↔ Seller
//     Price offers
//     Accept / Reject
//
// ChatHandler:
//     User ↔ User
//     User ↔ Group
//     Normal communication
// ============================================================

type ChatHandler struct {
	service *services.ChatService
	farmers *repository.FarmerRepository
}

// ============================================================
// CREATE CHAT HANDLER
// ============================================================

func NewChatHandler(
	service *services.ChatService,
	farmers *repository.FarmerRepository,
) *ChatHandler {

	return &ChatHandler{
		service: service,
		farmers: farmers,
	}
}

// ============================================================
// CHAT LIST PAGE DATA
//
// Used by:
//
// GET /chat
//
// Shows all conversations belonging to the logged-in user.
// ============================================================

type ChatListPageData struct {
	Farmer        interface{}
	Conversations []models.Conversation
	Users         []models.Farmer
	Query         string
	Error         string
}

type chatPresenceResponse struct {
	OnlineUserIDs []int `json:"online_user_ids"`
}

// PresenceHandler returns recently active chat users for client-side updates.
func (h *ChatHandler) PresenceHandler(w http.ResponseWriter, r *http.Request) {
	farmer, ok := middleware.FarmerFromContext(r)
	if !ok || farmer == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	middleware.MarkFarmerOnline(farmer.ID)
	users, err := h.farmers.ListForChat(farmer.ID, "")
	if err != nil {
		http.Error(w, "Could not load presence", http.StatusInternalServerError)
		return
	}

	onlineIDs := make([]int, 0)
	for _, user := range users {
		if middleware.IsFarmerOnline(user.ID) {
			onlineIDs = append(onlineIDs, user.ID)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	if err := json.NewEncoder(w).Encode(chatPresenceResponse{OnlineUserIDs: onlineIDs}); err != nil {
		log.Println("presence response error:", err)
	}
}

// ============================================================
// CHAT THREAD PAGE DATA
//
// Used by:
//
// GET /chat/view?id=123
//
// Contains the conversation and its messages.
// ============================================================

type ChatThreadPageData struct {
	Farmer       interface{}
	Conversation *models.Conversation
	Messages     []models.ChatMessage
	Members      []models.ConversationMember
	Error        string
}

// ============================================================
// CHAT HOME
//
// GET /chat
//
// Shows the user's conversations.
//
// Example:
//
// My Chats
//
// 🌽 Maize Farmers Association
// 🛠️ Farm Tools Sellers
// 👤 John
// ============================================================

func (h *ChatHandler) ListHandler(
	w http.ResponseWriter,
	r *http.Request,
) {

	farmer, ok := middleware.FarmerFromContext(r)

	if !ok || farmer == nil {
		http.Redirect(
			w,
			r,
			"/login",
			http.StatusSeeOther,
		)
		return
	}

	conversations, err := h.service.MyConversations(
		farmer.ID,
	)

	if err != nil {
		log.Println(
			"failed to load conversations:",
			err,
		)

		h.renderList(
			w,
			farmer,
			nil,
			err.Error(),
		)
		return
	}
	users, usersErr := h.farmers.ListForChat(farmer.ID, r.URL.Query().Get("q"))
	if usersErr != nil {
		h.renderList(w, farmer, conversations, usersErr.Error())
		return
	}

	data := ChatListPageData{
		Farmer:        farmer,
		Conversations: conversations,
		Users:         users,
		Query:         r.URL.Query().Get("q"),
	}

	if err := h.renderTemplate(
		w,
		"chat.html",
		data,
	); err != nil {

		log.Println(
			"chat render error:",
			err,
		)

		http.Error(
			w,
			"Internal Server Error",
			http.StatusInternalServerError,
		)
	}
}

// ============================================================
// VIEW CONVERSATION
//
// GET /chat/view?id=123
//
// Shows:
//
// - Conversation
// - Messages
// - Members
//
// The service checks that the user belongs to the conversation.
// ============================================================

func (h *ChatHandler) ViewHandler(
	w http.ResponseWriter,
	r *http.Request,
) {

	farmer, ok := middleware.FarmerFromContext(r)

	if !ok || farmer == nil {
		http.Redirect(
			w,
			r,
			"/login",
			http.StatusSeeOther,
		)
		return
	}

	conversationID, err := strconv.Atoi(
		r.URL.Query().Get("id"),
	)

	if err != nil || conversationID <= 0 {

		http.Error(
			w,
			"Invalid conversation ID",
			http.StatusBadRequest,
		)

		return
	}

	conversation, err := h.service.GetConversation(
		conversationID,
		farmer.ID,
	)

	if err != nil {
		http.Error(
			w,
			err.Error(),
			http.StatusForbidden,
		)
		return
	}

	messages, err := h.service.GetMessages(
		conversationID,
		farmer.ID,
	)

	if err != nil {
		log.Println(
			"failed to load chat messages:",
			err,
		)

		http.Error(
			w,
			"Failed to load messages",
			http.StatusInternalServerError,
		)

		return
	}

	members, err := h.service.GetMembers(
		conversationID,
		farmer.ID,
	)

	if err != nil {
		log.Println(
			"failed to load conversation members:",
			err,
		)

		http.Error(
			w,
			"Failed to load members",
			http.StatusInternalServerError,
		)

		return
	}

	data := ChatThreadPageData{
		Farmer:       farmer,
		Conversation: conversation,
		Messages:     messages,
		Members:      members,
	}

	if err := h.renderTemplate(
		w,
		"chat_thread.html",
		data,
	); err != nil {

		log.Println(
			"chat thread render error:",
			err,
		)

		http.Error(
			w,
			"Internal Server Error",
			http.StatusInternalServerError,
		)
	}
}

// ============================================================
// SEND MESSAGE
//
// POST /chat/send
//
// Form:
//
// conversation_id
// message
// ============================================================

func (h *ChatHandler) SendMessageHandler(
	w http.ResponseWriter,
	r *http.Request,
) {

	farmer, ok := middleware.FarmerFromContext(r)

	if !ok || farmer == nil {
		http.Redirect(
			w,
			r,
			"/login",
			http.StatusSeeOther,
		)
		return
	}

	if r.Method != http.MethodPost {
		http.Error(
			w,
			"Method Not Allowed",
			http.StatusMethodNotAllowed,
		)
		return
	}

	conversationID, err := strconv.Atoi(
		r.FormValue("conversation_id"),
	)

	if err != nil || conversationID <= 0 {

		http.Error(
			w,
			"Invalid conversation ID",
			http.StatusBadRequest,
		)

		return
	}

	message := strings.TrimSpace(
		r.FormValue("message"),
	)

	if message == "" {
		http.Redirect(
			w,
			r,
			"/chat/view?id="+strconv.Itoa(conversationID),
			http.StatusSeeOther,
		)
		return
	}

	_, err = h.service.SendMessage(
		conversationID,
		farmer.ID,
		message,
	)

	if err != nil {

		log.Println(
			"failed to send chat message:",
			err,
		)

		http.Redirect(
			w,
			r,
			"/chat/view?id="+strconv.Itoa(conversationID)+
				"&error="+
				err.Error(),
			http.StatusSeeOther,
		)

		return
	}

	http.Redirect(
		w,
		r,
		"/chat/view?id="+strconv.Itoa(conversationID),
		http.StatusSeeOther,
	)
}

// ============================================================
// CREATE GROUP
//
// POST /chat/group/create
//
// Form:
//
// name
//
// Example:
//
// "Maize Farmers Association"
// ============================================================

func (h *ChatHandler) CreateGroupHandler(
	w http.ResponseWriter,
	r *http.Request,
) {

	farmer, ok := middleware.FarmerFromContext(r)

	if !ok || farmer == nil {
		http.Redirect(
			w,
			r,
			"/login",
			http.StatusSeeOther,
		)
		return
	}

	if r.Method != http.MethodPost {
		http.Error(
			w,
			"Method Not Allowed",
			http.StatusMethodNotAllowed,
		)
		return
	}

	name := strings.TrimSpace(
		r.FormValue("name"),
	)

	conversation, err := h.service.CreateGroup(
		farmer.ID,
		name,
	)

	if err != nil {

		log.Println(
			"failed to create group:",
			err,
		)

		h.renderList(
			w,
			farmer,
			nil,
			err.Error(),
		)

		return
	}

	http.Redirect(
		w,
		r,
		"/chat/view?id="+strconv.Itoa(
			conversation.ID,
		),
		http.StatusSeeOther,
	)
}

// ============================================================
// CREATE PRIVATE CHAT
//
// POST /chat/private
//
// Form:
//
// user_id
//
// Example:
//
// James clicks:
//
// "Chat with John"
//
// The handler creates:
//
// James
// John
// ============================================================

func (h *ChatHandler) CreatePrivateChatHandler(
	w http.ResponseWriter,
	r *http.Request,
) {

	farmer, ok := middleware.FarmerFromContext(r)

	if !ok || farmer == nil {
		http.Redirect(
			w,
			r,
			"/login",
			http.StatusSeeOther,
		)
		return
	}

	if r.Method != http.MethodPost {
		http.Error(
			w,
			"Method Not Allowed",
			http.StatusMethodNotAllowed,
		)
		return
	}

	otherUserID, err := strconv.Atoi(
		r.FormValue("user_id"),
	)

	if err != nil || otherUserID <= 0 {

		http.Error(
			w,
			"Invalid user ID",
			http.StatusBadRequest,
		)

		return
	}

	conversation, err := h.service.CreatePrivateChat(
		farmer.ID,
		otherUserID,
	)

	if err != nil {

		log.Println(
			"failed to create private chat:",
			err,
		)

		http.Error(
			w,
			err.Error(),
			http.StatusBadRequest,
		)

		return
	}

	http.Redirect(
		w,
		r,
		"/chat/view?id="+strconv.Itoa(
			conversation.ID,
		),
		http.StatusSeeOther,
	)
}

// ============================================================
// ADD MEMBER
//
// POST /chat/member/add
//
// Form:
//
// conversation_id
// user_id
//
// IMPORTANT:
//
// The service checks that:
//
// 1. Conversation exists.
// 2. Conversation is a group.
// 3. Requester is already a member.
// 4. New user isn't already a member.
// ============================================================

func (h *ChatHandler) AddMemberHandler(
	w http.ResponseWriter,
	r *http.Request,
) {

	farmer, ok := middleware.FarmerFromContext(r)

	if !ok || farmer == nil {
		http.Redirect(
			w,
			r,
			"/login",
			http.StatusSeeOther,
		)
		return
	}

	if r.Method != http.MethodPost {
		http.Error(
			w,
			"Method Not Allowed",
			http.StatusMethodNotAllowed,
		)
		return
	}

	conversationID, err := strconv.Atoi(
		r.FormValue("conversation_id"),
	)

	if err != nil || conversationID <= 0 {
		http.Error(
			w,
			"Invalid conversation ID",
			http.StatusBadRequest,
		)
		return
	}

	userID, err := strconv.Atoi(
		r.FormValue("user_id"),
	)

	if err != nil || userID <= 0 {
		http.Error(
			w,
			"Invalid user ID",
			http.StatusBadRequest,
		)
		return
	}

	err = h.service.AddMember(
		conversationID,
		farmer.ID,
		userID,
	)

	if err != nil {

		log.Println(
			"failed to add member:",
			err,
		)

		http.Error(
			w,
			err.Error(),
			http.StatusBadRequest,
		)

		return
	}

	http.Redirect(
		w,
		r,
		"/chat/view?id="+strconv.Itoa(conversationID),
		http.StatusSeeOther,
	)
}

// ============================================================
// REMOVE MEMBER
//
// POST /chat/member/remove
//
// Only the group creator can remove another member.
// ============================================================

func (h *ChatHandler) RemoveMemberHandler(
	w http.ResponseWriter,
	r *http.Request,
) {

	farmer, ok := middleware.FarmerFromContext(r)

	if !ok || farmer == nil {
		http.Redirect(
			w,
			r,
			"/login",
			http.StatusSeeOther,
		)
		return
	}

	if r.Method != http.MethodPost {
		http.Error(
			w,
			"Method Not Allowed",
			http.StatusMethodNotAllowed,
		)
		return
	}

	conversationID, err := strconv.Atoi(
		r.FormValue("conversation_id"),
	)

	if err != nil || conversationID <= 0 {
		http.Error(
			w,
			"Invalid conversation ID",
			http.StatusBadRequest,
		)
		return
	}

	userID, err := strconv.Atoi(
		r.FormValue("user_id"),
	)

	if err != nil || userID <= 0 {
		http.Error(
			w,
			"Invalid user ID",
			http.StatusBadRequest,
		)
		return
	}

	err = h.service.RemoveMember(
		conversationID,
		farmer.ID,
		userID,
	)

	if err != nil {

		log.Println(
			"failed to remove member:",
			err,
		)

		http.Error(
			w,
			err.Error(),
			http.StatusBadRequest,
		)

		return
	}

	http.Redirect(
		w,
		r,
		"/chat/view?id="+strconv.Itoa(conversationID),
		http.StatusSeeOther,
	)
}

// ============================================================
// LEAVE GROUP
//
// POST /chat/leave
//
// Normal members can leave.
//
// The creator cannot leave until ownership transfer is added.
// ============================================================

func (h *ChatHandler) LeaveGroupHandler(
	w http.ResponseWriter,
	r *http.Request,
) {

	farmer, ok := middleware.FarmerFromContext(r)

	if !ok || farmer == nil {
		http.Redirect(
			w,
			r,
			"/login",
			http.StatusSeeOther,
		)
		return
	}

	if r.Method != http.MethodPost {
		http.Error(
			w,
			"Method Not Allowed",
			http.StatusMethodNotAllowed,
		)
		return
	}

	conversationID, err := strconv.Atoi(
		r.FormValue("conversation_id"),
	)

	if err != nil || conversationID <= 0 {
		http.Error(
			w,
			"Invalid conversation ID",
			http.StatusBadRequest,
		)
		return
	}

	err = h.service.LeaveGroup(
		conversationID,
		farmer.ID,
	)

	if err != nil {

		log.Println(
			"failed to leave group:",
			err,
		)

		http.Error(
			w,
			err.Error(),
			http.StatusBadRequest,
		)

		return
	}

	http.Redirect(
		w,
		r,
		"/chat",
		http.StatusSeeOther,
	)
}

// ============================================================
// DELETE MESSAGE
//
// POST /chat/message/delete
//
// Only the sender can delete their own message.
// ============================================================

func (h *ChatHandler) DeleteMessageHandler(
	w http.ResponseWriter,
	r *http.Request,
) {

	farmer, ok := middleware.FarmerFromContext(r)

	if !ok || farmer == nil {
		http.Redirect(
			w,
			r,
			"/login",
			http.StatusSeeOther,
		)
		return
	}

	if r.Method != http.MethodPost {
		http.Error(
			w,
			"Method Not Allowed",
			http.StatusMethodNotAllowed,
		)
		return
	}

	messageID, err := strconv.Atoi(
		r.FormValue("message_id"),
	)

	if err != nil || messageID <= 0 {
		http.Error(
			w,
			"Invalid message ID",
			http.StatusBadRequest,
		)
		return
	}

	err = h.service.DeleteMessage(
		messageID,
		farmer.ID,
	)

	if err != nil {

		log.Println(
			"failed to delete message:",
			err,
		)

		http.Error(
			w,
			err.Error(),
			http.StatusBadRequest,
		)

		return
	}

	conversationID := r.FormValue(
		"conversation_id",
	)

	if conversationID == "" {
		http.Redirect(
			w,
			r,
			"/chat",
			http.StatusSeeOther,
		)
		return
	}

	http.Redirect(
		w,
		r,
		"/chat/view?id="+conversationID,
		http.StatusSeeOther,
	)
}

// ============================================================
// RENDER CHAT LIST
// ============================================================

func (h *ChatHandler) renderList(
	w http.ResponseWriter,
	farmer interface{},
	conversations []models.Conversation,
	errorMessage string,
) {

	data := ChatListPageData{
		Farmer:        farmer,
		Conversations: conversations,
		Error:         errorMessage,
	}

	if err := h.renderTemplate(
		w,
		"chat.html",
		data,
	); err != nil {

		log.Println(
			"chat list render error:",
			err,
		)

		http.Error(
			w,
			"Internal Server Error",
			http.StatusInternalServerError,
		)
	}
}

// ============================================================
// TEMPLATE RENDERER
//
// We use ParseFiles here because this matches the existing
// Agro-Shield frontend structure:
//
// ../frontend/pages/
// ============================================================

func (h *ChatHandler) renderTemplate(
	w http.ResponseWriter,
	templateName string,
	data interface{},
) error {

	tmpl, err := template.ParseFiles(
		"../frontend/pages/" + templateName,
	)

	if err != nil {
		return err
	}

	return tmpl.Execute(
		w,
		data,
	)
}
