package tools

import (
	"sort"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/sgsoluciones/dolibarr-mcp/internal/dolapi"
	"github.com/sgsoluciones/dolibarr-mcp/internal/doldb"
	"github.com/sgsoluciones/dolibarr-mcp/internal/mapper"
)

// Deps holds shared dependencies for all tool handlers
type Deps struct {
	DB  *doldb.DB
	API *dolapi.Client
}

// actionEnums returns the sorted, unique entities and verbs supported by the
// state-change tool, derived from the single ValidActions() source of truth.
func actionEnums() (entities, verbs []any) {
	va := mapper.ValidActions()
	ents := make([]string, 0, len(va))
	verbSet := map[string]bool{}
	for ent, vs := range va {
		ents = append(ents, ent)
		for _, v := range vs {
			verbSet[v] = true
		}
	}
	sort.Strings(ents)
	vs := make([]string, 0, len(verbSet))
	for v := range verbSet {
		vs = append(vs, v)
	}
	sort.Strings(vs)
	return anySlice(ents), anySlice(vs)
}

func Register(server *mcp.Server, deps *Deps) {
	entityEnum := anySlice(mapper.ValidEntities())
	lineEntityEnum := anySlice([]string{"proposals", "orders", "purchases"})
	actionEntityEnum, actionVerbEnum := actionEnums()

	mcp.AddTool(server, &mcp.Tool{
		Name:        "dolibarr_search",
		Description: "Search any Dolibarr entity with filters. Entities: customers, products, proposals, projects, orders, purchases, warehouses, shipments, receptions. Supports text search, date range, customer filter, status filter, amount range. Returns compact list.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
		InputSchema: inputSchema[SearchInput](map[string][]any{"entity": entityEnum}),
	}, deps.HandleSearch)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "dolibarr_get",
		Description: "Get full details of a Dolibarr entity by ID or ref, including lines for documents (proposals, orders, purchases). Returns complete entity with all fields.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
		InputSchema: inputSchema[GetInput](map[string][]any{"entity": entityEnum}),
	}, deps.HandleGet)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "dolibarr_create",
		Annotations: &mcp.ToolAnnotations{DestructiveHint: boolPtr(false)},
		InputSchema: inputSchema[CreateInput](map[string][]any{"entity": entityEnum}),
		Description: `Create a new entity in Dolibarr. Uses friendly field names mapped automatically.

THREE reference fields, do NOT confuse them:
1. Document number (Dolibarr "ref", e.g. OF26073159): NEVER set it — auto-generated. Any 'ref' you send is ignored on create.
2. proposalkit_ref (ProposalKit tracking id): optional, set only if you have it.
3. customer_ref (native "Ref. cliente" = the customer's order number / "OC XXX", often filled in later): user data — set it when known. A duplicate lives in extrafields.ref_cliente.

For proposals/orders, ALWAYS fill ALL header fields:
- customer_id (REQUIRED — create fails without it), date, validity_end, delivery_date
- payment_term_id, payment_mode_id, availability_id (delivery time)
- shipping_method_id, source_id (demand reason), incoterms_id, incoterms_location
- note_public, note_private

Extrafields (custom fields) — use "extrafields": {"field_name": "value"}:
- Proposals: tos_attached (REQUIRED, values: "NoCgv"|"TOS.pdf"|"DSERRANO_CONDICIONES COMERCIALES S&G.pdf"), asunto (subject text), ref_cliente, jira_key, jira_url
- Orders: tos_attached (same values as proposals), ref_cliente

Include 'lines' array. Each line 'description' MUST be in HTML format and be detailed — explain what the item/service covers, its scope, specs, and relevant details. Use <h3> for title, <p> for paragraphs, <ul>/<ol> for lists, <strong> for emphasis, <table> for data. NEVER use plain text — always HTML. Also: qty, unit_price, vat_rate, product_type (0=product, 1=service). Optional: product_id, discount_percent, unit_id.`,
	}, deps.HandleCreate)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "dolibarr_update",
		Description: "Update an existing Dolibarr entity by ID. Pass only the fields to change.",
		Annotations: &mcp.ToolAnnotations{DestructiveHint: boolPtr(true), IdempotentHint: true},
		InputSchema: inputSchema[UpdateInput](map[string][]any{"entity": entityEnum}),
	}, deps.HandleUpdate)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "dolibarr_delete",
		Description: "Delete a Dolibarr entity by ID.",
		Annotations: &mcp.ToolAnnotations{DestructiveHint: boolPtr(true), IdempotentHint: true},
		InputSchema: inputSchema[DeleteInput](map[string][]any{"entity": entityEnum}),
	}, deps.HandleDelete)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "dolibarr_line",
		Annotations: &mcp.ToolAnnotations{DestructiveHint: boolPtr(true)},
		InputSchema: inputSchema[LineInput](map[string][]any{
			"entity": lineEntityEnum,
			"action": anySlice([]string{"add", "update", "delete"}),
		}),
		Description: `Manage lines on proposals, orders, or purchases. Actions: add, update, delete.
To edit or delete a SPECIFIC line: first call dolibarr_get on the document, read the target line's id from lines[].id, then pass it as line_id here. update/delete require line_id.
For add/update provide: description (MUST be HTML format — use <h3> for title, <p>, <ul>, <strong>, <table> etc. Be detailed about scope, specs, deliverables), qty, unit_price, vat_rate, product_type (0=product, 1=service). Optional: product_id, discount_percent, unit_id.
For extrafields on lines: use "extrafields": {"field_name": "value"}.`,
	}, deps.HandleLine)

	mcp.AddTool(server, &mcp.Tool{
		Name: "dolibarr_pep_budget",
		Description: `Load or replace the PEP budget of a project (sgcosting module). This is the figure the purchase orders are later compared against, so writing is deliberate by design.

mode=preview (DEFAULT, and what happens when mode is omitted): runs the rehearsal server-side and WRITES NOTHING. Returns what would be created, updated and removed, plus manual edits and charges that would be left orphaned. Always run this first.
mode=apply: writes. On a project that already has a budget it also needs replace=true.

If the budget carries manual edits, or the reload would leave charges without an element, the server answers 409 and applies nothing. That answer comes back as a readable report (conflict=true, preview=...), not as a failure — show it to a person. Only then resend the same call adding confirm_replace=true.

Each element accepts 13 columns. Required: code, level, label. Optional: parent_code, chapter, center_code, brand_ref, fk_product, unit, qty, cost_unit, price_unit, source. Any other key is rejected before sending. level starts at 1; every element above level 1 needs a parent_code that exists in the same payload.`,
		Annotations: &mcp.ToolAnnotations{DestructiveHint: boolPtr(true)},
		InputSchema: inputSchema[PEPBudgetInput](map[string][]any{
			"mode": anySlice([]string{"preview", "apply"}),
		}),
	}, deps.HandlePEPBudget)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "dolibarr_action",
		Description: "Change state of a Dolibarr document. Actions by entity — proposals: validate, close, settodraft, setinvoiced; orders: validate, close; projects: validate; purchases: validate, approve, makeorder, receive; shipments/receptions: validate, close. To SIGN/APPROVE a proposal, first 'validate' it, then 'close' it WITH status=2 (accepted/signed) or status=3 (refused) — closing a proposal requires the status field.",
		Annotations: &mcp.ToolAnnotations{DestructiveHint: boolPtr(true)},
		InputSchema: inputSchema[ActionInput](map[string][]any{
			"entity": actionEntityEnum,
			"action": actionVerbEnum,
		}),
	}, deps.HandleAction)
}
