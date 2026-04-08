package tools

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/sgsoluciones/dolibarr-mcp/internal/mapper"
	"github.com/sgsoluciones/dolibarr-mcp/internal/response"
)

type CreateInput struct {
	Entity string         `json:"entity" jsonschema:"required,description=Entity to create: customers|products|proposals|projects|orders|purchases|warehouses|shipments|receptions"`
	Data   map[string]any `json:"data" jsonschema:"required,description=Entity data. Use friendly names: customer_id/product_id/vat_rate/unit_price/discount_percent. For docs include lines array."`
}

type WriteOutput struct {
	Result string `json:"result" jsonschema:"description=JSON result"`
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
