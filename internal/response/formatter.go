package response

import (
	"encoding/json"
	"fmt"
	"time"
)

// SearchResponse is the compact format for list results
type SearchResponse struct {
	Count   int    `json:"count"`
	Results []any  `json:"results"`
}

// FormatDate returns date in YYYY-MM-DD or empty
func FormatDate(t *time.Time) string {
	if t == nil || t.IsZero() {
		return ""
	}
	return t.Format("2006-01-02")
}

// FormatMoney returns formatted amount with currency
func FormatMoney(amount float64, currency string) string {
	if currency == "" {
		currency = "USD"
	}
	return fmt.Sprintf("%.2f %s", amount, currency)
}

// ToJSON marshals any value to compact JSON bytes
func ToJSON(v any) string {
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf(`{"error":"marshal failed: %s"}`, err.Error())
	}
	return string(data)
}

// StatusName maps numeric status codes to human-readable labels per entity
func StatusName(entity string, code int) string {
	statuses := map[string]map[int]string{
		"proposals": {
			0: "Draft",
			1: "Validated",
			2: "Signed",
			3: "Not signed",
			4: "Billed",
		},
		"orders": {
			-1: "Cancelled",
			0:  "Draft",
			1:  "Validated",
			2:  "Shipped partially",
			3:  "Shipped completely",
		},
		"purchases": {
			0: "Draft",
			1: "Validated",
			2: "Approved",
			3: "Ordered",
			4: "Received partially",
			5: "Received completely",
			6: "Cancelled",
			9: "Refused",
		},
		"projects": {
			0: "Draft",
			1: "Validated/Open",
			2: "Closed",
		},
		"shipments": {
			0: "Draft",
			1: "Validated",
			2: "Closed",
		},
		"receptions": {
			0: "Draft",
			1: "Validated",
			2: "Closed",
		},
		"products": {
			0: "On sell (no)",
			1: "On sell (yes)",
		},
		"warehouses": {
			0: "Closed",
			1: "Open",
		},
		"customers": {
			0: "Closed",
			1: "Active",
		},
	}

	if entityStatuses, ok := statuses[entity]; ok {
		if label, ok := entityStatuses[code]; ok {
			return label
		}
	}
	return fmt.Sprintf("Unknown(%d)", code)
}
