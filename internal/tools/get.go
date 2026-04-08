package tools

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/sgsoluciones/dolibarr-mcp/internal/response"
)

type GetInput struct {
	Entity string `json:"entity" jsonschema:"Entity type: customers|products|proposals|projects|orders|purchases|warehouses|shipments|receptions"`
	ID     int64  `json:"id,omitempty" jsonschema:"Entity ID"`
	Ref    string `json:"ref,omitempty" jsonschema:"Entity reference (alternative to ID)"`
}

type GetOutput struct {
	Result string `json:"result" jsonschema:"JSON entity details"`
}

func (d *Deps) HandleGet(ctx context.Context, req *mcp.CallToolRequest, input GetInput) (*mcp.CallToolResult, GetOutput, error) {
	if input.ID == 0 && input.Ref == "" {
		return nil, GetOutput{}, fmt.Errorf("either id or ref is required")
	}

	result, err := d.DB.Fetch(ctx, input.Entity, input.ID, input.Ref)
	if err != nil {
		return nil, GetOutput{}, fmt.Errorf("get %s: %w", input.Entity, err)
	}

	return nil, GetOutput{Result: response.ToJSON(result)}, nil
}
