package tools

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/sgsoluciones/dolibarr-mcp/internal/mapper"
	"github.com/sgsoluciones/dolibarr-mcp/internal/response"
)

type DeleteInput struct {
	Entity string `json:"entity" jsonschema:"description=Entity to delete"`
	ID     int64  `json:"id" jsonschema:"description=Entity ID to delete"`
}

func (d *Deps) HandleDelete(ctx context.Context, req *mcp.CallToolRequest, input DeleteInput) (*mcp.CallToolResult, WriteOutput, error) {
	path := fmt.Sprintf("%s/%d", mapper.EntityToAPIPath(input.Entity), input.ID)

	result, err := d.API.Delete(ctx, path)
	if err != nil {
		return nil, WriteOutput{}, fmt.Errorf("delete %s/%d: %w", input.Entity, input.ID, err)
	}

	return nil, WriteOutput{Result: response.ToJSON(map[string]any{
		"success": true,
		"entity":  input.Entity,
		"id":      input.ID,
		"result":  string(result),
	})}, nil
}
