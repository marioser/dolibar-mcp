package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/sgsoluciones/dolibarr-mcp/internal/config"
	"github.com/sgsoluciones/dolibarr-mcp/internal/dolapi"
	"github.com/sgsoluciones/dolibarr-mcp/internal/doldb"
	"github.com/sgsoluciones/dolibarr-mcp/internal/tools"
)

func authMiddleware(token string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if auth == "" || !strings.HasPrefix(auth, "Bearer ") || strings.TrimPrefix(auth, "Bearer ") != token {
			http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func main() {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "config error: %v\n", err)
		os.Exit(1)
	}

	db, err := doldb.New(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "database error: %v\n", err)
		os.Exit(1)
	}
	defer db.Close()

	fmt.Fprintf(os.Stderr, "connected to database %s (entity=%d, currency=%s)\n",
		cfg.DBName, cfg.Entity, db.DolConfig().MainCurrency)

	apiClient := dolapi.New(cfg)

	server := mcp.NewServer(
		&mcp.Implementation{
			Name:    "dolibarr-mcp",
			Version: "2.0.0",
		},
		nil,
	)

	deps := &tools.Deps{DB: db, API: apiClient}
	tools.Register(server, deps)

	fmt.Fprintf(os.Stderr, "dolibarr-mcp v2.0.0 ready (transport=%s, 7 tools)\n", cfg.Transport)

	if cfg.Transport == "http" {
		addr := fmt.Sprintf(":%d", cfg.HTTPPort)

		handler := mcp.NewStreamableHTTPHandler(func(r *http.Request) *mcp.Server {
			return server
		}, nil)

		mux := http.NewServeMux()
		mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"status":"ok","version":"2.0.0"}`))
		})

		if cfg.AuthToken != "" {
			fmt.Fprintf(os.Stderr, "auth: Bearer token required\n")
			mux.Handle("/mcp", authMiddleware(cfg.AuthToken, handler))
			mux.Handle("/mcp/", authMiddleware(cfg.AuthToken, handler))
		} else {
			fmt.Fprintf(os.Stderr, "auth: WARNING - no MCP_AUTH_TOKEN set, server is OPEN\n")
			mux.Handle("/mcp", handler)
			mux.Handle("/mcp/", handler)
		}

		fmt.Fprintf(os.Stderr, "listening on %s\n", addr)
		if err := http.ListenAndServe(addr, mux); err != nil {
			log.Fatalf("http server error: %v", err)
		}
	} else {
		if err := server.Run(context.Background(), &mcp.StdioTransport{}); err != nil {
			log.Fatalf("server error: %v", err)
		}
	}
}
