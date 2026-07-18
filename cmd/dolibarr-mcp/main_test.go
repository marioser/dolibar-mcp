package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAuthMiddleware(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	h := authMiddleware("s3cret", next)

	cases := []struct {
		name       string
		header     string
		wantStatus int
	}{
		{"valid token", "Bearer s3cret", http.StatusOK},
		{"wrong token", "Bearer nope", http.StatusUnauthorized},
		{"missing bearer prefix", "s3cret", http.StatusUnauthorized},
		{"empty header", "", http.StatusUnauthorized},
		{"prefix of token", "Bearer s3cre", http.StatusUnauthorized},
		{"token with suffix", "Bearer s3crett", http.StatusUnauthorized},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/mcp", nil)
			if c.header != "" {
				req.Header.Set("Authorization", c.header)
			}
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code != c.wantStatus {
				t.Errorf("status = %d, want %d", rec.Code, c.wantStatus)
			}
		})
	}
}
