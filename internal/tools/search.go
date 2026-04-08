package tools

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/sgsoluciones/dolibarr-mcp/internal/doldb"
	"github.com/sgsoluciones/dolibarr-mcp/internal/response"
)

type SearchInput struct {
	Entity     string   `json:"entity" jsonschema:"description=Entity to search: customers|products|proposals|projects|orders|purchases|warehouses|shipments|receptions"`
	Query      string   `json:"query,omitempty" jsonschema:"description=Text search (ref or name or label)"`
	CustomerID int64    `json:"customer_id,omitempty" jsonschema:"description=Filter by customer/supplier ID"`
	Status     *int     `json:"status,omitempty" jsonschema:"description=Filter by status code"`
	DateFrom   string   `json:"date_from,omitempty" jsonschema:"description=Date from (YYYY-MM-DD)"`
	DateTo     string   `json:"date_to,omitempty" jsonschema:"description=Date to (YYYY-MM-DD)"`
	AmountMin  *float64 `json:"amount_min,omitempty" jsonschema:"description=Minimum amount (total_ht)"`
	AmountMax  *float64 `json:"amount_max,omitempty" jsonschema:"description=Maximum amount (total_ht)"`
	Limit      int      `json:"limit,omitempty" jsonschema:"description=Max results (default 25 max 100)"`
	Offset     int      `json:"offset,omitempty" jsonschema:"description=Offset for pagination"`
}

type SearchOutput struct {
	Result string `json:"result" jsonschema:"description=JSON search results"`
}

func (d *Deps) HandleSearch(ctx context.Context, req *mcp.CallToolRequest, input SearchInput) (*mcp.CallToolResult, SearchOutput, error) {
	results, total, err := d.DB.Search(ctx, doldb.SearchParams{
		Entity:     input.Entity,
		Query:      input.Query,
		CustomerID: input.CustomerID,
		Status:     input.Status,
		DateFrom:   input.DateFrom,
		DateTo:     input.DateTo,
		AmountMin:  input.AmountMin,
		AmountMax:  input.AmountMax,
		Limit:      input.Limit,
		Offset:     input.Offset,
	})
	if err != nil {
		return nil, SearchOutput{}, fmt.Errorf("search %s: %w", input.Entity, err)
	}

	resp := response.SearchResponse{
		Count:   total,
		Results: make([]any, len(results)),
	}
	for i, r := range results {
		resp.Results[i] = r
	}

	return nil, SearchOutput{Result: response.ToJSON(resp)}, nil
}
