package app

import (
	"backend/internal/repository"
	"backend/internal/services"
)

// ============================================================
// APPLICATION CONTAINER
//
// Container holds the main services used throughout Agro-Shield.
//
// Dependency flow:
//
// Repository
//     ↓
// Service
//     ↓
// Container
//     ↓
// Handler
//     ↓
// Route
// ============================================================

type Container struct {
	Auth         *services.AuthService
	Crop         *services.CropService
	Order        *services.OrderService
	Delivery     *services.DeliveryService // ← added
	Cart         *services.CartService
	AI           *services.AIService
	Negotiation  *services.NegotiationService
	Notification *services.NotificationService
	Chat         *services.ChatService
	Payment      *services.PaymentService // ← added
	Feedback *services.FeedbackService
	MarketEvents *repository.MarketEventRepository

	// Used by authentication middleware.
	FarmerRepo *repository.FarmerRepository
}

// ============================================================
// CREATE APPLICATION CONTAINER
// ============================================================

func NewContainer(
	farmerRepo *repository.FarmerRepository,
	cropRepo *repository.CropRepository,
	orderRepo *repository.OrderRepository,
	deliveryRepo *repository.DeliveryRepository, // ← added
	cartRepo *repository.CartRepository,
	diagnosisRepo *repository.DiagnosisRepository,
	negotiationRepo *repository.NegotiationRepository,
	negotiationMsgRepo *repository.NegotiationMessageRepository,
	notificationRepo *repository.NotificationRepository,
	marketEventRepo *repository.MarketEventRepository,

	// ========================================================
	// CHAT REPOSITORIES
	// ========================================================

	conversationRepo *repository.ConversationRepository,
	memberRepo *repository.ConversationMemberRepository,
	chatMessageRepo *repository.ChatMessageRepository,

	aiProvider services.AIProvider,

	// ========================================================
	// PAYMENTS          ← added
	// ========================================================

	paymentRepo *repository.PaymentRepository,
	flutterwaveClient *services.FlutterwaveClient,
	feedbackRepo *repository.FeedbackRepository,
) *Container {

	// ========================================================
	// 1. CART SERVICE
	// ========================================================

	cartService := services.NewCartService(
		cartRepo,
		cropRepo,
		marketEventRepo,
	)

	// ========================================================
	// 2. NOTIFICATION SERVICE
	// ========================================================

	notificationService := services.NewNotificationService(
		notificationRepo,
	)

	// ========================================================
	// 3. NEGOTIATION SERVICE
	// ========================================================

	negotiationService := services.NewNegotiationService(
		negotiationRepo,
		negotiationMsgRepo,
		cropRepo,
		cartService,
		notificationService,
		marketEventRepo,
	)

	// ========================================================
	// 4. CHAT SERVICE
	//
	// Chat is completely separate from negotiation.
	// ========================================================

	chatService := services.NewChatService(
		conversationRepo,
		memberRepo,
		chatMessageRepo,
		notificationService,
	)

	// ========================================================
	// 5. DELIVERY SERVICE
	// ========================================================

	deliveryService := services.NewDeliveryService(
		deliveryRepo,
		orderRepo,
		marketEventRepo,
	)

	// ========================================================
	// 6. PAYMENT SERVICE          ← added
	//
	// Coordinates payment records in PostgreSQL with the
	// Flutterwave API.
	// ========================================================

	paymentService := services.NewPaymentService(
		paymentRepo,
		flutterwaveClient,
	)

	// ========================================================
	// 7. RETURN APPLICATION CONTAINER
	// ========================================================

	return &Container{

		// ----------------------------------------------------
		// AUTH
		// ----------------------------------------------------

		Auth: services.NewAuthService(
			farmerRepo,
		),

		// ----------------------------------------------------
		// CROPS
		// ----------------------------------------------------

		Crop: services.NewCropService(
			cropRepo,
			marketEventRepo,
		),

		// ----------------------------------------------------
		// ORDERS
		// ----------------------------------------------------

		Order: services.NewOrderService(
			orderRepo,
			cropRepo,
		),

		// ----------------------------------------------------
		// DELIVERY
		// ----------------------------------------------------

		Delivery: deliveryService,

		// ----------------------------------------------------
		// CART
		// ----------------------------------------------------

		Cart: cartService,

		// ----------------------------------------------------
		// AI
		// ----------------------------------------------------

		AI: services.NewAIService(
			diagnosisRepo,
			aiProvider,
			marketEventRepo,
		),

		// ----------------------------------------------------
		// NEGOTIATION
		// ----------------------------------------------------

		Negotiation: negotiationService,

		// ----------------------------------------------------
		// NOTIFICATION
		// ----------------------------------------------------

		Notification: notificationService,

		// ----------------------------------------------------
		// CHAT
		// ----------------------------------------------------

		Chat:         chatService,
		MarketEvents: marketEventRepo,

		// ----------------------------------------------------
		// PAYMENT          ← added
		// ----------------------------------------------------

		Payment: paymentService,

		// ----------------------------------------------------
		// FARMER REPOSITORY
		// ----------------------------------------------------

		FarmerRepo: farmerRepo,
	}
}