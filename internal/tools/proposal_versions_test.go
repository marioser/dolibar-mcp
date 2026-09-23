package tools

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func freeze(t *testing.T, deps *Deps, in ProposalFreezeVersionInput) (*mcp.CallToolResult, WriteOutput, error) {
	t.Helper()
	return deps.HandleProposalFreezeVersion(context.Background(), nil, in)
}

// errorPayload decodes the structured error a failed freeze returns.
func errorPayload(t *testing.T, res *mcp.CallToolResult) map[string]any {
	t.Helper()
	if res == nil || !res.IsError {
		t.Fatalf("expected an error result, got %#v", res)
	}
	text := res.Content[0].(*mcp.TextContent).Text
	var out map[string]any
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		t.Fatalf("error result is not JSON: %q", text)
	}
	return out
}

func TestFreezePostsTheNoteToTheProposalRoute(t *testing.T) {
	c := &capture{reply: `{"id":31,"fk_propal":7,"version_num":2,"live_version_num":3,"pdf_filename":"PR2601-0007_v2.pdf","warning":""}`}
	deps, closeSrv := newFakeDolibarr(t, c)
	defer closeSrv()

	res, out, err := freeze(t, deps, ProposalFreezeVersionInput{ProposalID: 7, Note: "  Before the scope change  "})
	if e := toolErr(res, err); e != nil {
		t.Fatalf("unexpected error: %v", e)
	}

	if want := "sgproposalversion/proposals/7/versions"; c.path != want {
		t.Errorf("posted to %q, want %q", c.path, want)
	}
	if c.body["note"] != "Before the scope change" {
		t.Errorf("note sent as %q, want it trimmed", c.body["note"])
	}
	if !out.Success || out.ID != 7 || out.Action != ActionFreezeVersion {
		t.Errorf("unexpected output envelope: %+v", out)
	}
	result, ok := out.Result.(map[string]any)
	if !ok {
		t.Fatalf("result is %T, want the decoded module response", out.Result)
	}
	if result["version_num"] != float64(2) || result["live_version_num"] != float64(3) {
		t.Errorf("module response not carried through: %v", result)
	}
}

func TestFreezeRejectsMissingInputBeforeSending(t *testing.T) {
	cases := map[string]ProposalFreezeVersionInput{
		"no proposal": {Note: "why"},
		"negative id": {ProposalID: -3, Note: "why"},
		"no note":     {ProposalID: 7},
		"blank note":  {ProposalID: 7, Note: " \t\n"},
	}
	for name, in := range cases {
		t.Run(name, func(t *testing.T) {
			c := &capture{reply: `{}`}
			deps, closeSrv := newFakeDolibarr(t, c)
			defer closeSrv()

			if _, _, err := freeze(t, deps, in); err == nil {
				t.Fatal("expected a validation error")
			}
			if c.hits != 0 {
				t.Errorf("request sent %d time(s); invalid input must never reach Dolibarr", c.hits)
			}
		})
	}
}

func TestFreezeErrorsAreReadable(t *testing.T) {
	cases := []struct {
		name   string
		status int
		reply  string
		want   string
	}{
		{"missing proposal", http.StatusNotFound,
			`{"error":{"code":404,"message":"Not Found: Proposal not found"}}`, "proposal not found"},
		{"route not registered", http.StatusNotFound,
			`{"error":{"code":404,"message":"Not Found"}}`, proposalVersionAPIMissing},
		{"not implemented", http.StatusNotImplemented,
			`{"error":{"code":501,"message":"Not Implemented"}}`, proposalVersionAPIMissing},
		{"no permission", http.StatusForbidden,
			`{"error":{"code":403,"message":"Forbidden"}}`, "sgproposalversion 'create'"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := &capture{status: tc.status, reply: tc.reply}
			deps, closeSrv := newFakeDolibarr(t, c)
			defer closeSrv()

			res, _, err := freeze(t, deps, ProposalFreezeVersionInput{ProposalID: 7, Note: "why"})
			if err != nil {
				t.Fatalf("API failures must come back as an error result, got Go error %v", err)
			}
			got := errorPayload(t, res)
			if msg, _ := got["message"].(string); !strings.Contains(msg, tc.want) {
				t.Errorf("message = %q, want it to contain %q", msg, tc.want)
			}
			if got["status_code"] != float64(tc.status) {
				t.Errorf("status_code = %v, want %d", got["status_code"], tc.status)
			}
		})
	}
}

// A freeze is a POST: it must never be retried, or one call could freeze two
// versions when the first attempt landed but its answer was lost.
func TestFreezeServerErrorIsNotRetried(t *testing.T) {
	c := &capture{status: http.StatusInternalServerError, reply: `{"error":{"code":500,"message":"insert failed"}}`}
	deps, closeSrv := newFakeDolibarr(t, c)
	defer closeSrv()

	res, _, err := freeze(t, deps, ProposalFreezeVersionInput{ProposalID: 7, Note: "why"})
	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}
	got := errorPayload(t, res)
	if got["status_code"] != float64(500) {
		t.Errorf("status_code = %v, want 500", got["status_code"])
	}
	if c.hits != 1 {
		t.Errorf("freeze sent %d times, want exactly 1", c.hits)
	}
}

func TestVersionsRejectsMissingProposalID(t *testing.T) {
	// No DB wired: a missing id must be refused before any read is attempted.
	deps := &Deps{}
	if _, _, err := deps.HandleProposalVersions(context.Background(), nil, ProposalVersionsInput{}); err == nil {
		t.Fatal("expected proposal_id to be required")
	}
}
