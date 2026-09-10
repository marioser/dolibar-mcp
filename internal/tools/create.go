package tools

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/sgsoluciones/dolibarr-mcp/internal/mapper"
)

type CreateInput struct {
	Entity string         `json:"entity" jsonschema:"Entity to create: customers|products|proposals|projects|tasks|orders|purchases|warehouses|shipments|receptions"`
	Data   map[string]any `json:"data" jsonschema:"Entity data with friendly names. Proposals REQUIRE customer_id and extrafields.tos_attached (one of: NoCgv | TOS.pdf | 'DSERRANO_CONDICIONES COMERCIALES S&G.pdf'). NEVER set 'ref' — the OF number is auto-generated. Reference fields you MAY set: customer_ref (customer's order/OC number) and proposalkit_ref (ProposalKit id). Header: date, validity_end, delivery_date, payment_term_id, payment_mode_id, availability_id, shipping_method_id, source_id, incoterms_id, note_public, note_private. extrafields (object): tos_attached, ref_cliente, asunto, jira_key, jira_url. lines (array): each with HTML description (h3/p/ul/strong/table), qty, unit_price, vat_rate, product_type (0=product,1=service)."`
}

func (d *Deps) HandleCreate(ctx context.Context, req *mcp.CallToolRequest, input CreateInput) (*mcp.CallToolResult, WriteOutput, error) {
	if err := validateEntity(input.Entity); err != nil {
		return nil, WriteOutput{}, err
	}

	// Let Dolibarr auto-number the document; validate the create contract before
	// mapping friendly names to Dolibarr fields.
	stripAutoNumberRef(input.Entity, input.Data)
	if err := validateCreate(input.Entity, input.Data); err != nil {
		return nil, WriteOutput{}, err
	}

	path := mapper.EntityToAPIPath(input.Entity)
	payload := mapper.MapEntityToDolibarr(input.Entity, input.Data)

	result, err := d.API.Post(ctx, path, payload)
	if err != nil {
		return writeError("create "+input.Entity, err)
	}

	return nil, WriteOutput{
		Success: true,
		Entity:  input.Entity,
		Result:  parseResult(result),
	}, nil
}
