package tools

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestPriceHistoryDefaults(t *testing.T) {
	p, err := priceHistoryParams(PriceHistoryInput{ProductIDs: []int64{4242}})
	if err != nil {
		t.Fatal(err)
	}
	if p.Months != 12 || p.Percentile != 90 || p.CustomerID != 0 {
		t.Errorf("defaults = months %d, percentile %d, customer %d; want 12/90/0", p.Months, p.Percentile, p.CustomerID)
	}
}

func TestPriceHistoryNormalizesTheProductList(t *testing.T) {
	p, err := priceHistoryParams(PriceHistoryInput{
		ProductIDs:  []int64{7604, 4242, 7604},
		ProductRefs: []string{" C209405 ", "C209405", "209405NG"},
		CustomerID:  836,
		Months:      24,
		Percentile:  75,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(p.ProductIDs, []int64{7604, 4242}) {
		t.Errorf("ids = %v, want deduplicated in input order", p.ProductIDs)
	}
	if !reflect.DeepEqual(p.ProductRefs, []string{"C209405", "209405NG"}) {
		t.Errorf("refs = %v, want trimmed and deduplicated", p.ProductRefs)
	}
	if p.CustomerID != 836 || p.Months != 24 || p.Percentile != 75 {
		t.Errorf("params = %+v", p)
	}
}

func TestPriceHistoryRejectsBadInputBeforeTheDatabase(t *testing.T) {
	deps := &Deps{} // nil DB on purpose: a valid call would panic here

	tooMany := make([]int64, 51)
	for i := range tooMany {
		tooMany[i] = int64(i + 1)
	}
	split := make([]int64, 49)
	for i := range split {
		split[i] = int64(i + 1)
	}

	cases := map[string]PriceHistoryInput{
		"no products":          {},
		"only blank refs":      {ProductRefs: []string{" ", ""}},
		"zero id":              {ProductIDs: []int64{0}},
		"negative id":          {ProductIDs: []int64{-5}},
		"more than 50 ids":     {ProductIDs: tooMany},
		"more than 50 overall": {ProductIDs: split, ProductRefs: []string{"A", "B"}},
		"months too large":     {ProductIDs: []int64{1}, Months: 121},
		"months negative":      {ProductIDs: []int64{1}, Months: -1},
		"percentile too large": {ProductIDs: []int64{1}, Percentile: 101},
		"percentile negative":  {ProductIDs: []int64{1}, Percentile: -10},
		"negative customer":    {ProductIDs: []int64{1}, CustomerID: -836},
	}
	for name, in := range cases {
		t.Run(name, func(t *testing.T) {
			if _, _, err := deps.HandlePriceHistory(context.Background(), nil, in); err == nil {
				t.Fatalf("%s was accepted", name)
			}
		})
	}
}

func TestPriceHistoryAcceptsTheLimits(t *testing.T) {
	fifty := make([]int64, 50)
	for i := range fifty {
		fifty[i] = int64(i + 1)
	}
	for name, in := range map[string]PriceHistoryInput{
		"50 products":    {ProductIDs: fifty},
		"months 1":       {ProductIDs: []int64{1}, Months: 1},
		"months 120":     {ProductIDs: []int64{1}, Months: 120},
		"percentile 1":   {ProductIDs: []int64{1}, Percentile: 1},
		"percentile 100": {ProductIDs: []int64{1}, Percentile: 100},
	} {
		if _, err := priceHistoryParams(in); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
}

// The tool is listed as read-only with the inputs and the description an LLM
// needs to use it without guessing.
func TestPriceHistoryIsRegisteredReadOnly(t *testing.T) {
	ctx := context.Background()
	server := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0"}, nil)
	Register(server, &Deps{})

	serverTr, clientTr := mcp.NewInMemoryTransports()
	serverSession, err := server.Connect(ctx, serverTr, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer serverSession.Close()
	clientSession, err := mcp.NewClient(&mcp.Implementation{Name: "probe", Version: "0"}, nil).Connect(ctx, clientTr, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer clientSession.Close()

	listed, err := clientSession.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tool := range listed.Tools {
		if tool.Name != "dolibarr_price_history" {
			continue
		}
		if tool.Annotations == nil || !tool.Annotations.ReadOnlyHint {
			t.Error("dolibarr_price_history must be annotated read-only")
		}
		raw, _ := json.Marshal(tool.InputSchema)
		for _, field := range []string{"product_ids", "product_refs", "customer_id", "months", "percentile"} {
			if !strings.Contains(string(raw), `"`+field+`"`) {
				t.Errorf("input schema is missing %q: %s", field, raw)
			}
		}
		for _, must := range []string{"COP", "sgpurchasehistory", "invoice", "proposal", "read-only", "50"} {
			if !strings.Contains(tool.Description, must) {
				t.Errorf("description must mention %q", must)
			}
		}
		return
	}
	t.Fatalf("dolibarr_price_history was not registered (%d tools listed)", len(listed.Tools))
}
