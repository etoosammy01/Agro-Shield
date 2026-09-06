package main

import (
	"log"
	"net/http"
	"os"

	app "backend/internal"
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

	// ============================================================
	// 4. CREATE AI PROVIDER
	// ============================================================

	aiProvider, err := services.NewGeminiProvider(os.Getenv("GEMINI_API_KEY"))
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
	)

	// ============================================================
	// 6. REGISTER ROUTES
	// ============================================================

	routes.RegisterRoutes(container)

	// ============================================================
	// 7. START HTTP SERVER
	// ============================================================

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	log.Println("Server starting on port:", port)

	if err := http.ListenAndServe(":"+port, nil); err != nil {
		log.Println(err)
	}
}
