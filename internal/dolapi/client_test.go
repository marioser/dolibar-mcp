package dolapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sgsoluciones/dolibarr-mcp/internal/config"
)

func newTestClient(url string) *Client {
	return New(&config.Config{APIUrl: url, APIKey: "test"})
}

// POST must NOT be retried — retrying a non-idempotent create risks duplicates.
func TestDo_PostNotRetriedOn5xx(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	_, err := c.Post(context.Background(), "proposals", map[string]any{"x": 1})
	if err == nil {
		t.Fatal("expected error on 500")
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("POST was attempted %d times, want exactly 1 (no retry)", got)
	}
}

// GET is idempotent and SHOULD be retried on 5xx.
func TestDo_GetRetriedOn5xx(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			w.WriteHeader(http.StatusBadGateway) // first attempt fails
			return
		}
		w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	body, err := c.Get(context.Background(), "proposals/1")
	if err != nil {
		t.Fatalf("GET should have recovered on retry: %v", err)
	}
	if got := calls.Load(); got != 2 {
		t.Errorf("GET was attempted %d times, want 2", got)
	}
	if string(body) != `{"ok":true}` {
		t.Errorf("unexpected body: %s", body)
	}
}

// A cancelled context must abort the backoff wait instead of sleeping it out.
func TestDo_BackoffRespectsContext(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	c := newTestClient(srv.URL)
	start := time.Now()
	_, err := c.Get(ctx, "proposals/1") // idempotent → enters backoff after first 500
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected context error")
	}
	// Full backoff would be ~500ms+; context cancels at 50ms, so we must return well before that.
	if elapsed > 400*time.Millisecond {
		t.Errorf("backoff ignored context cancellation: took %v", elapsed)
	}
}
