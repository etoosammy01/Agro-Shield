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
	//
	// Connects Agro-Shield to the PostgreSQL database.
	//
	// The DATABASE_URL is loaded from the environment.
	//
	// Flow:
	//
	// Agro-Shield
	//      ↓
	// ConnectDB()
	//      ↓
	// PostgreSQL
	// ============================================================

	db, err := database.ConnectDB()
	if err != nil {
		log.Fatal(err)
	}

	// Make sure the database connection is closed when
	// the application stops.
	defer db.Close()

	// ============================================================
	// 2. RUN DATABASE MIGRATIONS
	//
	// Creates all required database tables if they do not
	// already exist.
	//
	// This includes:
	//
	// - Farmers
	// - Crops
	// - Diagnoses
	// - Diagnosis media
	// - Orders
	// - Negotiations
	// - Negotiation messages
	// - Cart items
	// - Notifications
	// - Conversations
	// - Conversation members
	// - Chat messages
	//
	// Flow:
	//
	// main.go
	//     ↓
	// RunMigration()
	//     ↓
	// PostgreSQL tables
	// ============================================================

	if err := database.RunMigration(db); err != nil {
		log.Fatal(err)
	}

	// ============================================================
	// 3. CREATE REPOSITORIES
	//
	// Repositories are responsible for communicating directly
	// with PostgreSQL.
	//
	// They contain database operations such as:
	//
	// INSERT
	// SELECT
	// UPDATE
	// DELETE
	//
	// Flow:
	//
	// Service
	//    ↓
	// Repository
	//    ↓
	// PostgreSQL
	// ============================================================

	// ------------------------------------------------------------
	// FARMER REPOSITORY
	//
	// Handles farmer/buyer account database operations.
	// ------------------------------------------------------------

	farmerRepo := repository.NewFarmerRepository(db)

	// ------------------------------------------------------------
	// CROP REPOSITORY
	//
	// Handles crop and marketplace product database operations.
	// ------------------------------------------------------------

	cropRepo := repository.NewCropRepository(db)

	// ------------------------------------------------------------
	// ORDER REPOSITORY
	//
	// Handles marketplace order database operations.
	// ------------------------------------------------------------

	orderRepo := repository.NewOrderRepository(db)

	// ------------------------------------------------------------
	// DIAGNOSIS REPOSITORY
	//
	// Handles AI diagnosis history stored in PostgreSQL.
	// ------------------------------------------------------------

	diagnosisRepo := repository.NewDiagnosisRepository(db)

	// ------------------------------------------------------------
	// NEGOTIATION REPOSITORY
	//
	// Handles negotiation records between buyers and sellers.
	// ------------------------------------------------------------

	negotiationRepo := repository.NewNegotiationRepository(db)

	// ------------------------------------------------------------
	// NEGOTIATION MESSAGE REPOSITORY
	//
	// Handles messages and offers inside negotiations.
	// ------------------------------------------------------------

	negotiationMsgRepo :=
		repository.NewNegotiationMessageRepository(db)

	// ------------------------------------------------------------
	// CART REPOSITORY
	//
	// Handles products that have entered the buyer's cart.
	//
	// Example:
	//
	// Negotiation accepted
	//        ↓
	// Cart item created
	//        ↓
	// Buyer sees product in cart
	// ------------------------------------------------------------

	cartRepo := repository.NewCartRepository(db)

	// ------------------------------------------------------------
	// NOTIFICATION REPOSITORY
	//
	// Handles notifications stored in PostgreSQL.
	//
	// Examples:
	//
	// - New negotiation offer
	// - Offer accepted
	// - Offer rejected
	// - Order created
	// ------------------------------------------------------------

	notificationRepo :=
		repository.NewNotificationRepository(db)
	marketEventRepo := repository.NewMarketEventRepository(db)

	// ============================================================
	// CHAT REPOSITORIES
	//
	// Agro-Shield chat has three database areas:
	//
	// 1. Conversations
	// 2. Conversation members
	// 3. Chat messages
	//
	// Flow:
	//
	// ChatService
	//      ↓
	// Chat Repositories
	//      ↓
	// PostgreSQL
	// ============================================================

	// ------------------------------------------------------------
	// CONVERSATION REPOSITORY
	//
	// Handles chat conversations.
	//
	// Examples:
	//
	// - Private conversation
	// - Maize Farmers Association
	// - Farm Tools Sellers
	// ------------------------------------------------------------

	conversationRepo :=
		repository.NewConversationRepository(db)

	// ------------------------------------------------------------
	// CONVERSATION MEMBER REPOSITORY
	//
	// Handles users who belong to conversations.
	//
	// Example:
	//
	// Maize Farmers Association
	//
	// Members:
	// - Farmer A
	// - Farmer B
	// - Farmer C
	// - Farmer D
	// ------------------------------------------------------------

	conversationMemberRepo :=
		repository.NewConversationMemberRepository(db)

	// ------------------------------------------------------------
	// CHAT MESSAGE REPOSITORY
	//
	// Handles normal chat messages.
	//
	// Example:
	//
	// "Good morning everyone."
	//
	// "The meeting is tomorrow."
	//
	// "Who has maize seeds available?"
	// ------------------------------------------------------------

	chatMessageRepo :=
		repository.NewChatMessageRepository(db)

	// ============================================================
	// 4. CREATE AI PROVIDER
	//
	// GeminiProvider handles communication between Agro-Shield
	// and the Gemini AI service.
	//
	// The API key is loaded from:
	//
	// GEMINI_API_KEY
	//
	// The AI provider is later given to AIService.
	// ============================================================

	aiProvider, err := services.NewGeminiProvider(
		os.Getenv("GEMINI_API_KEY"),
	)

	if err != nil {
		log.Fatal(err)
	}

	// ============================================================
	// 5. CREATE APPLICATION CONTAINER
	//
	// The application container connects all repositories
	// and services together.
	//
	// Dependency flow:
	//
	// PostgreSQL
	//     ↓
	// Repositories
	//     ↓
	// Services
	//     ↓
	// Container
	//     ↓
	// Routes / Handlers
	//
	// The Chat repositories are passed here so that
	// NewContainer() can create ChatService.
	// ============================================================

	container := app.NewContainer(
		farmerRepo,
		cropRepo,
		orderRepo,
		cartRepo,
		diagnosisRepo,
		negotiationRepo,
		negotiationMsgRepo,
		notificationRepo,
		marketEventRepo,

		// --------------------------------------------------------
		// CHAT REPOSITORIES
		// --------------------------------------------------------

		conversationRepo,
		conversationMemberRepo,
		chatMessageRepo,

		// --------------------------------------------------------
		// AI PROVIDER
		// --------------------------------------------------------

		aiProvider,
	)

	// ============================================================
	// 6. REGISTER ROUTES
	//
	// Connects URLs to their handlers.
	//
	// Examples:
	//
	// /login
	// /dashboard
	// /storage
	// /marketplace
	// /cart
	// /negotiations
	// /ai-assistant
	// /chat
	//
	// The container gives the routes access to the required
	// services.
	// ============================================================

	routes.RegisterRoutes(container)

	// ============================================================
	// 7. START HTTP SERVER
	//
	// Starts Agro-Shield on port 8080.
	//
	// Open:
	//
	// http://localhost:8080
	// ============================================================

	log.Println(
		"Server Starting on: http://localhost:8080...",
	)

	if err := http.ListenAndServe(":8080", nil); err != nil {
		log.Println(err)
	}
}
