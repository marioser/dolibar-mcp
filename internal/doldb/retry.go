package doldb

import (
	"context"
	"database/sql/driver"
	"errors"
	"io"
	"net"
	"syscall"
	"time"

	"github.com/go-sql-driver/mysql"
)

const (
	// maxQueryAttempts bounds how many times a read is replayed. Three attempts
	// with the backoff below spend at most ~150ms before giving up, which stays
	// well inside a tool call's budget while covering the common case: a pooled
	// connection that died since it was last used.
	maxQueryAttempts = 3

	// retryBaseBackoff is doubled on each attempt: 50ms, then 100ms.
	retryBaseBackoff = 50 * time.Millisecond
)

// transientMySQLErrors are server-side conditions that clear on their own. A
// SELECT replayed after any of them returns the same rows it would have the
// first time, so retrying is safe — this package only ever reads (writes go
// through the REST API in internal/dolapi).
var transientMySQLErrors = map[uint16]bool{
	1040: true, // ER_CON_COUNT_ERROR — too many connections
	1053: true, // ER_SERVER_SHUTDOWN — shutting down mid-query
	1205: true, // ER_LOCK_WAIT_TIMEOUT
	1213: true, // ER_LOCK_DEADLOCK
	2006: true, // CR_SERVER_GONE_ERROR
	2013: true, // CR_SERVER_LOST
}

// isTransient reports whether err is a connection-level or temporary server
// failure worth replaying. Deterministic failures (bad SQL, missing table,
// denied credentials, no rows) and caller cancellation are never retried:
// replaying them only delays the error the caller already earned.
func isTransient(err error) bool {
	if err == nil {
		return false
	}

	// The caller gave up or ran out of time. Honour that over any retry policy,
	// and check it first — a cancelled context surfaces as a torn connection.
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}

	if errors.Is(err, driver.ErrBadConn) || errors.Is(err, mysql.ErrInvalidConn) {
		return true
	}

	// The server hung up mid-read.
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return true
	}

	if errors.Is(err, syscall.ECONNRESET) ||
		errors.Is(err, syscall.ECONNREFUSED) ||
		errors.Is(err, syscall.EPIPE) ||
		errors.Is(err, syscall.ETIMEDOUT) {
		return true
	}

	var netErr net.Error
	if errors.As(err, &netErr) {
		return true
	}

	var myErr *mysql.MySQLError
	if errors.As(err, &myErr) {
		return transientMySQLErrors[myErr.Number]
	}

	return false
}

// withRetry runs fn, replaying it with exponential backoff while it fails with
// a transient error. It returns the last error seen, or the context error if
// the caller cancelled while we were backing off.
func withRetry(ctx context.Context, fn func() error) error {
	var lastErr error

	for attempt := 0; attempt < maxQueryAttempts; attempt++ {
		if attempt > 0 {
			backoff := retryBaseBackoff * time.Duration(1<<(attempt-1))
			select {
			case <-time.After(backoff):
			case <-ctx.Done():
				return ctx.Err()
			}
		}

		lastErr = fn()
		if !isTransient(lastErr) {
			return lastErr
		}

		// A transient failure is only worth replaying while the caller is still
		// waiting for the answer.
		if ctx.Err() != nil {
			return ctx.Err()
		}
	}

	return lastErr
}
