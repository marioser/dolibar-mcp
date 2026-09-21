package doldb

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"net"
	"syscall"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"
)

func TestIsTransient(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		// Connection-level failures: the query never reached a healthy server,
		// or the server dropped it. Safe to replay a SELECT.
		{"nil", nil, false},
		{"driver bad conn", driver.ErrBadConn, true},
		{"mysql invalid conn", mysql.ErrInvalidConn, true},
		{"wrapped bad conn", fmt.Errorf("query proposals: %w", driver.ErrBadConn), true},
		{"eof", io.EOF, true},
		{"unexpected eof", io.ErrUnexpectedEOF, true},
		{"connection reset", syscall.ECONNRESET, true},
		{"connection refused", syscall.ECONNREFUSED, true},
		{"broken pipe", syscall.EPIPE, true},
		{"net op error", &net.OpError{Op: "dial", Err: syscall.ECONNREFUSED}, true},
		{"net timeout", &net.OpError{Op: "read", Err: timeoutErr{}}, true},

		// MySQL server-side errors that resolve on their own.
		{"too many connections", &mysql.MySQLError{Number: 1040}, true},
		{"server shutdown", &mysql.MySQLError{Number: 1053}, true},
		{"lock wait timeout", &mysql.MySQLError{Number: 1205}, true},
		{"deadlock", &mysql.MySQLError{Number: 1213}, true},
		{"server gone away", &mysql.MySQLError{Number: 2006}, true},
		{"lost connection", &mysql.MySQLError{Number: 2013}, true},

		// Deterministic failures: replaying them only wastes the caller's time.
		{"no rows", sql.ErrNoRows, false},
		{"syntax error", &mysql.MySQLError{Number: 1064}, false},
		{"unknown column", &mysql.MySQLError{Number: 1054}, false},
		{"access denied", &mysql.MySQLError{Number: 1045}, false},
		{"unknown table", &mysql.MySQLError{Number: 1146}, false},
		{"plain error", errors.New("boom"), false},

		// The caller gave up. Retrying would ignore their decision.
		{"context canceled", context.Canceled, false},
		{"context deadline", context.DeadlineExceeded, false},
		{"wrapped canceled", fmt.Errorf("fetch: %w", context.Canceled), false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isTransient(tc.err); got != tc.want {
				t.Fatalf("isTransient(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

func TestWithRetrySucceedsOnFirstAttempt(t *testing.T) {
	calls := 0
	err := withRetry(context.Background(), func() error {
		calls++
		return nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if calls != 1 {
		t.Fatalf("calls = %d, want 1", calls)
	}
}

func TestWithRetryRecoversFromTransientError(t *testing.T) {
	calls := 0
	err := withRetry(context.Background(), func() error {
		calls++
		if calls < 3 {
			return driver.ErrBadConn
		}
		return nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if calls != 3 {
		t.Fatalf("calls = %d, want 3", calls)
	}
}

func TestWithRetryGivesUpAfterMaxAttempts(t *testing.T) {
	calls := 0
	err := withRetry(context.Background(), func() error {
		calls++
		return driver.ErrBadConn
	})
	if !errors.Is(err, driver.ErrBadConn) {
		t.Fatalf("err = %v, want driver.ErrBadConn", err)
	}
	if calls != maxQueryAttempts {
		t.Fatalf("calls = %d, want %d", calls, maxQueryAttempts)
	}
}

func TestWithRetryDoesNotRetryDeterministicError(t *testing.T) {
	calls := 0
	want := &mysql.MySQLError{Number: 1064, Message: "syntax error"}
	err := withRetry(context.Background(), func() error {
		calls++
		return want
	})
	if !errors.Is(err, want) {
		t.Fatalf("err = %v, want %v", err, want)
	}
	if calls != 1 {
		t.Fatalf("calls = %d, want 1", calls)
	}
}

func TestWithRetryStopsWhenContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	calls := 0
	err := withRetry(ctx, func() error {
		calls++
		cancel() // the caller walks away while we are backing off
		return driver.ErrBadConn
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if calls != 1 {
		t.Fatalf("calls = %d, want 1", calls)
	}
}

func TestWithRetryBacksOffBetweenAttempts(t *testing.T) {
	start := time.Now()
	calls := 0
	_ = withRetry(context.Background(), func() error {
		calls++
		return driver.ErrBadConn
	})
	// Two backoffs between three attempts: 50ms + 100ms.
	if elapsed := time.Since(start); elapsed < 140*time.Millisecond {
		t.Fatalf("elapsed = %v, want at least 140ms of backoff", elapsed)
	}
}

// timeoutErr is a net.Error that reports itself as a timeout.
type timeoutErr struct{}

func (timeoutErr) Error() string   { return "i/o timeout" }
func (timeoutErr) Timeout() bool   { return true }
func (timeoutErr) Temporary() bool { return true }
