package tools

import "testing"

func TestStripAutoNumberRef(t *testing.T) {
	// Proposal number is stripped, but customer references are preserved.
	data := map[string]any{
		"ref":          "PK-PROP-2026-0006", // forced number → must be removed
		"ref_ext":      "x",
		"ref_client":   "CLIENT-REF-1", // customer reference → must stay
		"customer_ref": "CLIENT-REF-2",
		"customer_id":  42,
	}
	stripAutoNumberRef("proposals", data)

	if _, ok := data["ref"]; ok {
		t.Error("ref (document number) should be stripped for proposals")
	}
	if _, ok := data["ref_ext"]; ok {
		t.Error("ref_ext should be stripped")
	}
	if data["ref_client"] != "CLIENT-REF-1" {
		t.Error("ref_client (customer reference) must be preserved")
	}
	if data["customer_ref"] != "CLIENT-REF-2" {
		t.Error("customer_ref must be preserved")
	}

	// Non auto-numbered entity keeps ref (e.g. product ref is user-defined).
	prod := map[string]any{"ref": "PROD-1"}
	stripAutoNumberRef("products", prod)
	if prod["ref"] != "PROD-1" {
		t.Error("products keep their user-defined ref")
	}
}

func TestValidateCreate_CustomerRequired(t *testing.T) {
	// proposal without customer_id → rejected
	if err := validateCreate("proposals", map[string]any{
		"extrafields": map[string]any{"tos_attached": "TOS.pdf"},
	}); err == nil {
		t.Error("proposal without customer_id should be rejected")
	}

	// proposal with customer_id=0 → rejected (0 is not a valid socid)
	if err := validateCreate("proposals", map[string]any{
		"customer_id": 0,
		"extrafields": map[string]any{"tos_attached": "TOS.pdf"},
	}); err == nil {
		t.Error("customer_id=0 should be rejected")
	}

	// valid proposal → ok
	if err := validateCreate("proposals", map[string]any{
		"customer_id": 42,
		"extrafields": map[string]any{"tos_attached": "TOS.pdf"},
	}); err != nil {
		t.Errorf("valid proposal rejected: %v", err)
	}

	// purchase requires supplier_id
	if err := validateCreate("purchases", map[string]any{}); err == nil {
		t.Error("purchase without supplier_id should be rejected")
	}
	if err := validateCreate("purchases", map[string]any{"supplier_id": 7}); err != nil {
		t.Errorf("valid purchase rejected: %v", err)
	}
}

func TestValidateCreate_TOSAttached(t *testing.T) {
	base := map[string]any{"customer_id": 1}

	// missing tos_attached on a proposal → rejected
	if err := validateCreate("proposals", copyData(base, nil)); err == nil {
		t.Error("proposal without tos_attached should be rejected")
	}

	// invalid value ("TOS") → rejected
	if err := validateCreate("proposals", copyData(base, map[string]any{"tos_attached": "TOS"})); err == nil {
		t.Error(`invalid tos_attached "TOS" should be rejected`)
	}

	// valid value → ok
	if err := validateCreate("proposals", copyData(base, map[string]any{"tos_attached": "NoCgv"})); err != nil {
		t.Errorf("valid tos_attached rejected: %v", err)
	}

	// orders don't require tos_attached, but validate it if present
	if err := validateCreate("orders", copyData(base, nil)); err != nil {
		t.Errorf("order without tos_attached should be allowed: %v", err)
	}
	if err := validateCreate("orders", copyData(base, map[string]any{"tos_attached": "bogus"})); err == nil {
		t.Error("order with invalid tos_attached should be rejected")
	}

	// customers have no such contract
	if err := validateCreate("customers", map[string]any{}); err != nil {
		t.Errorf("customer create should not require proposal fields: %v", err)
	}
}

func copyData(base map[string]any, extra map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range base {
		out[k] = v
	}
	if extra != nil {
		out["extrafields"] = extra
	}
	return out
}
