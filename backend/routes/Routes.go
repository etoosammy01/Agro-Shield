package routes

import (
	"fmt"
	"html"
	"net/http"
	"strings"

	"backend/handlers"
	app "backend/internal"
	"backend/internal/services"
	"backend/middleware"
	"backend/ussd"
)

func RegisterRoutes(
	container *app.Container,
	welcomeEmail services.WelcomeEmailNotifier,
) {
	// USSD gateways post menu selections to this provider-neutral endpoint.
	ussdHandler := ussd.NewHandler(container.FarmerRepo, container.Crop, services.NewWeatherService(), container.Order, container.Notification)
	http.Handle("/ussd", ussdHandler)

	// ========================================================
	// STATIC FILES
	// ========================================================

	http.Handle(
		"/static/",
		middleware.StaticCache(
			middleware.NoDirectoryListing(
				http.StripPrefix(
					"/static/",
					http.FileServer(http.Dir("../frontend")),
				),
			),
		),
	)

	// Public crawl entry points. Only genuinely public pages are listed; no
	// authenticated route or private listing is exposed here.
	http.HandleFunc("/robots.txt", middleware.OnlyGet(robotsHandler))
	http.HandleFunc("/sitemap.xml", middleware.OnlyGet(sitemapHandler))

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

	learningHandler := handlers.NewLearningHandler(container.AI)
	http.HandleFunc(
		"/learning",
		middleware.OnlyPath(
			"/learning",
			middleware.OnlyGet(learningHandler.Page),
		),
	)
	http.HandleFunc(
		"/learning/chat",
		middleware.OnlyPath(
			"/learning/chat",
			learningHandler.Chat,
		),
	)

	locationDirectory := handlers.NewLocationDirectory(services.NewNigeriaLocationService())
	http.HandleFunc("/locations/nigeria/states", middleware.OnlyPath(
		"/locations/nigeria/states",
		locationDirectory.States,
	))
	http.HandleFunc("/locations/nigeria/lgas", middleware.OnlyPath(
		"/locations/nigeria/lgas",
		locationDirectory.LGAs,
	))

	// ========================================================
	// AUTHENTICATION
	// ========================================================

	registerHandler := handlers.NewRegisterHandler(
		container.Auth,
		welcomeEmail,
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
		container.OTP,
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
	verifyPhoneHandler := handlers.NewVerifyPhoneHandler(container.OTP)
	http.HandleFunc("/verify-phone", middleware.OnlyPath("/verify-phone",
		middleware.RequireAuth(container.FarmerRepo, verifyPhoneHandler.Handler)))

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
		container.Wallet,
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

	// Closing an account is POST-only, inside the handler, because it needs the
	// parsed confirmation fields to decide whether to act.
	http.HandleFunc(
		"/profile/delete",
		middleware.OnlyPath(
			"/profile/delete",
			middleware.RequireAuth(
				container.FarmerRepo,
				profileEditHandler.DeleteHandler,
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

	checkoutHandler := handlers.NewCheckoutHandler(container.Crop, container.Wallet, container.OTP)
	marketplaceHandler := handlers.NewMarketplaceHandler(container.Crop, checkoutHandler, container.MarketEvents, container.Notification)

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
				func(w http.ResponseWriter, r *http.Request) {
					switch r.Method {
					case http.MethodPost:
						feedbackHandler.CreateFeedback(w, r)

					case http.MethodGet:
						feedbackHandler.GetFeedbackByFarmer(w, r)

					default:
						http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
					}
				},
			),
		),
	)

	http.HandleFunc(
		"/feedback/rating",
		middleware.OnlyPath(
			"/feedback/rating",
			middleware.RequireAuth(
				container.FarmerRepo,
				feedbackHandler.GetFarmerRating,
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
	// TYPING STATE
	//
	// GET /chat/typing?conversation_id=123
	// POST /chat/typing
	//
	// The handler checks conversation membership for both
	// directions and only reports the name of another member.
	// --------------------------------------------------------

	http.HandleFunc(
		"/chat/typing",
		middleware.OnlyPath(
			"/chat/typing",
			middleware.RequireAuth(
				container.FarmerRepo,
				chatHandler.TypingHandler,
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

	orderHandler := handlers.NewOrderHandler(container.Order, checkoutHandler)

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
		container.OTP,
	)
	walletHandler := handlers.NewWalletHandler(container.Wallet, container.OTP, container.Notification)

	http.HandleFunc("/wallet", middleware.OnlyPath(
		"/wallet",
		middleware.OnlyGet(middleware.RequireAuth(container.FarmerRepo, walletHandler.Page)),
	))
	// Read-only snapshot for the open wallet page. It returns committed values
	// only, so the page can notice a confirmed credit without computing one.
	http.HandleFunc("/wallet/status", middleware.OnlyPath(
		"/wallet/status",
		middleware.OnlyGet(middleware.RequireAuth(container.FarmerRepo, walletHandler.Status)),
	))
	http.HandleFunc("/wallet/otp/send", middleware.OnlyPath(
		"/wallet/otp/send",
		middleware.RequireAuth(container.FarmerRepo, walletHandler.SendOTP),
	))
	http.HandleFunc("/wallet/deposit", middleware.OnlyPath(
		"/wallet/deposit",
		middleware.RequireAuth(container.FarmerRepo, walletHandler.Deposit),
	))
	// The permanent funding account. Setup is a POST because it carries a BVN or
	// NIN; the check is a POST as well so a link preview or refresh cannot ask
	// the provider anything.
	http.HandleFunc("/wallet/virtual-account", middleware.OnlyPath(
		"/wallet/virtual-account",
		middleware.RequireAuth(container.FarmerRepo, walletHandler.CreateVirtualAccount),
	))
	http.HandleFunc("/wallet/virtual-account/check", middleware.OnlyPath(
		"/wallet/virtual-account/check",
		middleware.RequireAuth(container.FarmerRepo, walletHandler.CheckVirtualAccount),
	))
	http.HandleFunc("/wallet/withdraw", middleware.OnlyPath(
		"/wallet/withdraw",
		middleware.RequireAuth(container.FarmerRepo, walletHandler.Withdraw),
	))
	http.HandleFunc("/wallet/transfer", middleware.OnlyPath(
		"/wallet/transfer",
		middleware.RequireAuth(container.FarmerRepo, walletHandler.Transfer),
	))
	http.HandleFunc("/wallet/transfer/recipient", middleware.OnlyPath(
		"/wallet/transfer/recipient",
		middleware.RequireAuth(container.FarmerRepo, walletHandler.LookupTransferRecipient),
	))
	// A user may release their own request while it is still pending. The
	// service refuses once an admin has started paying it, because the money may
	// already be on its way.
	http.HandleFunc("/wallet/withdraw/cancel", middleware.OnlyPath(
		"/wallet/withdraw/cancel",
		middleware.RequireAuth(container.FarmerRepo, walletHandler.CancelWithdrawal),
	))
	http.HandleFunc("/wallet/transfer/cancel", middleware.OnlyPath(
		"/wallet/transfer/cancel",
		middleware.RequireAuth(container.FarmerRepo, walletHandler.CancelTransfer),
	))
	http.HandleFunc("/wallet/bank", middleware.OnlyPath(
		"/wallet/bank",
		middleware.RequireAuth(container.FarmerRepo, walletHandler.SaveBankDetails),
	))
	http.HandleFunc("/wallet/banks", middleware.OnlyPath(
		"/wallet/banks",
		middleware.RequireAuth(container.FarmerRepo, walletHandler.Banks),
	))
	http.HandleFunc("/wallet/lookup-account", middleware.OnlyPath(
		"/wallet/lookup-account",
		middleware.RequireAuth(container.FarmerRepo, walletHandler.LookupAccount),
	))
	http.HandleFunc("/admin/wallet-withdrawals", middleware.OnlyPath(
		"/admin/wallet-withdrawals",
		middleware.OnlyGet(middleware.RequireAuth(container.FarmerRepo, walletHandler.AdminPage)),
	))
	// Admins are managed from the approvals page itself, so granting the role no
	// longer needs a database client or a restart.
	http.HandleFunc("/admin/wallet-admins", middleware.OnlyPath(
		"/admin/wallet-admins",
		middleware.RequireAuth(container.FarmerRepo, walletHandler.SetWalletAdmin),
	))
	http.HandleFunc("/admin/wallet-withdrawals/approve", middleware.OnlyPath(
		"/admin/wallet-withdrawals/approve",
		middleware.RequireAuth(container.FarmerRepo, walletHandler.ApproveWithdrawal),
	))
	http.HandleFunc("/admin/wallet-withdrawals/reject", middleware.OnlyPath(
		"/admin/wallet-withdrawals/reject",
		middleware.RequireAuth(container.FarmerRepo, walletHandler.RejectWithdrawal),
	))
	http.HandleFunc("/admin/wallet-withdrawals/reconcile", middleware.OnlyPath(
		"/admin/wallet-withdrawals/reconcile",
		middleware.RequireAuth(container.FarmerRepo, walletHandler.ReconcileWithdrawal),
	))
	// The way back for a payout that never reached Flutterwave: the row is
	// 'processing' with funds reserved and no way to release them, because
	// Reject only matches 'pending'.
	http.HandleFunc("/admin/wallet-withdrawals/return-to-queue", middleware.OnlyPath(
		"/admin/wallet-withdrawals/return-to-queue",
		middleware.RequireAuth(container.FarmerRepo, walletHandler.ReturnWithdrawalToQueue),
	))
	// For a payout that reached Flutterwave but never recorded its transfer id:
	// the id is what settlement matches on, so without it neither the webhook nor
	// reconcile can pay or refund the row.
	http.HandleFunc("/admin/wallet-withdrawals/record-transfer", middleware.OnlyPath(
		"/admin/wallet-withdrawals/record-transfer",
		middleware.RequireAuth(container.FarmerRepo, walletHandler.RecordTransferID),
	))
	// Wallet-to-wallet transfers are reviewed on the same approvals page, so
	// they need no listing route of their own.
	http.HandleFunc("/admin/wallet-transfers/approve", middleware.OnlyPath(
		"/admin/wallet-transfers/approve",
		middleware.RequireAuth(container.FarmerRepo, walletHandler.ApproveTransfer),
	))
	http.HandleFunc("/admin/wallet-transfers/reject", middleware.OnlyPath(
		"/admin/wallet-transfers/reject",
		middleware.RequireAuth(container.FarmerRepo, walletHandler.RejectTransfer),
	))
	// Recreates a missing wallet for an existing account. It creates an empty
	// NGN wallet and never moves money, so it is safe to repeat.
	http.HandleFunc("/admin/wallets/repair", middleware.OnlyPath(
		"/admin/wallets/repair",
		middleware.RequireAuth(container.FarmerRepo, walletHandler.RepairWallet),
	))

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

// robotsHandler advertises only public pages and points crawlers away from
// authenticated and transactional paths. It intentionally does not disallow
// /static/ because product images need to be fetchable by crawlers.
func robotsHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = fmt.Fprint(w, `User-agent: *
Allow: /
Allow: /learning
Disallow: /dashboard
Disallow: /profile
Disallow: /profile/edit
Disallow: /complete-profile
Disallow: /verify-phone
Disallow: /storage
Disallow: /marketplace
Disallow: /market-insights
Disallow: /product
Disallow: /cart
Disallow: /orders
Disallow: /order
Disallow: /deliveries
Disallow: /delivery
Disallow: /wallet
Disallow: /wallet/
Disallow: /chat
Disallow: /negotiations
Disallow: /negotiation
Disallow: /notifications
Disallow: /ai-assistant
Disallow: /ai-diagnosis-history
Disallow: /feedback
Disallow: /sales
Disallow: /admin/
Disallow: /payments/
Disallow: /webhooks/
Disallow: /ussd
Disallow: /locations/
`)
	if base, ok := publicBaseURL(r); ok {
		_, _ = fmt.Fprintf(w, "\nSitemap: %s/sitemap.xml\n", base)
	}
}

// sitemapHandler lists only the public landing and learning pages. Private
// marketplace and product pages are intentionally absent.
func sitemapHandler(w http.ResponseWriter, r *http.Request) {
	base, ok := publicBaseURL(r)
	if !ok {
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	_, _ = fmt.Fprintf(w, `<?xml version="1.0" encoding="UTF-8"?>
<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
  <url><loc>%s/</loc></url>
  <url><loc>%s/learning</loc></url>
</urlset>
`, html.EscapeString(base), html.EscapeString(base))
}

func publicBaseURL(r *http.Request) (string, bool) {
	host := strings.TrimSpace(r.Host)
	if host == "" {
		return "", false
	}
	scheme := "http"
	if middleware.RequestIsHTTPS(r) {
		scheme = "https"
	}
	return scheme + "://" + host, true
}
