package tools

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/sgsoluciones/dolibarr-mcp/internal/dolapi"
	"github.com/sgsoluciones/dolibarr-mcp/internal/response"
)

// ProposalVersionsEndpoint is the sgproposalversion REST route (module >= 1.3.0)
// that freezes the live version of a proposal: POST {id}/versions.
const ProposalVersionsEndpoint = "sgproposalversion/proposals"

// ActionFreezeVersion names the write in the structured output.
const ActionFreezeVersion = "freeze_version"

// proposalVersionAPIMissing is what a caller reads when the route itself does
// not answer: the module is absent, disabled, or older than its REST API.
const proposalVersionAPIMissing = "sgproposalversion API not available (module not installed or < 1.3.0)"

type ProposalFreezeVersionInput struct {
	ProposalID int64  `json:"proposal_id" jsonschema:"Dolibarr proposal ID whose live version is frozen (required)"`
	Note       string `json:"note" jsonschema:"Why this version is being frozen, e.g. 'Before scope change requested by the client' (required, non-empty)"`
}

type ProposalVersionsInput struct {
	ProposalID int64 `json:"proposal_id" jsonschema:"Dolibarr proposal ID (required)"`
}

// ProposalVersionsOutput carries the history as decoded JSON. The jsonschema
// description on Result is load-bearing — see GetOutput.
type ProposalVersionsOutput struct {
	Result any `json:"result" jsonschema:"Proposal version history as decoded JSON: proposal_id, proposal_ref, live_version_num, count, versions[]"`
}

func (d *Deps) HandleProposalFreezeVersion(ctx context.Context, req *mcp.CallToolRequest, input ProposalFreezeVersionInput) (*mcp.CallToolResult, WriteOutput, error) {
	if input.ProposalID <= 0 {
		return nil, WriteOutput{}, fmt.Errorf("proposal_id is required")
	}
	note := strings.TrimSpace(input.Note)
	if note == "" {
		return nil, WriteOutput{}, fmt.Errorf("note is required and cannot be empty: say why this version is being frozen")
	}

	endpoint := fmt.Sprintf("%s/%d/versions", ProposalVersionsEndpoint, input.ProposalID)
	raw, err := d.API.Post(ctx, endpoint, map[string]any{"note": note})
	if err != nil {
		return proposalVersionWriteError(input.ProposalID, err)
	}

	return nil, WriteOutput{
		Success: true,
		Entity:  "proposals",
		ID:      input.ProposalID,
		Action:  ActionFreezeVersion,
		Result:  parseResult(raw),
	}, nil
}

func (d *Deps) HandleProposalVersions(ctx context.Context, req *mcp.CallToolRequest, input ProposalVersionsInput) (*mcp.CallToolResult, ProposalVersionsOutput, error) {
	if input.ProposalID <= 0 {
		return nil, ProposalVersionsOutput{}, fmt.Errorf("proposal_id is required")
	}

	result, err := d.DB.ListProposalVersions(ctx, input.ProposalID)
	if err != nil {
		return nil, ProposalVersionsOutput{}, fmt.Errorf("proposal versions of %d: %w", input.ProposalID, err)
	}
	return nil, ProposalVersionsOutput{Result: result}, nil
}

// proposalVersionWriteError maps the freeze failures a person can act on to a
// sentence that says what to do. Like writeError it returns an error result
// carrying the status code, so a client can still branch on it.
func proposalVersionWriteError(proposalID int64, err error) (*mcp.CallToolResult, WriteOutput, error) {
	operation := fmt.Sprintf("freeze version of proposal %d", proposalID)

	var apiErr *dolapi.APIError
	if !errors.As(err, &apiErr) {
		return writeError(operation, err)
	}

	upstream := pepAPIErrorText(err)
	message := ""
	switch apiErr.StatusCode {
	case http.StatusNotFound:
		if isProposalNotFound(apiErr) {
			message = fmt.Sprintf("proposal not found: %d", proposalID)
		} else {
			// Restler answers a bare 404 for a route nobody registered, which is
			// what an instance without the module looks like from here.
			message = proposalVersionAPIMissing
		}
	case http.StatusNotImplemented:
		message = proposalVersionAPIMissing
	case http.StatusForbidden:
		message = "missing permission: the API user needs sgproposalversion 'create' (and read access to proposals)"
	default:
		return writeError(operation, err)
	}

	content := response.ToJSON(map[string]any{
		"error":       true,
		"operation":   operation,
		"status_code": apiErr.StatusCode,
		"message":     message,
		"upstream":    upstream,
	})
	return &mcp.CallToolResult{
		IsError: true,
		Content: []mcp.Content{&mcp.TextContent{Text: content}},
	}, WriteOutput{}, nil
}

// isProposalNotFound tells a 404 raised by the module for a missing proposal
// from a 404 for a route that does not exist. The module names the proposal in
// its message; Restler's route miss does not.
func isProposalNotFound(apiErr *dolapi.APIError) bool {
	text := strings.ToLower(apiErr.Message + " " + apiErr.Raw)
	return strings.Contains(text, "propos") || strings.Contains(text, "propal")
}
