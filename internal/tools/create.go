package tools

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/sgsoluciones/dolibarr-mcp/internal/mapper"
)

type CreateInput struct {
	Entity string         `json:"entity" jsonschema:"Entity to create: customers|products|proposals|projects|orders|purchases|warehouses|shipments|receptions"`
	Data   map[string]any `json:"data" jsonschema:"Entity data with friendly names. For proposals: customer_id, date, validity_end, delivery_date, payment_term_id, payment_mode_id, availability_id, shipping_method_id, source_id, incoterms_id, note_public, note_private, extrafields (object for custom fields), lines (array with HTML description, qty, unit_price, vat_rate, product_type)."`
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
	payload := mapper.MapToDolibarr(input.Data)

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
