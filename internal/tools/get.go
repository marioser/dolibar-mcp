package tools

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type GetInput struct {
	Entity string `json:"entity" jsonschema:"Entity type: customers|products|proposals|projects|orders|purchases|warehouses|shipments|receptions"`
	ID     int64  `json:"id,omitempty" jsonschema:"Entity ID"`
	Ref    string `json:"ref,omitempty" jsonschema:"Entity reference (alternative to ID)"`
}

// GetOutput carries the entity as decoded JSON so it reaches the client as a
// single un-escaped object rather than a JSON string.
type GetOutput struct {
	Result any `json:"result"`
}

func (d *Deps) HandleGet(ctx context.Context, req *mcp.CallToolRequest, input GetInput) (*mcp.CallToolResult, GetOutput, error) {
	if err := validateEntity(input.Entity); err != nil {
		return nil, GetOutput{}, err
	}
	if input.ID == 0 && input.Ref == "" {
		return nil, GetOutput{}, fmt.Errorf("either id or ref is required")
	}

	result, err := d.DB.Fetch(ctx, input.Entity, input.ID, input.Ref)
	if err != nil {
		return nil, GetOutput{}, fmt.Errorf("get %s: %w", input.Entity, err)
	}

	return nil, GetOutput{Result: result}, nil
}
