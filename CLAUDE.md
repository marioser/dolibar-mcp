# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

An MCP (Model Context Protocol) server that exposes a Dolibarr ERP instance to LLM
clients through 8 tools. Written in Go (`go 1.25.0`), module `github.com/sgsoluciones/dolibarr-mcp`.

## Commands

```bash
# Build the binary
go build -o dolibarr-mcp ./cmd/dolibarr-mcp

# Run (needs a configured .env or exported env vars — see Configuration)
go run ./cmd/dolibarr-mcp

# Vet / format
go vet ./...
gofmt -l .

# Docker build (multi-stage, static binary → alpine). captain-definition targets this for CapRover.
docker build -t dolibarr-mcp .
```

Run the tests with `go test -count=1 ./...`. The `-count=1` matters: Go caches results,
and a pass can be reported from cache after an edit that should have invalidated it.

Two suites are gated behind env vars and skipped by default:

```bash
# Read layer against a real Dolibarr database
DOLIBARR_IT=1 go test ./internal/doldb/

# End-to-end writes against a real Dolibarr API — creates documents,
# so point it at a disposable instance, never production
DOLIBARR_E2E=1 go test ./internal/tools/
```

## The core architecture: read/write split (CQRS-style)

This is the single most important thing to understand, and it is not obvious from any
one file. The server talks to Dolibarr through **two completely different channels**
depending on whether the operation reads or writes:

- **Reads** go straight to the **MySQL database** (`internal/doldb`). `dolibarr_search`
  → `DB.Search`, `dolibarr_get` → `DB.Fetch`. This is fast and avoids the REST API's
  serialization overhead for list/detail views.
- **Writes** go through the **Dolibarr REST API** (`internal/dolapi`). `dolibarr_create`,
  `dolibarr_update`, `dolibarr_delete`, `dolibarr_line`, `dolibarr_action` all call
  `d.API.*`. Writes MUST go through the API so Dolibarr's business logic, triggers, and
  numbering run — never write to the DB directly.

`tools.Deps{DB, API}` is the seam: every handler receives both, and which one it uses
tells you whether it is a read or a write path.

## Package map

| Package | Responsibility |
|---------|----------------|
| `cmd/dolibarr-mcp` | Entry point. Wires config → DB → API → MCP server. Chooses stdio vs HTTP transport, mounts `/mcp` (+ Bearer auth) and `/health`. |
| `internal/config` | Env-based config. `DSN()` builds the MySQL DSN; `T(table)` prefixes table names with `DB_PREFIX` (default `llx_`). |
| `internal/doldb` | Read layer over MySQL. `Search`, `Fetch`, and `loadDolConfig` (reads Dolibarr `const` settings at startup — multicompany, multiprice, stock mode, main currency). |
| `internal/dolapi` | Write layer: thin REST client with `DOLAPIKEY` header, retry on 5xx (3 attempts), structured `APIError`. |
| `internal/mapper` | Translation layer between the MCP-facing "friendly" field names and Dolibarr's internal names/paths. |
| `internal/tools` | The 7 MCP tool handlers + `registry.go` (tool names + descriptions). |
| `internal/response` | Output formatting to compact JSON strings. |

## The mapper is the contract boundary — edit it deliberately

`internal/mapper/fields.go` is where the LLM-friendly vocabulary meets Dolibarr's
internal one. When adding fields or entities, this is almost always the file to touch:

- **`MapToDolibarr`** rewrites friendly keys → Dolibarr keys (`customer_id`→`socid`,
  `unit_price`→`subprice`, `description`→`desc`, …), converts date strings in
  `dateFields` to **Unix timestamps** (Dolibarr's API expects epoch ints, not
  `YYYY-MM-DD`), maps `extrafields` → `array_options` with an `options_` prefix per key,
  and recurses into `lines`.
- **`EntityToAPIPath`** maps entity names → REST paths (note the non-obvious ones:
  `customers`→`thirdparties`, `purchases`→`supplierorders`).
- **`EntityToLinePath`** — proposals use the **singular** `line` sub-resource for adds,
  everything else uses `lines`.
- **`ValidEntities` / `ValidActions`** are the source of truth for what's supported.

If a create/update silently drops a field, the alias is missing here first. Field-name
mismatches between DB columns and REST property names are a recurring class of bug —
the API assigns request keys directly onto PHP object properties, which differ from DB
column names.

## Line descriptions must be HTML

Tool descriptions in `registry.go` enforce a hard project rule: line `description`
fields on proposals/orders/purchases MUST be **HTML** (`<h3>`, `<p>`, `<ul>`, `<strong>`,
`<table>`), never plain text, and must be detailed (scope, specs, deliverables).
Dolibarr renders this HTML in documents. Keep this constraint intact when editing tool
descriptions or examples.

## Multi-entity, per-entity config

Dolibarr supports multi-company ("entities"). `DOLIBARR_ENTITY` selects one. At startup
`loadDolConfig` reads that entity's settings from the `const` table (queried as
`entity IN (0, <entity>)` so global rows apply). Queries that touch entity-scoped data
must respect the configured entity and prefix — use `DB.T(table)` and `DB.Entity()`,
never hardcode `llx_` or entity `1`.

## Configuration (env vars)

Loaded in `internal/config/config.go` via `godotenv` (a local `.env` is auto-loaded).
Required: `DB_PASS`, `DOLIBARR_API_URL`, `DOLIBARR_API_KEY` — startup fails without them.

| Var | Default | Purpose |
|-----|---------|---------|
| `DB_HOST` / `DB_PORT` / `DB_NAME` / `DB_USER` / `DB_PASS` | localhost / 3306 / dolibarr / dolibarr / — | MySQL read connection |
| `DB_PREFIX` | `llx_` | Table prefix |
| `DOLIBARR_API_URL` / `DOLIBARR_API_KEY` | — | REST write endpoint + key |
| `DOLIBARR_ENTITY` | `1` | Multicompany entity id |
| `MCP_TRANSPORT` | `stdio` | `stdio` or `http` |
| `MCP_HTTP_PORT` | `8080` | HTTP listen port |
| `MCP_AUTH_TOKEN` | — | Bearer token for `/mcp`. **If unset, the HTTP server is OPEN** (logged as a warning). |

## Transports

Default transport is `stdio` (for local MCP clients). For containerized/hosted
deployments (CapRover), set `MCP_TRANSPORT=http` — this is required or the web deploy
has nothing to serve. In HTTP mode the container must expose the listener on `8080`
(the `EXPOSE`d port); a mismatched upstream port surfaces as a 502 behind a proxy.
`/health` returns `{"status":"ok",...}` and is unauthenticated.
