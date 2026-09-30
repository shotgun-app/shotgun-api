package config

import (
	"os"
	"strconv"
	"time"
)

type Config struct {
	Port          string
	AllowedOrigin string
	DatabaseURL   string
	SessionTTL    time.Duration
	CookieSecure  bool
}

// Load reads settings from environment variables and falls back to defaults for local development
func Load() *Config {
	return &Config{
		Port:          getEnv("PORT", "8080"),
		AllowedOrigin: getEnv("ALLOWED_ORIGIN", "http://localhost:5173"),
		DatabaseURL:   getEnv("DATABASE_URL", "postgres://shotgun:shotgun@localhost:5432/shotgun?sslmode=disable"),
		SessionTTL:    time.Duration(getEnvInt("SESSION_TTL_HOURS", 24*30)) * time.Hour,
		CookieSecure:  getEnv("COOKIE_SECURE", "false") == "true",
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getEnvInt(key string, fallback int) int {
	if n, err := strconv.Atoi(os.Getenv(key)); err == nil && n > 0 {
		return n
	}
	return fallback
}
