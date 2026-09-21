package config

import (
	"net/url"
	"strings"
	"testing"
	"time"
)

func testConfig() *Config {
	return &Config{
		DBHost:           "db.example.com",
		DBPort:           3306,
		DBName:           "dolibarr",
		DBUser:           "dol",
		DBPass:           "s3cr3t",
		DBConnectTimeout: 5 * time.Second,
		DBReadTimeout:    30 * time.Second,
		DBWriteTimeout:   30 * time.Second,
	}
}

func dsnParams(t *testing.T, dsn string) url.Values {
	t.Helper()
	i := strings.Index(dsn, "?")
	if i < 0 {
		t.Fatalf("dsn has no parameters: %s", dsn)
	}
	v, err := url.ParseQuery(dsn[i+1:])
	if err != nil {
		t.Fatalf("parse dsn params: %v", err)
	}
	return v
}

func TestDSNCarriesNetworkTimeouts(t *testing.T) {
	// Without these, a database host that stops answering TCP hangs a tool call
	// until the OS gives up — minutes, not seconds.
	p := dsnParams(t, testConfig().DSN())

	for key, want := range map[string]string{
		"timeout":      "5s",
		"readTimeout":  "30s",
		"writeTimeout": "30s",
	} {
		if got := p.Get(key); got != want {
			t.Errorf("dsn %s = %q, want %q", key, got, want)
		}
	}
}

func TestDSNKeepsExistingParams(t *testing.T) {
	p := dsnParams(t, testConfig().DSN())

	for key, want := range map[string]string{
		"parseTime": "true",
		"charset":   "utf8mb4",
		"collation": "utf8mb4_unicode_ci",
	} {
		if got := p.Get(key); got != want {
			t.Errorf("dsn %s = %q, want %q", key, got, want)
		}
	}
}

func TestDSNAddressAndCredentials(t *testing.T) {
	dsn := testConfig().DSN()
	if want := "dol:s3cr3t@tcp(db.example.com:3306)/dolibarr?"; !strings.HasPrefix(dsn, want) {
		t.Fatalf("dsn = %q, want prefix %q", dsn, want)
	}
}

func TestDSNBracketsIPv6Host(t *testing.T) {
	cfg := testConfig()
	cfg.DBHost = "::1"
	if want := "tcp([::1]:3306)"; !strings.Contains(cfg.DSN(), want) {
		t.Fatalf("dsn = %q, want it to contain %q", cfg.DSN(), want)
	}
}

func TestEnvDurationOrDefault(t *testing.T) {
	cases := []struct {
		name  string
		value string
		want  time.Duration
	}{
		{"unset falls back", "", 7 * time.Second},
		{"go duration", "45s", 45 * time.Second},
		{"minutes", "2m", 2 * time.Minute},
		{"bare integer is seconds", "90", 90 * time.Second},
		{"garbage falls back", "soon", 7 * time.Second},
		// An explicit zero is a decision (the CACHE_TTL kill switch), not a
		// mistake, so it must survive.
		{"explicit zero is honoured", "0", 0},
		{"explicit zero duration is honoured", "0s", 0},
		{"negative falls back", "-5s", 7 * time.Second},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("DOLTEST_DURATION", tc.value)
			if got := envDurationOrDefault("DOLTEST_DURATION", 7*time.Second); got != tc.want {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}
