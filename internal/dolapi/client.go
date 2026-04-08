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

	var lastErr error
	for attempt := range 3 {
		if attempt > 0 {
			time.Sleep(time.Duration(attempt) * 500 * time.Millisecond)
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

		if resp.StatusCode >= 500 && attempt < 2 {
			lastErr = &APIError{StatusCode: resp.StatusCode, Raw: string(respBody)}
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
