package tools

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/sgsoluciones/dolibarr-mcp/internal/mapper"
)

type DeleteInput struct {
	Entity string `json:"entity" jsonschema:"Entity to delete"`
	ID     int64  `json:"id" jsonschema:"Entity ID to delete"`
}

func (d *Deps) HandleDelete(ctx context.Context, req *mcp.CallToolRequest, input DeleteInput) (*mcp.CallToolResult, WriteOutput, error) {
	if err := validateEntity(input.Entity); err != nil {
		return nil, WriteOutput{}, err
	}

	path := fmt.Sprintf("%s/%d", mapper.EntityToAPIPath(input.Entity), input.ID)

	result, err := d.API.Delete(ctx, path)
	if err != nil {
		return writeError(fmt.Sprintf("delete %s/%d", input.Entity, input.ID), err)
	}

	return nil, WriteOutput{
		Success: true,
		Entity:  input.Entity,
		ID:      input.ID,
		Result:  parseResult(result),
	}, nil
}
