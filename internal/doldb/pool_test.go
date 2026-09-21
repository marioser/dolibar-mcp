package doldb

import (
	"context"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/sgsoluciones/dolibarr-mcp/internal/config"
)

// unreachableConfig points at a port nothing is listening on, so every attempt
// to reach the "database" fails the way a down Dolibarr does.
func unreachableConfig(t *testing.T) *config.Config {
	t.Helper()

	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve port: %v", err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	l.Close() // nothing listens here now

	return &config.Config{
		DBHost:            "127.0.0.1",
		DBPort:            port,
		DBName:            "dolibarr",
		DBUser:            "dol",
		DBPass:            "pass",
		DBPrefix:          "llx_",
		Entity:            1,
		DBMaxOpenConns:    10,
		DBMaxIdleConns:    5,
		DBConnMaxLifetime: 5 * time.Minute,
		DBConnMaxIdleTime: 2 * time.Minute,
		DBConnectTimeout:  200 * time.Millisecond,
		DBReadTimeout:     200 * time.Millisecond,
		DBWriteTimeout:    200 * time.Millisecond,
		QueryTimeout:      500 * time.Millisecond,
	}
}

func TestNewSucceedsWhenDatabaseIsDown(t *testing.T) {
	// This is the whole point of the change: the MCP server is launched by the
	// client and lives for the session, so a database hiccup at startup must
	// not kill it.
	db, err := New(unreachableConfig(t))
	if err != nil {
		t.Fatalf("New returned an error with the database down: %v", err)
	}
	defer db.Close()

	if db.Prefix() != "llx_" || db.Entity() != 1 {
		t.Fatalf("pool is not usable: prefix=%q entity=%d", db.Prefix(), db.Entity())
	}
}

func TestVerifyFailsWhenDatabaseIsDown(t *testing.T) {
	db, err := New(unreachableConfig(t))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer db.Close()

	if err := db.Verify(context.Background()); err == nil {
		t.Fatal("Verify succeeded against a dead port, want an error")
	}
}

func TestVerifyGivesUpQuickly(t *testing.T) {
	db, err := New(unreachableConfig(t))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer db.Close()

	start := time.Now()
	_ = db.Verify(context.Background())
	if elapsed := time.Since(start); elapsed > 10*time.Second {
		t.Fatalf("Verify took %v, want it bounded by verifyTimeout", elapsed)
	}
}

func TestDolConfigFallsBackToDefaults(t *testing.T) {
	db, err := New(unreachableConfig(t))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer db.Close()

	got := db.DolConfig(context.Background())
	if got == nil {
		t.Fatal("DolConfig returned nil with the database down")
	}
	if got.MainCurrency != "USD" {
		t.Fatalf("MainCurrency = %q, want the %q default", got.MainCurrency, "USD")
	}
}

func TestDolConfigBacksOffAfterFailure(t *testing.T) {
	db, err := New(unreachableConfig(t))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer db.Close()

	_ = db.DolConfig(context.Background())

	// The second call is inside dolConfigRetryAfter, so it must serve the
	// cached fallback instead of hammering a database that is still down.
	start := time.Now()
	got := db.DolConfig(context.Background())
	elapsed := time.Since(start)

	if got == nil {
		t.Fatal("DolConfig returned nil")
	}
	if elapsed > 100*time.Millisecond {
		t.Fatalf("second DolConfig took %v, want it served from cache", elapsed)
	}
}

func TestHealthyFailsWhenDatabaseIsDown(t *testing.T) {
	db, err := New(unreachableConfig(t))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer db.Close()

	if err := db.Healthy(context.Background()); err == nil {
		t.Fatal("Healthy reported ok against a dead port")
	}
}

func TestQueryErrorNamesTheDatabase(t *testing.T) {
	cfg := unreachableConfig(t)
	db, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer db.Close()

	_, qErr := db.QueryContext(context.Background(), "SELECT 1")
	if qErr == nil {
		t.Fatal("query succeeded against a dead port")
	}
	// The driver's bare "invalid connection" tells an operator nothing about
	// which host to go and look at.
	for _, want := range []string{"Dolibarr database", cfg.DBHost, "dolibarr"} {
		if !strings.Contains(qErr.Error(), want) {
			t.Errorf("error %q does not mention %q", qErr.Error(), want)
		}
	}
}

func TestQueryRowErrorNamesTheDatabase(t *testing.T) {
	db, err := New(unreachableConfig(t))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer db.Close()

	var n int
	scanErr := db.QueryRowContext(context.Background(), "SELECT 1").Scan(&n)
	if scanErr == nil {
		t.Fatal("scan succeeded against a dead port")
	}
	if !strings.Contains(scanErr.Error(), "Dolibarr database") {
		t.Errorf("error %q is not the actionable message", scanErr.Error())
	}
}

func TestQueryContextRespectsCallerDeadline(t *testing.T) {
	db, err := New(unreachableConfig(t))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, _ = db.QueryContext(ctx, "SELECT 1")
	// Retries must not outlive the caller's deadline by more than one backoff.
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("query took %v after a 50ms deadline", elapsed)
	}
}

func TestQueryContextAppliesDefaultTimeout(t *testing.T) {
	cfg := unreachableConfig(t)
	db, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer db.Close()

	// An open-ended caller context still gets the configured bound.
	ctx, cancel := db.queryContext(context.Background())
	defer cancel()

	deadline, ok := ctx.Deadline()
	if !ok {
		t.Fatal("queryContext left the context without a deadline")
	}
	if remaining := time.Until(deadline); remaining > cfg.QueryTimeout {
		t.Fatalf("deadline is %v out, want at most %v", remaining, cfg.QueryTimeout)
	}
}

func TestQueryContextKeepsTighterCallerDeadline(t *testing.T) {
	db, err := New(unreachableConfig(t))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer db.Close()

	caller, cancelCaller := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancelCaller()

	ctx, cancel := db.queryContext(caller)
	defer cancel()

	deadline, ok := ctx.Deadline()
	if !ok {
		t.Fatal("no deadline")
	}
	if remaining := time.Until(deadline); remaining > 50*time.Millisecond {
		t.Fatalf("deadline is %v out, want the caller's tighter 10ms", remaining)
	}
}
