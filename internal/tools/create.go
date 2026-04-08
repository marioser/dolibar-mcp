package tools

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/sgsoluciones/dolibarr-mcp/internal/mapper"
	"github.com/sgsoluciones/dolibarr-mcp/internal/response"
)

type CreateInput struct {
	Entity string         `json:"entity" jsonschema:"Entity to create: customers|products|proposals|projects|orders|purchases|warehouses|shipments|receptions"`
	Data   map[string]any `json:"data" jsonschema:"Entity data with friendly names. For proposals: customer_id, date, validity_end, delivery_date, payment_term_id, payment_mode_id, availability_id, shipping_method_id, source_id, incoterms_id, note_public, note_private, extrafields (object for custom fields), lines (array with HTML description, qty, unit_price, vat_rate, product_type)."`
}

type WriteOutput struct {
	Result string `json:"result" jsonschema:"JSON result"`
}

func (d *Deps) HandleCreate(ctx context.Context, req *mcp.CallToolRequest, input CreateInput) (*mcp.CallToolResult, WriteOutput, error) {
	path := mapper.EntityToAPIPath(input.Entity)
	payload := mapper.MapToDolibarr(input.Data)

	result, err := d.API.Post(ctx, path, payload)
	if err != nil {
		return nil, WriteOutput{}, fmt.Errorf("create %s: %w", input.Entity, err)
	}

	return nil, WriteOutput{Result: response.ToJSON(map[string]any{
		"success": true,
		"entity":  input.Entity,
		"result":  string(result),
	})}, nil
}
