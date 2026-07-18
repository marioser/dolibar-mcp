package mapper

import (
	"reflect"
	"testing"
	"time"
)

func TestMapToDolibarr_Aliases(t *testing.T) {
	in := map[string]any{
		"customer_id":      42,
		"unit_price":       100.5,
		"description":      "hello",
		"vat_rate":         19,
		"discount_percent": 10,
		"unknown_field":    "kept",
	}
	out := MapToDolibarr(in)

	want := map[string]any{
		"socid":          42,
		"subprice":       100.5,
		"desc":           "hello",
		"tva_tx":         19,
		"remise_percent": 10,
		"unknown_field":  "kept",
	}
	if !reflect.DeepEqual(out, want) {
		t.Fatalf("alias mapping mismatch\n got: %#v\nwant: %#v", out, want)
	}
}

func TestMapToDolibarr_DateToTimestamp(t *testing.T) {
	const dateStr = "2024-01-15"
	expected, err := time.Parse("2006-01-02", dateStr)
	if err != nil {
		t.Fatal(err)
	}

	out := MapToDolibarr(map[string]any{
		"date":         dateStr,
		"validity_end": dateStr, // aliases to fin_validite (a dateField)
	})

	if got := out["date"]; got != expected.Unix() {
		t.Errorf("date: got %v, want %d", got, expected.Unix())
	}
	if got := out["fin_validite"]; got != expected.Unix() {
		t.Errorf("fin_validite: got %v, want %d", got, expected.Unix())
	}
}

func TestMapToDolibarr_DatePassthroughOnBadFormat(t *testing.T) {
	out := MapToDolibarr(map[string]any{"date": "not-a-date"})
	if got := out["date"]; got != "not-a-date" {
		t.Errorf("bad date should pass through unchanged, got %v", got)
	}

	// numeric timestamps must not be altered
	out = MapToDolibarr(map[string]any{"date": int64(1700000000)})
	if got := out["date"]; got != int64(1700000000) {
		t.Errorf("numeric date should pass through, got %v", got)
	}
}

func TestMapToDolibarr_Extrafields(t *testing.T) {
	out := MapToDolibarr(map[string]any{
		"extrafields": map[string]any{
			"tos_attached":     "TOS.pdf",
			"options_existing": "keep", // already prefixed, must not double-prefix
		},
	})

	if _, ok := out["extrafields"]; ok {
		t.Error("extrafields key should be removed after mapping")
	}
	opts, ok := out["array_options"].(map[string]any)
	if !ok {
		t.Fatalf("array_options missing or wrong type: %#v", out["array_options"])
	}
	if opts["options_tos_attached"] != "TOS.pdf" {
		t.Errorf("expected options_tos_attached, got %#v", opts)
	}
	if opts["options_existing"] != "keep" {
		t.Errorf("already-prefixed key must not be double-prefixed, got %#v", opts)
	}
}

func TestMapToDolibarr_RecursiveLines(t *testing.T) {
	out := MapToDolibarr(map[string]any{
		"lines": []any{
			map[string]any{"description": "line1", "unit_price": 5},
		},
	})
	lines, ok := out["lines"].([]any)
	if !ok || len(lines) != 1 {
		t.Fatalf("lines not preserved: %#v", out["lines"])
	}
	line := lines[0].(map[string]any)
	if line["desc"] != "line1" {
		t.Errorf("line description not mapped to desc: %#v", line)
	}
	if line["subprice"] != 5 {
		t.Errorf("line unit_price not mapped to subprice: %#v", line)
	}
}

func TestEntityToAPIPath(t *testing.T) {
	cases := map[string]string{
		"customers":  "thirdparties",
		"products":   "products",
		"proposals":  "proposals",
		"purchases":  "supplierorders",
		"warehouses": "warehouses",
		"unknown":    "unknown", // fallthrough returns input verbatim
	}
	for in, want := range cases {
		if got := EntityToAPIPath(in); got != want {
			t.Errorf("EntityToAPIPath(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestEntityToLinePath(t *testing.T) {
	if got := EntityToLinePath("proposals"); got != "line" {
		t.Errorf("proposals line path = %q, want singular %q", got, "line")
	}
	if got := EntityToLinePath("orders"); got != "lines" {
		t.Errorf("orders line path = %q, want %q", got, "lines")
	}
}

func TestValidEntities(t *testing.T) {
	got := ValidEntities()
	want := []string{
		"customers", "products", "proposals", "projects",
		"orders", "purchases", "warehouses", "shipments", "receptions",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ValidEntities mismatch\n got: %v\nwant: %v", got, want)
	}
}

func TestIsAutoNumbered(t *testing.T) {
	for _, e := range []string{"proposals", "orders", "purchases", "projects", "shipments", "receptions"} {
		if !IsAutoNumbered(e) {
			t.Errorf("%s should be auto-numbered", e)
		}
	}
	for _, e := range []string{"products", "customers", "warehouses"} {
		if IsAutoNumbered(e) {
			t.Errorf("%s should NOT be auto-numbered (user-defined ref)", e)
		}
	}
}

func TestIsValidTOSAttached(t *testing.T) {
	if !IsValidTOSAttached("NoCgv") || !IsValidTOSAttached("TOS.pdf") {
		t.Error("known TOS values should be valid")
	}
	if IsValidTOSAttached("TOS") {
		t.Error(`"TOS" is not a valid tos_attached value`)
	}
}

func TestMapToDolibarr_CustomerRefAlias(t *testing.T) {
	out := MapToDolibarr(map[string]any{"customer_ref": "ABC", "client_ref": "DEF"})
	if out["ref_client"] != "DEF" && out["ref_client"] != "ABC" {
		t.Errorf("customer_ref/client_ref should map to ref_client: %#v", out)
	}
}

func TestValidActions(t *testing.T) {
	actions := ValidActions()
	proposals, ok := actions["proposals"]
	if !ok {
		t.Fatal("proposals missing from ValidActions")
	}
	want := []string{"validate", "close", "settodraft", "setinvoiced"}
	if !reflect.DeepEqual(proposals, want) {
		t.Errorf("proposal actions mismatch\n got: %v\nwant: %v", proposals, want)
	}
	if _, ok := actions["customers"]; ok {
		t.Error("customers should not support state actions")
	}
}
