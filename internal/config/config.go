package config

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"strconv"
	"time"

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

	// Connection pool and network timeouts. Without an explicit dial/read
	// timeout a database host that stops answering TCP hangs a tool call until
	// the OS gives up, which can be minutes.
	DBMaxOpenConns    int
	DBMaxIdleConns    int
	DBConnMaxLifetime time.Duration
	DBConnMaxIdleTime time.Duration
	DBConnectTimeout  time.Duration
	DBReadTimeout     time.Duration
	DBWriteTimeout    time.Duration

	// QueryTimeout bounds a single MCP tool call against the database.
	QueryTimeout time.Duration

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
		DBHost:   envOrDefault("DB_HOST", "localhost"),
		DBPort:   envIntOrDefault("DB_PORT", 3306),
		DBName:   envOrDefault("DB_NAME", "dolibarr"),
		DBUser:   envOrDefault("DB_USER", "dolibarr"),
		DBPass:   envOrDefault("DB_PASS", ""),
		DBPrefix: envOrDefault("DB_PREFIX", "llx_"),

		DBMaxOpenConns:    envIntOrDefault("DB_MAX_OPEN_CONNS", 10),
		DBMaxIdleConns:    envIntOrDefault("DB_MAX_IDLE_CONNS", 5),
		DBConnMaxLifetime: envDurationOrDefault("DB_CONN_MAX_LIFETIME", 5*time.Minute),
		DBConnMaxIdleTime: envDurationOrDefault("DB_CONN_MAX_IDLE_TIME", 2*time.Minute),
		DBConnectTimeout:  envDurationOrDefault("DB_CONNECT_TIMEOUT", 5*time.Second),
		DBReadTimeout:     envDurationOrDefault("DB_READ_TIMEOUT", 30*time.Second),
		DBWriteTimeout:    envDurationOrDefault("DB_WRITE_TIMEOUT", 30*time.Second),
		QueryTimeout:      envDurationOrDefault("DB_QUERY_TIMEOUT", 45*time.Second),

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

// DSN builds the go-sql-driver connection string. Parameters are encoded with
// url.Values; the credentials are not, because go-sql-driver's ParseDSN does
// not unescape them — a password containing "/" would have to be changed.
func (c *Config) DSN() string {
	params := url.Values{}
	params.Set("parseTime", "true")
	params.Set("charset", "utf8mb4")
	params.Set("collation", "utf8mb4_unicode_ci")
	// Bound every stage of the connection. timeout covers the TCP dial;
	// readTimeout is what stops a half-open socket from hanging a tool call
	// forever when the server disappears without closing it.
	params.Set("timeout", c.DBConnectTimeout.String())
	params.Set("readTimeout", c.DBReadTimeout.String())
	params.Set("writeTimeout", c.DBWriteTimeout.String())

	addr := net.JoinHostPort(c.DBHost, strconv.Itoa(c.DBPort))

	return fmt.Sprintf("%s:%s@tcp(%s)/%s?%s",
		c.DBUser, c.DBPass, addr, c.DBName, params.Encode())
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

// envDurationOrDefault reads a Go duration string (e.g. "30s", "5m"). A bare
// integer is read as seconds so operators are not tripped up by the format.
func envDurationOrDefault(key string, fallback time.Duration) time.Duration {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	if d, err := time.ParseDuration(v); err == nil && d > 0 {
		return d
	}
	if n, err := strconv.Atoi(v); err == nil && n > 0 {
		return time.Duration(n) * time.Second
	}
	return fallback
}
