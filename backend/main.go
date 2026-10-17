package main

import (
	"log"
	"net/http"

	app "backend/internal"
	"backend/internal/config"
	"backend/internal/database"
	"backend/internal/repository"
	"backend/internal/services"
	learningproviders "backend/internal/services/learning"
	"backend/routes"
)

func main() {
	// Load application configuration once at startup.
	cfg, err := config.Load()
	if err != nil {
		log.Fatal(err)
	}

	// ============================================================
	// 1. CONNECT TO DATABASE
	// ============================================================

	db, err := database.ConnectDB(cfg.DatabaseURL)
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
	// BANK LOOKUP REPOSITORY          ← added
	// Caches which bank an account number belongs to, and the name
	// registered on it, so a number is swept against the bank
	// shortlist at most once.
	// ------------------------------------------------------------
	bankLookupRepo := repository.NewBankLookupRepository(db)

	// ------------------------------------------------------------
	// ONE-TIME PASSCODE REPOSITORY          ← added
	// Backs password resets and wallet payout verification.
	// ------------------------------------------------------------
	otpRepo := repository.NewOTPRepository(db)

	// ------------------------------------------------------------
	// FEEDBACK REPOSITORY
	// ------------------------------------------------------------
	feedbackRepo := repository.NewFeedbackRepository(db)

	// ============================================================
	// 4. CREATE AI PROVIDERS
	// ============================================================

	// ============================================================
	// 4. CREATE AI PROVIDERS
	// ============================================================

	// ------------------------------------------------------------
	// DIAGNOSIS AI
	// Gemini handles diagnosis only.
	// ------------------------------------------------------------

	diagnosisProvider, err := services.NewGeminiDiagnosisProvider(
		cfg.GeminiAPIKey,
	)
	if err != nil {
		log.Fatal(err)
	}

	// ------------------------------------------------------------
	// LEARNING AI
	// Groq is primary. OpenRouter is the backup.
	// ------------------------------------------------------------

	var primaryLearning services.LearningProvider
	var backupLearning services.LearningProvider

	// Create Groq only when its API key is configured.
	if cfg.GroqAPIKey != "" {
		primaryLearning, err = learningproviders.NewGroqLearningProvider(
			cfg.GroqAPIKey,
		)
		if err != nil {
			log.Fatal(err)
		}
	}

	// Create OpenRouter only when its API key is configured.
	if cfg.OpenRouterAPIKey != "" {
		backupLearning, err = learningproviders.NewOpenRouterLearningProvider(
			cfg.OpenRouterAPIKey,
		)
		if err != nil {
			log.Fatal(err)
		}
	}

	// Combine both providers into one failover learning service.
	learningProvider := learningproviders.NewFailoverLearningProvider(
		primaryLearning,
		backupLearning,
	)

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
		cfg.FlutterwaveSecretKey,
		cfg.FlutterwaveSecretHash,
		cfg.PaymentRedirectURL,
		cfg.FlutterwavePaymentOptions,
	)
	walletAdminIDs, err := services.ParseWalletAdminIDs(cfg.WalletAdminUserIDs)
	if err != nil {
		log.Fatal(err)
	}
	commissionBasisPoints, err := services.ParseCommissionPercent(cfg.CommissionPercent)
	if err != nil {
		log.Fatal(err)
	}

	// ============================================================
	// CREATE SMS SENDER          ← added
	//
	// Delivers one-time passcodes. SMS_PROVIDER=log (the default)
	// prints codes to the server log so development needs no SMS
	// account; SMS_PROVIDER=termii sends real messages and requires
	// TERMII_API_KEY and TERMII_SENDER_ID.
	// ============================================================

	smsSender := services.NewSMSSender(
		cfg.SMSProvider,
		cfg.TermiiAPIKey,
		cfg.TermiiSenderID,
		cfg.TermiiBaseURL,
	)
	if cfg.SMSProvider == "log" && cfg.AppEnv != "development" {
		log.Printf("WARNING: SMS_PROVIDER=log in %s — one-time passcodes are being written to this log instead of being sent, and anyone with log access can read them", cfg.AppEnv)
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

		// AI providers
		diagnosisProvider,
		learningProvider,

		// Payments          ← added
		paymentRepo,
		walletRepo,
		bankLookupRepo,
		otpRepo,
		smsSender,
		flutterwaveClient,
		feedbackRepo,
		walletAdminIDs,
		commissionBasisPoints,
	)

	// ============================================================
	// 6. REGISTER ROUTES
	// ============================================================

	routes.RegisterRoutes(container)

	// ============================================================
	// 7. START HTTP SERVER
	// ============================================================

	if err = http.ListenAndServe(":"+cfg.Port, http.DefaultServeMux); err != nil {
		log.Fatal(err)
	}
}
