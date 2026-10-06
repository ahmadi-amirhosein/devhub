package config

import (
	"errors"
	"os"
)

type Config struct {
	Addr        string
	DatabaseURL string
	JWTSecret   string
	CORSOrigin  string
	AIBackend   string
	OllamaURL   string
	OllamaModel string
	GeminiKey      string
	GeminiModel    string
	AnthropicKey   string
	AnthropicModel string
}

func Load() (Config, error) {
	c := Config{
		Addr:        env("ADDR", ":8080"),
		DatabaseURL: os.Getenv("DATABASE_URL"),
		JWTSecret:   os.Getenv("JWT_SECRET"),
		CORSOrigin:  env("CORS_ORIGIN", "http://localhost:3000"),
		AIBackend:   env("AI_BACKEND", "stub"),
		OllamaURL:   env("OLLAMA_URL", "http://localhost:11434"),
		OllamaModel: env("OLLAMA_MODEL", "qwen2.5:7b-instruct"),
		GeminiKey:      os.Getenv("GEMINI_API_KEY"),
		GeminiModel:    env("GEMINI_MODEL", "gemini-3.6-flash"),
		AnthropicKey:   os.Getenv("ANTHROPIC_API_KEY"),
		AnthropicModel: env("ANTHROPIC_MODEL", "claude-haiku-4-5-20251001"),
	}
	if c.DatabaseURL == "" {
		return c, errors.New("DATABASE_URL is required")
	}
	if len(c.JWTSecret) < 32 {
		return c, errors.New("JWT_SECRET must be at least 32 characters")
	}
	switch c.AIBackend {
	case "gemini":
		if c.GeminiKey == "" {
			return c, errors.New("GEMINI_API_KEY is required when AI_BACKEND=gemini")
		}
	case "claude":
		if c.AnthropicKey == "" {
			return c, errors.New("ANTHROPIC_API_KEY is required when AI_BACKEND=claude")
		}
	}
	return c, nil
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
