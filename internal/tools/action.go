package tools

import (
	"context"
	"fmt"
	"slices"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/sgsoluciones/dolibarr-mcp/internal/mapper"
)

type ActionInput struct {
	Entity      string `json:"entity" jsonschema:"Entity type: proposals|orders|projects|purchases|shipments|receptions"`
	ID          int64  `json:"id" jsonschema:"Entity ID"`
	Action      string `json:"action" jsonschema:"Action: validate|close|settodraft|setinvoiced|approve|makeorder|receive"`
	WarehouseID int64  `json:"warehouse_id,omitempty" jsonschema:"Warehouse ID (for stock-related actions)"`
	Status      int    `json:"status,omitempty" jsonschema:"Sign status for proposal 'close': 2=accepted/signed, 3=refused. Required when closing a proposal."`
	Note        string `json:"note,omitempty" jsonschema:"Optional private note recorded with the action"`
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
	switch {
	case input.Action == "close" && input.Entity == "proposals":
		// Dolibarr requires the sign status to close a proposal: 2=accepted, 3=refused.
		if input.Status != 2 && input.Status != 3 {
			return nil, WriteOutput{}, fmt.Errorf("closing a proposal requires status 2 (accepted/signed) or 3 (refused)")
		}
		m := map[string]any{"status": input.Status}
		if input.Note != "" {
			m["note_private"] = input.Note
		}
		payload = m
	case input.WarehouseID > 0:
		payload = map[string]any{"idwarehouse": input.WarehouseID}
	default:
		payload = map[string]any{"notrigger": 0}
	}

	result, err := d.API.Post(ctx, endpoint, payload)
	if err != nil {
		return writeError(fmt.Sprintf("%s %s/%d", input.Action, input.Entity, input.ID), err)
	}

	return nil, WriteOutput{
		Success: true,
		Entity:  input.Entity,
		ID:      input.ID,
		Action:  input.Action,
		Result:  parseResult(result),
	}, nil
}
