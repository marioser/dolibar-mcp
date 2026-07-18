package tools

import (
	"encoding/json"
	"testing"
)

func TestParseResult(t *testing.T) {
	// JSON object decodes to a map (embeds as real JSON, not an escaped string)
	v := parseResult(json.RawMessage(`{"id":42}`))
	m, ok := v.(map[string]any)
	if !ok {
		t.Fatalf("expected map, got %T", v)
	}
	if m["id"] != float64(42) {
		t.Errorf("id = %v, want 42", m["id"])
	}

	// bare number
	if got := parseResult(json.RawMessage(`7`)); got != float64(7) {
		t.Errorf("number = %v, want 7", got)
	}

	// empty input → nil
	if got := parseResult(nil); got != nil {
		t.Errorf("empty = %v, want nil", got)
	}

	// non-JSON falls back to raw string
	if got := parseResult(json.RawMessage(`not json`)); got != "not json" {
		t.Errorf("fallback = %v, want raw string", got)
	}
}

func TestValidateEntity(t *testing.T) {
	if err := validateEntity("proposals"); err != nil {
		t.Errorf("valid entity rejected: %v", err)
	}
	if err := validateEntity("garbage"); err == nil {
		t.Error("invalid entity accepted, expected error")
	}
}

// TestWriteOutputSingleEncoded guards the Phase 1 fix: the API result must embed
// as a nested JSON object, never as an escaped string field.
func TestWriteOutputSingleEncoded(t *testing.T) {
	out := WriteOutput{
		Success: true,
		Entity:  "proposals",
		Result:  parseResult(json.RawMessage(`{"id":99}`)),
	}
	data, err := json.Marshal(out)
	if err != nil {
		t.Fatal(err)
	}
	var back struct {
		Result struct {
			ID int `json:"id"`
		} `json:"result"`
	}
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatalf("result was not a nested object: %s (%v)", data, err)
	}
	if back.Result.ID != 99 {
		t.Errorf("nested id unreadable: %s", data)
	}
}
