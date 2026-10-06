package config

import (
	"fmt"
	"os"

	"github.com/joho/godotenv"
)

type Config struct {
	Port        string
	Ai_Api      string
	DatabaseURL string
}

func Load() (Config, error) {
	_ = godotenv.Load()
	port := os.Getenv("PORT")
	if port == "" {
		return Config{}, fmt.Errorf("PORT must not be empty")
	}
	Ai_Model := os.Getenv("GEMINI_API_KEY")
	if Ai_Model == "" {
		return Config{}, fmt.Errorf("GEMINI_API_KEY must not be empty")
	}
	Database := os.Getenv("DATABASE_URL")
	if Database == "" {
		return Config{}, fmt.Errorf("DATABASE_URL must not be empty")
	}
	return Config{
		Port:        port,
		Ai_Api:      Ai_Model,
		DatabaseURL: Database,
	}, nil
}
