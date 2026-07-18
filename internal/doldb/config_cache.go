package doldb

import (
	"context"
	"database/sql"
	"strings"
)

type DolConfig struct {
	ProductPerEntityShared bool
	CompanyPerEntityShared bool
	MulticompanyProduct    bool
	MultiPrices            bool
	StockCalculateOnOrder  int
	MainCurrency           string
}

// dolConfigKeys are the Dolibarr const names read at startup.
var dolConfigKeys = []string{
	"MAIN_PRODUCT_PERENTITY_SHARED",
	"MAIN_COMPANY_PERENTITY_SHARED",
	"MULTICOMPANY_PRODUCT_SHARING_ENABLED",
	"PRODUIT_MULTIPRICES",
	"STOCK_CALCULATE_ON_VALIDATE_ORDER",
	"MAIN_MONNAIE",
}

// loadDolConfig reads all needed Dolibarr constants in a single query (instead
// of one round-trip per key). Entity-specific values override global (entity 0).
func (d *DB) loadDolConfig(ctx context.Context) (*DolConfig, error) {
	cfg := &DolConfig{MainCurrency: "USD"}

	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(dolConfigKeys)), ",")
	q := "SELECT name, value FROM " + d.T("const") +
		" WHERE name IN (" + placeholders + ") AND entity IN (0, ?) ORDER BY entity ASC"

	args := make([]any, 0, len(dolConfigKeys)+1)
	for _, k := range dolConfigKeys {
		args = append(args, k)
	}
	args = append(args, d.Entity())

	rows, err := d.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	// entity-specific rows come last (ORDER BY entity ASC) and overwrite global.
	values := make(map[string]string, len(dolConfigKeys))
	for rows.Next() {
		var name string
		var val sql.NullString
		if err := rows.Scan(&name, &val); err != nil {
			return nil, err
		}
		if val.Valid {
			values[name] = val.String
		} else {
			values[name] = ""
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	truthy := func(name string) bool {
		v, ok := values[name]
		return ok && v != "" && v != "0"
	}
	cfg.ProductPerEntityShared = truthy("MAIN_PRODUCT_PERENTITY_SHARED")
	cfg.CompanyPerEntityShared = truthy("MAIN_COMPANY_PERENTITY_SHARED")
	cfg.MulticompanyProduct = truthy("MULTICOMPANY_PRODUCT_SHARING_ENABLED")
	cfg.MultiPrices = truthy("PRODUIT_MULTIPRICES")
	if values["STOCK_CALCULATE_ON_VALIDATE_ORDER"] == "1" {
		cfg.StockCalculateOnOrder = 1
	}
	if c := values["MAIN_MONNAIE"]; c != "" {
		cfg.MainCurrency = c
	}

	return cfg, nil
}
