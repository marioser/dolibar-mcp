package dolapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sgsoluciones/dolibarr-mcp/internal/config"
)

func clientFor(t *testing.T, h http.HandlerFunc) (*Client, *int) {
	t.Helper()

	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)

	c := New(&config.Config{APIUrl: srv.URL, APIKey: "test-key"})

	calls := 0
	c.OnWrite(func() { calls++ })
	return c, &calls
}

func okHandler(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"id":1}`))
}

func TestWriteInvalidatesTheReadCache(t *testing.T) {
	// A create, update or state change makes any cached read suspect.
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete} {
		t.Run(method, func(t *testing.T) {
			c, calls := clientFor(t, okHandler)

			if _, err := c.Do(context.Background(), method, "proposals/1", nil); err != nil {
				t.Fatalf("%s: %v", method, err)
			}
			if *calls != 1 {
				t.Fatalf("%s fired the hook %d times, want 1", method, *calls)
			}
		})
	}
}

func TestReadDoesNotInvalidateTheReadCache(t *testing.T) {
	// Throwing the cache away on every GET would defeat the point of having it.
	c, calls := clientFor(t, okHandler)

	if _, err := c.Get(context.Background(), "proposals/1"); err != nil {
		t.Fatalf("get: %v", err)
	}
	if *calls != 0 {
		t.Fatalf("a GET fired the invalidation hook %d times, want 0", *calls)
	}
}

func TestFailedWriteDoesNotInvalidate(t *testing.T) {
	// A rejected write changed nothing, so the cache is still accurate.
	c, calls := clientFor(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error":"customer_id is required"}`))
	})

	if _, err := c.Post(context.Background(), "proposals", map[string]any{}); err == nil {
		t.Fatal("the request succeeded, want a 400")
	}
	if *calls != 0 {
		t.Fatalf("a failed write fired the hook %d times, want 0", *calls)
	}
}

func TestClientWithoutHookDoesNotPanic(t *testing.T) {
	// Tests and any caller that does not care about caching build the client
	// without a hook.
	srv := httptest.NewServer(http.HandlerFunc(okHandler))
	defer srv.Close()

	c := New(&config.Config{APIUrl: srv.URL, APIKey: "test-key"})
	if _, err := c.Post(context.Background(), "proposals", map[string]any{}); err != nil {
		t.Fatalf("post: %v", err)
	}
}
