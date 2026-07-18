package response

import (
	"encoding/json"
	"testing"
	"time"
)

func TestToJSON_Compact(t *testing.T) {
	got := ToJSON(map[string]any{"a": 1, "b": "x"})
	var back map[string]any
	if err := json.Unmarshal([]byte(got), &back); err != nil {
		t.Fatalf("ToJSON produced invalid JSON: %q (%v)", got, err)
	}
	if back["b"] != "x" {
		t.Errorf("roundtrip mismatch: %#v", back)
	}
}

func TestToJSON_RawMessageNotDoubleEncoded(t *testing.T) {
	// A json.RawMessage must be embedded as real JSON, not an escaped string.
	// This guards the Phase 1 fix against regressing into string(result).
	raw := json.RawMessage(`{"id":42}`)
	got := ToJSON(map[string]any{"result": raw})

	var back struct {
		Result struct {
			ID int `json:"id"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(got), &back); err != nil {
		t.Fatalf("raw message was not embedded as JSON object: %q (%v)", got, err)
	}
	if back.Result.ID != 42 {
		t.Errorf("nested id not readable: %q", got)
	}
}

func TestFormatDate(t *testing.T) {
	d := time.Date(2024, 3, 9, 15, 4, 5, 0, time.UTC)
	if got := FormatDate(&d); got != "2024-03-09" {
		t.Errorf("FormatDate = %q, want 2024-03-09", got)
	}
	if got := FormatDate(nil); got != "" {
		t.Errorf("nil date should be empty, got %q", got)
	}
	var zero time.Time
	if got := FormatDate(&zero); got != "" {
		t.Errorf("zero date should be empty, got %q", got)
	}
}

func TestFormatMoney(t *testing.T) {
	if got := FormatMoney(1234.5, "COP"); got != "1234.50 COP" {
		t.Errorf("FormatMoney = %q, want 1234.50 COP", got)
	}
	if got := FormatMoney(10, ""); got != "10.00 USD" {
		t.Errorf("empty currency should default to USD, got %q", got)
	}
}

func TestStatusName(t *testing.T) {
	cases := []struct {
		entity string
		code   int
		want   string
	}{
		{"proposals", 2, "Signed"},
		{"orders", -1, "Cancelled"},
		{"purchases", 5, "Received completely"},
		{"proposals", 99, "Unknown(99)"},
		{"nonexistent", 1, "Unknown(1)"},
	}
	for _, c := range cases {
		if got := StatusName(c.entity, c.code); got != c.want {
			t.Errorf("StatusName(%q,%d) = %q, want %q", c.entity, c.code, got, c.want)
		}
	}
}
