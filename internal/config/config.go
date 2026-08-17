package config

import (
	"os"
)

type Config struct {
	Port         string
	DatabasePath string
	SessionKey   string
	LLMAPIKey    string
	// LLMModel is the instance-wide default model. A user's own llm_model
	// setting wins over it; when both are empty the compiled fallback applies.
	LLMModel string
	// LLMAPIURL points at any OpenAI-compatible chat-completions endpoint.
	// Empty means the compiled default (Groq).
	LLMAPIURL string
	DevMode   bool
}

func Load() *Config {
	return &Config{
		Port:         getEnv("RECALL_PORT", "8084"),
		DatabasePath: getEnv("RECALL_DB_PATH", "recall.db"),
		SessionKey:   getEnv("RECALL_SESSION_KEY", "change-me-in-production-32chars!!"),
		LLMAPIKey:    getEnv("LLM_API_KEY", ""),
		LLMModel:     getEnv("LLM_MODEL", ""),
		LLMAPIURL:    getEnv("LLM_API_URL", ""),
		DevMode:      getEnv("RECALL_ENV", "dev") == "dev",
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
