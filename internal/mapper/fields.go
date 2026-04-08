package mapper

// MapToDolibarr translates friendly field names to Dolibarr internal names in a payload.
func MapToDolibarr(data map[string]any) map[string]any {
	aliases := map[string]string{
		"customer_id":      "socid",
		"supplier_id":      "socid",
		"product_id":       "fk_product",
		"project_id":       "fk_project",
		"warehouse_id":     "fk_warehouse",
		"vat_rate":         "tva_tx",
		"unit_price":       "subprice",
		"discount_percent": "remise_percent",
		"unit_id":          "fk_unit",
		"payment_term_id":  "fk_cond_reglement",
		"payment_mode_id":  "fk_mode_reglement",
		"shipping_method_id": "fk_shipping_method",
		"name":             "nom",
		"title":            "title",
	}

	out := make(map[string]any, len(data))
	for k, v := range data {
		if dolKey, ok := aliases[k]; ok {
			out[dolKey] = v
		} else {
			out[k] = v
		}
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
