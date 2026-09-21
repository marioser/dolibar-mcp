# dolibarr-mcp

An MCP server that gives an LLM client real, working access to a [Dolibarr](https://www.dolibarr.org/) ERP — search it, read it, and write to it through nine tools, without the model ever touching SQL or the REST API directly.

Written in Go, single static binary, no runtime dependencies.

```
Claude Code / Claude Desktop / any MCP client
                  │
                  │  MCP (stdio or HTTP)
                  ▼
           dolibarr-mcp
             ╱       ╲
    reads  ╱           ╲  writes
          ▼             ▼
   MySQL (direct)   Dolibarr REST API
```

## Why reads and writes go different ways

This is the one design decision that explains everything else, so it is worth a paragraph.

**Reads go straight to the MySQL database.** Listing two hundred proposals through the REST API means two hundred PHP object hydrations; the same query against the database is one round trip. For the browse-and-read half of the work — which is most of what an LLM does — the API is pure overhead.

**Writes go through the Dolibarr REST API, always.** Dolibarr's business logic lives in PHP: document numbering, triggers, stock movements, workflow state. A write straight to the database skips all of it and quietly corrupts your ERP. There is no shortcut here and the server does not offer one.

That split is also what makes the read cache safe to have: every write in this server passes through one place, so there is exactly one point that needs to invalidate the cache. See [Operating it](#operating-it).

## What you get

Nine tools across ten entity types (`customers`, `products`, `proposals`, `projects`, `tasks`, `orders`, `purchases`, `warehouses`, `shipments`, `receptions`):

| Tool | What it does |
|------|--------------|
| `dolibarr_search` | Search any entity. Filters: free text, customer, status, date range, amount range, with limit/offset. Returns a compact list. |
| `dolibarr_get` | Full detail of one entity by id or ref, including nested lines for documents. |
| `dolibarr_create` | Create an entity using friendly field names (`customer_id`, `unit_price`, `delivery_date`…) that are translated to Dolibarr's internal ones. |
| `dolibarr_update` | Update an entity. Same friendly vocabulary. |
| `dolibarr_delete` | Delete an entity by id. |
| `dolibarr_line` | Add, update or delete lines on proposals, orders and purchases. |
| `dolibarr_action` | Change document state — validate, close, approve, receive. Per entity: proposals (`validate`, `close`, `settodraft`, `setinvoiced`), orders (`validate`, `close`), projects (`validate`), purchases (`validate`, `approve`, `makeorder`, `receive`), shipments and receptions (`validate`, `close`). |
| `dolibarr_document` | Attach a file to a document, or list what is already attached. |
| `dolibarr_pep_budget` | Load or replace a project's PEP budget (the `sgcosting` module). Defaults to a server-side dry run that writes nothing. |

Two behaviours are worth knowing before you use the write tools:

- **Signing a proposal is two calls, not one.** `validate` it first, then `close` it **with** `status=2` (accepted) or `status=3` (refused). Closing a proposal without a status will not do what you meant.
- **Line descriptions must be HTML.** `<h3>`, `<p>`, `<ul>`, `<strong>`, `<table>`. Dolibarr renders that HTML into the generated PDF, so plain text produces a document that looks broken to your customer. The tool descriptions enforce this.

## Requirements

- Go 1.25+ (only to build — the result is a static binary)
- A Dolibarr instance you can reach two ways: its MySQL database (read access is enough) and its REST API with a key
- Dolibarr's REST API module enabled

## Install

```bash
git clone https://github.com/marioser/dolibar-mcp.git
cd dolibar-mcp
go build -o dolibarr-mcp ./cmd/dolibarr-mcp
```

## Configure

Copy `.env.example` to `.env` and fill it in. The binary loads `.env` from its working directory automatically; exported environment variables work too and take precedence.

Three values are required — the server refuses to start without them:

```bash
DB_PASS=...                                          # MySQL password
DOLIBARR_API_URL=https://erp.example.com/api/index.php
DOLIBARR_API_KEY=...                                 # Dolibarr API key
```

Everything else has a working default:

| Variable | Default | Purpose |
|---|---|---|
| `DB_HOST` / `DB_PORT` / `DB_NAME` / `DB_USER` | `localhost` / `3306` / `dolibarr` / `dolibarr` | MySQL read connection |
| `DB_PREFIX` | `llx_` | Table prefix |
| `DOLIBARR_ENTITY` | `1` | Multi-company entity id |
| `MCP_TRANSPORT` | `stdio` | `stdio` or `http` |
| `MCP_HTTP_PORT` | `8080` | HTTP listen port |
| `MCP_AUTH_TOKEN` | — | Bearer token for `/mcp`. **Leave it unset and the HTTP server is open to anyone who can reach it.** The server logs a warning, but it will start. |

Pool sizes, network timeouts and the read cache are tunable too — `.env.example` documents each one with its default. You do not need to touch them to get started.

## Connect an MCP client

### Claude Code / Claude Desktop (stdio)

The binary reads `.env` from its working directory, so the cleanest wiring is a one-line wrapper that `cd`s there first. This keeps your secrets out of any config file you might commit:

```bash
#!/usr/bin/env bash
cd "$(dirname "$0")" || exit 1
exec ./dolibarr-mcp
```

Save it next to the binary as `run-dolibarr-mcp.sh`, `chmod +x` it, and point your client at it:

```json
{
  "mcpServers": {
    "dolibarr": {
      "command": "/absolute/path/to/run-dolibarr-mcp.sh"
    }
  }
}
```

On startup the server writes to stderr what it connected to:

```
connected to database dolibarr (entity=1, currency=COP)
dolibarr-mcp v2.5.0 ready (transport=stdio, 9 tools)
```

### Hosted (HTTP)

Set `MCP_TRANSPORT=http` — without it a container has nothing to serve. Set `MCP_AUTH_TOKEN` as well unless the listener is genuinely private.

```bash
MCP_TRANSPORT=http MCP_HTTP_PORT=8080 MCP_AUTH_TOKEN=... ./dolibarr-mcp
```

- `POST /mcp` — the MCP endpoint, `Authorization: Bearer <token>` when a token is set
- `GET /health` — unauthenticated, see below

A `Dockerfile` (multi-stage, static binary on Alpine) and a `captain-definition` for CapRover are included. The container exposes `8080`; a proxy pointed at a different upstream port shows up as a 502.

## Operating it

### Health

`/health` pings the database. It does not merely report that the process is alive:

```bash
$ curl localhost:8080/health
{"cache":{"enabled":true,"ttl":"30s","entries":12,"max_entries":500,
          "hits":47,"misses":19,"hit_rate":0.71},
 "database":"ok","status":"ok","version":"2.5.0"}
```

When the database is unreachable it answers **503** with the reason, so an orchestrator can tell a healthy container from a live-but-useless one:

```json
{"error":"database unreachable: dial tcp ...","status":"degraded","version":"2.5.0"}
```

### A database outage does not kill the server

The process is launched by your MCP client and lives for the whole session, so it is built not to die on a blip:

- **Startup never exits on a database error.** The connection pool is lazy; unreachability is a warning on stderr and the first tool call connects.
- **Reads retry** — three attempts with backoff, on connection-level failures and on transient MySQL errors (too many connections, server shutdown, lock wait timeout, deadlock, gone away, lost connection). Bad SQL, a missing table, denied credentials and cancellation are never retried.
- **Everything is bounded** — dial, read and write timeouts on the connection, plus a deadline on each tool call.
- **Errors name the host.** You get `cannot reach the Dolibarr database at host:3306 (database "dolibarr")`, not the driver's bare `invalid connection`.

### The read cache

An LLM re-reads the same proposal and repeats the same search several times while it reasons. That duplicate load is entirely wasted, so reads are cached with a short TTL.

Correctness comes first at every point where the two could conflict:

- **The whole cache is dropped after every successful write**, hooked into the REST client rather than into each handler — every write already goes through one function, so a new write tool inherits the invalidation with nothing to remember.
- **A read that started before a write is returned to its caller but never stored**, because it is already a pre-write snapshot.
- **Errors are never cached.** One blip does not become thirty seconds of manufactured failures.
- **Identical concurrent reads collapse into one database round trip.**

Set `CACHE_TTL=0` to turn it off entirely.

## Development

```bash
go test -count=1 ./...     # -count=1 matters: Go caches passes
go vet ./...
gofmt -l .
```

Two suites are gated behind environment variables and skipped by default:

```bash
# Read layer against a real Dolibarr database
DOLIBARR_IT=1 go test ./internal/doldb/

# End-to-end writes against a real Dolibarr API. This CREATES DOCUMENTS —
# point it at a disposable instance, never at production.
DOLIBARR_E2E=1 go test ./internal/tools/
```

### Package map

| Package | Responsibility |
|---|---|
| `cmd/dolibarr-mcp` | Entry point. Wires config → DB → API → MCP server, picks the transport, mounts `/mcp` and `/health`. |
| `internal/config` | Environment-based config. Builds the MySQL DSN and prefixes table names. |
| `internal/doldb` | Read layer over MySQL: `Search`, `Fetch`, transient-failure retry, and the read cache. |
| `internal/dolapi` | Write layer: REST client with retry on 5xx, structured errors, and the cache-invalidation hook. |
| `internal/mapper` | Translation between the LLM-friendly vocabulary and Dolibarr's internal names and paths. |
| `internal/tools` | The nine tool handlers and the registry that describes them. |
| `internal/response` | Output formatting. |

### Before you add a field or an entity

`internal/mapper/fields.go` is the contract boundary, and it is where field bugs start. Dolibarr's REST API assigns request keys straight onto PHP object properties, and those names differ from the database column names — so a field can be accepted with `200 OK` and silently discarded.

Read [`docs/dolibarr-api-evidence.md`](docs/dolibarr-api-evidence.md) first. It records what the API actually does, measured with real requests rather than inferred. Two of its findings are silent failures. Guessing here is expensive.

## License

No license is declared for this repository, which under default copyright means all
rights are reserved. If you intend to share or reuse it, add a `LICENSE` file first.
