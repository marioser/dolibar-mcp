package tools

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/sgsoluciones/dolibarr-mcp/internal/dolapi"
	"github.com/sgsoluciones/dolibarr-mcp/internal/mapper"
	"github.com/sgsoluciones/dolibarr-mcp/internal/response"
)

// WriteOutput is the structured result of a write operation. The upstream API
// response is carried in Result as decoded JSON (not a re-encoded string), so
// the tool output reaches the client as a single, un-escaped JSON object.
type WriteOutput struct {
	Success  bool   `json:"success"`
	Entity   string `json:"entity,omitempty"`
	ID       int64  `json:"id,omitempty"`
	Action   string `json:"action,omitempty"`
	ParentID int64  `json:"parent_id,omitempty"`
	LineID   int64  `json:"line_id,omitempty"`
	// The jsonschema description is load-bearing — see GetOutput.
	Result any `json:"result,omitempty" jsonschema:"Upstream Dolibarr API response as decoded JSON (object, array or scalar)"`
}

// parseResult decodes a raw API JSON response into a Go value so it embeds as
// real JSON in the tool output. Falls back to the raw string if it is not JSON.
func parseResult(raw json.RawMessage) any {
	if len(raw) == 0 {
		return nil
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return string(raw)
	}
	return v
}

// validateEntity returns an actionable error (listing valid entities) when the
// entity is not supported, so the LLM can self-correct instead of the request
// hitting the API with a garbage path.
func validateEntity(entity string) error {
	if !mapper.IsValidEntity(entity) {
		return fmt.Errorf("invalid entity %q. Valid entities: %v", entity, mapper.ValidEntities())
	}
	return nil
}

// writeError turns a write failure into a tool error result. When the cause is
// a Dolibarr API error, it surfaces the status code and upstream message as
// structured JSON so the client can distinguish 400 (validation) from 404 (bad
// id) from 401 (auth) and self-correct. Non-API errors fall back to a wrapped
// Go error (which the SDK still packs as a tool error).
func writeError(operation string, err error) (*mcp.CallToolResult, WriteOutput, error) {
	var apiErr *dolapi.APIError
	if errors.As(err, &apiErr) {
		content := response.ToJSON(map[string]any{
			"error":       true,
			"operation":   operation,
			"status_code": apiErr.StatusCode,
			"message":     apiErr.Message,
		})
		return &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{&mcp.TextContent{Text: content}},
		}, WriteOutput{}, nil
	}
	return nil, WriteOutput{}, fmt.Errorf("%s: %w", operation, err)
}
