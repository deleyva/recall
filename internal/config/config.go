package config

import (
	"os"
)

type Config struct {
	Port         string
	DatabasePath string
	SessionKey   string
	GeminiAPIKey string
}

func Load() *Config {
	return &Config{
		Port:         getEnv("RECALL_PORT", "8080"),
		DatabasePath: getEnv("RECALL_DB_PATH", "recall.db"),
		SessionKey:   getEnv("RECALL_SESSION_KEY", "change-me-in-production-32chars!!"),
		GeminiAPIKey: getEnv("GEMINI_API_KEY", ""),
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
