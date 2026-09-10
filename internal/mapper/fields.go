package mapper

import (
	"slices"
	"time"
)

// dateFields are Dolibarr fields that expect Unix timestamps
var dateFields = map[string]bool{
	"date":               true,
	"date_livraison":     true,
	"delivery_date":      true,
	"fin_validite":       true,
	"date_lim_reglement": true,
	"date_commande":      true,
	"date_start":         true,
	"date_end":           true,
}

// toTimestamp converts a date string (YYYY-MM-DD or RFC3339) to Unix timestamp.
// Returns the original value if conversion fails or if it's already a number.
func toTimestamp(v any) any {
	s, ok := v.(string)
	if !ok {
		return v // already a number or other type
	}
	// Try YYYY-MM-DD
	if t, err := time.Parse("2006-01-02", s); err == nil {
		return t.Unix()
	}
	// Try RFC3339
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t.Unix()
	}
	return v
}

// nativeDescriptionEntities keep their description under "description". The generic
// alias renames it to "desc", which is correct for proposal and order lines but wrong
// for these documents: Dolibarr answers 200 and silently discards the unknown field,
// so the text is lost without any error to notice.
var nativeDescriptionEntities = map[string]bool{
	"projects": true,
}

// MapEntityToDolibarr translates a document payload to Dolibarr's internal names,
// honoring the entity so fields that differ between a document and its lines are not
// confused. Prefer it over MapToDolibarr wherever the entity is known.
func MapEntityToDolibarr(entity string, data map[string]any) map[string]any {
	out := MapToDolibarr(data)
	if nativeDescriptionEntities[entity] {
		if v, ok := out["desc"]; ok {
			out["description"] = v
			delete(out, "desc")
		}
	}
	return out
}

// MapToDolibarr translates friendly field names to Dolibarr internal names in a payload.
func MapToDolibarr(data map[string]any) map[string]any {
	aliases := map[string]string{
		"customer_id":        "socid",
		"supplier_id":        "socid",
		"product_id":         "fk_product",
		"project_id":         "fk_project",
		"warehouse_id":       "fk_warehouse",
		"vat_rate":           "tva_tx",
		"unit_price":         "subprice",
		"discount_percent":   "remise_percent",
		"unit_id":            "fk_unit",
		"payment_term_id":    "cond_reglement_id",
		"payment_mode_id":    "mode_reglement_id",
		"availability_id":    "availability_id",
		"source_id":          "demand_reason_id",
		"demand_reason_id":   "demand_reason_id",
		"shipping_method_id": "fk_shipping_method",
		"incoterms_id":       "fk_incoterms",
		"incoterms_location": "location_incoterms",
		"description":        "desc",
		"customer_ref":       "ref_client", // customer's order number ("Ref. cliente" / OC), NOT the proposal number
		"client_ref":         "ref_client",
		"proposalkit_ref":    "ref_ext", // ProposalKit tracking id (external reference)
		"label":              "label",
		"name":               "nom",
		"title":              "title",
		"date":               "date",
		"due_date":           "date_lim_reglement",
		"delivery_date":      "delivery_date",
		"validity_end":       "fin_validite",
		// Project header. These Dolibarr names were confirmed to round-trip through
		// PUT /projects/{id}; without an alias the caller has to guess them.
		"budget":                "budget_amount",
		"is_public":             "public",
		"opportunity_amount":    "opp_amount",
		"opportunity_percent":   "opp_percent",
		"opportunity_status_id": "fk_opp_status",
		// Project tasks. The parent is keyed as fk_project by the API even though
		// the column is fk_projet, and workload is stored in seconds.
		"planned_hours": "planned_workload",
	}

	out := make(map[string]any, len(data))
	for k, v := range data {
		key := k
		if dolKey, ok := aliases[k]; ok {
			key = dolKey
		}
		// Convert date strings to Unix timestamps for Dolibarr API
		if dateFields[key] {
			v = toTimestamp(v)
		}
		out[key] = v
	}

	// Map "extrafields" to "array_options" with options_ prefix on each key
	if ef, ok := out["extrafields"].(map[string]any); ok {
		opts := make(map[string]any, len(ef))
		for k, v := range ef {
			if len(k) > 8 && k[:8] == "options_" {
				opts[k] = v
			} else {
				opts["options_"+k] = v
			}
		}
		out["array_options"] = opts
		delete(out, "extrafields")
	}

	// Map lines recursively
	if lines, ok := out["lines"].([]any); ok {
		for i, line := range lines {
			if lineMap, ok := line.(map[string]any); ok {
				lines[i] = MapToDolibarr(lineMap)
			}
		}
		out["lines"] = lines
	}

	return out
}

// EntityToAPIPath maps entity names to Dolibarr REST API paths
func EntityToAPIPath(entity string) string {
	paths := map[string]string{
		"customers":  "thirdparties",
		"products":   "products",
		"proposals":  "proposals",
		"projects":   "projects",
		"tasks":      "tasks",
		"orders":     "orders",
		"purchases":  "supplierorders",
		"warehouses": "warehouses",
		"shipments":  "shipments",
		"receptions": "receptions",
	}
	if p, ok := paths[entity]; ok {
		return p
	}
	return entity
}

// EntityToLinePath returns the sub-resource name for line operations
func EntityToLinePath(entity string) string {
	switch entity {
	case "proposals":
		return "line" // POST proposals/{id}/line (singular for add)
	default:
		return "lines"
	}
}

// ValidEntities returns the list of supported entity names
func ValidEntities() []string {
	return []string{
		"customers", "products", "proposals", "projects", "tasks",
		"orders", "purchases", "warehouses", "shipments", "receptions",
	}
}

// IsValidEntity reports whether entity is one of the supported entity names.
func IsValidEntity(entity string) bool {
	return slices.Contains(ValidEntities(), entity)
}

// autoNumberedEntities generate their own ref from a numbering mask. A
// client-supplied ref would override the mask, so it is stripped on create.
var autoNumberedEntities = map[string]bool{
	"proposals":  true,
	"orders":     true,
	"purchases":  true,
	"projects":   true,
	"tasks":      true,
	"shipments":  true,
	"receptions": true,
}

// explicitAutoRefEntities are auto-numbered too, but their API refuses a payload
// with no ref at all: POST /projects without one answers 400 "ref field missing".
// The literal "auto" is what asks Dolibarr to apply the mask. Verified on 23:
//
//	POST /projects {"title":"x"}              -> 400 ref field missing
//	POST /projects {"ref":"auto","title":"x"} -> 200
//
// Stripping the ref for these made every create fail.
var explicitAutoRefEntities = map[string]bool{
	"projects": true,
	"tasks":    true, // POST /tasks answers 400 "ref field missing" the same way
}

// AutoRefValue returns the ref an auto-numbered entity must carry on create, and
// whether it needs one at all. Entities that want no ref key return ("", false).
func AutoRefValue(entity string) (string, bool) {
	if explicitAutoRefEntities[entity] {
		return "auto", true
	}
	return "", false
}

// IsAutoNumbered reports whether an entity's ref is generated by Dolibarr and
// must not be supplied on create.
func IsAutoNumbered(entity string) bool {
	return autoNumberedEntities[entity]
}

// TOSAttachedValues are the accepted values for the proposal/order tos_attached
// extrafield. These are instance-specific: keep them in sync with the Dolibarr
// extrafield definition (llx_extrafields "param" for tos_attached).
var TOSAttachedValues = []string{
	"NoCgv",
	"TOS.pdf",
	"DSERRANO_CONDICIONES COMERCIALES S&G.pdf",
}

// IsValidTOSAttached reports whether v is an accepted tos_attached value.
func IsValidTOSAttached(v string) bool {
	return slices.Contains(TOSAttachedValues, v)
}

// ValidActions returns valid state-change actions per entity
func ValidActions() map[string][]string {
	return map[string][]string{
		"proposals":  {"validate", "close", "settodraft", "setinvoiced"},
		"orders":     {"validate", "close"},
		"projects":   {"validate"},
		"purchases":  {"validate", "approve", "makeorder", "receive"},
		"shipments":  {"validate", "close"},
		"receptions": {"validate", "close"},
	}
}
