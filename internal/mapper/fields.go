package mapper

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
		"payment_term_id":    "fk_cond_reglement",
		"payment_mode_id":    "fk_mode_reglement",
		"availability_id":    "availability_id",
		"source_id":          "demand_reason_id",
		"demand_reason_id":   "demand_reason_id",
		"shipping_method_id": "fk_shipping_method",
		"incoterms_id":       "fk_incoterms",
		"incoterms_location": "location_incoterms",
		"description":        "desc",
		"label":              "label",
		"name":               "nom",
		"title":              "title",
		"date":               "date",
		"due_date":           "date_lim_reglement",
		"delivery_date":      "date_livraison",
		"validity_end":       "fin_validite",
	}

	out := make(map[string]any, len(data))
	for k, v := range data {
		if dolKey, ok := aliases[k]; ok {
			out[dolKey] = v
		} else {
			out[k] = v
		}
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
		"customers", "products", "proposals", "projects",
		"orders", "purchases", "warehouses", "shipments", "receptions",
	}
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
