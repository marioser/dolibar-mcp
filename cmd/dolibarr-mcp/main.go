package main

import (
	"context"
	"crypto/subtle"
	"encoding/json"
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

// version is the single source of truth for what this binary reports.
const version = "2.4.2"

func authMiddleware(token string, next http.Handler) http.Handler {
	expected := []byte(token)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		presented, ok := strings.CutPrefix(auth, "Bearer ")
		// Constant-time compare avoids leaking the token via response timing.
		if !ok || subtle.ConstantTimeCompare([]byte(presented), expected) != 1 {
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
		// Only an unusable DSN reaches here — nothing a retry would fix.
		fmt.Fprintf(os.Stderr, "database error: %v\n", err)
		os.Exit(1)
	}
	defer db.Close()

	// A database that is briefly unreachable must NOT take the server down.
	// This process is launched by the MCP client and lives for the whole
	// session: exiting here used to leave every tool dead until the user
	// restarted the client, even after Dolibarr came back seconds later.
	if err := db.Verify(context.Background()); err != nil {
		fmt.Fprintf(os.Stderr,
			"WARNING: database %s@%s:%d is not reachable yet (%v)\n"+
				"         the server is starting anyway and will connect on the first tool call\n",
			cfg.DBName, cfg.DBHost, cfg.DBPort, err)
	} else {
		fmt.Fprintf(os.Stderr, "connected to database %s (entity=%d, currency=%s)\n",
			cfg.DBName, cfg.Entity, db.DolConfig(context.Background()).MainCurrency)
	}

	apiClient := dolapi.New(cfg)

	server := mcp.NewServer(
		&mcp.Implementation{
			Name:    "dolibarr-mcp",
			Version: version,
		},
		nil,
	)

	deps := &tools.Deps{DB: db, API: apiClient}
	tools.Register(server, deps)

	fmt.Fprintf(os.Stderr, "dolibarr-mcp v%s ready (transport=%s, 9 tools)\n", version, cfg.Transport)

	if cfg.Transport == "http" {
		addr := fmt.Sprintf(":%d", cfg.HTTPPort)

		handler := mcp.NewStreamableHTTPHandler(func(r *http.Request) *mcp.Server {
			return server
		}, nil)

		mux := http.NewServeMux()
		// The health check pings the database. A server that answers 200 while
		// every tool fails is worse than one that reports itself unhealthy:
		// orchestrators need the difference to restart or route around it.
		mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			if err := db.Healthy(r.Context()); err != nil {
				w.WriteHeader(http.StatusServiceUnavailable)
				body, _ := json.Marshal(map[string]string{
					"status":  "degraded",
					"version": version,
					"error":   "database unreachable: " + err.Error(),
				})
				w.Write(body)
				return
			}
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"status":"ok","version":"` + version + `","database":"ok"}`))
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
