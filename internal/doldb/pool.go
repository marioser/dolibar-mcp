package doldb

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"
	"time"

	_ "github.com/go-sql-driver/mysql"

	"github.com/sgsoluciones/dolibarr-mcp/internal/config"
)

const (
	// verifyTimeout bounds the startup and health-check pings. It only has to
	// cover a round-trip to the database, not a real query.
	verifyTimeout = 5 * time.Second

	// dolConfigTTL is how long the Dolibarr constants are trusted before they
	// are read again. Long enough that no tool call pays for the extra query,
	// short enough that a change in llx_const takes effect without a restart.
	dolConfigTTL = 5 * time.Minute

	// dolConfigRetryAfter is the shorter TTL used when the load failed and we
	// are serving defaults: keep trying, but not on every single tool call.
	dolConfigRetryAfter = 15 * time.Second
)

type DB struct {
	*sql.DB
	cfg *config.Config

	mu       sync.Mutex
	dolCfg   *DolConfig
	dolCfgAt time.Time
	dolCfgOK bool
}

// New opens the connection pool. It does NOT require the database to be
// reachable: sql.Open is lazy, and a Dolibarr that is briefly down must not
// take the MCP server down with it. The server is long-lived and launched by
// the MCP client, so exiting here would leave the tools dead for the rest of
// the session even after the database came back. Callers check reachability
// with Verify and report it as a warning.
func New(cfg *config.Config) (*DB, error) {
	db, err := sql.Open("mysql", cfg.DSN())
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}

	db.SetMaxOpenConns(cfg.DBMaxOpenConns)
	db.SetMaxIdleConns(cfg.DBMaxIdleConns)
	// Recycling connections well before MySQL's own wait_timeout is what keeps
	// stale sockets out of the pool in the first place; the retry layer is the
	// net for the ones that still slip through.
	db.SetConnMaxLifetime(cfg.DBConnMaxLifetime)
	db.SetConnMaxIdleTime(cfg.DBConnMaxIdleTime)

	return &DB{DB: db, cfg: cfg}, nil
}

// Verify checks that the database answers and that the Dolibarr constants can
// be read. It is safe to call at any time, and callers may treat a failure as
// a warning rather than a fatal error.
func (d *DB) Verify(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, verifyTimeout)
	defer cancel()

	if err := withRetry(ctx, func() error { return d.DB.PingContext(ctx) }); err != nil {
		return fmt.Errorf("ping db: %w", err)
	}

	cfg, err := d.loadDolConfig(ctx)
	if err != nil {
		return fmt.Errorf("load dolibarr config: %w", err)
	}

	d.mu.Lock()
	d.dolCfg, d.dolCfgAt, d.dolCfgOK = cfg, time.Now(), true
	d.mu.Unlock()

	return nil
}

// DolConfig returns the cached Dolibarr constants, reloading them when the TTL
// has expired. It never returns nil and never blocks on a dead database for
// longer than verifyTimeout: if the reload fails, the last known values (or
// the built-in defaults) are served and the reload is retried sooner.
func (d *DB) DolConfig(ctx context.Context) *DolConfig {
	d.mu.Lock()
	cached, at, ok := d.dolCfg, d.dolCfgAt, d.dolCfgOK
	d.mu.Unlock()

	ttl := dolConfigTTL
	if !ok {
		ttl = dolConfigRetryAfter
	}
	if cached != nil && time.Since(at) < ttl {
		return cached
	}

	loadCtx, cancel := context.WithTimeout(ctx, verifyTimeout)
	defer cancel()

	fresh, err := d.loadDolConfig(loadCtx)

	d.mu.Lock()
	defer d.mu.Unlock()
	if err != nil {
		// Back off before trying again, but keep serving something usable.
		d.dolCfgAt, d.dolCfgOK = time.Now(), false
		if d.dolCfg == nil {
			d.dolCfg = defaultDolConfig()
		}
		return d.dolCfg
	}
	d.dolCfg, d.dolCfgAt, d.dolCfgOK = fresh, time.Now(), true
	return fresh
}

// queryContext gives a read a deadline of its own when the caller did not set
// a tighter one. Without it an MCP tool call inherits an open-ended context
// and waits on the database for as long as the client is willing to hold.
func (d *DB) queryContext(ctx context.Context) (context.Context, context.CancelFunc) {
	timeout := d.cfg.QueryTimeout
	if timeout <= 0 {
		return context.WithCancel(ctx)
	}
	if deadline, ok := ctx.Deadline(); ok && time.Until(deadline) <= timeout {
		return context.WithCancel(ctx)
	}
	return context.WithTimeout(ctx, timeout)
}

// Healthy reports whether the database currently answers a ping.
func (d *DB) Healthy(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, verifyTimeout)
	defer cancel()
	return d.DB.PingContext(ctx)
}

// QueryContext shadows the embedded *sql.DB method so every read in this
// package goes through the retry layer. database/sql only replays a query
// itself when it can prove the connection was dead *before* the query was
// written; a connection that dies mid-flight surfaces as a hard error, which
// is exactly the failure this wrapper absorbs.
//
// Note the deadline is the caller's: cancelling here would close the returned
// rows. Tool handlers set the per-call deadline, and the DSN's readTimeout is
// the low-level backstop.
func (d *DB) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	var rows *sql.Rows
	err := withRetry(ctx, func() error {
		var qErr error
		rows, qErr = d.DB.QueryContext(ctx, query, args...)
		return qErr
	})
	if err != nil {
		return nil, d.describe(err)
	}
	return rows, nil
}

// QueryRowContext shadows the embedded method and returns a Row whose Scan
// carries the same retry policy. *sql.Row defers its error to Scan, so the
// replay has to happen there.
func (d *DB) QueryRowContext(ctx context.Context, query string, args ...any) *Row {
	return &Row{db: d, ctx: ctx, query: query, args: args}
}

// Row mirrors the part of *sql.Row this package uses, with retries.
type Row struct {
	db    *DB
	ctx   context.Context
	query string
	args  []any
}

func (r *Row) Scan(dest ...any) error {
	err := withRetry(r.ctx, func() error {
		return r.db.DB.QueryRowContext(r.ctx, r.query, r.args...).Scan(dest...)
	})
	return r.db.describe(err)
}

// describe turns a connection failure into a message that says what to check,
// instead of the driver's bare "invalid connection". Everything else — no
// rows, bad SQL, cancellation — is passed through untouched so callers can
// still match on it.
func (d *DB) describe(err error) error {
	if err == nil || !isTransient(err) {
		return err
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return fmt.Errorf(
		"cannot reach the Dolibarr database at %s:%d (database %q) after %d attempts — check that MySQL is up and reachable from this host: %w",
		d.cfg.DBHost, d.cfg.DBPort, d.cfg.DBName, maxQueryAttempts, err,
	)
}

func (d *DB) Prefix() string {
	return d.cfg.DBPrefix
}

func (d *DB) Entity() int {
	return d.cfg.Entity
}

func (d *DB) T(table string) string {
	return d.cfg.T(table)
}
