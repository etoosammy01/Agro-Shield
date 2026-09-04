package main

import (
	"database/sql"
	"log"
	"net/http"
	"os"

	_ "github.com/jackc/pgx/v5/stdlib" // registers the "pgx" driver for database/sql
)

func mustEnv(key string) string {
	v := os.Getenv(key)
	if v == "" {
		log.Fatalf("missing required env var: %s", key)
	}
	return v
}

func main() {
	databaseURL := mustEnv("DATABASE_URL")
	secretKey := mustEnv("FLUTTERWAVE_SECRET_KEY")
	secretHash := mustEnv("FLUTTERWAVE_SECRET_HASH")
	redirectURL := mustEnv("PAYMENT_REDIRECT_URL")

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		log.Fatalf("open db: %v", err)
	}
	defer db.Close()

	if err := db.Ping(); err != nil {
		log.Fatalf("ping db: %v", err)
	}

	fw := NewFlutterwaveClient(secretKey, secretHash, redirectURL)
	handlers := &PaymentHandlers{DB: db, FW: fw}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /orders/pay", handlers.InitiatePayment)
	mux.HandleFunc("GET /payments/callback", handlers.Callback)
	mux.HandleFunc("POST /webhooks/flutterwave", handlers.Webhook)

	log.Printf("listening on :%s", port)
	if err := http.ListenAndServe(":"+port, mux); err != nil {
		log.Fatal(err)
	}
}
