package tools

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/sgsoluciones/dolibarr-mcp/internal/mapper"
	"github.com/sgsoluciones/dolibarr-mcp/internal/response"
)

type LineInput struct {
	Action   string         `json:"action" jsonschema:"Line action: add|update|delete"`
	Entity   string         `json:"entity" jsonschema:"Parent entity: proposals|orders|purchases"`
	ParentID int64          `json:"parent_id" jsonschema:"Parent document ID"`
	LineID   int64          `json:"line_id,omitempty" jsonschema:"Line ID (required for update and delete)"`
	Data     map[string]any `json:"data,omitempty" jsonschema:"Line data: description/qty/unit_price/vat_rate/product_id/discount_percent/product_type/unit_id"`
}

func (d *Deps) HandleLine(ctx context.Context, req *mcp.CallToolRequest, input LineInput) (*mcp.CallToolResult, WriteOutput, error) {
	apiPath := mapper.EntityToAPIPath(input.Entity)

	switch input.Action {
	case "add":
		if input.Data == nil {
			return nil, WriteOutput{}, fmt.Errorf("data is required for add action")
		}
		payload := mapper.MapToDolibarr(input.Data)
		linePath := mapper.EntityToLinePath(input.Entity)
		endpoint := fmt.Sprintf("%s/%d/%s", apiPath, input.ParentID, linePath)

		result, err := d.API.Post(ctx, endpoint, payload)
		if err != nil {
			return nil, WriteOutput{}, fmt.Errorf("add line to %s/%d: %w", input.Entity, input.ParentID, err)
		}
		return nil, WriteOutput{Result: response.ToJSON(map[string]any{
			"success":   true,
			"action":    "add",
			"parent_id": input.ParentID,
			"result":    string(result),
		})}, nil

	case "update":
		if input.LineID == 0 {
			return nil, WriteOutput{}, fmt.Errorf("line_id is required for update action")
		}
		if input.Data == nil {
			return nil, WriteOutput{}, fmt.Errorf("data is required for update action")
		}
		payload := mapper.MapToDolibarr(input.Data)
		endpoint := fmt.Sprintf("%s/%d/lines/%d", apiPath, input.ParentID, input.LineID)

		result, err := d.API.Put(ctx, endpoint, payload)
		if err != nil {
			return nil, WriteOutput{}, fmt.Errorf("update line %d on %s/%d: %w", input.LineID, input.Entity, input.ParentID, err)
		}
		return nil, WriteOutput{Result: response.ToJSON(map[string]any{
			"success":   true,
			"action":    "update",
			"parent_id": input.ParentID,
			"line_id":   input.LineID,
			"result":    string(result),
		})}, nil

	case "delete":
		if input.LineID == 0 {
			return nil, WriteOutput{}, fmt.Errorf("line_id is required for delete action")
		}
		endpoint := fmt.Sprintf("%s/%d/lines/%d", apiPath, input.ParentID, input.LineID)

		result, err := d.API.Delete(ctx, endpoint)
		if err != nil {
			return nil, WriteOutput{}, fmt.Errorf("delete line %d from %s/%d: %w", input.LineID, input.Entity, input.ParentID, err)
		}
		return nil, WriteOutput{Result: response.ToJSON(map[string]any{
			"success":   true,
			"action":    "delete",
			"parent_id": input.ParentID,
			"line_id":   input.LineID,
			"result":    string(result),
		})}, nil

	default:
		return nil, WriteOutput{}, fmt.Errorf("invalid action: %s (expected add, update, or delete)", input.Action)
	}
}
