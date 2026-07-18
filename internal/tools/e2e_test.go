package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/sgsoluciones/dolibarr-mcp/internal/config"
	"github.com/sgsoluciones/dolibarr-mcp/internal/dolapi"
)

// toolErr extracts a failure from a handler return: API failures surface as an
// error result (IsError), not as a Go error, so both must be checked.
func toolErr(res *mcp.CallToolResult, err error) error {
	if err != nil {
		return err
	}
	if res != nil && res.IsError {
		var b strings.Builder
		for _, c := range res.Content {
			if tc, ok := c.(*mcp.TextContent); ok {
				b.WriteString(tc.Text)
			}
		}
		return fmt.Errorf("tool error result: %s", b.String())
	}
	return nil
}

// End-to-end test of the proposal fixes against a real Dolibarr REST API.
// Skipped unless DOLIBARR_E2E=1. Requires DOLIBARR_API_URL, DOLIBARR_API_KEY and
// TEST_CUSTOMER_ID (a valid client socid). It CREATES a real proposal in the
// target instance, so point it at a disposable copy — never production.
func TestProposalE2E(t *testing.T) {
	if os.Getenv("DOLIBARR_E2E") == "" {
		t.Skip("set DOLIBARR_E2E=1 to run the end-to-end proposal test")
	}
	custID, _ := strconv.ParseInt(os.Getenv("TEST_CUSTOMER_ID"), 10, 64)
	if custID == 0 {
		t.Fatal("TEST_CUSTOMER_ID required")
	}

	client := dolapi.New(&config.Config{
		APIUrl: os.Getenv("DOLIBARR_API_URL"),
		APIKey: os.Getenv("DOLIBARR_API_KEY"),
	})
	deps := &Deps{API: client}
	ctx := context.Background()

	// #6 — create without customer_id must be rejected by the MCP (no API call).
	if _, _, err := deps.HandleCreate(ctx, nil, CreateInput{Entity: "proposals", Data: map[string]any{
		"extrafields": map[string]any{"tos_attached": "NoCgv"},
	}}); err == nil {
		t.Error("#6: proposal without customer_id should be rejected")
	} else {
		t.Logf("#6 OK: %v", err)
	}

	// #7 — invalid tos_attached must be rejected.
	if _, _, err := deps.HandleCreate(ctx, nil, CreateInput{Entity: "proposals", Data: map[string]any{
		"customer_id": custID,
		"extrafields": map[string]any{"tos_attached": "TOS"},
	}}); err == nil {
		t.Error(`#7: invalid tos_attached "TOS" should be rejected`)
	} else {
		t.Logf("#7 OK: %v", err)
	}

	// #4 — create with a FORCED ref and a customer_ref. The forced number must be
	// ignored (auto-numbered), while the customer reference must be kept.
	res, out, err := deps.HandleCreate(ctx, nil, CreateInput{Entity: "proposals", Data: map[string]any{
		"customer_id":     custID,
		"ref":             "PK-FORCED-9999", // OF number → must be ignored
		"proposalkit_ref": "PKIT-999",       // ProposalKit id (ref_ext) → must be kept
		"customer_ref":    "MI-REF-CLIENTE", // customer OC → must be kept
		"date":            "2026-07-18",
		"extrafields":     map[string]any{"tos_attached": "NoCgv"},
		"lines": []any{map[string]any{
			"description": "<p>Linea de prueba e2e</p>", "qty": 1, "unit_price": 100, "vat_rate": 19, "product_type": 1,
		}},
	}})
	if e := toolErr(res, err); e != nil {
		t.Fatalf("#4 create failed: %v", e)
	}
	newID := asInt64(t, out.Result)
	t.Logf("#4 created proposal id=%d", newID)

	prop := getProposal(t, client, ctx, newID)
	ref, _ := prop["ref"].(string)
	refClient, _ := prop["ref_client"].(string)
	if ref == "PK-FORCED-9999" || strings.HasPrefix(ref, "PK-") {
		t.Errorf("#4 FAIL: forced ref was honored (ref=%q); expected auto-number", ref)
	} else {
		t.Logf("#4 OK: auto-numbered ref=%q (forced value ignored)", ref)
	}
	if refClient != "MI-REF-CLIENTE" {
		t.Errorf("#4 FAIL: customer reference lost (ref_client=%q)", refClient)
	} else {
		t.Logf("#4 OK: customer reference preserved (ref_client=%q)", refClient)
	}
	refExt, _ := prop["ref_ext"].(string)
	if refExt != "PKIT-999" {
		t.Errorf("#4 FAIL: ProposalKit ref lost (ref_ext=%q, want PKIT-999)", refExt)
	} else {
		t.Logf("#4 OK: ProposalKit ref preserved (ref_ext=%q)", refExt)
	}

	// #5 — validate, then close with status=2 (accepted/signed). Must succeed and
	// leave the proposal in the signed state. NOTE: requires an instance whose
	// PROPAL_VALIDATE / PROPAL_CLOSE_SIGNED triggers succeed (e.g. Notification
	// needs SMTP, Workflow needs order-creation config); a failing trigger rolls
	// the action back — that is instance config, not an MCP defect.
	vres, _, verr := deps.HandleAction(ctx, nil, ActionInput{Entity: "proposals", ID: newID, Action: "validate"})
	if e := toolErr(vres, verr); e != nil {
		t.Fatalf("#5 validate failed (check instance triggers): %v", e)
	}
	prop = getProposal(t, client, ctx, newID)
	t.Logf("#5 after validate: ref=%v statut=%v", prop["ref"], statusOf(prop))
	cres, _, cerr := deps.HandleAction(ctx, nil, ActionInput{Entity: "proposals", ID: newID, Action: "close", Status: 2})
	if e := toolErr(cres, cerr); e != nil {
		t.Fatalf("#5 close(status=2) failed (check instance triggers): %v", e)
	}
	prop = getProposal(t, client, ctx, newID)
	if statusOf(prop) != "2" {
		t.Errorf("#5 FAIL: proposal not signed (statut=%v, want 2)", statusOf(prop))
	} else {
		t.Logf("#5 OK: proposal validated and signed (ref=%v, statut=2)", prop["ref"])
	}
}

func statusOf(prop map[string]any) string {
	return fmt.Sprintf("%v", prop["statut"])
}

func asInt64(t *testing.T, v any) int64 {
	switch n := v.(type) {
	case float64:
		return int64(n)
	case string:
		id, _ := strconv.ParseInt(n, 10, 64)
		return id
	default:
		t.Fatalf("unexpected result type %T (%v)", v, v)
		return 0
	}
}

func getProposal(t *testing.T, c *dolapi.Client, ctx context.Context, id int64) map[string]any {
	raw, err := c.Get(ctx, "proposals/"+strconv.FormatInt(id, 10))
	if err != nil {
		t.Fatalf("get proposal %d: %v", id, err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("decode proposal: %v", err)
	}
	return m
}
