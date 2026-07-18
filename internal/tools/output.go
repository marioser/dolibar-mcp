package tools

import (
	"encoding/json"
	"fmt"

	"github.com/sgsoluciones/dolibarr-mcp/internal/mapper"
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
	Result   any    `json:"result,omitempty"`
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
