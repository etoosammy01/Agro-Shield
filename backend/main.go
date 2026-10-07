package main

import (
	"log"
	"net/http"
	"os"

	app "backend/internal"
	"backend/internal/config"
	"backend/internal/database"
	"backend/internal/repository"
	"backend/internal/services"
	"backend/routes"
)

func main() {

	// ============================================================
	// 1. CONNECT TO DATABASE
	// ============================================================

	db, err := database.ConnectDB()
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	// ============================================================
	// 2. RUN DATABASE MIGRATIONS
	// ============================================================

	if err := database.RunMigration(db); err != nil {
		log.Fatal(err)
	}

	// ============================================================
	// 3. CREATE REPOSITORIES
	// ============================================================

	// ------------------------------------------------------------
	// FARMER REPOSITORY
	// ------------------------------------------------------------
	farmerRepo := repository.NewFarmerRepository(db)

	// ------------------------------------------------------------
	// CROP REPOSITORY
	// ------------------------------------------------------------
	cropRepo := repository.NewCropRepository(db)

	// ------------------------------------------------------------
	// ORDER REPOSITORY
	// ------------------------------------------------------------
	orderRepo := repository.NewOrderRepository(db)

	// ------------------------------------------------------------
	// DELIVERY REPOSITORY          ← added
	// Handles delivery tracking for completed orders.
	// ------------------------------------------------------------
	deliveryRepo := repository.NewDeliveryRepository(db)

	// ------------------------------------------------------------
	// DIAGNOSIS REPOSITORY
	// ------------------------------------------------------------
	diagnosisRepo := repository.NewDiagnosisRepository(db)

	// ------------------------------------------------------------
	// NEGOTIATION REPOSITORY
	// ------------------------------------------------------------
	negotiationRepo := repository.NewNegotiationRepository(db)

	// ------------------------------------------------------------
	// NEGOTIATION MESSAGE REPOSITORY
	// ------------------------------------------------------------
	negotiationMsgRepo := repository.NewNegotiationMessageRepository(db)

	// ------------------------------------------------------------
	// CART REPOSITORY
	// ------------------------------------------------------------
	cartRepo := repository.NewCartRepository(db)

	// ------------------------------------------------------------
	// NOTIFICATION REPOSITORY
	// ------------------------------------------------------------
	notificationRepo := repository.NewNotificationRepository(db)
	marketEventRepo := repository.NewMarketEventRepository(db)

	// ============================================================
	// CHAT REPOSITORIES
	// ============================================================

	conversationRepo := repository.NewConversationRepository(db)
	conversationMemberRepo := repository.NewConversationMemberRepository(db)
	chatMessageRepo := repository.NewChatMessageRepository(db)

	// ------------------------------------------------------------
	// PAYMENT REPOSITORY          ← added
	// Handles payment and webhook-delivery database operations.
	// ------------------------------------------------------------
	paymentRepo := repository.NewPaymentRepository(db)
	walletRepo := repository.NewWalletRepository(db)

	// ------------------------------------------------------------
	// FEEDBACK REPOSITORY
	// ------------------------------------------------------------
	feedbackRepo := repository.NewFeedbackRepository(db)

	// ============================================================
	// 4. CREATE AI PROVIDER
	// ============================================================

	aiProvider, err := services.NewGeminiProvider(os.Getenv("GEMINI_API_KEY"))
	if err != nil {
		log.Fatal(err)
	}

	// ============================================================
	// CREATE FLUTTERWAVE CLIENT          ← added
	//
	// Handles communication between Agro-Shield and Flutterwave
	// for checkout payments.
	//
	// Loaded from:
	//
	// FLUTTERWAVE_SECRET_KEY
	// FLUTTERWAVE_SECRET_HASH
	// PAYMENT_REDIRECT_URL
	// ============================================================

	flutterwaveClient := services.NewFlutterwaveClient(
		os.Getenv("FLUTTERWAVE_SECRET_KEY"),
		os.Getenv("FLUTTERWAVE_SECRET_HASH"),
		os.Getenv("PAYMENT_REDIRECT_URL"),
	)
	walletAdminIDs, err := services.ParseWalletAdminIDs(os.Getenv("WALLET_ADMIN_USER_IDS"))
	if err != nil {
		log.Fatal(err)
	}

	// ============================================================
	// 5. CREATE APPLICATION CONTAINER
	// ============================================================

	container := app.NewContainer(
		farmerRepo,
		cropRepo,
		orderRepo,
		deliveryRepo, // ← now correctly created and passed
		cartRepo,
		diagnosisRepo,
		negotiationRepo,
		negotiationMsgRepo,
		notificationRepo,
		marketEventRepo,

		// Chat repositories
		conversationRepo,
		conversationMemberRepo,
		chatMessageRepo,

		// AI provider
		aiProvider,

		// Payments          ← added
		paymentRepo,
		walletRepo,
		flutterwaveClient,
		feedbackRepo,
		walletAdminIDs,
	)

	// ============================================================
	// 6. REGISTER ROUTES
	// ============================================================

	routes.RegisterRoutes(container)

	// ============================================================
	// 7. START HTTP SERVER
	// ============================================================

	cfg, err := config.Load()
	if err != nil {
		log.Fatal(err)
	}
	if err = http.ListenAndServe(":"+cfg.Port, http.DefaultServeMux); err != nil {
		log.Fatal(err)
	}
}
