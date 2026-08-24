package handlers

import (
	"backend/internal/services"
	"backend/middleware"
	"html/template"
	"net/http"
	"strconv"
)

// ============================================================
// NOTIFICATION HANDLER
//
// Responsibility:
// - Receives notification-related HTTP requests.
// - Gets the currently logged-in farmer/buyer.
// - Calls NotificationService.
// - Sends notification data to the frontend.
//
// The handler does NOT communicate directly with PostgreSQL.
// ============================================================

type NotificationHandler struct {
	notificationService *services.NotificationService
}

// ============================================================
// CREATE NOTIFICATION HANDLER
// ============================================================
//
// Connects the NotificationService to the handler.
//

func NewNotificationHandler(
	notificationService *services.NotificationService,
) *NotificationHandler {

	return &NotificationHandler{
		notificationService: notificationService,
	}
}

// ============================================================
// NOTIFICATIONS PAGE
//
// GET /notifications
//
// Displays all notifications belonging to the currently
// logged-in user.
// ============================================================

func (h *NotificationHandler) NotificationsHandler(
	w http.ResponseWriter,
	r *http.Request,
) {

	// --------------------------------------------------------
	// Get the logged-in farmer/buyer.
	//
	// RequireAuth middleware attaches the authenticated farmer
	// to the request context.
	// --------------------------------------------------------

	farmer, ok := middleware.FarmerFromContext(r)

	if !ok || farmer == nil {
		http.Error(
			w,
			"Unauthorized",
			http.StatusUnauthorized,
		)
		return
	}

	// --------------------------------------------------------
	// Get all notifications belonging to this user.
	// --------------------------------------------------------

	notifications, err := h.notificationService.GetUserNotifications(
		farmer.ID,
	)

	if err != nil {
		http.Error(
			w,
			"Failed to load notifications",
			http.StatusInternalServerError,
		)
		return
	}

	// --------------------------------------------------------
	// Get the number of unread notifications.
	//
	// This will later be displayed as the 🔔 badge.
	// --------------------------------------------------------

	unreadCount, err := h.notificationService.GetUnreadCount(
		farmer.ID,
	)

	if err != nil {
		http.Error(
			w,
			"Failed to load notification count",
			http.StatusInternalServerError,
		)
		return
	}

	// --------------------------------------------------------
	// Data sent to the notifications HTML template.
	// --------------------------------------------------------

	data := struct {
		Farmer        interface{}
		Notifications interface{}
		UnreadCount   int
	}{
		Farmer:        farmer,
		Notifications: notifications,
		UnreadCount:   unreadCount,
	}

	// --------------------------------------------------------
	// Load notification page.
	// --------------------------------------------------------

	tmpl, err := template.ParseFiles(
		"../frontend/pages/notification.html",
	)

	if err != nil {
		http.Error(
			w,
			"Failed to load notifications page",
			http.StatusInternalServerError,
		)
		return
	}

	// --------------------------------------------------------
	// Render notification page.
	// --------------------------------------------------------

	err = tmpl.Execute(w, data)

	if err != nil {
		http.Error(
			w,
			"Failed to render notifications page",
			http.StatusInternalServerError,
		)
		return
	}
}

// ============================================================
// MARK ONE NOTIFICATION AS READ
//
// POST /notifications/read?id=123
//
// Only the owner of the notification can mark it as read.
// ============================================================

func (h *NotificationHandler) MarkAsReadHandler(
	w http.ResponseWriter,
	r *http.Request,
) {

	// --------------------------------------------------------
	// Get logged-in user.
	// --------------------------------------------------------

	farmer, ok := middleware.FarmerFromContext(r)

	if !ok || farmer == nil {
		http.Error(
			w,
			"Unauthorized",
			http.StatusUnauthorized,
		)
		return
	}

	// --------------------------------------------------------
	// Get notification ID from URL.
	//
	// Example:
	//
	// /notifications/read?id=15
	// --------------------------------------------------------

	notificationIDString := r.URL.Query().Get("id")

	notificationID, err := strconv.Atoi(notificationIDString)

	if err != nil {
		http.Error(
			w,
			"Invalid notification ID",
			http.StatusBadRequest,
		)
		return
	}

	// --------------------------------------------------------
	// Mark notification as read.
	// --------------------------------------------------------

	err = h.notificationService.MarkAsRead(
		notificationID,
		farmer.ID,
	)

	if err != nil {
		http.Error(
			w,
			"Failed to mark notification as read",
			http.StatusInternalServerError,
		)
		return
	}

	// --------------------------------------------------------
	// Return to notifications page.
	// --------------------------------------------------------

	http.Redirect(
		w,
		r,
		"/notifications",
		http.StatusSeeOther,
	)
}

// ============================================================
// MARK ALL NOTIFICATIONS AS READ
//
// POST /notifications/read-all
// ============================================================

func (h *NotificationHandler) MarkAllAsReadHandler(
	w http.ResponseWriter,
	r *http.Request,
) {

	// --------------------------------------------------------
	// Get logged-in user.
	// --------------------------------------------------------

	farmer, ok := middleware.FarmerFromContext(r)

	if !ok || farmer == nil {
		http.Error(
			w,
			"Unauthorized",
			http.StatusUnauthorized,
		)
		return
	}

	// --------------------------------------------------------
	// Mark all notifications belonging to this user as read.
	// --------------------------------------------------------

	err := h.notificationService.MarkAllAsRead(
		farmer.ID,
	)

	if err != nil {
		http.Error(
			w,
			"Failed to mark notifications as read",
			http.StatusInternalServerError,
		)
		return
	}

	// --------------------------------------------------------
	// Return to notifications page.
	// --------------------------------------------------------

	http.Redirect(
		w,
		r,
		"/notifications",
		http.StatusSeeOther,
	)
}
