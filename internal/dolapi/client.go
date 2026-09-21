package dolapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/sgsoluciones/dolibarr-mcp/internal/config"
)

type Client struct {
	http    *http.Client
	baseURL string
	apiKey  string

	// onWrite runs after any successful non-GET request. It is the single
	// choke point that clears the read cache, so a new write tool cannot
	// forget to invalidate: everything already goes through Do.
	onWrite func()
}

// OnWrite registers the callback invoked after every successful write. Wiring
// it here rather than in each handler means the invalidation cannot drift out
// of sync with the set of write tools.
func (c *Client) OnWrite(fn func()) {
	c.onWrite = fn
}

// invalidate fires the write hook for methods that can change Dolibarr data.
func (c *Client) invalidate(method string) {
	if c.onWrite == nil || method == http.MethodGet || method == http.MethodHead {
		return
	}
	c.onWrite()
}

type APIError struct {
	StatusCode int
	Message    string
	Raw        string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("dolibarr api %d: %s", e.StatusCode, e.Message)
}

func New(cfg *config.Config) *Client {
	return &Client{
		http: &http.Client{
			Timeout: 30 * time.Second,
			Transport: &http.Transport{
				MaxIdleConns:        10,
				MaxIdleConnsPerHost: 10,
				IdleConnTimeout:     90 * time.Second,
				TLSHandshakeTimeout: 10 * time.Second,
				ForceAttemptHTTP2:   false,
			},
		},
		baseURL: strings.TrimRight(cfg.APIUrl, "/"),
		apiKey:  cfg.APIKey,
	}
}

func (c *Client) Do(ctx context.Context, method, endpoint string, body any) (json.RawMessage, error) {
	url := c.baseURL + "/" + strings.TrimLeft(endpoint, "/")

	var bodyBytes []byte
	if body != nil {
		var err error
		bodyBytes, err = json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("marshal body: %w", err)
		}
	}

	// Only idempotent methods are retried. POST is not idempotent — retrying a
	// create that may have already succeeded (timeout/502 after the record was
	// written) would duplicate proposals, orders, or lines.
	maxAttempts := 3
	if method == http.MethodPost {
		maxAttempts = 1
	}

	var lastErr error
	for attempt := 0; attempt < maxAttempts; attempt++ {
		if attempt > 0 {
			// Context-aware backoff: abort immediately if the caller cancelled.
			select {
			case <-time.After(time.Duration(attempt) * 500 * time.Millisecond):
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}

		var reqBody io.Reader
		if bodyBytes != nil {
			reqBody = bytes.NewReader(bodyBytes)
		}

		req, err := http.NewRequestWithContext(ctx, method, url, reqBody)
		if err != nil {
			return nil, fmt.Errorf("create request: %w", err)
		}

		req.Header.Set("DOLAPIKEY", c.apiKey)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json")
		req.Header.Set("User-Agent", "PrismaMCP/2.0")

		if bodyBytes != nil {
			req.ContentLength = int64(len(bodyBytes))
			req.Header.Set("Content-Length", strconv.Itoa(len(bodyBytes)))
		}

		resp, err := c.http.Do(req)
		if err != nil {
			lastErr = err
			continue
		}

		respBody, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			lastErr = err
			continue
		}

		if resp.StatusCode >= 500 && attempt < maxAttempts-1 {
			lastErr = &APIError{StatusCode: resp.StatusCode, Message: string(respBody), Raw: string(respBody)}
			continue
		}

		if resp.StatusCode >= 400 {
			msg := string(respBody)
			var parsed map[string]any
			if json.Unmarshal(respBody, &parsed) == nil {
				if m, ok := parsed["error"].(string); ok {
					msg = m
				} else if m, ok := parsed["message"].(string); ok {
					msg = m
				}
			}
			return nil, &APIError{StatusCode: resp.StatusCode, Message: msg, Raw: string(respBody)}
		}

		// The write landed: anything the read cache is holding may now be out
		// of date. Invalidate before the caller can issue its next read.
		c.invalidate(method)

		return json.RawMessage(respBody), nil
	}

	return nil, fmt.Errorf("after retries: %w", lastErr)
}

func (c *Client) Get(ctx context.Context, endpoint string) (json.RawMessage, error) {
	return c.Do(ctx, http.MethodGet, endpoint, nil)
}

func (c *Client) Post(ctx context.Context, endpoint string, body any) (json.RawMessage, error) {
	return c.Do(ctx, http.MethodPost, endpoint, body)
}

func (c *Client) Put(ctx context.Context, endpoint string, body any) (json.RawMessage, error) {
	return c.Do(ctx, http.MethodPut, endpoint, body)
}

func (c *Client) Delete(ctx context.Context, endpoint string) (json.RawMessage, error) {
	return c.Do(ctx, http.MethodDelete, endpoint, nil)
}
