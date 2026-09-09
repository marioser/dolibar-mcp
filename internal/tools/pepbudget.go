package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/sgsoluciones/dolibarr-mcp/internal/dolapi"
)

// PEPBudgetEndpoint is the sgcosting REST route that loads a project PEP budget.
const PEPBudgetEndpoint = "sgcosting/budget"

// Modes accepted by dolibarr_pep_budget. The zero value ("") means preview:
// a caller that says nothing performs the rehearsal, never a write.
const (
	PEPModePreview = "preview"
	PEPModeApply   = "apply"
)

// pepElementColumns are the only keys sgcosting reads from a budget element.
// Anything else is rejected before the request leaves this process: a typo such
// as "cost_units" would otherwise load a budget worth zero without a word.
var pepElementColumns = map[string]bool{
	"code":        true,
	"parent_code": true,
	"level":       true,
	"label":       true,
	"chapter":     true,
	"center_code": true,
	"brand_ref":   true,
	"fk_product":  true,
	"unit":        true,
	"qty":         true,
	"cost_unit":   true,
	"price_unit":  true,
	"source":      true,
}

type PEPBudgetInput struct {
	ProjectID      int64            `json:"project_id" jsonschema:"Target Dolibarr project ID (required)"`
	Elements       []map[string]any `json:"elements" jsonschema:"PEP tree elements. Required per element: code, level, label. Optional: parent_code, chapter, center_code, brand_ref, fk_product, unit, qty, cost_unit, price_unit, source. Any other key is rejected."`
	Mode           string           `json:"mode,omitempty" jsonschema:"preview (default) runs the rehearsal and writes nothing; apply writes. Omitting this never writes."`
	ProposalID     int64            `json:"proposal_id,omitempty" jsonschema:"Source proposal ID (optional)"`
	Replace        bool             `json:"replace,omitempty" jsonschema:"Only with mode=apply. false (default) refuses a project that already has a budget; true overwrites the existing tree."`
	ConfirmReplace bool             `json:"confirm_replace,omitempty" jsonschema:"Only with mode=apply. Confirms an overwrite that the server reported as a 409 conflict (manual edits or charges left orphaned). Send it after reading the conflict report, never before."`
}

func (d *Deps) HandlePEPBudget(ctx context.Context, req *mcp.CallToolRequest, input PEPBudgetInput) (*mcp.CallToolResult, WriteOutput, error) {
	mode := strings.TrimSpace(input.Mode)
	if mode == "" {
		mode = PEPModePreview
	}
	if mode != PEPModePreview && mode != PEPModeApply {
		return nil, WriteOutput{}, fmt.Errorf("invalid mode '%s' (expected %s or %s)", input.Mode, PEPModePreview, PEPModeApply)
	}

	if err := validatePEPBudget(input); err != nil {
		return nil, WriteOutput{}, err
	}

	dryRun := mode == PEPModePreview

	// The endpoint tests replace BEFORE dry_run: with replace=false it rejects a
	// project that already has a budget and never reaches the rehearsal. Sending
	// replace=true on a preview keeps the rehearsal available exactly where it
	// matters most; it still writes nothing, because dry_run returns first.
	replace := input.Replace
	if dryRun {
		replace = true
	}

	payload := map[string]any{
		"fk_projet": input.ProjectID,
		"elements":  input.Elements,
		// Sent explicitly: the endpoint defaults replace to true, so staying
		// silent would overwrite an existing budget by omission.
		"replace": replace,
		"dry_run": dryRun,
		// Only meaningful on apply; a preview never reaches the write path.
		"confirm_replace": !dryRun && input.ConfirmReplace,
	}
	if input.ProposalID > 0 {
		payload["fk_propal"] = input.ProposalID
	}

	raw, err := d.API.Post(ctx, PEPBudgetEndpoint, payload)
	if err != nil {
		var apiErr *dolapi.APIError
		if errors.As(err, &apiErr) && apiErr.StatusCode == 409 {
			// A 409 is not a failure to swallow: it is the report the user
			// asked for before losing manual corrections. It travels back
			// whole, as a readable result, not as an error string.
			return nil, WriteOutput{Result: pepConflictReport(mode, input, apiErr)}, nil
		}
		return nil, WriteOutput{}, fmt.Errorf("pep budget %s on project %d: %s", mode, input.ProjectID, pepAPIErrorText(err))
	}

	body := pepDecodeObject(raw)

	if dryRun {
		out := map[string]any{
			"success":    true,
			"applied":    false,
			"mode":       PEPModePreview,
			"project_id": input.ProjectID,
			"elements":   len(input.Elements),
			"conflict":   pepBool(body, "conflict"),
			"preview":    pepOrRaw(body, raw),
		}
		if pepBool(body, "conflict") {
			out["next_step"] = "The rehearsal found manual edits or charges that would be left orphaned. " +
				"Show the preview to a person. To overwrite anyway, resend with mode=apply, replace=true and confirm_replace=true."
		} else {
			out["next_step"] = "Nothing was written. To load it, resend with mode=apply (add replace=true if the project already has a budget)."
		}
		return nil, WriteOutput{Result: out}, nil
	}

	return nil, WriteOutput{Result: map[string]any{
		"success":    true,
		"applied":    true,
		"mode":       PEPModeApply,
		"project_id": input.ProjectID,
		"replace":    input.Replace,
		"result":     pepOrRaw(body, raw),
	}}, nil
}

// pepConflictReport turns a 409 into a readable result. The report is nested
// under error.preview and NOT under error.details: Restler merges the details
// with `+` onto an array that already carries code and message, so reading the
// wrong key returns an empty report — and an empty report reads as "all clean".
func pepConflictReport(mode string, input PEPBudgetInput, apiErr *dolapi.APIError) map[string]any {
	envelope := pepDecodeObject(json.RawMessage(apiErr.Raw))
	errObj, _ := envelope["error"].(map[string]any)

	out := map[string]any{
		"success":     false,
		"applied":     false,
		"conflict":    true,
		"mode":        mode,
		"project_id":  input.ProjectID,
		"status_code": apiErr.StatusCode,
		"message":     pepString(errObj, "message"),
	}

	preview, ok := errObj["preview"].(map[string]any)
	if ok && len(preview) > 0 {
		out["preview_available"] = true
		out["preview"] = preview
		out["next_step"] = "Nothing was written. Show this report to a person before deciding. " +
			"To overwrite anyway, resend the same call with confirm_replace=true."
		return out
	}

	// The report did not arrive. Say so loudly and hand back the raw body:
	// reporting "no conflicts found" here is exactly how a confirmed overwrite
	// silently orphans money.
	out["preview_available"] = false
	out["next_step"] = "The server refused the load with 409 but the conflict report did not arrive. " +
		"Do NOT send confirm_replace: nobody can tell what would be overwritten. Inspect raw_error."
	out["raw_error"] = apiErr.Raw
	return out
}

// validatePEPBudget rejects what sgcosting would reject, before any request is
// sent. code, level and label are the three required columns.
func validatePEPBudget(input PEPBudgetInput) error {
	if input.ProjectID <= 0 {
		return fmt.Errorf("project_id is required")
	}
	if len(input.Elements) == 0 {
		return fmt.Errorf("elements is required and cannot be empty")
	}

	for i, el := range input.Elements {
		pos := i + 1

		if unknown := pepUnknownColumns(el); len(unknown) > 0 {
			return fmt.Errorf("element %d has unknown column(s) %s. Accepted: %s",
				pos, strings.Join(unknown, ", "), strings.Join(pepColumnList(), ", "))
		}
		if strings.TrimSpace(pepString(el, "code")) == "" {
			return fmt.Errorf("element %d is missing 'code' (required)", pos)
		}
		code := strings.TrimSpace(pepString(el, "code"))
		if strings.TrimSpace(pepString(el, "label")) == "" {
			return fmt.Errorf("element %s is missing 'label' (required)", code)
		}
		level, ok := pepNumber(el, "level")
		if !ok {
			return fmt.Errorf("element %s is missing 'level' (required, numeric)", code)
		}
		if level < 1 {
			return fmt.Errorf("element %s has level %v, which must be 1 or greater", code, level)
		}
	}

	return nil
}

func pepUnknownColumns(el map[string]any) []string {
	var unknown []string
	for k := range el {
		if !pepElementColumns[k] {
			unknown = append(unknown, k)
		}
	}
	sort.Strings(unknown)
	return unknown
}

func pepColumnList() []string {
	cols := make([]string, 0, len(pepElementColumns))
	for k := range pepElementColumns {
		cols = append(cols, k)
	}
	sort.Strings(cols)
	return cols
}

// pepAPIErrorText unwraps the Dolibarr error envelope so the caller reads the
// message instead of a JSON blob.
func pepAPIErrorText(err error) string {
	var apiErr *dolapi.APIError
	if !errors.As(err, &apiErr) {
		return err.Error()
	}
	envelope := pepDecodeObject(json.RawMessage(apiErr.Raw))
	if errObj, ok := envelope["error"].(map[string]any); ok {
		if msg := pepString(errObj, "message"); msg != "" {
			return fmt.Sprintf("dolibarr api %d: %s", apiErr.StatusCode, msg)
		}
	}
	return apiErr.Error()
}

func pepDecodeObject(raw json.RawMessage) map[string]any {
	var out map[string]any
	if json.Unmarshal(raw, &out) != nil {
		return nil
	}
	return out
}

// pepOrRaw keeps the server answer verbatim when it is not a JSON object.
func pepOrRaw(body map[string]any, raw json.RawMessage) any {
	if body != nil {
		return body
	}
	return string(raw)
}

func pepString(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	s, _ := m[key].(string)
	return s
}

func pepBool(m map[string]any, key string) bool {
	if m == nil {
		return false
	}
	b, _ := m[key].(bool)
	return b
}

// pepNumber reads a numeric column regardless of how the transport typed it.
func pepNumber(m map[string]any, key string) (float64, bool) {
	if m == nil {
		return 0, false
	}
	switch v := m[key].(type) {
	case float64:
		return v, true
	case float32:
		return float64(v), true
	case int:
		return float64(v), true
	case int32:
		return float64(v), true
	case int64:
		return float64(v), true
	case json.Number:
		f, err := v.Float64()
		return f, err == nil
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
		return f, err == nil
	default:
		return 0, false
	}
}
