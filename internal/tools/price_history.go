package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/sgsoluciones/dolibarr-mcp/internal/doldb"
)

// Limits shared with the sgpurchasehistory REST API (GET /sgpurchasehistory/stats).
const (
	priceHistoryMaxProducts = 50
	priceHistoryMaxMonths   = 120
)

type PriceHistoryInput struct {
	ProductIDs  []int64  `json:"product_ids,omitempty" jsonschema:"Dolibarr product IDs (rowid). Use this or product_refs, or both; at most 50 products in total."`
	ProductRefs []string `json:"product_refs,omitempty" jsonschema:"Product references (llx_product.ref, e.g. C209405), resolved to IDs in the configured entity. Unknown refs are listed in no_encontrados."`
	CustomerID  int64    `json:"customer_id,omitempty" jsonschema:"Optional customer (third party) ID. When set, ventas.ultima_cliente carries the last sale of each product to that customer."`
	Months      int      `json:"months,omitempty" jsonschema:"Averaging window in calendar months, 1-120. Default 12."`
	Percentile  int      `json:"percentile,omitempty" jsonschema:"Purchase-price percentile to quote with, 1-100. Default 90."`
}

// The jsonschema tag on Result is load-bearing — see GetOutput.
type PriceHistoryOutput struct {
	Result any `json:"result" jsonschema:"Price history as decoded JSON: months, percentile_p, socid, productos[] with product_id, ref, label, compras and ventas; optional no_encontrados[] and aviso"`
}

func (d *Deps) HandlePriceHistory(ctx context.Context, req *mcp.CallToolRequest, input PriceHistoryInput) (*mcp.CallToolResult, PriceHistoryOutput, error) {
	params, err := priceHistoryParams(input)
	if err != nil {
		return nil, PriceHistoryOutput{}, err
	}
	result, err := d.DB.PriceHistory(ctx, params)
	if err != nil {
		return nil, PriceHistoryOutput{}, fmt.Errorf("price history: %w", err)
	}
	return nil, PriceHistoryOutput{Result: result}, nil
}

// priceHistoryParams validates and normalizes the input before any query:
// defaults applied, ids and refs deduplicated in input order, refs trimmed.
func priceHistoryParams(in PriceHistoryInput) (doldb.PriceHistoryParams, error) {
	p := doldb.PriceHistoryParams{
		CustomerID: in.CustomerID,
		Months:     in.Months,
		Percentile: in.Percentile,
	}
	if p.Months == 0 {
		p.Months = doldb.AvgMonthsDefault
	}
	if p.Percentile == 0 {
		p.Percentile = doldb.PercentileDefault
	}
	if p.Months < 1 || p.Months > priceHistoryMaxMonths {
		return p, fmt.Errorf("months must be between 1 and %d, got %d", priceHistoryMaxMonths, in.Months)
	}
	if p.Percentile < 1 || p.Percentile > 100 {
		return p, fmt.Errorf("percentile must be between 1 and 100, got %d", in.Percentile)
	}
	if p.CustomerID < 0 {
		return p, fmt.Errorf("customer_id must be a positive id, got %d", in.CustomerID)
	}

	seenID := map[int64]bool{}
	for _, id := range in.ProductIDs {
		if id <= 0 {
			return p, fmt.Errorf("product_ids must be positive ids, got %d", id)
		}
		if !seenID[id] {
			seenID[id] = true
			p.ProductIDs = append(p.ProductIDs, id)
		}
	}
	seenRef := map[string]bool{}
	for _, ref := range in.ProductRefs {
		ref = strings.TrimSpace(ref)
		if ref == "" || seenRef[ref] {
			continue
		}
		seenRef[ref] = true
		p.ProductRefs = append(p.ProductRefs, ref)
	}

	total := len(p.ProductIDs) + len(p.ProductRefs)
	if total == 0 {
		return p, fmt.Errorf("give at least one product in product_ids or product_refs")
	}
	if total > priceHistoryMaxProducts {
		return p, fmt.Errorf("at most %d products per call, got %d: split the request", priceHistoryMaxProducts, total)
	}
	return p, nil
}
