package tools

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/sgsoluciones/dolibarr-mcp/internal/config"
	"github.com/sgsoluciones/dolibarr-mcp/internal/dolapi"
)

// capture records what the fake Dolibarr received, so the tests assert on the
// request that actually left the process instead of on the source text.
type capture struct {
	hits   int
	path   string
	body   map[string]any
	status int
	reply  string
}

func newFakeDolibarr(t *testing.T, c *capture) (*Deps, func()) {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.hits++
		c.path = strings.TrimPrefix(r.URL.Path, "/")
		raw, _ := io.ReadAll(r.Body)
		c.body = nil
		_ = json.Unmarshal(raw, &c.body)

		status := c.status
		if status == 0 {
			status = http.StatusOK
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(c.reply))
	}))

	deps := &Deps{API: dolapi.New(&config.Config{APIUrl: srv.URL, APIKey: "test-key"})}
	return deps, srv.Close
}

func validElements() []map[string]any {
	return []map[string]any{
		{"code": "1", "level": float64(1), "label": "Obra civil"},
		{"code": "1.1", "parent_code": "1", "level": float64(2), "label": "Excavacion",
			"qty": float64(10), "cost_unit": float64(1000), "price_unit": float64(1500), "unit": "m3"},
	}
}

func call(t *testing.T, deps *Deps, in PEPBudgetInput) (map[string]any, error) {
	t.Helper()
	_, out, err := deps.HandlePEPBudget(context.Background(), nil, in)
	if err != nil {
		return nil, err
	}
	// Result travels decoded (not as a JSON string), so round-trip it to get
	// the canonical map the assertions below expect.
	raw, merr := json.Marshal(out.Result)
	if merr != nil {
		t.Fatalf("tool result is not marshalable: %v (%#v)", merr, out.Result)
	}
	var parsed map[string]any
	if uerr := json.Unmarshal(raw, &parsed); uerr != nil {
		t.Fatalf("tool result is not a JSON object: %v (%s)", uerr, raw)
	}
	return parsed, nil
}

// --- rehearsal is the default -------------------------------------------------

func TestOmittedModeRunsTheRehearsalAndNeverWrites(t *testing.T) {
	c := &capture{reply: `{"ok":true,"conflict":false,"status":"created","created":["1","1.1"],
		"updated":[],"removed":[],"manual_edits":[],"orphaned":[],"orphan_check":"ok"}`}
	deps, closeSrv := newFakeDolibarr(t, c)
	defer closeSrv()

	got, err := call(t, deps, PEPBudgetInput{ProjectID: 1, Elements: validElements()})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if c.path != PEPBudgetEndpoint {
		t.Errorf("posted to %q, want %q", c.path, PEPBudgetEndpoint)
	}
	if c.body["dry_run"] != true {
		t.Errorf("dry_run sent as %v, want true: omitting mode must not write", c.body["dry_run"])
	}
	if got["applied"] != false {
		t.Errorf("applied=%v, want false", got["applied"])
	}
	if got["mode"] != PEPModePreview {
		t.Errorf("mode=%v, want %q", got["mode"], PEPModePreview)
	}
	preview, ok := got["preview"].(map[string]any)
	if !ok {
		t.Fatalf("preview missing from rehearsal result: %v", got)
	}
	created, _ := preview["created"].([]any)
	if len(created) != 2 {
		t.Errorf("preview.created has %d entries, want 2 (the report must arrive whole)", len(created))
	}
}

func TestOmittedReplaceNeverOverwrites(t *testing.T) {
	c := &capture{reply: `{"status":"created","elements":2,"orphan_charges":[]}`}
	deps, closeSrv := newFakeDolibarr(t, c)
	defer closeSrv()

	if _, err := call(t, deps, PEPBudgetInput{ProjectID: 1, Mode: PEPModeApply, Elements: validElements()}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// The endpoint defaults replace to true. Staying silent would overwrite by
	// omission, so the tool must send false explicitly.
	if _, present := c.body["replace"]; !present {
		t.Fatal("replace not sent: the endpoint would default it to true and overwrite by omission")
	}
	if c.body["replace"] != false {
		t.Errorf("replace sent as %v, want false", c.body["replace"])
	}
}

// The endpoint tests replace before dry_run, so replace=false would turn the
// rehearsal into a 400 on the very projects that already have a budget — the
// ones where the rehearsal is worth running. A preview writes nothing either
// way, because dry_run returns before any write.
func TestRehearsalIsNotBlockedByTheReplaceGuard(t *testing.T) {
	c := &capture{reply: `{"ok":true,"conflict":true,"status":"replaced","manual_edits":[],"orphaned":[],"orphan_check":"ok"}`}
	deps, closeSrv := newFakeDolibarr(t, c)
	defer closeSrv()

	if _, err := call(t, deps, PEPBudgetInput{ProjectID: 1, Elements: validElements()}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if c.body["dry_run"] != true {
		t.Fatalf("dry_run=%v, want true", c.body["dry_run"])
	}
	if c.body["replace"] != true {
		t.Errorf("replace=%v on a preview, want true: replace=false makes the endpoint "+
			"reject the rehearsal with 400 whenever the project already has a budget", c.body["replace"])
	}
}

func TestApplyModeWrites(t *testing.T) {
	c := &capture{reply: `{"status":"replaced","elements":2,"orphan_charges":[]}`}
	deps, closeSrv := newFakeDolibarr(t, c)
	defer closeSrv()

	got, err := call(t, deps, PEPBudgetInput{
		ProjectID: 7, ProposalID: 42, Mode: PEPModeApply, Replace: true, Elements: validElements(),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if c.body["dry_run"] != false {
		t.Errorf("dry_run=%v, want false on apply", c.body["dry_run"])
	}
	if c.body["replace"] != true {
		t.Errorf("replace=%v, want true", c.body["replace"])
	}
	if c.body["fk_projet"] != float64(7) {
		t.Errorf("fk_projet=%v, want 7", c.body["fk_projet"])
	}
	if c.body["fk_propal"] != float64(42) {
		t.Errorf("fk_propal=%v, want 42", c.body["fk_propal"])
	}
	if got["applied"] != true {
		t.Errorf("applied=%v, want true", got["applied"])
	}
	result, _ := got["result"].(map[string]any)
	if result["status"] != "replaced" {
		t.Errorf("result.status=%v, want replaced", result["status"])
	}
}

func TestPreviewNeverForwardsConfirmReplace(t *testing.T) {
	c := &capture{reply: `{"ok":true,"conflict":true,"manual_edits":[{"code":"1.1"}],"orphaned":[]}`}
	deps, closeSrv := newFakeDolibarr(t, c)
	defer closeSrv()

	// A caller that sets both must still get a rehearsal, not a write.
	if _, err := call(t, deps, PEPBudgetInput{ProjectID: 1, ConfirmReplace: true, Elements: validElements()}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if c.body["dry_run"] != true {
		t.Errorf("dry_run=%v, want true", c.body["dry_run"])
	}
	if c.body["confirm_replace"] != false {
		t.Errorf("confirm_replace=%v on a preview, want false", c.body["confirm_replace"])
	}
}

func TestConfirmReplaceIsForwardedOnApply(t *testing.T) {
	c := &capture{reply: `{"status":"replaced","elements":2,"orphan_charges":[]}`}
	deps, closeSrv := newFakeDolibarr(t, c)
	defer closeSrv()

	if _, err := call(t, deps, PEPBudgetInput{
		ProjectID: 1, Mode: PEPModeApply, Replace: true, ConfirmReplace: true, Elements: validElements(),
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if c.body["confirm_replace"] != true {
		t.Errorf("confirm_replace=%v, want true", c.body["confirm_replace"])
	}
}

// --- the 409 conflict report --------------------------------------------------

// conflict409 mirrors what Restler actually emits: Compose::message() merges the
// RestException details with `+` onto array('code'=>..,'message'=>..), so the
// report lands at error.preview. `details` is deliberately present and useless
// here: reading it instead of `preview` must fail the test, not return silence.
const conflict409 = `{"error":{"code":409,
	"message":"Esta recarga pisaria cambios hechos a mano o dejaria imputaciones sin elemento. No se aplico nada.",
	"details":{},
	"preview":{
		"ok":true,"conflict":true,"status":"replaced","orphan_check":"ok",
		"created":[],"updated":["1"],
		"removed":["2.1","2.2"],
		"manual_edits":[
			{"code":"2.1","label":"Montaje","user":"jperez","date":"2026-08-30 11:04:00"},
			{"code":"2.2","label":"Tablero","user":"mgomez","date":"2026-09-01 09:12:00"}],
		"orphaned":[
			{"code":"2.1","lineas":5,"comprometido":16454946,"real":16454946,"consumido":16454946},
			{"code":"2.2","lineas":3,"comprometido":1566720,"real":0,"consumido":1566720}]}}}`

func TestConflictReportReachesTheCallerWhole(t *testing.T) {
	c := &capture{status: http.StatusConflict, reply: conflict409}
	deps, closeSrv := newFakeDolibarr(t, c)
	defer closeSrv()

	got, err := call(t, deps, PEPBudgetInput{
		ProjectID: 1, Mode: PEPModeApply, Replace: true, Elements: validElements(),
	})
	if err != nil {
		t.Fatalf("409 must come back as a readable report, not as an error: %v", err)
	}

	if got["conflict"] != true {
		t.Errorf("conflict=%v, want true", got["conflict"])
	}
	if got["applied"] != false {
		t.Errorf("applied=%v, want false: a 409 writes nothing", got["applied"])
	}
	if got["success"] != false {
		t.Errorf("success=%v, want false", got["success"])
	}
	if got["status_code"] != float64(409) {
		t.Errorf("status_code=%v, want 409", got["status_code"])
	}
	if got["preview_available"] != true {
		t.Errorf("preview_available=%v, want true", got["preview_available"])
	}
	if msg, _ := got["message"].(string); !strings.Contains(msg, "No se aplico nada") {
		t.Errorf("message=%q, want the server explanation", msg)
	}

	preview, ok := got["preview"].(map[string]any)
	if !ok {
		t.Fatalf("preview missing: the conflict report was thrown away. got=%v", got)
	}

	// Who touched what.
	edits, _ := preview["manual_edits"].([]any)
	if len(edits) != 2 {
		t.Fatalf("manual_edits has %d entries, want 2", len(edits))
	}
	first, _ := edits[0].(map[string]any)
	if first["code"] != "2.1" || first["user"] != "jperez" {
		t.Errorf("manual_edits[0]=%v, want code 2.1 edited by jperez", first)
	}

	// How much money would be left without an element.
	orphaned, _ := preview["orphaned"].([]any)
	if len(orphaned) != 2 {
		t.Fatalf("orphaned has %d entries, want 2", len(orphaned))
	}
	var lines float64
	var consumed float64
	for _, o := range orphaned {
		row, _ := o.(map[string]any)
		l, _ := row["lineas"].(float64)
		c, _ := row["consumido"].(float64)
		lines += l
		consumed += c
	}
	if lines != 8 {
		t.Errorf("orphaned lines total %v, want 8", lines)
	}
	if consumed != 16454946+1566720 {
		t.Errorf("orphaned amount total %v, want %v", consumed, float64(16454946+1566720))
	}

	if next, _ := got["next_step"].(string); !strings.Contains(next, "confirm_replace") {
		t.Errorf("next_step=%q, want it to name confirm_replace", next)
	}
}

// A 409 is a decision point, not a transient failure. The client retries 5xx;
// retrying this one would hammer the ERP with a load nobody confirmed.
func TestConflictIsNotRetried(t *testing.T) {
	c := &capture{status: http.StatusConflict, reply: conflict409}
	deps, closeSrv := newFakeDolibarr(t, c)
	defer closeSrv()

	if _, err := call(t, deps, PEPBudgetInput{ProjectID: 1, Mode: PEPModeApply, Replace: true, Elements: validElements()}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.hits != 1 {
		t.Errorf("server hit %d times, want 1", c.hits)
	}
}

// The failure path of the report reader: a 409 whose report did not arrive must
// NOT read as "all clean". A guard that lets an unreadable conflict through is
// how a confirmed overwrite orphans money in silence.
func TestConflictWithoutReportIsFlaggedNotSwallowed(t *testing.T) {
	for name, reply := range map[string]string{
		"no preview key":  `{"error":{"code":409,"message":"conflicto","details":{}}}`,
		"empty preview":   `{"error":{"code":409,"message":"conflicto","preview":{}}}`,
		"not json at all": `<html>502 from the proxy</html>`,
	} {
		t.Run(name, func(t *testing.T) {
			c := &capture{status: http.StatusConflict, reply: reply}
			deps, closeSrv := newFakeDolibarr(t, c)
			defer closeSrv()

			got, err := call(t, deps, PEPBudgetInput{
				ProjectID: 1, Mode: PEPModeApply, Replace: true, Elements: validElements(),
			})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if got["conflict"] != true {
				t.Errorf("conflict=%v, want true", got["conflict"])
			}
			if got["preview_available"] != false {
				t.Errorf("preview_available=%v, want false", got["preview_available"])
			}
			if _, present := got["preview"]; present {
				t.Errorf("preview present but unreadable; it must not look like a clean report: %v", got["preview"])
			}
			if raw, _ := got["raw_error"].(string); raw == "" {
				t.Error("raw_error missing: nothing may be discarded when the report is unreadable")
			}
			next, _ := got["next_step"].(string)
			if !strings.Contains(next, "Do NOT send confirm_replace") {
				t.Errorf("next_step=%q, want it to forbid blind confirmation", next)
			}
		})
	}
}

// --- other server errors ------------------------------------------------------

func TestValidationErrorFromServerIsNotReportedAsSuccess(t *testing.T) {
	c := &capture{status: http.StatusBadRequest,
		reply: `{"error":{"code":400,"message":"El proyecto ya tiene presupuesto. Enviar replace=true para reemplazarlo."}}`}
	deps, closeSrv := newFakeDolibarr(t, c)
	defer closeSrv()

	_, err := call(t, deps, PEPBudgetInput{ProjectID: 1, Mode: PEPModeApply, Elements: validElements()})
	if err == nil {
		t.Fatal("a 400 must surface as an error")
	}
	if !strings.Contains(err.Error(), "ya tiene presupuesto") {
		t.Errorf("error=%q, want the server message readable", err.Error())
	}
}

// --- client-side validation: nothing leaves the process ------------------------

func TestInvalidPayloadNeverReachesTheServer(t *testing.T) {
	cases := map[string]PEPBudgetInput{
		"no project":          {Elements: validElements()},
		"project 0":           {ProjectID: 0, Elements: validElements()},
		"no elements":         {ProjectID: 1, Elements: nil},
		"empty elements list": {ProjectID: 1, Elements: []map[string]any{}},
		"missing code": {ProjectID: 1, Elements: []map[string]any{
			{"level": float64(1), "label": "Sin codigo"}}},
		"blank code": {ProjectID: 1, Elements: []map[string]any{
			{"code": "   ", "level": float64(1), "label": "Espacios"}}},
		"missing label": {ProjectID: 1, Elements: []map[string]any{
			{"code": "1", "level": float64(1)}}},
		"blank label": {ProjectID: 1, Elements: []map[string]any{
			{"code": "1", "level": float64(1), "label": "  "}}},
		"missing level": {ProjectID: 1, Elements: []map[string]any{
			{"code": "1", "label": "Sin nivel"}}},
		"level zero": {ProjectID: 1, Elements: []map[string]any{
			{"code": "1", "level": float64(0), "label": "Nivel cero"}}},
		"non numeric level": {ProjectID: 1, Elements: []map[string]any{
			{"code": "1", "level": "uno", "label": "Nivel texto"}}},
		"unknown column": {ProjectID: 1, Elements: []map[string]any{
			{"code": "1", "level": float64(1), "label": "Typo", "cost_units": float64(1000)}}},
		"bad mode": {ProjectID: 1, Mode: "aplicar", Elements: validElements()},
		// The second element is the one that is broken: validation must reach it.
		"broken element at the end": {ProjectID: 1, Elements: []map[string]any{
			{"code": "1", "level": float64(1), "label": "Ok"},
			{"code": "2", "level": float64(1)}}},
	}

	for name, in := range cases {
		t.Run(name, func(t *testing.T) {
			c := &capture{reply: `{"status":"created"}`}
			deps, closeSrv := newFakeDolibarr(t, c)
			defer closeSrv()

			if _, err := call(t, deps, in); err == nil {
				t.Fatal("expected a validation error")
			}
			if c.hits != 0 {
				t.Errorf("server was hit %d times; an invalid payload must never leave the process", c.hits)
			}
		})
	}
}

func TestUnknownColumnErrorNamesTheOffenderAndTheAccepted(t *testing.T) {
	deps, closeSrv := newFakeDolibarr(t, &capture{})
	defer closeSrv()

	_, err := call(t, deps, PEPBudgetInput{ProjectID: 1, Elements: []map[string]any{
		{"code": "1", "level": float64(1), "label": "Typo", "cost_units": float64(1000)}}})
	if err == nil {
		t.Fatal("expected a validation error")
	}
	if !strings.Contains(err.Error(), "cost_units") {
		t.Errorf("error=%q, want it to name cost_units", err.Error())
	}
	if !strings.Contains(err.Error(), "cost_unit,") && !strings.Contains(err.Error(), "cost_unit ") {
		t.Errorf("error=%q, want it to list the accepted columns", err.Error())
	}
}

// The 13 columns sgcosting reads, all accepted, none rejected.
func TestAllThirteenKnownColumnsAreAccepted(t *testing.T) {
	c := &capture{reply: `{"ok":true,"conflict":false}`}
	deps, closeSrv := newFakeDolibarr(t, c)
	defer closeSrv()

	full := map[string]any{
		"code": "1.1", "parent_code": "1", "level": float64(2), "label": "Excavacion",
		"chapter": "OBRA", "center_code": "MO", "brand_ref": "ACME-1", "fk_product": float64(9),
		"unit": "m3", "qty": float64(10), "cost_unit": float64(1000), "price_unit": float64(1500),
		"source": "apu",
	}
	if len(full) != 13 {
		t.Fatalf("this fixture must cover the 13 documented columns, it has %d", len(full))
	}

	if _, err := call(t, deps, PEPBudgetInput{ProjectID: 1, Elements: []map[string]any{
		{"code": "1", "level": float64(1), "label": "Obra"}, full}}); err != nil {
		t.Fatalf("all 13 columns must be accepted: %v", err)
	}
	if c.hits != 1 {
		t.Errorf("server hit %d times, want 1", c.hits)
	}

	sent, _ := c.body["elements"].([]any)
	if len(sent) != 2 {
		t.Fatalf("elements sent: %d, want 2", len(sent))
	}
	// Columns travel untouched: no alias mangling on the way out.
	second, _ := sent[1].(map[string]any)
	for k, v := range full {
		if second[k] != v {
			t.Errorf("column %s arrived as %v, want %v", k, second[k], v)
		}
	}
}

// The tool must actually reach an MCP client: Register() panics at startup when
// a schema cannot be derived, and a schema that derives but drops a field is
// worse — the agent simply never learns the flag exists.
func TestToolIsReachableOverMCPWithItsFlags(t *testing.T) {
	ctx := context.Background()

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0"}, nil)
	Register(srv, &Deps{})

	serverTr, clientTr := mcp.NewInMemoryTransports()
	serverSession, err := srv.Connect(ctx, serverTr, nil)
	if err != nil {
		t.Fatalf("server connect: %v", err)
	}
	defer serverSession.Close()

	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "0"}, nil)
	clientSession, err := client.Connect(ctx, clientTr, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	defer clientSession.Close()

	listed, err := clientSession.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}

	for _, tool := range listed.Tools {
		if tool.Name != "dolibarr_pep_budget" {
			continue
		}
		schema := map[string]any{}
		encoded, err := json.Marshal(tool.InputSchema)
		if err != nil {
			t.Fatalf("input schema does not marshal: %v", err)
		}
		if err := json.Unmarshal(encoded, &schema); err != nil {
			t.Fatalf("input schema is not an object: %v", err)
		}
		props, _ := schema["properties"].(map[string]any)
		for _, field := range []string{"project_id", "elements", "mode", "proposal_id", "replace", "confirm_replace"} {
			if _, ok := props[field]; !ok {
				t.Errorf("input schema is missing %q (schema: %s)", field, encoded)
			}
		}
		if !strings.Contains(tool.Description, "WRITES NOTHING") {
			t.Error("the description must tell the agent that preview writes nothing")
		}
		return
	}
	t.Fatalf("dolibarr_pep_budget was not registered (%d tools listed)", len(listed.Tools))
}
