package config

import (
	"fmt"
	"os"
	"strconv"

	"github.com/joho/godotenv"
)

type Config struct {
	// Database (reads)
	DBHost   string
	DBPort   int
	DBName   string
	DBUser   string
	DBPass   string
	DBPrefix string

	// API REST (writes)
	APIUrl string
	APIKey string

	// MCP
	Transport string
	HTTPPort  int

	// Dolibarr
	Entity int

	// Auth
	AuthToken string
}

func Load() (*Config, error) {
	_ = godotenv.Load()

	cfg := &Config{
		DBHost:    envOrDefault("DB_HOST", "localhost"),
		DBPort:    envIntOrDefault("DB_PORT", 3306),
		DBName:    envOrDefault("DB_NAME", "dolibarr"),
		DBUser:    envOrDefault("DB_USER", "dolibarr"),
		DBPass:    envOrDefault("DB_PASS", ""),
		DBPrefix:  envOrDefault("DB_PREFIX", "llx_"),
		APIUrl:    envOrDefault("DOLIBARR_API_URL", ""),
		APIKey:    envOrDefault("DOLIBARR_API_KEY", ""),
		Transport: envOrDefault("MCP_TRANSPORT", "stdio"),
		HTTPPort:  envIntOrDefault("MCP_HTTP_PORT", 8080),
		Entity:    envIntOrDefault("DOLIBARR_ENTITY", 1),
		AuthToken: envOrDefault("MCP_AUTH_TOKEN", ""),
	}

	if cfg.DBPass == "" {
		return nil, fmt.Errorf("DB_PASS is required")
	}
	if cfg.APIUrl == "" {
		return nil, fmt.Errorf("DOLIBARR_API_URL is required")
	}
	if cfg.APIKey == "" {
		return nil, fmt.Errorf("DOLIBARR_API_KEY is required")
	}

	return cfg, nil
}

func (c *Config) DSN() string {
	return fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?parseTime=true&charset=utf8mb4&collation=utf8mb4_unicode_ci",
		c.DBUser, c.DBPass, c.DBHost, c.DBPort, c.DBName)
}

func (c *Config) T(table string) string {
	return c.DBPrefix + table
}

func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envIntOrDefault(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return fallback
}
