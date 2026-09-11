package routes

import (
	"backend/handlers"
	app "backend/internal"
	"backend/internal/services"
	"backend/middleware"
	"backend/ussd"
	"net/http"
)

func RegisterRoutes(container *app.Container) {
	// USSD gateways post menu selections to this provider-neutral endpoint.
	ussdHandler := ussd.NewHandler(container.FarmerRepo, container.Crop, services.NewWeatherService(), container.Order, container.Notification)
	http.Handle("/ussd", ussdHandler)

	// ========================================================
	// STATIC FILES
	// ========================================================

	http.Handle(
		"/static/",
		http.StripPrefix(
			"/static/",
			http.FileServer(http.Dir("../frontend")),
		),
	)

	// ========================================================
	// HOME
	// ========================================================

	http.HandleFunc(
		"/",
		middleware.OnlyPath(
			"/",
			middleware.OnlyGet(
				handlers.IndexHandler,
			),
		),
	)

	// ========================================================
	// AUTHENTICATION
	// ========================================================

	registerHandler := handlers.NewRegisterHandler(
		container.Auth,
	)

	http.HandleFunc(
		"/register",
		middleware.OnlyPath(
			"/register",
			registerHandler.RegisterHandler,
		),
	)

	loginHandler := handlers.NewLoginHandler(
		container.Auth,
	)

	http.HandleFunc(
		"/login",
		middleware.OnlyPath(
			"/login",
			loginHandler.LoginHandler,
		),
	)

	http.HandleFunc(
		"/logout",
		middleware.OnlyPath(
			"/logout",
			middleware.OnlyGet(
				handlers.LogoutHandler,
			),
		),
	)

	forgotPasswordHandler := handlers.NewForgotPasswordHandler(
		container.Auth,
	)

	http.HandleFunc(
		"/forgot-password",
		middleware.OnlyPath(
			"/forgot-password",
			forgotPasswordHandler.Handler,
		),
	)

	completeProfileHandler := handlers.NewCompleteProfileHandler(
		container.Auth,
	)

	http.HandleFunc(
		"/complete-profile",
		middleware.OnlyPath(
			"/complete-profile",
			middleware.RequireAuth(
				container.FarmerRepo,
				completeProfileHandler.Handler,
			),
		),
	)
	// ========================================================
	// DASHBOARD
	// ========================================================

	dashboardHandler := handlers.NewDashboardHandler(
		container.Crop,
		container.Order,
		container.Cart,
		container.AI,
		container.Negotiation,
		services.NewWeatherService(),
		container.Notification,
		container.MarketEvents,
	)

	http.HandleFunc(
		"/dashboard",
		middleware.OnlyPath(
			"/dashboard",
			middleware.OnlyGet(
				middleware.RequireAuth(
					container.FarmerRepo,
					dashboardHandler.DashBoard,
				),
			),
		),
	)

	http.HandleFunc(
		"/market-insights",
		middleware.OnlyPath(
			"/market-insights",
			middleware.OnlyGet(middleware.RequireAuth(container.FarmerRepo, dashboardHandler.MarketInsights)),
		),
	)

	// ========================================================
	// PROFILE
	// ========================================================

	profileHandler := handlers.NewProfileHandler(
		container.Crop,
		container.Order,
		container.FarmerRepo,
	)

	http.HandleFunc(
		"/profile",
		middleware.OnlyPath(
			"/profile",
			middleware.OnlyGet(
				middleware.RequireAuth(
					container.FarmerRepo,
					profileHandler.ProfileHandler,
				),
			),
		),
	)

	profileEditHandler := handlers.NewProfileEditHandler(
		container.Auth,
	)

	http.HandleFunc(
		"/profile/edit",
		middleware.OnlyPath(
			"/profile/edit",
			middleware.RequireAuth(
				container.FarmerRepo,
				profileEditHandler.Handler,
			),
		),
	)

	// ========================================================
	// STORAGE
	// ========================================================

	storageHandler := handlers.NewStorageHandler(
		container.Crop,
	)
	salesHandler := handlers.NewSalesHandler(container.Order)
	http.HandleFunc("/sales", middleware.OnlyPath("/sales", middleware.RequireAuth(container.FarmerRepo, salesHandler.Handler)))

	http.HandleFunc(
		"/storage",
		middleware.OnlyPath(
			"/storage",
			middleware.RequireAuth(
				container.FarmerRepo,
				storageHandler.StorageHandler,
			),
		),
	)

	// ========================================================
	// MARKETPLACE
	// ========================================================

	marketplaceHandler := handlers.NewMarketplaceHandler(
		container.Crop,
		container.Order,
		container.MarketEvents,
	)

	http.HandleFunc(
		"/marketplace",
		middleware.OnlyPath(
			"/marketplace",
			middleware.RequireAuth(
				container.FarmerRepo,
				marketplaceHandler.MarketplaceHandler,
			),
		),
	)

	// ========================================================
	// FEEDBACK
	// ========================================================

	feedbackHandler := handlers.NewFeedbackHandler(
		container.Feedback,
	)

	http.HandleFunc(
		"/feedback",
		middleware.OnlyPath(
			"/feedback",
			middleware.RequireAuth(
				container.FarmerRepo,
				feedbackHandler.CreateFeedback,
			),
		),
	)

	// ========================================================
	// PRODUCT DETAILS
	// ========================================================

	productHandler := handlers.NewProductHandler(
		container.Crop,
		container.MarketEvents,
	)

	http.HandleFunc(
		"/product",
		middleware.OnlyPath(
			"/product",
			middleware.RequireAuth(
				container.FarmerRepo,
				productHandler.ProductDetailsHandler,
			),
		),
	)

	// ========================================================
	// CART
	// ========================================================

	cartHandler := handlers.NewCartHandler(
		container.Cart,
		container.MarketEvents,
	)

	http.HandleFunc(
		"/cart",
		middleware.OnlyPath(
			"/cart",
			middleware.RequireAuth(
				container.FarmerRepo,
				cartHandler.Handler,
			),
		),
	)

	// ========================================================
	// NOTIFICATIONS
	// ========================================================

	notificationHandler := handlers.NewNotificationHandler(
		container.Notification,
	)

	// View notifications
	//
	// GET /notifications

	http.HandleFunc(
		"/notifications",
		middleware.OnlyPath(
			"/notifications",
			middleware.RequireAuth(
				container.FarmerRepo,
				notificationHandler.NotificationsHandler,
			),
		),
	)

	// Mark one notification as read
	//
	// POST /notifications/read?id=123

	http.HandleFunc(
		"/notifications/read",
		middleware.OnlyPath(
			"/notifications/read",
			middleware.RequireAuth(
				container.FarmerRepo,
				notificationHandler.MarkAsReadHandler,
			),
		),
	)

	// Mark all notifications as read
	//
	// POST /notifications/read-all

	http.HandleFunc(
		"/notifications/read-all",
		middleware.OnlyPath(
			"/notifications/read-all",
			middleware.RequireAuth(
				container.FarmerRepo,
				notificationHandler.MarkAllAsReadHandler,
			),
		),
	)

	// ========================================================
	// AI ASSISTANT
	// ========================================================

	aiHandler := handlers.NewAIAssistantHandler(
		container.AI,
	)

	diagnosisHistoryHandler := handlers.NewAIDiagnosisHistoryHandler(
		container.AI,
	)

	http.HandleFunc(
		"/ai-assistant",
		middleware.OnlyPath(
			"/ai-assistant",
			middleware.RequireAuth(
				container.FarmerRepo,
				aiHandler.Handler,
			),
		),
	)

	http.HandleFunc(
		"/ai-diagnosis-history",
		middleware.OnlyPath(
			"/ai-diagnosis-history",
			middleware.RequireAuth(
				container.FarmerRepo,
				diagnosisHistoryHandler.Handler,
			),
		),
	)

	// ========================================================
	// NEGOTIATIONS
	// ========================================================

	negotiationHandler := handlers.NewNegotiationHandler(
		container.Negotiation,
		container.MarketEvents,
	)

	// Negotiation list
	//
	// GET /negotiations

	http.HandleFunc(
		"/negotiations",
		middleware.OnlyPath(
			"/negotiations",
			middleware.OnlyGet(
				middleware.RequireAuth(
					container.FarmerRepo,
					negotiationHandler.ListHandler,
				),
			),
		),
	)

	// Negotiation conversation
	//
	// GET /negotiation?id=123
	//
	// The handler also handles POST actions.

	http.HandleFunc(
		"/negotiation",
		middleware.OnlyPath(
			"/negotiation",
			middleware.RequireAuth(
				container.FarmerRepo,
				negotiationHandler.ThreadHandler,
			),
		),
	)

	// Start negotiation
	//
	// POST /negotiation/start

	http.HandleFunc(
		"/negotiation/start",
		middleware.OnlyPath(
			"/negotiation/start",
			middleware.RequireAuth(
				container.FarmerRepo,
				negotiationHandler.StartHandler,
			),
		),
	)

	// ========================================================
	// CHAT
	// ========================================================
	//
	// General Agro-Shield communication.
	//
	// Chat is different from negotiation.
	//
	// Negotiation:
	//
	// Buyer ↔ Seller
	// Price negotiation
	// Offers
	// Accept / Reject
	//
	// Chat:
	//
	// User ↔ User
	// User ↔ Group
	// Normal communication
	//
	// Examples:
	//
	// 🌽 Maize Farmers Association
	// 🛠️ Farm Tools Sellers
	// 👨🏽‍🌾 Benue Farmers
	// 👤 Private conversation with another user
	//
	// ========================================================

	chatHandler := handlers.NewChatHandler(
		container.Chat,
		container.FarmerRepo,
	)

	// --------------------------------------------------------
	// CHAT HOME
	//
	// GET /chat
	//
	// Shows all conversations belonging to the user.
	// --------------------------------------------------------

	http.HandleFunc(
		"/chat",
		middleware.OnlyPath(
			"/chat",
			middleware.OnlyGet(
				middleware.RequireAuth(
					container.FarmerRepo,
					chatHandler.ListHandler,
				),
			),
		),
	)

	// --------------------------------------------------------
	// VIEW CHAT
	//
	// GET /chat/view?id=123
	//
	// Displays:
	//
	// - Conversation
	// - Messages
	// - Members
	// --------------------------------------------------------

	http.HandleFunc(
		"/chat/view",
		middleware.OnlyPath(
			"/chat/view",
			middleware.OnlyGet(
				middleware.RequireAuth(
					container.FarmerRepo,
					chatHandler.ViewHandler,
				),
			),
		),
	)

	http.HandleFunc(
		"/chat/presence",
		middleware.OnlyPath(
			"/chat/presence",
			middleware.OnlyGet(
				middleware.RequireAuth(
					container.FarmerRepo,
					chatHandler.PresenceHandler,
				),
			),
		),
	)

	// --------------------------------------------------------
	// SEND MESSAGE
	//
	// POST /chat/send
	// --------------------------------------------------------

	http.HandleFunc(
		"/chat/send",
		middleware.OnlyPath(
			"/chat/send",
			middleware.RequireAuth(
				container.FarmerRepo,
				chatHandler.SendMessageHandler,
			),
		),
	)

	// --------------------------------------------------------
	// CREATE GROUP
	//
	// POST /chat/group/create
	//
	// Example:
	//
	// Maize Farmers Association
	//
	// --------------------------------------------------------

	http.HandleFunc(
		"/chat/group/create",
		middleware.OnlyPath(
			"/chat/group/create",
			middleware.RequireAuth(
				container.FarmerRepo,
				chatHandler.CreateGroupHandler,
			),
		),
	)

	// --------------------------------------------------------
	// CREATE PRIVATE CHAT
	//
	// POST /chat/private
	//
	// Form:
	//
	// user_id
	// --------------------------------------------------------

	http.HandleFunc(
		"/chat/private",
		middleware.OnlyPath(
			"/chat/private",
			middleware.RequireAuth(
				container.FarmerRepo,
				chatHandler.CreatePrivateChatHandler,
			),
		),
	)

	// --------------------------------------------------------
	// ADD MEMBER
	//
	// POST /chat/member/add
	// --------------------------------------------------------

	http.HandleFunc(
		"/chat/member/add",
		middleware.OnlyPath(
			"/chat/member/add",
			middleware.RequireAuth(
				container.FarmerRepo,
				chatHandler.AddMemberHandler,
			),
		),
	)

	// --------------------------------------------------------
	// REMOVE MEMBER
	//
	// POST /chat/member/remove
	// --------------------------------------------------------

	http.HandleFunc(
		"/chat/member/remove",
		middleware.OnlyPath(
			"/chat/member/remove",
			middleware.RequireAuth(
				container.FarmerRepo,
				chatHandler.RemoveMemberHandler,
			),
		),
	)

	// --------------------------------------------------------
	// LEAVE GROUP
	//
	// POST /chat/leave
	// --------------------------------------------------------

	http.HandleFunc(
		"/chat/leave",
		middleware.OnlyPath(
			"/chat/leave",
			middleware.RequireAuth(
				container.FarmerRepo,
				chatHandler.LeaveGroupHandler,
			),
		),
	)

	// --------------------------------------------------------
	// DELETE MESSAGE
	//
	// POST /chat/message/delete
	// --------------------------------------------------------

	http.HandleFunc(
		"/chat/message/delete",
		middleware.OnlyPath(
			"/chat/message/delete",
			middleware.RequireAuth(
				container.FarmerRepo,
				chatHandler.DeleteMessageHandler,
			),
		),
	)
	// ========================================================
	// ORDERS
	// ========================================================

	orderHandler := handlers.NewOrderHandler(container.Order)

	// Order list (buyer purchases / farmer sales)
	// GET /orders
	http.HandleFunc(
		"/orders",
		middleware.OnlyPath(
			"/orders",
			middleware.OnlyGet(
				middleware.RequireAuth(
					container.FarmerRepo,
					orderHandler.List,
				),
			),
		),
	)

	// Single order detail
	// GET /order?id=123
	http.HandleFunc(
		"/order",
		middleware.OnlyPath(
			"/order",
			middleware.OnlyGet(
				middleware.RequireAuth(
					container.FarmerRepo,
					orderHandler.Detail,
				),
			),
		),
	)

	// Place a new order (marketplace "Buy" form)
	// POST /orders
	http.HandleFunc(
		"/orders/place",
		middleware.OnlyPath(
			"/orders/place",
			middleware.RequireAuth(
				container.FarmerRepo,
				orderHandler.Place,
			),
		),
	)

	// ========================================================
	// DELIVERIES
	// ========================================================

	deliveryHandler := handlers.NewDeliveryHandler(container.Delivery)

	// Delivery list
	// GET /deliveries
	http.HandleFunc(
		"/deliveries",
		middleware.OnlyPath(
			"/deliveries",
			middleware.OnlyGet(
				middleware.RequireAuth(
					container.FarmerRepo,
					deliveryHandler.List,
				),
			),
		),
	)

	// Single delivery detail
	// GET /delivery?id=123
	http.HandleFunc(
		"/delivery",
		middleware.OnlyPath(
			"/delivery",
			middleware.OnlyGet(
				middleware.RequireAuth(
					container.FarmerRepo,
					deliveryHandler.Detail,
				),
			),
		),
	)

	// Create delivery from an order
	// POST /deliveries/create
	http.HandleFunc(
		"/deliveries/create",
		middleware.OnlyPath(
			"/deliveries/create",
			middleware.RequireAuth(
				container.FarmerRepo,
				deliveryHandler.Create,
			),
		),
	)

	// Update delivery status
	// POST /deliveries/status
	http.HandleFunc(
		"/deliveries/status",
		middleware.OnlyPath(
			"/deliveries/status",
			middleware.RequireAuth(
				container.FarmerRepo,
				deliveryHandler.UpdateStatus,
			),
		),
	)

	// Update tracking info
	// POST /deliveries/tracking
	http.HandleFunc(
		"/deliveries/tracking",
		middleware.OnlyPath(
			"/deliveries/tracking",
			middleware.RequireAuth(
				container.FarmerRepo,
				deliveryHandler.UpdateTracking,
			),
		),
	)

	// Mark delivered
	// POST /deliveries/delivered
	http.HandleFunc(
		"/deliveries/delivered",
		middleware.OnlyPath(
			"/deliveries/delivered",
			middleware.RequireAuth(
				container.FarmerRepo,
				deliveryHandler.MarkDelivered,
			),
		),
	)

	// Mark failed
	// POST /deliveries/failed
	http.HandleFunc(
		"/deliveries/failed",
		middleware.OnlyPath(
			"/deliveries/failed",
			middleware.RequireAuth(
				container.FarmerRepo,
				deliveryHandler.MarkFailed,
			),
		),
	)

	// Cancel delivery
	// POST /deliveries/cancel
	http.HandleFunc(
		"/deliveries/cancel",
		middleware.OnlyPath(
			"/deliveries/cancel",
			middleware.RequireAuth(
				container.FarmerRepo,
				deliveryHandler.Cancel,
			),
		),
	)

	// ========================================================
	// PAYMENTS
	// ========================================================
	//
	// Checkout via Flutterwave (hosted checkout page, paid by
	// bank transfer or card).
	//
	// ========================================================

	paymentHandler := handlers.NewPaymentHandler(
		container.Payment,
	)

	// Initiate a payment for an order - requires the buyer to
	// be logged in.
	//
	// POST /orders/pay

	http.HandleFunc(
		"/orders/pay",
		middleware.OnlyPath(
			"/orders/pay",
			middleware.RequireAuth(
				container.FarmerRepo,
				paymentHandler.InitiatePayment,
			),
		),
	)

	// Customer is redirected here by Flutterwave after paying.
	// Not auth-gated - the customer's session may have expired
	// during checkout, and this only confirms status; it never
	// gives value on its own (the webhook below does that).
	//
	// GET /payments/callback

	http.HandleFunc(
		"/payments/callback",
		middleware.OnlyPath(
			"/payments/callback",
			paymentHandler.Callback,
		),
	)

	// Flutterwave calls this server-to-server. Not auth-gated -
	// it's verified via the verif-hash header instead, inside
	// PaymentService.ProcessWebhook.
	//
	// POST /webhooks/flutterwave

	http.HandleFunc(
		"/webhooks/flutterwave",
		middleware.OnlyPath(
			"/webhooks/flutterwave",
			paymentHandler.Webhook,
		),
	)
}
