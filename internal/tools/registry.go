package tools

import (
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/sgsoluciones/dolibarr-mcp/internal/dolapi"
	"github.com/sgsoluciones/dolibarr-mcp/internal/doldb"
)

// Deps holds shared dependencies for all tool handlers
type Deps struct {
	DB  *doldb.DB
	API *dolapi.Client
}

func Register(server *mcp.Server, deps *Deps) {
	mcp.AddTool(server, &mcp.Tool{
		Name: "dolibarr_search",
		Description: "Search any Dolibarr entity with filters. Entities: customers, products, proposals, projects, orders, purchases, warehouses, shipments, receptions. Supports text search, date range, customer filter, status filter, amount range. Returns compact list.",
	}, deps.HandleSearch)

	mcp.AddTool(server, &mcp.Tool{
		Name: "dolibarr_get",
		Description: "Get full details of a Dolibarr entity by ID or ref, including lines for documents (proposals, orders, purchases). Returns complete entity with all fields.",
	}, deps.HandleGet)

	mcp.AddTool(server, &mcp.Tool{
		Name: "dolibarr_create",
		Description: `Create a new entity in Dolibarr. Uses friendly field names mapped automatically.

For proposals/orders, ALWAYS fill ALL header fields:
- customer_id (required), date, validity_end, delivery_date
- payment_term_id, payment_mode_id, availability_id (delivery time)
- shipping_method_id, source_id (demand reason), incoterms_id, incoterms_location
- note_public, note_private

Extrafields (custom fields) — use "extrafields": {"field_name": "value"}:
- Proposals: tos_attached (REQUIRED, values: "NoCgv"|"TOS.pdf"|"DSERRANO_CONDICIONES COMERCIALES S&G.pdf"), asunto (subject text), ref_cliente, jira_key, jira_url
- Orders: tos_attached (same values as proposals), ref_cliente

Include 'lines' array. Each line MUST have a detailed 'description' that clearly explains what the item/service covers, its scope, and relevant details — NOT just a short label. Also: qty, unit_price, vat_rate, product_type (0=product, 1=service). Optional: product_id, discount_percent, unit_id.`,
	}, deps.HandleCreate)

	mcp.AddTool(server, &mcp.Tool{
		Name: "dolibarr_update",
		Description: "Update an existing Dolibarr entity by ID. Pass only the fields to change.",
	}, deps.HandleUpdate)

	mcp.AddTool(server, &mcp.Tool{
		Name: "dolibarr_delete",
		Description: "Delete a Dolibarr entity by ID.",
	}, deps.HandleDelete)

	mcp.AddTool(server, &mcp.Tool{
		Name: "dolibarr_line",
		Description: `Manage lines on proposals, orders, or purchases. Actions: add, update, delete.
For add/update provide: description (MUST be detailed and descriptive, not just a label — explain what the item/service covers), qty, unit_price, vat_rate, product_type (0=product, 1=service). Optional: product_id, discount_percent, unit_id.
For extrafields on lines: use "extrafields": {"field_name": "value"}.`,
	}, deps.HandleLine)

	mcp.AddTool(server, &mcp.Tool{
		Name: "dolibarr_action",
		Description: "Change state of a Dolibarr document. Actions by entity — proposals: validate, close, settodraft, setinvoiced; orders: validate, close; projects: validate; purchases: validate, approve, makeorder, receive; shipments/receptions: validate, close.",
	}, deps.HandleAction)
}
