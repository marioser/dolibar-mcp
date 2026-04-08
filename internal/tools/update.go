package tools

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/sgsoluciones/dolibarr-mcp/internal/mapper"
	"github.com/sgsoluciones/dolibarr-mcp/internal/response"
)

type UpdateInput struct {
	Entity string         `json:"entity" jsonschema:"Entity to update"`
	ID     int64          `json:"id" jsonschema:"Entity ID to update"`
	Data   map[string]any `json:"data" jsonschema:"Fields to update (only changed fields needed)"`
}

func (d *Deps) HandleUpdate(ctx context.Context, req *mcp.CallToolRequest, input UpdateInput) (*mcp.CallToolResult, WriteOutput, error) {
	path := fmt.Sprintf("%s/%d", mapper.EntityToAPIPath(input.Entity), input.ID)
	payload := mapper.MapToDolibarr(input.Data)

	result, err := d.API.Put(ctx, path, payload)
	if err != nil {
		return nil, WriteOutput{}, fmt.Errorf("update %s/%d: %w", input.Entity, input.ID, err)
	}

	return nil, WriteOutput{Result: response.ToJSON(map[string]any{
		"success": true,
		"entity":  input.Entity,
		"id":      input.ID,
		"result":  string(result),
	})}, nil
}
