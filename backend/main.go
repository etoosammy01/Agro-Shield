package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	app "backend/internal"
	"backend/internal/config"
	"backend/internal/database"
	"backend/internal/otp"
	"backend/internal/repository"
	"backend/internal/services"
	learningproviders "backend/internal/services/learning"
	"backend/middleware"
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
	if err := database.RunSessionMigration(db); err != nil {
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
	// SESSION REPOSITORY
	// Stores persistent, hashed user sessions in PostgreSQL.
	// ------------------------------------------------------------
	sessionRepo := repository.NewSessionRepository(db)

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
	staleDepositAfter, err := services.ParseStaleDepositAfter(cfg.WalletStaleDepositHours)
	if err != nil {
		log.Fatal(err)
	}

	// ============================================================
	// CREATE OTP PROVIDER
	//
	// An OTP.dev API key automatically enables SMS delivery and verification.
	// Development without a key writes codes to the log.
	// ============================================================

	otpProvider, err := newOTPProvider(cfg)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("OTP SMS transport=%s", cfg.SMSProvider)

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
		sessionRepo,
		otpRepo,
		otpProvider,
		flutterwaveClient,
		feedbackRepo,
		walletAdminIDs,
		commissionBasisPoints,
		staleDepositAfter,
	)

	// ============================================================
	// 6. REGISTER ROUTES
	// ============================================================

	var emailSender services.EmailSender
	if cfg.BrevoAPIKey != "" && cfg.BrevoFromEmail != "" {
		emailSender = services.NewBrevoEmailSender(cfg.BrevoAPIKey, cfg.BrevoFromEmail, cfg.BrevoFromName)
	} else {
		log.Print("welcome_email disabled: configure BREVO_API_KEY and BREVO_FROM_EMAIL")
	}
	welcomeEmail := services.NewWelcomeEmailService(emailSender)

	routes.RegisterRoutes(container, welcomeEmail)
	// Health checks are deliberately outside the application router so they
	// remain cheap and are not affected by page-level middleware decisions.
	http.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok\n"))
	})
	http.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if err := db.PingContext(ctx); err != nil {
			log.Printf("readiness check failed: %v", err)
			http.Error(w, "not ready", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ready\n"))
	})

	middleware.SetTrustedProxies(cfg.TrustedProxyCIDRs)

	// ============================================================
	// 7. START HTTP SERVER
	// ============================================================

	// The reconciliation loop must stop when the server is shutting down, but
	// in-flight HTTP requests are allowed to finish under srv.Shutdown.
	reconcileCtx, stopReconciliation := context.WithCancel(context.Background())
	defer stopReconciliation()
	go container.Wallet.RunReconciliation(reconcileCtx, services.DefaultReconciliationInterval)

	server := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           middleware.RequestLogger(middleware.Compression(middleware.Recoverer(middleware.RequestGuard(http.DefaultServeMux)))),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       5 * time.Minute,
		WriteTimeout:      5 * time.Minute,
		IdleTimeout:       2 * time.Minute,
		MaxHeaderBytes:    1 << 20,
	}

	shutdownComplete := make(chan struct{})
	go func() {
		defer close(shutdownComplete)
		signalCtx, stopSignals := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stopSignals()
		<-signalCtx.Done()

		log.Println("shutdown requested; draining HTTP connections")
		stopReconciliation()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			log.Printf("graceful shutdown failed: %v", err)
			_ = server.Close()
		}
		if err := welcomeEmail.Close(shutdownCtx); err != nil {
			log.Printf("welcome_email shutdown: %v", err)
		}
	}()

	log.Printf("HTTP server listening on :%s", cfg.Port)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
	<-shutdownComplete
}

func newOTPProvider(cfg config.Config) (otp.Provider, error) {
	switch cfg.SMSProvider {
	case "log":
		return otp.LogProvider{}, nil
	case "otpdev":
		return otp.NewOTPDevProvider(otp.OTPDevConfig{
			APIKey:     cfg.OTPDevAPIKey,
			Sender:     cfg.OTPDevSender,
			TemplateID: cfg.OTPDevTemplateID,
			BaseURL:    cfg.OTPDevBaseURL,
		})
	default:
		return nil, errors.New("unsupported OTP SMS provider")
	}
}
