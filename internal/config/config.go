package config

import "os"

type Config struct {
	DatabaseURL string
	HTTPPort    string
	APIKey      string
}

func Load() Config {
	return Config{
		DatabaseURL: getEnv("DATABASE_URL", "postgres://splitstack:splitstack@localhost:5432/splitstack?sslmode=disable"),
		HTTPPort:    getEnv("HTTP_PORT", "8080"),
		APIKey:      getEnv("API_KEY", "dev-secret-key"),
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
