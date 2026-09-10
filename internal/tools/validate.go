package tools

import (
	"fmt"

	"github.com/sgsoluciones/dolibarr-mcp/internal/mapper"
)

// stripAutoNumberRef removes ONLY the document number (ref, e.g. "OF26073159")
// for auto-numbered entities so Dolibarr assigns it from its numbering mask
// instead of honoring a client-supplied value.
//
// The other reference fields are user/integration data and are left intact:
//   - ref_ext: ProposalKit's tracking id
//   - ref_client / customer_ref, and the ref_cliente extrafield: the customer's
//     order number (e.g. "OC XXX")
func stripAutoNumberRef(entity string, data map[string]any) {
	if !mapper.IsAutoNumbered(entity) {
		return
	}
	// Some resources reject a payload with no ref at all and expect the literal
	// "auto" to trigger the mask; the rest want the key gone entirely.
	if auto, needsRef := mapper.AutoRefValue(entity); needsRef {
		data["ref"] = auto
		return
	}
	delete(data, "ref")
}

// validateCreate enforces the required data contract so creation does not
// silently produce incomplete documents.
func validateCreate(entity string, data map[string]any) error {
	switch entity {
	case "proposals", "orders":
		if !hasValue(data, "customer_id", "socid") {
			return fmt.Errorf("customer_id is required to create a %s", entity)
		}
	case "purchases":
		if !hasValue(data, "supplier_id", "socid") {
			return fmt.Errorf("supplier_id is required to create a purchase")
		}
	case "projects":
		// Dolibarr marks title mandatory on the project resource; without it the
		// document is created unusable rather than rejected.
		if !hasValue(data, "title", "label") {
			return fmt.Errorf("title is required to create a project")
		}
	case "tasks":
		// POST /tasks answers 400 without a label, and a task with no parent
		// project is orphaned and invisible in the project view.
		if !hasValue(data, "label", "title") {
			return fmt.Errorf("label is required to create a task")
		}
		if !hasValue(data, "project_id", "fk_project") {
			return fmt.Errorf("project_id is required to create a task")
		}
	}

	// tos_attached: required for proposals, and validated against the allowed
	// set whenever supplied (proposals and orders).
	if entity == "proposals" || entity == "orders" {
		tos, present := extrafieldString(data, "tos_attached")
		if entity == "proposals" && !present {
			return fmt.Errorf("extrafields.tos_attached is required for proposals; valid values: %v", mapper.TOSAttachedValues)
		}
		if present && !mapper.IsValidTOSAttached(tos) {
			return fmt.Errorf("invalid tos_attached %q; valid values: %v", tos, mapper.TOSAttachedValues)
		}
	}
	return nil
}

// hasValue reports whether any of the keys holds a non-empty, non-zero value.
func hasValue(data map[string]any, keys ...string) bool {
	for _, k := range keys {
		v, ok := data[k]
		if !ok || v == nil {
			continue
		}
		s := fmt.Sprintf("%v", v)
		if s != "" && s != "0" {
			return true
		}
	}
	return false
}

// extrafieldString returns the string value of an extrafield and whether it was present.
func extrafieldString(data map[string]any, name string) (string, bool) {
	ef, ok := data["extrafields"].(map[string]any)
	if !ok {
		return "", false
	}
	v, ok := ef[name]
	if !ok {
		return "", false
	}
	s, isStr := v.(string)
	return s, isStr
}
