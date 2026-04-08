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
		Description: "Create a new entity in Dolibarr. For proposals/orders: include 'lines' array with description, qty, unit_price, vat_rate, product_id. Uses friendly field names (customer_id, product_id, vat_rate).",
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
		Description: "Manage lines on proposals, orders, or purchases. Actions: add, update, delete. For add/update provide: description, qty, unit_price, vat_rate, product_id, discount_percent, product_type (0=product, 1=service), unit_id.",
	}, deps.HandleLine)

	mcp.AddTool(server, &mcp.Tool{
		Name: "dolibarr_action",
		Description: "Change state of a Dolibarr document. Actions by entity — proposals: validate, close, settodraft, setinvoiced; orders: validate, close; projects: validate; purchases: validate, approve, makeorder, receive; shipments/receptions: validate, close.",
	}, deps.HandleAction)
}
