package config

import "os"

type Config struct {
	Port          string
	AllowedOrigin string
}

// Load reads settings from environment variables and falls back to defaults for local development
func Load() *Config {
	return &Config{
		Port:          getEnv("PORT", "8080"),
		AllowedOrigin: getEnv("ALLOWED_ORIGIN", "http://localhost:5173"),
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
