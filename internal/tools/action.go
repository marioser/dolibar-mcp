package tools

import (
	"context"
	"fmt"
	"slices"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/sgsoluciones/dolibarr-mcp/internal/mapper"
	"github.com/sgsoluciones/dolibarr-mcp/internal/response"
)

type ActionInput struct {
	Entity      string `json:"entity" jsonschema:"required,description=Entity type: proposals|orders|projects|purchases|shipments|receptions"`
	ID          int64  `json:"id" jsonschema:"required,description=Entity ID"`
	Action      string `json:"action" jsonschema:"required,description=Action: validate|close|settodraft|setinvoiced|approve|makeorder|receive"`
	WarehouseID int64  `json:"warehouse_id,omitempty" jsonschema:"description=Warehouse ID (for stock-related actions)"`
}

func (d *Deps) HandleAction(ctx context.Context, req *mcp.CallToolRequest, input ActionInput) (*mcp.CallToolResult, WriteOutput, error) {
	validActions := mapper.ValidActions()
	entityActions, ok := validActions[input.Entity]
	if !ok {
		return nil, WriteOutput{}, fmt.Errorf("entity %s does not support state actions", input.Entity)
	}

	if !slices.Contains(entityActions, input.Action) {
		return nil, WriteOutput{}, fmt.Errorf("invalid action '%s' for %s. Valid: %v", input.Action, input.Entity, entityActions)
	}

	apiPath := mapper.EntityToAPIPath(input.Entity)
	endpoint := fmt.Sprintf("%s/%d/%s", apiPath, input.ID, input.Action)

	var payload any
	if input.WarehouseID > 0 {
		payload = map[string]any{"idwarehouse": input.WarehouseID}
	} else {
		payload = map[string]any{"notrigger": 0}
	}

	result, err := d.API.Post(ctx, endpoint, payload)
	if err != nil {
		return nil, WriteOutput{}, fmt.Errorf("%s %s/%d: %w", input.Action, input.Entity, input.ID, err)
	}

	return nil, WriteOutput{Result: response.ToJSON(map[string]any{
		"success": true,
		"entity":  input.Entity,
		"id":      input.ID,
		"action":  input.Action,
		"result":  string(result),
	})}, nil
}
