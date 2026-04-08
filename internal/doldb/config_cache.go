package doldb

import (
	"context"
	"database/sql"
)

type DolConfig struct {
	ProductPerEntityShared  bool
	CompanyPerEntityShared  bool
	MulticompanyProduct     bool
	MultiPrices             bool
	StockCalculateOnOrder   int
	MainCurrency            string
}

func (d *DB) loadDolConfig(ctx context.Context) (*DolConfig, error) {
	cfg := &DolConfig{}

	keys := map[string]*bool{
		"MAIN_PRODUCT_PERENTITY_SHARED":       &cfg.ProductPerEntityShared,
		"MAIN_COMPANY_PERENTITY_SHARED":       &cfg.CompanyPerEntityShared,
		"MULTICOMPANY_PRODUCT_SHARING_ENABLED": &cfg.MulticompanyProduct,
		"PRODUIT_MULTIPRICES":                  &cfg.MultiPrices,
	}

	for key, dest := range keys {
		var val sql.NullString
		err := d.QueryRowContext(ctx,
			"SELECT value FROM "+d.T("const")+" WHERE name = ? AND entity IN (0, ?) LIMIT 1",
			key, d.Entity(),
		).Scan(&val)
		if err == sql.ErrNoRows {
			continue
		}
		if err != nil {
			return nil, err
		}
		*dest = val.Valid && val.String != "" && val.String != "0"
	}

	// Stock calculation mode
	var stockCalc sql.NullString
	err := d.QueryRowContext(ctx,
		"SELECT value FROM "+d.T("const")+" WHERE name = 'STOCK_CALCULATE_ON_VALIDATE_ORDER' AND entity IN (0, ?) LIMIT 1",
		d.Entity(),
	).Scan(&stockCalc)
	if err == nil && stockCalc.Valid {
		if stockCalc.String == "1" {
			cfg.StockCalculateOnOrder = 1
		}
	}

	// Main currency
	var currency sql.NullString
	err = d.QueryRowContext(ctx,
		"SELECT value FROM "+d.T("const")+" WHERE name = 'MAIN_MONNAIE' AND entity IN (0, ?) LIMIT 1",
		d.Entity(),
	).Scan(&currency)
	if err == nil && currency.Valid && currency.String != "" {
		cfg.MainCurrency = currency.String
	} else {
		cfg.MainCurrency = "USD"
	}

	return cfg, nil
}
